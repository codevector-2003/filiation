package library

import (
	"context"
	"errors"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/codevector-2003/filiation/internal/blobs"
	"github.com/codevector-2003/filiation/internal/config"
	"github.com/codevector-2003/filiation/internal/model"
	"github.com/codevector-2003/filiation/internal/store"
	"github.com/codevector-2003/filiation/internal/text/pdftest"
)

// seedPDF is a real PDF, so a test can read and search it. Its page 2 says
// something only it says.
var seedPDF = pdftest.Make(
	"Data reuse and the open data citation advantage\n"+
		"Heather A. Piwowar and Todd J. Vision\n"+
		"Background. Attribution to the original contributor upon reuse of published data is\n"+
		"important both as a reward for data creators and to document the provenance of\n"+
		"research findings. Previous studies have found that papers with publicly available\n"+
		"datasets receive a higher number of citations than similar studies without available\n"+
		"data. However, few previous analyses have had the statistical power to control for the\n"+
		"many variables known to predict citation rate, which has led to uncertain estimates.",
	"Results. We found that studies that made data available in a public repository\n"+
		"received nine percent more citations than similar studies for which the data\n"+
		"was not made available. Data reuse was traced through the gene expression\n"+
		"microarray repository, and the citation benefit was robust to many confounders,\n"+
		"including journal impact factor, date of publication and author country.",
)

// pdfHosts stands in for publishers and repositories: one local server
// answering for any host name, so nothing leaves the machine.
type pdfHosts struct {
	srv  *httptest.Server
	mu   sync.Mutex
	hits map[string]int
}

func newPDFHosts(t *testing.T) *pdfHosts {
	h := &pdfHosts{hits: map[string]int{}}
	h.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		h.hits[r.Host+r.URL.Path]++
		h.mu.Unlock()
		switch r.URL.Path {
		case "/seed.pdf", "/seed-copy.pdf": // the same bytes under two links
			w.Write(seedPDF)
		case "/retired.pdf":
			w.Write(pdftest.Make("The same paper, under the ID OpenAlex retired when it merged two records."))
		case "/titled.pdf":
			w.Write(pdftest.Doc{Title: "Measuring the stub", Pages: []string{"Ada Lovelace\nWe measure the stub, " +
				"and find it longer than expected in every one of the cases we looked at closely."}}.Bytes())
		case "/stub.pdf":
			w.Write(pdftest.Make("A reference that was known only by its ID until it was fetched."))
		case "/preprint.pdf":
			w.Write([]byte("%PDF-1.7\na preprint\n%%EOF\n"))
		case "/landing":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte("<!doctype html><title>Read online</title>"))
		case "/forbidden":
			w.WriteHeader(http.StatusForbidden)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(h.srv.Close)
	return h
}

func (h *pdfHosts) transport() http.RoundTripper {
	addr := h.srv.Listener.Addr().String()
	return &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}}
}

func (h *pdfHosts) count(hostPath string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.hits[hostPath]
}

func link(host, path, version string) model.Location {
	return model.Location{PDFURL: "http://" + host + path, Host: host, Version: version}
}

// openFetchTest is a library folder with the M0 seed and its 54 references.
// Every work is marked as looked up — no OpenAlex request is made — and the
// seed and three references get links on the local hosts:
//
//	seed  published, served
//	ref A publisher refuses; preprint served
//	ref B only a landing page
//	ref C the seed's own bytes again, under another link
func openFetchTest(t *testing.T) (*Library, *pdfHosts, map[string]model.Work) {
	t.Helper()
	return openFetchTestWith(t, maps.Clone(peerj))
}

