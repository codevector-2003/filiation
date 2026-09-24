package main

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/codevector-2003/filiation/internal/config"
)

// These run the real command tree against a temporary config directory, a
// temporary cache and OpenAlex responses recorded on 24 Sept 2026. Nothing
// here touches the network or the user's own library.
//
// They are not parallel: t.Setenv is needed to keep a FILIATION_DB in the
// developer's environment from pointing a test at a real library.

const fixtureDir = "../../internal/sources/openalex/testdata"

type replay struct {
	t      *testing.T
	mu     sync.Mutex
	routes map[string]string // key -> fixture file or "404"
	calls  int
}

func (r *replay) RoundTrip(req *http.Request) (*http.Response, error) {
	key := req.URL.Path
	if f := req.URL.Query().Get("filter"); f != "" {
		key += "?filter=" + f
	}
	r.mu.Lock()
	r.calls++
	file, ok := r.routes[key]
	r.mu.Unlock()

	status, body := 200, ""
	switch {
	case !ok:
		r.t.Errorf("unexpected request %s", key)
		status = 400
	case file == "404":
		status = 404
	default:
		raw, err := os.ReadFile(filepath.Join(fixtureDir, file))
		if err != nil {
			r.t.Fatalf("read fixture: %v", err)
		}
		body = string(raw)
	}
	return &http.Response{StatusCode: status, Header: http.Header{},
		Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

// harness is one user's machine: a config directory, a cache, and a terminal
// that is or is not interactive.
type harness struct {
	t         *testing.T
	configDir string
	cacheDir  string
	routes    map[string]string
	replay    *replay
}

func newHarness(t *testing.T, routes map[string]string) *harness {
	t.Helper()
	t.Setenv(config.EnvDB, "")
	dir := t.TempDir()
	return &harness{
		t:         t,
		configDir: filepath.Join(dir, "config"),
		cacheDir:  filepath.Join(dir, "cache"),
		routes:    routes,
		replay:    &replay{t: t, routes: routes},
	}
}

// run executes fil with args, feeding stdin, and returns the exit code and
// what it printed.
func (h *harness) run(interactive bool, stdin string, args ...string) (int, string, string) {
	h.t.Helper()
	var out, errOut bytes.Buffer
	a := &app{
		stdin:       strings.NewReader(stdin),
		stdout:      &out,
		stderr:      &errOut,
		interactive: interactive,
		configDir:   h.configDir,
		cacheDir:    h.cacheDir,
		transport:   h.replay,
	}
	code := a.run(h.t.Context(), args)
	return code, out.String(), errOut.String()
}

var peerj = map[string]string{"/works/doi:10.7717/peerj.4375": "work_W2741809807.json"}

var attention = map[string]string{
	"/works?filter=title.search:Attention is all you need": "search_attention.json",
}

func TestAddM0DefinitionOfDone(t *testing.T) {
	// fil add 10.7717/peerj.4375 writes a row and prints the title; running it
	// twice adds nothing; the 54 references are stubs with edges.
	h := newHarness(t, peerj)

	code, out, errOut := h.run(false, "", "add", "10.7717/peerj.4375")
	if code != exitOK {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	for _, want := range []string{
		"Added: The state of OA",
		"W2741809807 · doi:10.7717/peerj.4375 · PeerJ · gold open access",
		"References: 54 — 54 new to your library, 54 citations recorded.",
		"Library:    55 works (54 not fetched yet), 54 citations.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout lacks %q:\n%s", want, out)
		}
	}
	// A first run says where the library went and that no email is set.
	if !strings.Contains(errOut, "Created your library at") || !strings.Contains(errOut, "contact email") {
		t.Errorf("first-run notes missing from stderr:\n%s", errOut)
	}

	code, out, errOut = h.run(false, "", "add", "10.7717/peerj.4375")
	if code != exitOK {
		t.Fatalf("second add: exit %d\n%s", code, errOut)
	}
	if !strings.Contains(out, "Already in your library: The state of OA") ||
		!strings.Contains(out, "Library:    55 works (54 not fetched yet), 54 citations.") {
		t.Errorf("second add:\n%s", out)
	}
	if errOut != "" {
		t.Errorf("second run repeated the first-run notes:\n%s", errOut)
	}
	if h.replay.calls != 1 {
		t.Errorf("requests = %d, want 1 — the second add should come from the cache", h.replay.calls)
	}
}

func TestAddFirstRunChoosesLocation(t *testing.T) {
	h := newHarness(t, peerj)
	chosen := filepath.Join(t.TempDir(), "Research Library")

	code, out, errOut := h.run(true, chosen+"\n", "add", "10.7717/peerj.4375")
	if code != exitOK {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "Press Enter to accept, or type another folder") {
		t.Errorf("no location prompt:\n%s", out)
	}
	want := filepath.Join(chosen, config.DBName)
	if _, err := os.Stat(want); err != nil {
		t.Errorf("library not created at the chosen folder: %v", err)
	}

	// And it is remembered: where reports it, with no first-run note.
	_, out, _ = h.run(false, "", "where")
	if !strings.Contains(out, "Library: "+want) || strings.Contains(out, "not created yet") {
		t.Errorf("where after first run:\n%s", out)
	}
}

func TestAddInvalidInputCreatesNothing(t *testing.T) {
	h := newHarness(t, nil)
	code, _, errOut := h.run(false, "", "add", "2017")
	if code != exitInvalidInput {
		t.Errorf("exit %d, want %d", code, exitInvalidInput)
	}
	if !strings.Contains(errOut, "PMID:2017") || !strings.Contains(errOut, "fil add takes a DOI") {
		t.Errorf("stderr lacks the hint:\n%s", errOut)
	}
	// A typo on a first run must not create a library or a config file.
	if _, err := os.Stat(h.configDir); !os.IsNotExist(err) {
		t.Errorf("config directory created for invalid input")
	}
}

func TestAddTitleNonInteractiveLists(t *testing.T) {
	h := newHarness(t, attention)
	code, out, errOut := h.run(false, "", "add", "Attention", "is", "all", "you", "need")
	if code != exitAmbiguous {
		t.Fatalf("exit %d, want %d\n%s", code, exitAmbiguous, errOut)
	}
	if !strings.Contains(errOut, "1. * Attention Is All You Need (2025, preprint)") ||
		!strings.Contains(errOut, "--accept-first") {
		t.Errorf("candidates or hint missing:\n%s", errOut)
	}
	if strings.Contains(out, "Added") {
		t.Errorf("something was added without a choice:\n%s", out)
	}
}

func TestAddTitleInteractiveChoice(t *testing.T) {
	h := newHarness(t, attention)
	// First-run prompt: Enter. Candidate prompt: a bad answer, then 1.
	code, out, errOut := h.run(true, "\nseven\n1\n", "add", "Attention is all you need")
	if code != exitOK {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	if !strings.Contains(out, `"seven" is not one of the numbers above.`) {
		t.Errorf("a bad choice was not asked again:\n%s", out)
	}
	if !strings.Contains(out, "Added: Attention Is All You Need") {
		t.Errorf("choice not added:\n%s", out)
	}
	if h.replay.calls != 1 {
		t.Errorf("requests = %d, want 1 — the chosen candidate needs no second fetch", h.replay.calls)
	}
}

func TestAddTitleInteractiveCancel(t *testing.T) {
	h := newHarness(t, attention)
	code, out, _ := h.run(true, "\n\n", "add", "Attention is all you need")
	if code != exitOK || !strings.Contains(out, "Cancelled. Nothing was added.") {
		t.Errorf("exit %d:\n%s", code, out)
	}
}

func TestAddTitleAcceptFirst(t *testing.T) {
	h := newHarness(t, attention)
	code, out, errOut := h.run(false, "", "add", "--accept-first", "Attention is all you need")
	if code != exitOK || !strings.Contains(out, "Added: Attention Is All You Need") {
		t.Errorf("exit %d\n%s\n%s", code, out, errOut)
	}
}

func TestAddDeadEndIsExplained(t *testing.T) {
	h := newHarness(t, map[string]string{"/works/doi:10.1145/3292500": "work_doi_proceedings.json"})
	code, out, _ := h.run(false, "", "add", "10.1145/3292500")
	if code != exitOK {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out, "OpenAlex has no reference list for this work") ||
		!strings.Contains(out, "a gap in the data, not in fil") {
		t.Errorf("dead end not explained:\n%s", out)
	}
}

func TestAddUnresolved(t *testing.T) {
	h := newHarness(t, map[string]string{"/works/W99999999999": "404"})
	code, _, errOut := h.run(false, "", "add", "W99999999999")
	if code != exitUnresolved || !strings.Contains(errOut, "OpenAlex has no record of it") {
		t.Errorf("exit %d:\n%s", code, errOut)
	}
	if strings.Contains(errOut, "<html") {
		t.Errorf("OpenAlex's HTML 404 page reached the user:\n%s", errOut)
	}
}

func TestAddNoCache(t *testing.T) {
	h := newHarness(t, peerj)
	for range 2 {
		if code, _, errOut := h.run(false, "", "--no-cache", "add", "10.7717/peerj.4375"); code != exitOK {
			t.Fatalf("exit %d\n%s", code, errOut)
		}
	}
	if h.replay.calls != 2 {
		t.Errorf("requests = %d, want 2 with --no-cache", h.replay.calls)
	}
}

func TestAddWithDBFlag(t *testing.T) {
	h := newHarness(t, peerj)
	db := filepath.Join(t.TempDir(), "elsewhere.db")
	if code, _, errOut := h.run(false, "", "--db", db, "add", "10.7717/peerj.4375"); code != exitOK {
		t.Fatalf("exit %d\n%s", code, errOut)
	}
	if _, err := os.Stat(db); err != nil {
		t.Errorf("--db ignored: %v", err)
	}
}

func TestInvalidConfigNamesTheFile(t *testing.T) {
	h := newHarness(t, nil)
	if err := os.MkdirAll(h.configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(h.configDir, config.FileName)
	if err := os.WriteFile(path, []byte("max_node = 5000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := h.run(false, "", "add", "10.7717/peerj.4375")
	if code != exitConfig || !strings.Contains(errOut, "Fix or delete "+path) {
		t.Errorf("exit %d:\n%s", code, errOut)
	}
}

func TestWhereBeforeFirstRun(t *testing.T) {
	h := newHarness(t, nil)
	code, out, _ := h.run(false, "", "where")
	if code != exitOK {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{"Library: ", "not created yet", "Config:  ", "Cache:   " + h.cacheDir} {
		if !strings.Contains(out, want) {
			t.Errorf("where lacks %q:\n%s", want, out)
		}
	}
	// where only reports; it must not create anything.
	if _, err := os.Stat(h.configDir); !os.IsNotExist(err) {
		t.Errorf("where created the config directory")
	}
}

func TestCacheClear(t *testing.T) {
	h := newHarness(t, peerj)
	h.run(false, "", "add", "10.7717/peerj.4375")
	code, out, _ := h.run(false, "", "cache", "clear")
	if code != exitOK || !strings.Contains(out, "Cleared the cache at "+h.cacheDir) {
		t.Errorf("exit %d:\n%s", code, out)
	}
	// Cleared means the next add goes to OpenAlex again.
	h.run(false, "", "add", "10.7717/peerj.4375")
	if h.replay.calls != 2 {
		t.Errorf("requests = %d, want 2 after clearing", h.replay.calls)
	}
}

func TestUsageErrors(t *testing.T) {
	h := newHarness(t, nil)
	for _, args := range [][]string{{"add"}, {"add", "--bogus", "x"}, {"where", "extra"}, {"bogus"}} {
		code, _, errOut := h.run(false, "", args...)
		if code != exitInvalidInput {
			t.Errorf("fil %v: exit %d, want %d\n%s", args, code, exitInvalidInput, errOut)
		}
	}
}

func TestVersion(t *testing.T) {
	h := newHarness(t, nil)
	code, out, _ := h.run(false, "", "version")
	if code != exitOK || !strings.HasPrefix(out, "fil "+Version) {
		t.Errorf("exit %d: %q", code, out)
	}
}

func TestThousands(t *testing.T) {
	for n, want := range map[int]string{0: "0", 999: "999", 1000: "1,000", 1234567: "1,234,567", -4200: "-4,200"} {
		if got := thousands(n); got != want {
			t.Errorf("thousands(%d) = %q, want %q", n, got, want)
		}
	}
}
