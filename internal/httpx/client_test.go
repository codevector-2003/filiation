package httpx

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codevector-2003/filiation/internal/errs"
)

// reply is one scripted response. A non-nil err is a network failure.
type reply struct {
	status int
	header http.Header
	body   string
	err    error
}

// script is a stub transport: it answers each request with the next reply and
// records what it was sent. No unit test in this package touches the network.
type script struct {
	mu      sync.Mutex
	replies []reply
	seen    []*http.Request
}

func (s *script) RoundTrip(req *http.Request) (*http.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seen = append(s.seen, req)
	if len(s.replies) == 0 {
		return nil, errors.New("script: no reply left for " + req.URL.String())
	}
	r := s.replies[0]
	s.replies = s.replies[1:]
	if r.err != nil {
		return nil, r.err
	}
	h := r.header
	if h == nil {
		h = http.Header{}
	}
	return &http.Response{
		StatusCode: r.status,
		Header:     h,
		Body:       io.NopCloser(strings.NewReader(r.body)),
		Request:    req,
	}, nil
}

func (s *script) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.seen)
}

// newTest builds a client whose sleeps are recorded instead of taken, with a
// bucket fast enough not to slow the suite. jitter is the identity, so the
// recorded waits are the backoff schedule itself.
func newTest(t *testing.T, opts Options, replies ...reply) (*Client, *script, *[]time.Duration) {
	t.Helper()
	s := &script{replies: replies}
	opts.Transport = s
	if opts.RatePerSecond == 0 {
		opts.RatePerSecond = 1000
	}
	c, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var slept []time.Duration
	c.sleep = func(ctx context.Context, d time.Duration) error {
		slept = append(slept, d)
		return ctx.Err()
	}
	c.jitter = func(d time.Duration) time.Duration { return d }
	return c, s, &slept
}

func header(kv ...string) http.Header {
	h := http.Header{}
	for i := 0; i+1 < len(kv); i += 2 {
		h.Set(kv[i], kv[i+1])
	}
	return h
}

func TestNewRequiresARate(t *testing.T) {
	t.Parallel()
	if _, err := New(Options{}); err == nil {
		t.Error("New with no rate succeeded — an unlimited client must be impossible to build by accident")
	}
}

func TestGetSendsContactAndReturnsBody(t *testing.T) {
	t.Parallel()
	c, s, _ := newTest(t, Options{Contact: "me@example.org", ContactParam: "mailto"},
		reply{status: 200, body: `{"id":"W1"}`})

	body, err := c.Get(t.Context(), "https://api.openalex.org/works/W1?select=id")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(body) != `{"id":"W1"}` {
		t.Errorf("body = %q", body)
	}

	req := s.seen[0]
	if got := req.URL.Query().Get("mailto"); got != "me@example.org" {
		t.Errorf("mailto = %q, want the contact", got)
	}
	if got := req.URL.Query().Get("select"); got != "id" {
		t.Errorf("select = %q — adding the contact dropped the caller's own query", got)
	}
	if ua := req.Header.Get("User-Agent"); !strings.Contains(ua, "mailto:me@example.org") {
		t.Errorf("User-Agent = %q, want it to carry the contact", ua)
	}
	if got := req.Header.Get("Accept"); got != "application/json" {
		t.Errorf("Accept = %q", got)
	}
}

