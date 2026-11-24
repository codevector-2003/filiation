package update

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/codevector-2003/filiation/internal/sources/github"
)

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"0.4.4", "0.4.3", true},
		{"v0.5.0", "0.4.9", true},
		{"0.10.0", "0.9.9", true},
		{"0.4.3", "0.4.3", false},
		{"0.4.2", "0.4.3", false},
		{"0.5.0", "0.5.0-rc.1", true},
		{"0.5.0-rc.1", "0.5.0", false},
		{"0.5.0-rc.2", "0.5.0-rc.1", true},
		{"0.4.4", "dev", false}, // a build of one's own is never offered one
		{"0.4.4", "0.0.0-dev", true},
		{"latest", "0.4.3", false},
	} {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestNotes(t *testing.T) {
	body := "Download the archive for your system.\n\n## Changelog\n" +
		"* 66475f8f335eb53f739a4b78cc0dcd9e1ba7b363 Desktop: Reader tabs, several papers open at once (#52)\n" +
		"* 7ca9d39 README and website: a 45-second demo of the app (#51)\n"
	want := []string{"Desktop: Reader tabs, several papers open at once", "README and website: a 45-second demo of the app"}
	if got := notes(body); !slices.Equal(got, want) {
		t.Errorf("notes = %q, want %q", got, want)
	}
	if got := notes("nothing like a changelog"); got == nil || len(got) != 0 {
		t.Errorf("notes of plain text = %#v, want an empty list", got)
	}
}

func TestChecksum(t *testing.T) {
	sum := strings.Repeat("ab", 32)
	list := "0000000000000000000000000000000000000000000000000000000000000000  fil_0.4.4_linux_amd64.tar.gz\n" +
		strings.ToUpper(sum) + "  Filiation-Setup-0.4.4-windows-amd64.exe\n"
	if got, ok := checksum(list, "Filiation-Setup-0.4.4-windows-amd64.exe"); !ok || got != sum {
		t.Errorf("checksum = %q, %v", got, ok)
	}
	if _, ok := checksum(list, "Filiation-Setup-0.4.4-windows-arm64.exe"); ok {
		t.Error("found a checksum for a file not listed")
	}
}

// release serves a fake latest release whose installer is setup, with the
// checksum listed as sum.
func release(t *testing.T, setup []byte, sum string) *github.Client {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/codevector-2003/filiation/releases/latest":
			fmt.Fprintf(w, `{"tag_name":"v0.4.4","html_url":"page","published_at":"2026-12-02T10:00:00Z",
"body":"## Changelog\n* 1234567 Desktop: updates (#53)\n",
"assets":[{"name":"checksums.txt","browser_download_url":"%[1]s/sums","size":1},
{"name":"Filiation-Setup-0.4.4-windows-amd64.exe","browser_download_url":"%[1]s/setup","size":%[2]d}]}`, srv.URL, len(setup))
		case "/sums":
			fmt.Fprintf(w, "%s  Filiation-Setup-0.4.4-windows-amd64.exe\n", sum)
		case "/setup":
			w.Write(setup)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return &github.Client{Base: srv.URL}
}

func waitFor(t *testing.T, u *Updater, phases ...string) State {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if s := u.Status(); slices.Contains(phases, s.Phase) {
			return s
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("still %+v after 10 s, want one of %v", u.Status(), phases)
	return State{}
}

func TestDownloadCheckInstall(t *testing.T) {
	setup := []byte("pretend this is Setup")
	h := sha256.Sum256(setup)
	dir := t.TempDir()
	var started []string
	u := &Updater{Current: "0.4.3", Source: release(t, setup, hex.EncodeToString(h[:])), Dir: dir, CanInstall: true,
		Start: func(path string, args ...string) error {
			started = append([]string{path}, args...)
			return nil
		}}

	if err := u.Install(); err == nil {
		t.Fatal("Install ran before anything was downloaded")
	}
	info, err := u.Check(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !info.Newer || !info.CanInstall || info.Latest != "0.4.4" || !slices.Equal(info.Notes, []string{"Desktop: updates"}) || info.Size != int64(len(setup)) {
		t.Fatalf("info = %+v", info)
	}
	// A download left from an earlier update goes.
	stale := filepath.Join(dir, "Filiation-Setup-0.4.2-windows-amd64.exe")
	os.WriteFile(stale, []byte("old"), 0o644)

	if err := u.Download(); err != nil {
		t.Fatal(err)
	}
	s := waitFor(t, u, Ready, Failed)
	if s.Phase != Ready || s.Done != int64(len(setup)) {
		t.Fatalf("after the download: %+v", s)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("an older download was left behind")
	}
	if err := u.Install(); err != nil {
		t.Fatal(err)
	}
	want := append([]string{filepath.Join(dir, "Filiation-Setup-0.4.4-windows-amd64.exe")}, InstallArgs...)
	if !slices.Equal(started, want) {
		t.Fatalf("started %q, want %q", started, want)
	}
}

func TestADownloadThatDoesNotMatchIsNeverRun(t *testing.T) {
	dir := t.TempDir()
	ran := false
	u := &Updater{Current: "0.4.3", Source: release(t, []byte("tampered"), strings.Repeat("0", 64)), Dir: dir, CanInstall: true,
		Start: func(string, ...string) error { ran = true; return nil }}
	if _, err := u.Check(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := u.Download(); err != nil {
		t.Fatal(err)
	}
	s := waitFor(t, u, Ready, Failed)
	if s.Phase != Failed || !strings.Contains(s.Error, "checksum") {
		t.Fatalf("a mismatched download: %+v", s)
	}
	if err := u.Install(); err == nil || ran {
		t.Fatal("a download that failed its checksum was run")
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*")); len(left) != 0 {
		t.Errorf("left behind: %v", left)
	}
}

// Before v0.4.4 the installer was attached after GoReleaser wrote
// checksums.txt, so the checksums did not list it. Such a release is offered
// as a download, never as an update that would fail its check.
func TestAReleaseWhoseChecksumsOmitTheInstaller(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/codevector-2003/filiation/releases/latest":
			fmt.Fprintf(w, `{"tag_name":"v0.4.4","assets":[{"name":"checksums.txt","browser_download_url":"%[1]s/sums","size":1},
{"name":"Filiation-Setup-0.4.4-windows-amd64.exe","browser_download_url":"%[1]s/setup","size":5}]}`, srv.URL)
		case "/sums":
			fmt.Fprintf(w, "%s  fil_0.4.4_windows_amd64.zip\n", strings.Repeat("a", 64))
		}
	}))
	t.Cleanup(srv.Close)
	u := &Updater{Current: "0.4.3", Source: &github.Client{Base: srv.URL}, Dir: t.TempDir(), CanInstall: true}
	info, err := u.Check(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !info.Newer || info.CanInstall {
		t.Fatalf("checksums without the installer: %+v; want newer, offered as a download", info)
	}
}

func TestOnlyASetupInstallUpdatesItself(t *testing.T) {
	u := &Updater{Current: "0.4.3", Source: release(t, []byte("x"), strings.Repeat("0", 64)), Dir: t.TempDir()}
	info, err := u.Check(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !info.Newer || info.CanInstall {
		t.Fatalf("a copy not installed by Setup: %+v; want newer, and not able to install", info)
	}
	if err := u.Download(); err == nil {
		t.Fatal("a copy not installed by Setup downloaded the installer")
	}
	dev := &Updater{Current: "dev", Source: u.Source, Dir: t.TempDir(), CanInstall: true}
	if info, _ := dev.Check(t.Context()); info.Newer {
		t.Fatal("a development build was offered an update")
	}
}
