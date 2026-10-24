package pdfs

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

const pdfBody = "%PDF-1.7\n1 0 obj\n<<>>\nendobj\n%%EOF\n"

// hosts is a local server that answers for several host names, so a test
// can see which host each request reached and when. Nothing leaves the
// machine.
type hosts struct {
	srv *httptest.Server
	mu  sync.Mutex
	log []hit
}

type hit struct {
	host, path, cookie, ua string
	at                     time.Time
}

func newHosts(t *testing.T) *hosts {
	h := &hosts{}
	h.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		h.log = append(h.log, hit{r.Host, r.URL.Path, r.Header.Get("Cookie"), r.Header.Get("User-Agent"), time.Now()})
		h.mu.Unlock()
		switch r.URL.Path {
		case "/paper.pdf":
			w.Write([]byte(pdfBody))
		case "/junk-then-pdf":
			w.Write([]byte("\xef\xbb\xbf\n" + pdfBody))
		case "/landing":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte("<!doctype html><title>Sign in</title>"))
		case "/forbidden":
			w.WriteHeader(http.StatusForbidden)
		case "/gone":
			w.WriteHeader(http.StatusNotFound)
		case "/broken":
			w.WriteHeader(http.StatusInternalServerError)
		case "/to-b":
			// Sets a cookie on the way, which must never come back.
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "x"})
			http.Redirect(w, r, "http://b.test/paper.pdf", http.StatusFound)
		case "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
		case "/slow":
			time.Sleep(2 * time.Second)
			w.Write([]byte(pdfBody))
		default:
			w.WriteHeader(http.StatusTeapot)
		}
	}))
	t.Cleanup(h.srv.Close)
	return h
}

// client sends every host name to the local server.
func (h *hosts) client(t *testing.T, perHost rate.Limit, timeout time.Duration) *Client {
	t.Helper()
	addr := h.srv.Listener.Addr().String()
	tr := &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}}
	c, err := New(Options{UserAgent: "fil-test", Transport: tr, Timeout: timeout, perHost: perHost})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func (h *hosts) hits(host string) []hit {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []hit
	for _, x := range h.log {
		if strings.HasPrefix(x.host, host) {
			out = append(out, x)
		}
	}
	return out
}

func TestGetOutcomes(t *testing.T) {
	t.Parallel()
	h := newHosts(t)
	c := h.client(t, rate.Inf, time.Minute)
	for _, tt := range []struct {
		path   string
		want   Outcome
		status int
	}{
		{"/paper.pdf", OK, 0},
		{"/junk-then-pdf", OK, 0},
		{"/landing", NotPDF, 200},
		{"/forbidden", Refused, 403},
		{"/gone", NotFound, 404},
		{"/broken", HTTPError, 500},
		{"/loop", RedirectLoop, 0},
	} {
		d, err := c.Get(t.Context(), "http://a.test"+tt.path)
		if tt.want == OK {
			if err != nil {
				t.Errorf("%s: %v", tt.path, err)
				continue
			}
			body, _ := io.ReadAll(d.Body)
			d.Body.Close()
			if !strings.HasSuffix(string(body), pdfBody) {
				t.Errorf("%s: body %q — the peeked head must not be lost", tt.path, body)
			}
			continue
		}
		var f *Failure
		if !errors.As(err, &f) || f.Outcome != tt.want || f.Status != tt.status {
			t.Errorf("%s: err = %v, want %s (HTTP %d)", tt.path, err, tt.want, tt.status)
		}
	}
	if _, err := c.Get(t.Context(), "ftp://a.test/x.pdf"); err == nil {
		t.Error("an ftp link was fetched")
	}
}

// Every redirect hop waits on its own host's limit, and no cookie set on the
// way is ever sent.
func TestRedirectHopsAreRateLimitedAndCookieless(t *testing.T) {
	t.Parallel()
	h := newHosts(t)
	c := h.client(t, rate.Every(200*time.Millisecond), time.Minute)

	// Three links on three different hosts, each with a limit of its own, so
	// they start together; all redirect to b.test. Only b.test's limit, on
	// the hop, can space their arrivals there.
	var wg sync.WaitGroup
	for _, from := range []string{"a1.test", "a2.test", "a3.test"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := c.Get(t.Context(), "http://"+from+"/to-b")
			if err != nil {
				t.Errorf("Get: %v", err)
				return
			}
			io.Copy(io.Discard, d.Body)
			d.Body.Close()
		}()
	}
	wg.Wait()

	b := h.hits("b.test")
	if len(b) != 3 {
		t.Fatalf("b.test saw %d requests, want 3", len(b))
	}
	for i := 1; i < len(b); i++ {
		if gap := b[i].at.Sub(b[i-1].at); gap < 150*time.Millisecond {
			t.Errorf("b.test requests %d and %d were %s apart; the hop skipped b.test's limit", i-1, i, gap)
		}
	}
	for _, x := range h.hits("") {
		if x.cookie != "" {
			t.Errorf("a cookie was sent to %s%s: %q", x.host, x.path, x.cookie)
		}
		if x.ua != "fil-test" {
			t.Errorf("User-Agent to %s = %q", x.host, x.ua)
		}
	}
}

func TestTimeoutAndCancel(t *testing.T) {
	t.Parallel()
	h := newHosts(t)
	c := h.client(t, rate.Inf, 300*time.Millisecond)
	var f *Failure
	if _, err := c.Get(t.Context(), "http://a.test/slow"); !errors.As(err, &f) || f.Outcome != Timeout {
		t.Errorf("slow server = %v, want a timeout", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := c.Get(ctx, "http://a.test/paper.pdf"); !errors.As(err, &f) || f.Outcome != Cancelled {
		t.Errorf("cancelled = %v, want cancelled", err)
	}
}
