package ui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/codevector-2003/filiation/internal/sources/github"
	"github.com/codevector-2003/filiation/internal/update"
)

func TestUpdateRoutes(t *testing.T) {
	t.Parallel()
	setup := []byte("pretend Setup")
	sum := sha256.Sum256(setup)
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/codevector-2003/filiation/releases/latest":
			fmt.Fprintf(w, `{"tag_name":"v0.4.4","html_url":"page","published_at":"2026-12-02T10:00:00Z","body":"",
"assets":[{"name":"checksums.txt","browser_download_url":"%[1]s/sums","size":1},
{"name":"Filiation-Setup-0.4.4-windows-amd64.exe","browser_download_url":"%[1]s/setup","size":%[2]d}]}`, srv.URL, len(setup))
		case "/sums":
			fmt.Fprintf(w, "%s  Filiation-Setup-0.4.4-windows-amd64.exe\n", hex.EncodeToString(sum[:]))
		case "/setup":
			w.Write(setup)
		}
	}))
	t.Cleanup(srv.Close)
	started := ""
	u := &update.Updater{Current: "0.4.3", Source: &github.Client{Base: srv.URL}, Dir: t.TempDir(), CanInstall: true,
		Start: func(path string, _ ...string) error { started = path; return nil }}
	h := New(Options{Version: "0.4.3", Updater: u})

	info := decode[update.Info](t, ask(t, h, "GET", "/api/update"))
	if !info.Newer || !info.CanInstall || info.Latest != "0.4.4" {
		t.Fatalf("check = %+v", info)
	}
	if w := post(t, h, "/api/update/install", ``); w.Code != http.StatusConflict {
		t.Errorf("install before a download = %d", w.Code)
	}
	if w := post(t, h, "/api/update/download", ``); w.Code != http.StatusOK {
		t.Fatalf("download = %d %s", w.Code, w.Body)
	}
	deadline := time.Now().Add(10 * time.Second)
	var st update.State
	for time.Now().Before(deadline) {
		st = decode[update.State](t, ask(t, h, "GET", "/api/update/status"))
		if st.Phase == update.Ready || st.Phase == update.Failed {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if st.Phase != update.Ready {
		t.Fatalf("status = %+v", st)
	}
	if w := post(t, h, "/api/update/install", ``); w.Code != http.StatusNoContent || started == "" {
		t.Fatalf("install = %d, started %q", w.Code, started)
	}

	// Without an updater, as in fil or a test, updates are simply not offered.
	plain := New(Options{Version: "0.4.3"})
	if got := decode[map[string]any](t, ask(t, plain, "GET", "/api/update")); got["unavailable"] != true {
		t.Errorf("no updater = %v", got)
	}
}

func TestUpdateCheckOffline(t *testing.T) {
	t.Parallel()
	u := &update.Updater{Current: "0.4.3", Source: &github.Client{Base: "http://127.0.0.1:1"}, Dir: t.TempDir()}
	w := ask(t, New(Options{Version: "0.4.3", Updater: u}), "GET", "/api/update")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("offline check = %d %s", w.Code, w.Body)
	}
	if got := decode[map[string]any](t, w); got["offline"] != true {
		t.Errorf("offline check said %v", got)
	}
}
