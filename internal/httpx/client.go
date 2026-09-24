package httpx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/codevector-2003/filiation/internal/errs"
)

const (
	// DefaultMaxRetries is §7's "retry three times, then mark the batch failed
	// and continue".
	DefaultMaxRetries = 3

	// DefaultMaxRetryWait is the longest Retry-After this client will sleep
	// through. A 429 asking for longer is not a burst to wait out: it is the
	// daily allowance spent, with a reset hours away, and blocking a CLI for
	// that long looks like a hang. The request fails as transient instead, with
	// the wait attached for the caller to report.
	DefaultMaxRetryWait = 60 * time.Second

	// DefaultTimeout bounds one attempt, not the whole call with its retries.
	DefaultTimeout = 30 * time.Second

	// DefaultCacheMaxAge is how long a cached response is served. The cache
	// exists so a debugging loop does not refetch (ADR-004), not to be a
	// mirror; a week keeps citation counts from drifting far.
	DefaultCacheMaxAge = 7 * 24 * time.Hour

	// maxBodyBytes caps a response read. A 200-work OpenAlex page is a few
	// megabytes; anything past this is a misbehaving server, not data.
	maxBodyBytes = 64 << 20

	backoffBase = time.Second
	backoffCap  = 30 * time.Second
)

// Options configures one Client. Each upstream service gets its own Client,
// because each has its own rate limit: a token bucket shared between OpenAlex
// and Unpaywall would throttle one for the other's sake.
type Options struct {
	// RatePerSecond is the token bucket's rate. Required. For OpenAlex it is 5,
	// against a measured first 429 at ~11 req/s (spike 3).
	RatePerSecond float64

	// Burst is the bucket size. Zero means 1, which spaces requests evenly
	// rather than letting a burst run into the limit.
	Burst int

	// Contact is the user's email. Optional (D11 measured no benefit from it),
	// but it is what OpenAlex asks for and how they reach a misbehaving client.
	// It goes into the User-Agent, and into the query as ContactParam if that
	// is set.
	Contact string

	// ContactParam is the query parameter the upstream wants the contact in:
	// "mailto" for OpenAlex, "email" for Unpaywall. Empty sends it in the
	// User-Agent only.
	ContactParam string

	// UserAgent names the tool. Empty means "fil".
	UserAgent string

	// MaxRetries is how many times a transient failure is retried. Zero means
	// DefaultMaxRetries; negative means never retry.
	MaxRetries int

	// MaxRetryWait caps how long a Retry-After is honoured. Zero means
	// DefaultMaxRetryWait.
	MaxRetryWait time.Duration

	// Timeout bounds each attempt. Zero means DefaultTimeout.
	Timeout time.Duration

	// Cache, if set, serves repeated GETs without a request. Nil disables
	// caching, which is what --no-cache does.
	Cache Cache

	// CacheMaxAge is how old a cached response may be. Zero means
	// DefaultCacheMaxAge.
	CacheMaxAge time.Duration

	// Transport replaces the network. Tests pass a stub here so that no unit
	// test touches the network; production leaves it nil.
	Transport http.RoundTripper
}

// Client is the only way anything in Filiation makes an HTTP request. See the
// package documentation for why.
//
// It is safe for concurrent use: the limiter is shared, so concurrent callers
// queue on the bucket rather than multiplying the rate.
type Client struct {
	http      *http.Client
	limiter   *rate.Limiter
	opts      Options
	userAgent string

	// sleep and jitter are replaced in tests, so that backoff can be asserted
	// on without the suite actually waiting for it.
	sleep  func(ctx context.Context, d time.Duration) error
	jitter func(d time.Duration) time.Duration

	mu    sync.Mutex
	quota Quota
}

