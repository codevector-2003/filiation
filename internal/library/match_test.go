package library

import (
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"testing"

	"github.com/codevector-2003/filiation/internal/errs"
	"github.com/codevector-2003/filiation/internal/text/pdftest"
)

// attentionWork is the first work of the recorded title search, as a single
// lookup would answer it.
func attentionWork(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(fixtureDir, "search_attention.json"))
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Results []json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(raw, &page); err != nil || len(page.Results) == 0 {
		t.Fatalf("search fixture: %v", err)
	}
	return string(page.Results[0])
}

func localDoc(t *testing.T, l *Library, name, title string, pdf []byte) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(file, pdf, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := l.ImportLocal(t.Context(), file, title, nil)
	if err != nil {
		t.Fatal(err)
	}
	return got.Work.FilID
}

func TestFindALocalDocumentByTitle(t *testing.T) {
	t.Parallel()
	routes := maps.Clone(peerj)
	routes["/works?filter=title.search:Attention is all you need"] = "search_attention.json"
	routes["/works/W2626778328"] = attentionWork(t)
	l, _ := openImportTest(t, routes)
	ctx := t.Context()
	pdf := pdftest.Doc{Title: "Attention is all you need",
		Pages: []string{"Ashish Vaswani, Noam Shazeer, Niki Parmar\nThe dominant sequence transduction models are based on\n" +
			"complex recurrent or convolutional neural networks that include an encoder and a decoder."}}.Bytes()
	fil := localDoc(t, l, "transformer.pdf", "Attention is all you need", pdf)

	found, err := l.TitleCandidates(ctx, fil)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) == 0 || found[0].Work.OpenAlexID != "W2626778328" || !found[0].TitleMatch || !found[0].PDFMatches || found[0].InLibrary != "" {
		t.Fatalf("candidates = %+v", found)
	}
	for _, c := range found[1:] {
		if c.PDFMatches {
			t.Errorf("%s matches the PDF, but its title is not on it", c.Work.DisplayTitle())
		}
	}

	got, err := l.LinkLocal(ctx, fil, "W2626778328", false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Folded || got.Work.FilID != fil || got.Work.OpenAlexID != "W2626778328" || got.Work.DisplayTitle() != "Attention Is All You Need" {
		t.Errorf("linked = %+v; the document keeps its fil ID and takes the work's record", got)
	}
	if files, _ := l.db.FilesOf(ctx, fil); len(files) != 1 {
		t.Errorf("its PDF: %d files", len(files))
	}
	if _, err := l.LinkLocal(ctx, fil, "W2626778328", false); !errors.Is(err, errs.ErrInvalidInput) {
		t.Errorf("linking it twice: %v", err)
	}
	if _, err := l.TitleCandidates(ctx, fil); !errors.Is(err, errs.ErrInvalidInput) {
		t.Errorf("searching for a linked paper: %v", err)
	}
}

func TestLinkALocalDocumentToAPaperAlreadyHere(t *testing.T) {
	t.Parallel()
	l, _ := openImportTest(t, peerj)
	ctx := t.Context()
	seed, err := l.Add(ctx, "10.7717/peerj.4375", AddOptions{})
	if err != nil {
		t.Fatal(err)
	}
	fil := localDoc(t, l, "oa.pdf", "My copy of the OA paper", pdftest.Make(seedFront(seedTitle)))
	col, err := l.CreateCollection(ctx, "Reading", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.FileInCollection(ctx, col, []string{fil}, true); err != nil {
		t.Fatal(err)
	}

	got, err := l.LinkLocal(ctx, fil, "https://doi.org/10.7717/peerj.4375", true)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Folded || got.Work.FilID != seed.Work.FilID {
		t.Errorf("linked = %+v; it becomes the seed", got)
	}
	if w, err := l.getWork(ctx, fil); err != nil || w.FilID != seed.Work.FilID {
		t.Errorf("the document's fil ID finds %+v, %v", w, err)
	}
	if files, _ := l.db.FilesOf(ctx, seed.Work.FilID); len(files) != 1 {
		t.Errorf("the seed has %d files; the document's PDF came with it", len(files))
	}
	rows, _ := l.List(ctx, ListOptions{CollectionID: col})
	if len(rows) != 1 || rows[0].FilID != seed.Work.FilID {
		t.Errorf("Reading holds %+v", rows)
	}
	if n, _ := l.Summarise(ctx); n.Works != 55 {
		t.Errorf("works = %d, want the seed and its 54 references", n.Works)
	}
}
