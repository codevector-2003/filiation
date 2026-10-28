package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codevector-2003/filiation/internal/text/pdftest"
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
	if !strings.Contains(out, "PDFs:       0 of 1 looked up have a legal PDF link; 54 not looked up yet") ||
		!strings.Contains(out, "0 papers have their PDF on disk in papers/") {
		t.Errorf("stats:\n%s", out)
	}

	// Asked to download it, fil says there is nothing legal to try.
	code, out, _ = h.run(false, "", "fetch", "10.7717/peerj.4375")
	if code == exitOK || !strings.Contains(out, "No legal PDF link to try") {
		t.Errorf("fetch <paper> with no link: exit %d\n%s", code, out)
	}
}

// seedWithLink stands in for an OpenAlex answer giving the seed a PDF link
// on a local host — the real seed has none.
const seedWithLink = `{"meta":{"count":1},"results":[{"id":"https://openalex.org/W2741809807",
	"open_access":{"is_oa":true,"oa_status":"gold"},
	"locations":[{"is_oa":true,"pdf_url":"http://peerj.test/articles/4375.pdf",
		"landing_page_url":"https://doi.org/10.7717/peerj.4375","version":"publishedVersion","license":"cc-by"}]}]}`

// stateOfOA stands in for the seed's PDF: real, two pages, readable.
var stateOfOA = pdftest.Make(
	"The state of OA: a large-scale analysis of the prevalence and impact of\n"+
		"open access articles. Despite growing interest in open access to scholarly\n"+
		"literature, there is an unmet need for large-scale, up-to-date and\n"+
		"reproducible studies assessing the prevalence and characteristics of open\n"+
		"access. We address this need using three samples, each of one hundred thousand\n"+
		"articles, investigating three populations of articles and their access status.",
	"We estimate that at least 28 percent of the scholarly literature is open access,\n"+
		"and that this proportion is growing, driven particularly by growth in gold and\n"+
		"hybrid. We also find that green open access, deposited in repositories, and\n"+
		"hybrid open access are cited more often than closed articles in this sample.",
)

func TestFetchOnePaper(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(stateOfOA)
	}))
	defer srv.Close()
	h := newHarness(t, map[string]string{
		"/works/doi:10.7717/peerj.4375":         "work_W2741809807.json",
		"/works?filter=openalex_id:W2741809807": seedWithLink,
	})
	addr := srv.Listener.Addr().String()
	h.pdfs = &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}}
	if code, _, errOut := h.run(false, "", "add", "10.7717/peerj.4375"); code != exitOK {
		t.Fatalf("add: exit %d\n%s", code, errOut)
	}

	code, out, errOut := h.run(false, "", "fetch", "10.7717/peerj.4375")
	if code != exitOK || !strings.Contains(out, "Downloaded:") {
		t.Fatalf("fetch <paper>: exit %d\n%s\n%s", code, out, errOut)
	}
	path, _, _ := strings.Cut(strings.TrimSpace(out[strings.Index(out, "Downloaded:")+len("Downloaded:"):]), "\n")
	if filepath.Base(path) != "published.pdf" || !strings.Contains(path, filepath.Join("Filiation", "My Library", "papers")) {
		t.Errorf("saved at %q", path)
	}
	if b, err := os.ReadFile(path); err != nil || !strings.HasPrefix(string(b), "%PDF-") {
		t.Errorf("the file is not the PDF: %v", err)
	}
	// Read as soon as it is downloaded.
	if !strings.Contains(out, "Text:  searchable, 2 pages") {
		t.Errorf("fetch <paper> did not read the PDF:\n%s", out)
	}

	_, out, _ = h.run(false, "", "fetch", "10.7717/peerj.4375")
	if !strings.Contains(out, "Already in your library:") || !strings.Contains(out, "read before") {
		t.Errorf("second fetch <paper>:\n%s", out)
	}
	_, out, _ = h.run(false, "", "stats")
	if !strings.Contains(out, "1 paper has its PDF on disk in papers/") || !strings.Contains(out, "Text:       1 paper searchable") {
		t.Errorf("stats:\n%s", out)
	}

	// M3 step 7, done when: a passage is found, with its page.
	code, out, errOut = h.run(false, "", "search", "green", "open", "access", "hybrid")
	if code != exitOK || !strings.Contains(out, "page 2") || !strings.Contains(out, "«green open access»") ||
		!strings.Contains(out, path) {
		t.Errorf("search: exit %d\n%s\n%s", code, out, errOut)
	}
	code, out, _ = h.run(false, "", "search", "nowhere", "to", "be", "found")
	if code != exitNotFound || !strings.Contains(out, "No passage contains all of those words.") {
		t.Errorf("search for absent words: exit %d\n%s", code, out)
	}
	if code, _, errOut := h.run(false, "", "search", "-", "+"); code != exitInvalidInput || !strings.Contains(errOut, "at least one word") {
		t.Errorf("search with no words: exit %d\n%s", code, errOut)
	}
	_, out, _ = h.run(false, "", "extract")
	if !strings.Contains(out, "Nothing to read") {
		t.Errorf("extract after fetch read everything:\n%s", out)
	}
	_, out, _ = h.run(false, "", "extract", "--again")
	if !strings.Contains(out, "Read:       1 PDF") || !strings.Contains(out, "1 searchable") {
		t.Errorf("extract --again:\n%s", out)
	}
	_, out, _ = h.run(false, "", "extract", "10.7717/peerj.4375")
	if !strings.Contains(out, "Text:  searchable, 2 pages; read before") {
		t.Errorf("extract <paper>:\n%s", out)
	}

	// Deleted by hand: stats looks at the disk, not the last scan.
	if err := os.RemoveAll(filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
	_, out, _ = h.run(false, "", "stats")
	if !strings.Contains(out, "0 papers have their PDF on disk in papers/") {
		t.Errorf("stats after deleting the PDF:\n%s", out)
	}
}

func TestFetchOnAnEmptyLibrary(t *testing.T) {
	h := newHarness(t, nil)
	for _, args := range [][]string{{"fetch"}, {"fetch", "--dry-run"}} {
		code, out, _ := h.run(false, "", args...)
		if code != exitOK || !strings.Contains(out, "Your library is empty") {
			t.Errorf("%v: exit %d:\n%s", args, code, out)
		}
	}
}

func TestFetchNeedsALibraryFolder(t *testing.T) {
	h := newHarness(t, map[string]string{"/works/doi:10.7717/peerj.4375": "work_W2741809807.json"})
	db := filepath.Join(t.TempDir(), "old.db")
	if code, _, errOut := h.run(false, "", "--db", db, "add", "10.7717/peerj.4375"); code != exitOK {
		t.Fatalf("add: exit %d\n%s", code, errOut)
	}
	code, _, errOut := h.run(false, "", "--db", db, "fetch")
	if code == exitOK || !strings.Contains(errOut, "fil library move") {
		t.Errorf("fetch on a database-only library: exit %d\n%s", code, errOut)
	}
}
