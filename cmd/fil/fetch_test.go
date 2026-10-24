package main

import (
	"strings"
	"testing"
)

// The M0 seed is gold open access, yet none of its twelve OpenAlex locations
// offers a direct PDF link — only landing pages (recorded 24 Oct 2026). fil
// must say so, not claim there is no open copy.
func TestFetchDryRunForOnePaper(t *testing.T) {
	routes := map[string]string{
		"/works/doi:10.7717/peerj.4375":         "work_W2741809807.json",
		"/works?filter=openalex_id:W2741809807": "locations_W2741809807.json",
	}
	h := newHarness(t, routes)
	if code, _, errOut := h.run(false, "", "add", "10.7717/peerj.4375"); code != exitOK {
		t.Fatalf("add: exit %d\n%s", code, errOut)
	}

	code, out, errOut := h.run(false, "", "fetch", "--dry-run", "10.7717/peerj.4375")
	if code != exitOK {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "The state of OA") || !strings.Contains(out, "Open access, but only as a web page") {
		t.Errorf("fetch --dry-run <paper>:\n%s", out)
	}

	// Looked up once: asking again makes no request.
	calls := h.replay.calls
	h.run(false, "", "fetch", "--dry-run", "10.7717/peerj.4375")
	if h.replay.calls != calls {
		t.Errorf("a second look made %d requests", h.replay.calls-calls)
	}

	_, out, _ = h.run(false, "", "stats")
	if !strings.Contains(out, "PDFs:       0 of 1 looked up have a legal PDF link; 54 not looked up yet") {
		t.Errorf("stats:\n%s", out)
	}
}

func TestFetchNeedsDryRunForNow(t *testing.T) {
	h := newHarness(t, nil)
	code, _, errOut := h.run(false, "", "fetch")
	if code != exitInvalidInput || !strings.Contains(errOut, "--dry-run") {
		t.Errorf("exit %d:\n%s", code, errOut)
	}
}

func TestFetchDryRunOnAnEmptyLibrary(t *testing.T) {
	h := newHarness(t, nil)
	code, out, _ := h.run(false, "", "fetch", "--dry-run")
	if code != exitOK || !strings.Contains(out, "Your library is empty") {
		t.Errorf("exit %d:\n%s", code, out)
	}
}
