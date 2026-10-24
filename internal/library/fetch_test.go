package library

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/codevector-2003/filiation/internal/config"
	"github.com/codevector-2003/filiation/internal/model"
	"github.com/codevector-2003/filiation/internal/store"
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
			w.Write([]byte("%PDF-1.7\nthe seed\n%%EOF\n"))
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
	h := newPDFHosts(t)
	dir := t.TempDir()
	cfg := &config.Config{MaxNodes: 500, MaxDepth: 3, MaxRefsPerWork: 100, FolderCollections: true}
	if err := cfg.UseFolder(filepath.Join(dir, "Lib")); err != nil {
		t.Fatal(err)
	}
	l, err := Open(t.Context(), cfg, Options{CacheDir: filepath.Join(dir, "cache"),
		Transport: &replay{t: t, routes: peerj}, PDFTransport: h.transport()})
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
	works := map[string]model.Work{"seed": added.Work, "A": n.Cites[0], "B": n.Cites[1], "C": n.Cites[2]}

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
	seed := byID[works["seed"].FilID]
	if filepath.Base(seed.File) != "published.pdf" || !strings.Contains(seed.File, "Piwowar-2018-The-state-of-OA") {
		t.Errorf("seed saved at %q", seed.File)
	}
	if a := byID[works["A"].FilID]; filepath.Base(a.File) != "preprint.pdf" || len(a.Attempts) != 2 {
		t.Errorf("A = %+v; want the preprint after the publisher refused", a)
	}
	if c := byID[works["C"].FilID]; c.File != "" {
		t.Errorf("C kept a copy of the seed's bytes at %q", c.File)
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