// openFetchTestWith is openFetchTest answering OpenAlex from routes, which a
// test may add to before it fetches.
func openFetchTestWith(t *testing.T, routes map[string]string) (*Library, *pdfHosts, map[string]model.Work) {
	t.Helper()
	h := newPDFHosts(t)
	dir := t.TempDir()
	cfg := &config.Config{MaxNodes: 500, MaxDepth: 3, MaxRefsPerWork: 100, FolderCollections: true}
	if err := cfg.UseFolder(filepath.Join(dir, "Lib")); err != nil {
		t.Fatal(err)
	}
	l, err := Open(t.Context(), cfg, Options{CacheDir: filepath.Join(dir, "cache"),
		Transport: &replay{t: t, routes: routes, details: true}, PDFTransport: h.transport()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	ctx := t.Context()
	added, err := l.Add(ctx, "10.7717/peerj.4375", AddOptions{})
	if err != nil {
		t.Fatal(err)
	}
	n, _ := l.Neighbours(ctx, "10.7717/peerj.4375")
	works := map[string]model.Work{"seed": added.Work, "A": n.Cites[0], "B": n.Cites[1], "C": n.Cites[2], "D": n.Cites[3], "E": n.Cites[4]}

	// A, B and C have titles, as fetch gives a reference before downloading
	// it (hydrateStubs), so these tests need no recorded hydration. D stays
	// a stub, for the test of that.
	err = l.db.Tx(ctx, func(tx *store.Tx) error {
		for _, k := range []string{"A", "B", "C"} {
			w := works[k]
			title := "Reference " + k
			w.Title, w.Hydrated, w.FetchedRefs = &title, true, true
			if err := tx.UpsertWork(ctx, &w); err != nil {
				return err
			}
			works[k] = w
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	ids, err := l.db.ToLocate(ctx, 1000, 0)
	if err != nil {
		t.Fatal(err)
	}
	set := map[string]model.PDFLinks{
		works["seed"].OpenAlexID: {Availability: model.PDFLink, Links: []model.Location{link("pub.test", "/seed.pdf", "published")}},
		works["A"].OpenAlexID: {Availability: model.PDFLink, Links: []model.Location{
			link("pub.test", "/forbidden", "published"), link("repo.test", "/preprint.pdf", "preprint")}},
		works["B"].OpenAlexID: {Availability: model.PDFLink, Links: []model.Location{link("pub.test", "/landing", "published")}},
		works["C"].OpenAlexID: {Availability: model.PDFLink, Links: []model.Location{link("mirror.test", "/seed-copy.pdf", "")}},
	}
	err = l.db.Tx(ctx, func(tx *store.Tx) error {
		for _, id := range ids {
			links, ok := set[id]
			if !ok {
				links = model.PDFLinks{Availability: model.PDFNoOpenCopy}
			}
			if err := tx.PutPDFLinks(ctx, id, links); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return l, h, works
}

// M3 step 6, done when (the half that does not depend on Unpaywall): fetch
// puts PDFs in papers/, and a second run downloads nothing.
func TestFetchDownloadsAndASecondRunDownloadsNothing(t *testing.T) {
	t.Parallel()
	l, h, works := openFetchTest(t)
	ctx := t.Context()

	var progress int
	res, err := l.Fetch(ctx, FetchOptions{Progress: func(Fetched) { progress++ }})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if res.Tried != 4 || res.Downloaded != 2 || progress != 4 {
		t.Fatalf("Fetch = tried %d, downloaded %d, progress %d; want 4, 2, 4", res.Tried, res.Downloaded, progress)
	}
	if res.Outcomes["refused"] != 1 || res.Outcomes["not-a-pdf"] != 1 || res.Outcomes["duplicate"] != 1 {
		t.Errorf("outcomes = %v", res.Outcomes)
	}

	byID := map[string]FetchedPaper{}
	for _, p := range res.Papers {
		byID[p.Work.FilID] = p
	}
	if a := byID[works["A"].FilID]; filepath.Base(a.File) != "preprint.pdf" || len(a.Attempts) != 2 {
		t.Errorf("A = %+v; want the preprint after the publisher refused", a)
	}
	// The seed and C are served the same bytes, downloading at the same time.
	// Whichever is recorded first keeps them; the other is a duplicate. Never
	// both — two workers must not each find the bytes new.
	seed, c := byID[works["seed"].FilID], byID[works["C"].FilID]
	switch {
	case seed.File != "" && c.File != "":
		t.Errorf("both kept the same bytes: %q and %q", seed.File, c.File)
	case seed.File != "":
		if filepath.Base(seed.File) != "published.pdf" || !strings.Contains(seed.File, "Piwowar-2018-The-state-of-OA") {
			t.Errorf("seed saved at %q", seed.File)
		}
		if c.Attempts[0].Outcome != "duplicate" {
			t.Errorf("C = %+v; want a duplicate", c.Attempts)
		}
	case c.File != "":
		if seed.Attempts[0].Outcome != "duplicate" {
			t.Errorf("seed = %+v; want a duplicate", seed.Attempts)
		}
	default:
		t.Error("neither the seed nor C kept the PDF")
	}
	if n, _ := l.FetchedOnDisk(ctx); n != 2 {
		t.Errorf("PDFs on disk = %d", n)
	}
	if left, _ := os.ReadDir(filepath.Join(l.Folder(), ".filiation", "tmp")); len(left) != 0 {
		t.Errorf("temporary files left: %v", left)
	}

	// Nothing new to download, and the failed links are left alone.
	again, err := l.Fetch(ctx, FetchOptions{})
	if err != nil || again.Tried != 0 {
		t.Errorf("second Fetch = tried %d, %v; want nothing", again.Tried, err)
	}
	if h.count("pub.test/seed.pdf") != 1 || h.count("pub.test/landing") != 1 {
		t.Errorf("a second run asked again: seed %d, landing %d", h.count("pub.test/seed.pdf"), h.count("pub.test/landing"))
	}

	// The paper says why it has no PDF.
	attempts, err := l.Attempts(ctx, works["B"].FilID)
	if err != nil || len(attempts) != 1 || attempts[0].Outcome != "not-a-pdf" {
		t.Errorf("B's attempts = %+v, %v", attempts, err)
	}
}

// A PDF the researcher deleted is not downloaded again unasked; naming the
// paper asks.
func TestFetchRespectsADeletedPDF(t *testing.T) {
	t.Parallel()
	l, h, works := openFetchTest(t)
	ctx := t.Context()
	res, err := l.Fetch(ctx, FetchOptions{Max: 1})
	if err != nil || res.Downloaded != 1 || res.Papers[0].Work.FilID != works["seed"].FilID {
		t.Fatalf("Fetch Max 1 = %+v, %v; want the seed alone", res, err)
	}
	if err := os.RemoveAll(filepath.Dir(res.Papers[0].File)); err != nil {
		t.Fatal(err)
	}

	again, err := l.Fetch(ctx, FetchOptions{Max: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range again.Papers {
		if p.Work.FilID == works["seed"].FilID {
			t.Errorf("the deleted PDF was downloaded again unasked")
		}
	}

	one, err := l.FetchOne(ctx, "10.7717/peerj.4375")
	if err != nil || one.File == "" || one.Already {
		t.Fatalf("FetchOne = %+v, %v; naming the paper should download it", one, err)
	}
	if h.count("pub.test/seed.pdf") != 2 {
		t.Errorf("seed downloads = %d, want 2", h.count("pub.test/seed.pdf"))
	}
	if again, _ := l.FetchOne(ctx, "10.7717/peerj.4375"); !again.Already || again.File != one.File {
		t.Errorf("FetchOne when present = %+v; want the file already there", again)
	}
}

func TestFetchCancelledKeepsNothingHalfWritten(t *testing.T) {
	t.Parallel()
	l, _, _ := openFetchTest(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := l.Fetch(ctx, FetchOptions{}); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled Fetch = %v", err)
	}
	if left, _ := os.ReadDir(filepath.Join(l.Folder(), ".filiation", "tmp")); len(left) != 0 {
		t.Errorf("temporary files left: %v", left)
	}
}

func TestFetchNeedsAFolder(t *testing.T) {
	t.Parallel()
	l, _ := openTest(t, peerj, Options{})
	if _, err := l.Fetch(t.Context(), FetchOptions{}); !errors.Is(err, ErrNoFolder) {
		t.Errorf("Fetch = %v, want ErrNoFolder", err)
	}
}

// Two downloads bringing the same bytes for different papers, saved at the
// same moment: exactly one keeps them. Forced 25 times over, because a
// timing that lets one finish first hides the race (it passed locally and
// failed in CI on #6 before the lock).
func TestKeepIsOneStepUnderRace(t *testing.T) {
	t.Parallel()
	l, _, _ := openFetchTest(t)
	ctx := t.Context()
	n, err := l.Neighbours(ctx, "10.7717/peerj.4375")
	if err != nil {
		t.Fatal(err)
	}
	// A fresh pair of papers each round: a paper gets one PDF, not thirty.
	for i := range 25 {
		pair := [2]model.Work{}
		for j := range pair {
			if pair[j], err = l.workWithAuthors(ctx, n.Cites[2*i+j].FilID); err != nil {
				t.Fatal(err)
			}
		}
		body := "%PDF-1.7\nrace " + strconv.Itoa(i) + "\n%%EOF\n"
		var staged [2]*blobs.Staged
		for j := range staged {
			if staged[j], err = l.files.Stage(strings.NewReader(body), blobs.WriteOptions{PDF: true}); err != nil {
				t.Fatal(err)
			}
		}
		start := make(chan struct{})
		var kept [2]bool
		var wg sync.WaitGroup
		for j, w := range pair {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				var a store.Attempt
				_, dup, err := l.keep(ctx, w, link("x.test", "/"+strconv.Itoa(i), "published"), staged[j], &a)
				staged[j].Discard()
				if err != nil {
					t.Error(err)
				}
				kept[j] = !dup
			}()
		}
		close(start)
		wg.Wait()
		if kept[0] == kept[1] {
			t.Fatalf("round %d: kept by the first %v, by the second %v; want exactly one", i, kept[0], kept[1])
		}
	}
}

// A library folder that cannot be written is the disk's fault, not the
// link's: fetch stops, and the link is not left alone for a month.
func TestFetchLocalFailureIsNotTheLinksFault(t *testing.T) {
	t.Parallel()
	l, _, works := openFetchTest(t)
	ctx := t.Context()
	tmp := filepath.Join(l.Folder(), ".filiation", "tmp")
	if err := os.RemoveAll(tmp); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmp, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Fetch(ctx, FetchOptions{Max: 1}); err == nil {
		t.Fatal("Fetch succeeded with nowhere to save")
	}
	if a, _ := l.Attempts(ctx, works["seed"].FilID); len(a) != 0 {
		t.Errorf("attempts recorded against the link: %+v", a)
	}

	os.Remove(tmp)
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := l.Fetch(ctx, FetchOptions{Max: 1})
	if err != nil || res.Downloaded != 1 {
		t.Errorf("after the disk was fixed: %+v, %v; want the seed downloaded", res, err)
	}
}

func TestIsPDFPath(t *testing.T) {
	t.Parallel()
	for p, want := range map[string]bool{"a/published.pdf": true, "a/x.PDF": true, "a/x.PdF": true, "a/notes.txt": false, "a/pdf": false} {
		if got := isPDFPath(p); got != want {
			t.Errorf("isPDFPath(%q) = %v", p, got)
		}
	}
}

// A paper fil knows only by its ID is fetched before its PDF is downloaded,
// so its folder is named for it and search shows its title — not
// "Unknown-nd-Untitled".
func TestFetchGivesAStubATitleFirst(t *testing.T) {
	t.Parallel()
	routes := maps.Clone(peerj)
	l, _, works := openFetchTestWith(t, routes)
	ctx := t.Context()
	d := works["D"]
	if !d.IsStub() {
		t.Fatalf("D = %+v, want a stub", d)
	}
	routes["/works?filter=openalex_id:"+d.OpenAlexID] = `{"meta":{"count":1},"results":[{
		"id":"https://openalex.org/` + d.OpenAlexID + `","title":"Measuring the stub",
		"publication_year":2017,"type":"article",
		"authorships":[{"author":{"id":"https://openalex.org/A5000000001","display_name":"Ada Lovelace"}}],"referenced_works":[]}]}`
	if err := l.db.Tx(ctx, func(tx *store.Tx) error {
		return tx.PutPDFLinks(ctx, d.OpenAlexID, model.PDFLinks{Availability: model.PDFLink,
			Links: []model.Location{link("pub.test", "/stub.pdf", "published")}})
	}); err != nil {
		t.Fatal(err)
	}

	res, err := l.Fetch(ctx, FetchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Hydrated != 1 {
		t.Errorf("Hydrated = %d, want D", res.Hydrated)
	}
	var file string
	for _, p := range res.Papers {
		if p.Work.FilID == d.FilID {
			file = p.File
		}
	}
	if !strings.Contains(filepath.Base(filepath.Dir(file)), "Lovelace-2017-Measuring-the-stub") {
		t.Errorf("D's PDF is at %q; want a folder named for it", file)
	}
	got, err := l.Find(ctx, d.FilID)
	if err != nil || !got.Hydrated || got.DisplayTitle() != "Measuring the stub" {
		t.Errorf("D after fetch = %+v, %v", got, err)
	}
}

// Two papers on the list that hydration finds to be one — OpenAlex merged E
// into D — are downloaded once, not twice under two names (found in review,
// PR #9).
func TestFetchDownloadsAMergedPaperOnce(t *testing.T) {
	t.Parallel()
	routes := maps.Clone(peerj)
	l, _, works := openFetchTestWith(t, routes)
	ctx := t.Context()
	d, e := works["D"], works["E"]
	record := `{"id":"https://openalex.org/` + d.OpenAlexID + `","title":"Measuring the stub",
		"publication_year":2017,"type":"article","referenced_works":[]}`
	// The batch returns D alone, for either order; E's single lookup answers
	// with D: E was merged into it.
	routes["/works?filter=openalex_id:"+d.OpenAlexID+"|"+e.OpenAlexID] = `{"meta":{"count":1},"results":[` + record + `]}`
	routes["/works?filter=openalex_id:"+e.OpenAlexID+"|"+d.OpenAlexID] = `{"meta":{"count":1},"results":[` + record + `]}`
	routes["/works/"+e.OpenAlexID] = record
	if err := l.db.Tx(ctx, func(tx *store.Tx) error {
		if err := tx.PutPDFLinks(ctx, d.OpenAlexID, model.PDFLinks{Availability: model.PDFLink,
			Links: []model.Location{link("pub.test", "/stub.pdf", "published")}}); err != nil {
			return err
		}
		return tx.PutPDFLinks(ctx, e.OpenAlexID, model.PDFLinks{Availability: model.PDFLink,
			Links: []model.Location{link("repo.test", "/retired.pdf", "published")}})
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := l.Fetch(ctx, FetchOptions{}); err != nil {
		t.Fatal(err)
	}
	files, err := l.db.FilesOf(ctx, d.FilID)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Errorf("D has %d files after the merge, want 1: %+v", len(files), files)
	}
}
