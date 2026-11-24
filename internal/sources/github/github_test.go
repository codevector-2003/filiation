package github

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const latestJSON = `{
  "tag_name": "v0.4.4",
  "body": "notes",
  "html_url": "https://github.com/codevector-2003/filiation/releases/tag/v0.4.4",
  "published_at": "2026-12-02T10:00:00Z",
  "assets": [
    {"name": "checksums.txt", "browser_download_url": "%s/files/checksums.txt", "size": 12},
    {"name": "Filiation-Setup-0.4.4-windows-amd64.exe", "browser_download_url": "%s/files/setup.exe", "size": 11}
  ]
}`

func fakeGitHub(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var agents []string
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agents = append(agents, r.Header.Get("User-Agent"))
		switch r.URL.Path {
		case "/repos/codevector-2003/filiation/releases/latest":
			w.Write([]byte(strings.ReplaceAll(latestJSON, "%s", srv.URL)))
		case "/files/checksums.txt":
			w.Write([]byte("abc  setup.exe\n"))
		case "/files/setup.exe":
			w.Write([]byte("hello setup"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &agents
}

func TestLatestAndDownload(t *testing.T) {
	srv, agents := fakeGitHub(t)
	c := &Client{Base: srv.URL, UserAgent: "Filiation/0.4.3"}
	ctx := t.Context()

	r, err := c.Latest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if r.Version != "0.4.4" || r.Tag != "v0.4.4" || r.Notes != "notes" || r.Published.Year() != 2026 || len(r.Assets) != 2 {
		t.Fatalf("latest = %+v", r)
	}
	setup, ok := r.Asset("Filiation-Setup-0.4.4-windows-amd64.exe")
	if !ok {
		t.Fatal("no installer among the assets")
	}
	sums, _ := r.Asset("checksums.txt")
	if text, err := c.Text(ctx, sums); err != nil || text != "abc  setup.exe\n" {
		t.Fatalf("checksums = %q, %v", text, err)
	}
	var got bytes.Buffer
	var last [2]int64
	if err := c.Download(ctx, setup, &got, func(done, total int64) { last = [2]int64{done, total} }); err != nil {
		t.Fatal(err)
	}
	if got.String() != "hello setup" || last != [2]int64{11, 11} {
		t.Fatalf("downloaded %q, last progress %v", got.String(), last)
	}
	for _, ua := range *agents {
		if ua != "Filiation/0.4.3" {
			t.Errorf("a request sent User-Agent %q, want only the app and its version", ua)
		}
	}
}

func TestDownloadRefusesAShortFile(t *testing.T) {
	srv, _ := fakeGitHub(t)
	c := &Client{Base: srv.URL}
	// The release says 11 bytes; a server that sends no length and stops
	// early must not pass for a finished download.
	short := Asset{Name: "setup.exe", URL: srv.URL + "/files/checksums.txt", Size: 99}
	var buf bytes.Buffer
	if err := c.Download(t.Context(), short, &buf, nil); err == nil {
		t.Fatal("a file shorter than it should be was taken as complete")
	}
}

func TestOffline(t *testing.T) {
	c := &Client{Base: "http://127.0.0.1:1"} // nothing listens on port 1
	if _, err := c.Latest(t.Context()); !errors.Is(err, ErrOffline) {
		t.Fatalf("no server: %v, want ErrOffline", err)
	}
	srv, _ := fakeGitHub(t)
	if _, err := (&Client{Base: srv.URL, Repo: "someone/else"}).Latest(t.Context()); err == nil || errors.Is(err, ErrOffline) {
		t.Fatalf("a 404: %v, want an error that is not ErrOffline", err)
	}
}