func TestGetWithoutContact(t *testing.T) {
	t.Parallel()
	// Contact is optional (D11): absent, nothing is invented in its place.
	c, s, _ := newTest(t, Options{ContactParam: "mailto"}, reply{status: 200})
	if _, err := c.Get(t.Context(), "https://api.openalex.org/works/W1"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if s.seen[0].URL.Query().Has("mailto") {
		t.Errorf("mailto sent with no contact configured")
	}
	if ua := s.seen[0].Header.Get("User-Agent"); ua != "fil" {
		t.Errorf("User-Agent = %q, want plain \"fil\"", ua)
	}
}

func TestRetriesTransientFailures(t *testing.T) {
	t.Parallel()
	c, s, slept := newTest(t, Options{},
		reply{status: 503},
		reply{err: errors.New("connection reset by peer")},
		reply{status: 200, body: "ok"})

	body, err := c.Get(t.Context(), "https://api.openalex.org/works/W1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(body) != "ok" {
		t.Errorf("body = %q", body)
	}
	if s.calls() != 3 {
		t.Errorf("calls = %d, want 3", s.calls())
	}
	want := []time.Duration{1 * time.Second, 2 * time.Second}
	if len(*slept) != len(want) || (*slept)[0] != want[0] || (*slept)[1] != want[1] {
		t.Errorf("waits = %v, want the exponential schedule %v", *slept, want)
	}
}

func TestRetryHonoursRetryAfter(t *testing.T) {
	t.Parallel()
	c, _, slept := newTest(t, Options{},
		reply{status: 429, header: header("Retry-After", "7")},
		reply{status: 200})

	if _, err := c.Get(t.Context(), "https://api.openalex.org/works/W1"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(*slept) != 1 || (*slept)[0] != 7*time.Second {
		t.Errorf("waits = %v, want [7s] — the server's Retry-After beats a shorter backoff", *slept)
	}
}

func TestGivesUpAsTransient(t *testing.T) {
	t.Parallel()
	c, s, _ := newTest(t, Options{},
		reply{status: 500}, reply{status: 502}, reply{status: 503}, reply{status: 504})

	_, err := c.Get(t.Context(), "https://api.openalex.org/works/W1")
	// §7: one bad batch must not kill the run, and ErrTransient is how the
	// expander knows to skip it and carry on.
	if !errors.Is(err, errs.ErrTransient) {
		t.Fatalf("error = %v, want ErrTransient", err)
	}
	if s.calls() != DefaultMaxRetries+1 {
		t.Errorf("calls = %d, want %d", s.calls(), DefaultMaxRetries+1)
	}
	if StatusCode(err) != 504 {
		t.Errorf("StatusCode = %d, want the last status, 504", StatusCode(err))
	}
}

func TestLongRetryAfterFailsFast(t *testing.T) {
	t.Parallel()
	// The daily allowance is spent and resets in an hour. Waiting that out would
	// look like a hang; retrying would get the same answer.
	c, s, slept := newTest(t, Options{},
		reply{status: 429, header: header("Retry-After", "3600")})

	_, err := c.Get(t.Context(), "https://api.openalex.org/works/W1")
	if !errors.Is(err, errs.ErrTransient) {
		t.Fatalf("error = %v, want ErrTransient", err)
	}
	if s.calls() != 1 || len(*slept) != 0 {
		t.Errorf("calls = %d, waits = %v; want one call and no wait", s.calls(), *slept)
	}
	if d, ok := RetryAfter(err); !ok || d != time.Hour {
		t.Errorf("RetryAfter = %v, %v; want 1h for the CLI to report", d, ok)
	}
}

func TestNonTransientStatusFailsAtOnce(t *testing.T) {
	t.Parallel()
	c, s, _ := newTest(t, Options{},
		reply{status: 400, body: `{"error":"Invalid query parameters","message":"101 is over the limit of 100"}`})

	_, err := c.Get(t.Context(), "https://api.openalex.org/works?filter=x")
	if errors.Is(err, errs.ErrTransient) {
		t.Errorf("a 400 was treated as transient; retrying it cannot help")
	}
	if s.calls() != 1 {
		t.Errorf("calls = %d, want 1", s.calls())
	}
	if StatusCode(err) != 400 {
		t.Errorf("StatusCode = %d, want 400", StatusCode(err))
	}
	// OpenAlex only ever explains a 400 in the body.
	if !strings.Contains(err.Error(), "over the limit of 100") {
		t.Errorf("error %q lost the explanation from the body", err)
	}
}

func TestNotFoundIsDistinguishable(t *testing.T) {
	t.Parallel()
	c, _, _ := newTest(t, Options{}, reply{status: 404})
	_, err := c.Get(t.Context(), "https://api.openalex.org/works/W999")
	// sources/openalex maps this to errs.ErrUnresolved. It must not look like a
	// network problem, or dead identifiers would be retried forever.
	if StatusCode(err) != 404 || errors.Is(err, errs.ErrTransient) {
		t.Errorf("error = %v; want a non-transient 404", err)
	}
}

func TestNegativeMaxRetriesNeverRetries(t *testing.T) {
	t.Parallel()
	c, s, _ := newTest(t, Options{MaxRetries: -1}, reply{status: 503})
	if _, err := c.Get(t.Context(), "https://api.openalex.org/works/W1"); !errors.Is(err, errs.ErrTransient) {
		t.Errorf("error = %v, want ErrTransient", err)
	}
	if s.calls() != 1 {
		t.Errorf("calls = %d, want 1", s.calls())
	}
}

func TestCancelStopsRetrying(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	c, s, _ := newTest(t, Options{}, reply{status: 503}, reply{status: 200})
	// Ctrl-C lands during the backoff.
	c.sleep = func(ctx context.Context, d time.Duration) error {
		cancel()
		return ctx.Err()
	}

	_, err := c.Get(ctx, "https://api.openalex.org/works/W1")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled — cancelling an expansion must actually stop it", err)
	}
	if s.calls() != 1 {
		t.Errorf("calls = %d, want 1 — a request was made after cancellation", s.calls())
	}
}

func TestCacheServesRepeatsWithoutARequest(t *testing.T) {
	t.Parallel()
	cache, err := NewDirCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c, s, _ := newTest(t, Options{Cache: cache, Contact: "me@example.org", ContactParam: "mailto"},
		reply{status: 200, body: "first"})

	for range 3 {
		body, err := c.Get(t.Context(), "https://api.openalex.org/works/W1")
		if err != nil || string(body) != "first" {
			t.Fatalf("Get = %q, %v", body, err)
		}
	}
	if s.calls() != 1 {
		t.Errorf("calls = %d, want 1 — repeats should come from the cache", s.calls())
	}

	// A different contact is the same request as far as the cache is
	// concerned, so changing one's email does not empty it.
	c2, s2, _ := newTest(t, Options{Cache: cache, Contact: "new@example.org", ContactParam: "mailto"})
	if body, err := c2.Get(t.Context(), "https://api.openalex.org/works/W1"); err != nil || string(body) != "first" {
		t.Errorf("Get with a new contact = %q, %v; want a cache hit", body, err)
	}
	if s2.calls() != 0 {
		t.Errorf("a changed contact caused a request")
	}
}

func TestFailuresAreNotCached(t *testing.T) {
	t.Parallel()
	cache, err := NewDirCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c, s, _ := newTest(t, Options{Cache: cache, MaxRetries: -1},
		reply{status: 503}, reply{status: 200, body: "recovered"})

	if _, err := c.Get(t.Context(), "https://api.openalex.org/works/W1"); err == nil {
		t.Fatal("first Get succeeded, want the 503")
	}
	body, err := c.Get(t.Context(), "https://api.openalex.org/works/W1")
	if err != nil || string(body) != "recovered" {
		t.Errorf("second Get = %q, %v; want a fresh request, not a cached failure", body, err)
	}
	if s.calls() != 2 {
		t.Errorf("calls = %d, want 2", s.calls())
	}
}

func TestQuotaIsReadFromResponses(t *testing.T) {
	t.Parallel()
	c, _, _ := newTest(t, Options{}, reply{status: 200, header: header(
		"X-RateLimit-Limit", "1000",
		"X-RateLimit-Remaining", "983",
		"X-RateLimit-Reset", "73233",
	)})

	if _, ok := c.Quota(); ok {
		t.Errorf("Quota reported before any response")
	}
	if _, err := c.Get(t.Context(), "https://api.openalex.org/works?filter=x"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	q, ok := c.Quota()
	if !ok {
		t.Fatal("Quota not recorded")
	}
	if q.Limit != 1000 || q.Remaining != 983 || q.Reset != 73233*time.Second {
		t.Errorf("Quota = %+v, want the headers from spike 3", q)
	}
}

func TestRateLimiterSpacesRequests(t *testing.T) {
	t.Parallel()
	// Real time, and the only test that waits: 5 requests at 20/s with a burst
	// of one must take at least four intervals of 50ms.
	replies := make([]reply, 5)
	for i := range replies {
		replies[i] = reply{status: 200}
	}
	c, _, _ := newTest(t, Options{RatePerSecond: 20}, replies...)

	start := time.Now()
	for range 5 {
		if _, err := c.Get(t.Context(), "https://api.openalex.org/works/W1"); err != nil {
			t.Fatalf("Get: %v", err)
		}
	}
	if elapsed := time.Since(start); elapsed < 190*time.Millisecond {
		t.Errorf("5 requests at 20/s took %s, want at least ~200ms — the bucket is not limiting", elapsed)
	}
}

func TestParseRetryAfter(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		in   string
		want time.Duration
	}{
		{"", 0},
		{"7", 7 * time.Second},
		{" 120 ", 2 * time.Minute},
		{"-5", 0},
		{"soon", 0},
		{"Thu, 24 Sep 2026 12:00:30 GMT", 30 * time.Second},
		{"Thu, 24 Sep 2026 11:00:00 GMT", 0}, // in the past
	}
	for _, tt := range tests {
		if got := parseRetryAfter(tt.in, now); got != tt.want {
			t.Errorf("parseRetryAfter(%q) = %s, want %s", tt.in, got, tt.want)
		}
	}
}

func TestBackoffAndJitter(t *testing.T) {
	t.Parallel()
	for attempt, want := range map[int]time.Duration{
		1: 1 * time.Second, 2: 2 * time.Second, 3: 4 * time.Second,
		6: backoffCap, 60: backoffCap,
	} {
		if got := backoff(attempt); got != want {
			t.Errorf("backoff(%d) = %s, want %s", attempt, got, want)
		}
	}
	for range 1000 {
		d := 8 * time.Second
		if j := equalJitter(d); j < d/2 || j >= d {
			t.Fatalf("equalJitter(%s) = %s, want within [%s, %s)", d, j, d/2, d)
		}
	}
}
