// Package pdfs downloads open-access PDFs from the links OpenAlex gives
// (ADR-013). It is the one place fil fetches a PDF, so the rules live here:
//
//   - no cookies, no credentials, a plain User-Agent that names fil;
//   - one request per second to each host, on every redirect hop — a link
//     through doi.org reaches a publisher, and that publisher's server is the
//     one to spare (spike 9, found in review);
//   - the body must start like a PDF. Anything else — a landing page, a login
//     page, a bot wall — is refused as not a PDF and never read for links;
//   - the body is streamed, never held in memory or cached: papers/ is where
//     PDFs live.
//
// It does not use internal/httpx, which reads whole bodies and caches them —
// right for JSON from an API, wrong for files.
package pdfs

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	// PerHost is the request rate to any one host.
	PerHost = rate.Limit(1)

	// DefaultTimeout bounds one download, end to end. The 90th-percentile
	// download in spike 9 took 4 s; a minute leaves room for a large file on a
	// slow link without letting a stalled server hold a slot for ever.
	DefaultTimeout = 2 * time.Minute

	maxRedirects = 10
)

// Outcome is how one download ended, in words a person can act on.
type Outcome string

const (
	OK           Outcome = "ok"
	Refused      Outcome = "refused"   // 401, 403: the host will not serve fil
	NotFound     Outcome = "not-found" // 404, 410
	HTTPError    Outcome = "http-error"
	NotPDF       Outcome = "not-a-pdf" // an HTML page or anything else
	TooLarge     Outcome = "too-large"
	Timeout      Outcome = "timeout"
	Network      Outcome = "network"
	RedirectLoop Outcome = "redirect-loop"
	Cancelled    Outcome = "cancelled"
)

// Failure is a download that did not produce a PDF.
type Failure struct {
	Outcome Outcome
	Status  int // the HTTP status, when there was one
	Err     error
}

func (f *Failure) Error() string {
	switch {
	case f.Status != 0 && f.Err != nil:
		return fmt.Sprintf("%s (HTTP %d): %v", f.Outcome, f.Status, f.Err)
	case f.Status != 0:
		return fmt.Sprintf("%s (HTTP %d)", f.Outcome, f.Status)
	case f.Err != nil:
		return fmt.Sprintf("%s: %v", f.Outcome, f.Err)
	}
	return string(f.Outcome)
}

func (f *Failure) Unwrap() error { return f.Err }

// Options configure a Client.
type Options struct {
	// UserAgent names fil and where to reach its maintainers. Required.
	UserAgent string
	// Timeout bounds one download; zero means DefaultTimeout.
	Timeout time.Duration
	// Transport replaces the network. Tests point it at a local server.
	Transport http.RoundTripper
	// perHost overrides PerHost, for tests that must not wait a second a hop.
	perHost rate.Limit
}

// Client downloads PDFs. It is safe for concurrent use; the per-host limits
// are shared by every download it makes.
type Client struct {
	http    *http.Client
	ua      string
	perHost rate.Limit

	mu    sync.Mutex
	hosts map[string]*rate.Limiter
}

// New builds a Client.
func New(opts Options) (*Client, error) {
	if opts.UserAgent == "" {
		return nil, errors.New("pdfs: a User-Agent is required")
	}
	if opts.Timeout == 0 {
		opts.Timeout = DefaultTimeout
	}
	if opts.perHost == 0 {
		opts.perHost = PerHost
	}
	c := &Client{ua: opts.UserAgent, perHost: opts.perHost, hosts: map[string]*rate.Limiter{}}
	c.http = &http.Client{
		// No Jar: no cookies, ever.
		Transport: opts.Transport,
		Timeout:   opts.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return errTooManyRedirects
			}
			// The hop waits on its own host's limit. The User-Agent is
			// carried over; nothing else is added.
			return c.wait(req.Context(), req.URL)
		},
	}
	return c, nil
}

var errTooManyRedirects = errors.New("too many redirects")

// Host is a URL's host as fil records it: lower case, without www.
func Host(u *url.URL) string {
	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
}

func (c *Client) wait(ctx context.Context, u *url.URL) error {
	h := Host(u)
	c.mu.Lock()
	l, ok := c.hosts[h]
	if !ok {
		l = rate.NewLimiter(c.perHost, 1)
		c.hosts[h] = l
	}
	c.mu.Unlock()
	return l.Wait(ctx)
}

// Download is a PDF being received. Read Body to the end, then Close it.
type Download struct {
	Body     io.ReadCloser
	FinalURL string // after redirects
	Started  time.Time
}

// Get starts downloading rawURL. It returns once the response has begun and
// its first bytes look like a PDF; anything else comes back as a *Failure.
// The caller streams Body to disk and enforces the size limit.
func (c *Client) Get(ctx context.Context, rawURL string) (*Download, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, &Failure{Outcome: HTTPError, Err: fmt.Errorf("not a web address: %q", rawURL)}
	}
	if err := c.wait(ctx, u); err != nil {
		return nil, &Failure{Outcome: Cancelled, Err: err}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, &Failure{Outcome: HTTPError, Err: err}
	}
	req.Header.Set("User-Agent", c.ua)
	req.Header.Set("Accept", "application/pdf,*/*;q=0.5")

	started := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, classify(ctx, err)
	}
	switch {
	case resp.StatusCode == http.StatusOK:
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		resp.Body.Close()
		return nil, &Failure{Outcome: Refused, Status: resp.StatusCode}
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		resp.Body.Close()
		return nil, &Failure{Outcome: NotFound, Status: resp.StatusCode}
	default:
		resp.Body.Close()
		return nil, &Failure{Outcome: HTTPError, Status: resp.StatusCode}
	}

	// The PDF header may follow a little junk, within the first 1024 bytes.
	br := bufio.NewReaderSize(resp.Body, 1024)
	head, err := br.Peek(1024)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, bufio.ErrBufferFull) {
		resp.Body.Close()
		return nil, classify(ctx, err)
	}
	if !strings.Contains(string(head), "%PDF-") {
		resp.Body.Close()
		kind := strings.SplitN(resp.Header.Get("Content-Type"), ";", 2)[0]
		if kind == "" {
			kind = "unknown content"
		}
		return nil, &Failure{Outcome: NotPDF, Status: resp.StatusCode, Err: errors.New(kind)}
	}
	return &Download{
		Body:     readCloser{br, resp.Body},
		FinalURL: resp.Request.URL.String(),
		Started:  started,
	}, nil
}

type readCloser struct {
	io.Reader
	io.Closer
}

// Classify turns an error from reading or requesting into a Failure.
func Classify(ctx context.Context, err error) *Failure { return classify(ctx, err) }

func classify(ctx context.Context, err error) *Failure {
	var f *Failure
	if errors.As(err, &f) {
		return f
	}
	if ctx.Err() != nil {
		return &Failure{Outcome: Cancelled, Err: ctx.Err()}
	}
	if errors.Is(err, errTooManyRedirects) {
		return &Failure{Outcome: RedirectLoop, Err: err}
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() || errors.Is(err, context.DeadlineExceeded) {
		return &Failure{Outcome: Timeout, Err: err}
	}
	return &Failure{Outcome: Network, Err: err}
}