// New builds a Client, applying defaults to the zero fields of opts.
func New(opts Options) (*Client, error) {
	if opts.RatePerSecond <= 0 {
		return nil, errors.New("httpx: RatePerSecond must be positive — an unlimited client is how a free tier gets banned")
	}
	if opts.Burst <= 0 {
		opts.Burst = 1
	}
	if opts.MaxRetries == 0 {
		opts.MaxRetries = DefaultMaxRetries
	}
	if opts.MaxRetries < 0 {
		opts.MaxRetries = 0
	}
	if opts.MaxRetryWait <= 0 {
		opts.MaxRetryWait = DefaultMaxRetryWait
	}
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultTimeout
	}
	if opts.CacheMaxAge <= 0 {
		opts.CacheMaxAge = DefaultCacheMaxAge
	}

	ua := opts.UserAgent
	if ua == "" {
		ua = "fil"
	}
	if opts.Contact != "" {
		ua += " (mailto:" + opts.Contact + ")"
	}

	transport := opts.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}

	return &Client{
		http:      &http.Client{Transport: transport, Timeout: opts.Timeout},
		limiter:   rate.NewLimiter(rate.Limit(opts.RatePerSecond), opts.Burst),
		opts:      opts,
		userAgent: ua,
		sleep:     sleepCtx,
		jitter:    equalJitter,
	}, nil
}

// Get fetches rawURL and returns the response body.
//
// A 2xx is returned, and cached if a cache is set. A 429, any 5xx or a network
// failure is retried with backoff, and if it never succeeds the error wraps
// errs.ErrTransient — the expander's signal to skip the batch and carry on (§7).
// Any other status fails at once with a *StatusError, which a source inspects:
// a 404 from OpenAlex means the work is unresolved, not that the network is down.
//
// Cancelling ctx stops the call at the next wait, whether that is the token
// bucket, a backoff or the request itself.
func (c *Client) Get(ctx context.Context, rawURL string) ([]byte, error) {
	// The cache key is the URL as the caller built it, before the contact is
	// added. Changing one's email address must not empty the cache, and the
	// address has no business being written into file headers on disk.
	key := rawURL
	if c.opts.Cache != nil {
		if body, ok, err := c.opts.Cache.Get(key, c.opts.CacheMaxAge); err == nil && ok {
			return body, nil
		}
	}

	reqURL, err := c.withContact(rawURL)
	if err != nil {
		return nil, fmt.Errorf("httpx: GET %s: %w", rawURL, err)
	}

	var lastErr error
	for attempt := 0; attempt <= c.opts.MaxRetries; attempt++ {
		if attempt > 0 {
			wait := c.jitter(backoff(attempt))
			if ra, ok := RetryAfter(lastErr); ok && ra > wait {
				wait = ra
			}
			if err := c.sleep(ctx, wait); err != nil {
				return nil, err
			}
		}
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, fmt.Errorf("httpx: GET %s: %w", key, err)
		}

		body, err := c.do(ctx, key, reqURL)
		if err == nil {
			if c.opts.Cache != nil {
				_ = c.opts.Cache.Put(key, body) // never fail a fetch over the cache
			}
			return body, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !errors.Is(err, errs.ErrTransient) {
			return nil, err
		}
		// Asked to wait longer than we are willing to: stop now rather than
		// retry into the same answer. The wait stays on the error.
		if ra, ok := RetryAfter(err); ok && ra > c.opts.MaxRetryWait {
			return nil, err
		}
		lastErr = err
	}
	return nil, fmt.Errorf("httpx: gave up after %d attempts: %w", c.opts.MaxRetries+1, lastErr)
}

// do makes one attempt.
func (c *Client) do(ctx context.Context, key, reqURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("httpx: GET %s: %w", key, err)
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		// Dropped connection, DNS failure, timeout: all may not recur.
		return nil, fmt.Errorf("httpx: GET %s: %w: %w", key, errs.ErrTransient, err)
	}
	defer resp.Body.Close()

	c.recordQuota(resp.Header)

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("httpx: GET %s: read body: %w: %w", key, errs.ErrTransient, err)
	}
	if len(body) > maxBodyBytes {
		return nil, fmt.Errorf("httpx: GET %s: response larger than %d bytes", key, maxBodyBytes)
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return body, nil
	}
	return nil, &StatusError{
		URL:        key,
		StatusCode: resp.StatusCode,
		RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()),
		Body:       snippet(body),
	}
}

// withContact adds the contact to the query, if configured.
func (c *Client) withContact(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	if c.opts.Contact == "" || c.opts.ContactParam == "" {
		return u.String(), nil
	}
	q := u.Query()
	q.Set(c.opts.ContactParam, c.opts.Contact)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// StatusError is a response with a status other than 2xx.
type StatusError struct {
	URL        string // without the contact parameter
	StatusCode int

	// RetryAfter is the server's requested wait, zero if it sent none.
	RetryAfter time.Duration

	// Body is the start of the response, because OpenAlex explains a 400 in it
	// — "101 IDs is over the limit of 100" is only ever said there.
	Body string
}

func (e *StatusError) Error() string {
	msg := fmt.Sprintf("httpx: GET %s: HTTP %d", e.URL, e.StatusCode)
	if e.RetryAfter > 0 {
		msg += fmt.Sprintf(" (retry after %s)", e.RetryAfter)
	}
	if e.Body != "" {
		msg += ": " + e.Body
	}
	return msg
}

// Is makes a 429 or 5xx match errs.ErrTransient, so callers can test for it
// without knowing this type exists.
func (e *StatusError) Is(target error) bool {
	return target == errs.ErrTransient && e.Transient()
}

// Transient reports whether this status may not recur: rate limited, or a
// server-side failure.
func (e *StatusError) Transient() bool {
	return e.StatusCode == http.StatusTooManyRequests || e.StatusCode >= 500
}

// RetryAfter returns the server's requested wait carried by err, if any.
//
// The errs package deliberately keeps this off ErrTransient: a caller deciding
// whether to retry should not have to understand when. This is the when, for
// the one caller that reports it — "OpenAlex asked us to wait 6h; your daily
// allowance is spent".
func RetryAfter(err error) (time.Duration, bool) {
	var se *StatusError
	if errors.As(err, &se) && se.RetryAfter > 0 {
		return se.RetryAfter, true
	}
	return 0, false
}

// StatusCode returns the HTTP status carried by err, or 0 if there is none.
func StatusCode(err error) int {
	var se *StatusError
	if errors.As(err, &se) {
		return se.StatusCode
	}
	return 0
}

// Quota is the allowance the upstream last reported, from OpenAlex's
// X-RateLimit-* headers.
//
// It is read from responses rather than compiled in because OpenAlex has
// already changed its pricing once: the documented allowance was 100,000
// credits a day and the measured one is 1,000 (D11).
type Quota struct {
	Limit     int
	Remaining int
	Reset     time.Duration // until the allowance refills
	Observed  time.Time
}

// Quota returns the last allowance reported, and false if no response has
// carried one yet.
func (c *Client) Quota() (Quota, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.quota, !c.quota.Observed.IsZero()
}

func (c *Client) recordQuota(h http.Header) {
	remaining, err := strconv.Atoi(h.Get("X-RateLimit-Remaining"))
	if err != nil {
		return
	}
	q := Quota{Remaining: remaining, Observed: time.Now()}
	if limit, err := strconv.Atoi(h.Get("X-RateLimit-Limit")); err == nil {
		q.Limit = limit
	}
	if reset, err := strconv.Atoi(h.Get("X-RateLimit-Reset")); err == nil {
		q.Reset = time.Duration(reset) * time.Second
	}
	c.mu.Lock()
	c.quota = q
	c.mu.Unlock()
}

// parseRetryAfter reads either form HTTP allows: delay-seconds or an HTTP date.
func parseRetryAfter(v string, now time.Time) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs < 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := t.Sub(now); d > 0 {
			return d
		}
	}
	return 0
}

// backoff is the exponential ceiling for a retry: 1s, 2s, 4s, ... capped.
func backoff(attempt int) time.Duration {
	d := backoffBase << (attempt - 1)
	if d <= 0 || d > backoffCap {
		return backoffCap
	}
	return d
}

// equalJitter picks uniformly from the upper half of d, so clients that failed
// together do not retry together. Full jitter, drawing from all of [0, d), can
// draw nearly zero, which against a 429 is just an immediate second 429.
func equalJitter(d time.Duration) time.Duration {
	if d <= 1 {
		return d
	}
	half := d / 2
	return half + rand.N(half)
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func snippet(body []byte) string {
	const limit = 300
	s := strings.TrimSpace(string(body))
	if len(s) > limit {
		s = s[:limit] + "…"
	}
	return s
}
