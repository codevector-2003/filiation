package library

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codevector-2003/filiation/internal/errs"
	"github.com/codevector-2003/filiation/internal/text"
	"github.com/codevector-2003/filiation/internal/text/pdftest"
)

// M3 step 7, done when: a passage in the seed is found, with its page. The
// seed's PDF is fetched and read in one run; the search names page 2.
func TestFetchReadsSoSearchFindsAPassageWithItsPage(t *testing.T) {
	t.Parallel()
	l, _, works := openFetchTest(t)
	ctx := t.Context()

	var reads int
	res, err := l.Fetch(ctx, FetchOptions{ReadProgress: func(Extracted) { reads++ }})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	// The seed and ref A's preprint were downloaded; both are read. The
	// preprint's bytes are a PDF header and nothing else.
	if res.Read.Done != 2 || res.Read.Counts["ok"] != 1 || res.Read.Counts["unreadable"] != 1 || reads != 2 {
		t.Fatalf("Read = %+v (progress %d); want the seed ok and the preprint unreadable", res.Read, reads)
	}

	found, err := l.Search(ctx, "nine percent more citations", SearchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(found.Hits) != 1 {
		t.Fatalf("got %d hits, want 1: %+v", len(found.Hits), found.Hits)
	}
	// The seed and C are served the same bytes, and whichever downloads
	// first keeps them (TestKeepIsOneStepUnderRace).
	h := found.Hits[0]
	if h.Work.FilID != works["seed"].FilID && h.Work.FilID != works["C"].FilID || h.Page != 2 {
		t.Errorf("hit = %+v; want the seed (or C), page 2", h)
	}
	if h.Work.FilID == works["seed"].FilID && (h.Work.Title == nil || len(h.Work.Authors) == 0) {
		t.Errorf("hit = %+v; want the seed's title and authors", h.Work)
	}
	if !strings.Contains(h.Snippet, MarkStart+"nine"+MarkEnd) {
		t.Errorf("snippet %q does not mark the match", h.Snippet)
	}
	if _, err := os.Stat(h.File); err != nil {
		t.Errorf("the hit's file: %v", err)
	}
	if found.Unread != 0 {
		t.Errorf("Unread = %d after fetch read everything", found.Unread)
	}

	// Stemmed: "citation" finds "citations".
	if found, _ := l.Search(ctx, "citation benefit", SearchOptions{}); len(found.Hits) != 1 {
		t.Errorf("citation benefit: %+v", found.Hits)
	}
	// Nothing to read the second time.
	again, err := l.ExtractText(ctx, ExtractOptions{})
	if err != nil || again.Total != 0 {
		t.Errorf("a second read = %+v, %v; want nothing to do", again, err)
	}
	counts, _ := l.TextCounts(ctx)
	if counts["ok"] != 1 || counts["unreadable"] != 1 {
		t.Errorf("TextCounts = %v", counts)
	}
}

// putPDF puts a PDF in a paper's folder by hand, as a researcher would.
func putPDF(t *testing.T, l *Library, ref, name string, pdf []byte) string {
	t.Helper()
	f, err := l.FolderFor(t.Context(), ref)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(f.Path, name)
	if err := os.WriteFile(p, pdf, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestExtractTextReadsPDFsPutThereByHand(t *testing.T) {
	t.Parallel()
	l, _, works := openFetchTest(t)
	ctx := t.Context()
	putPDF(t, l, works["A"].FilID, "My copy.PDF", pdftest.Make(
		"A paper about the reproducibility of psychological science and its replication\n"+
			"crisis, set over enough lines that its text layer reads as ordinary prose to the\n"+
			"check that tells text from a scan or a garbled layer of symbols."))
	putPDF(t, l, works["B"].FilID, "scan.pdf", pdftest.Make("", "", ""))

	// Before reading, a search says what it cannot see yet — once a scan
	// has seen the files. Search itself does not scan; extract does.
	if _, err := l.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	before, err := l.Search(ctx, "reproducibility", SearchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Hits) != 0 {
		t.Errorf("found before reading: %+v", before.Hits)
	}
	if before.Unread != 2 {
		t.Errorf("Unread = %d, want 2", before.Unread)
	}

	res, err := l.ExtractText(ctx, ExtractOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Done != 2 || res.Counts["ok"] != 1 || res.Counts["no-text"] != 1 {
		t.Fatalf("ExtractText = %+v", res)
	}
	for _, p := range res.Papers {
		if p.Status == "no-text" && (p.Reason == "" || p.Passages != 0) {
			t.Errorf("the scan: %+v; want a reason and no passages", p)
		}
	}
	after, _ := l.Search(ctx, "reproducibility", SearchOptions{})
	if len(after.Hits) != 1 || after.Hits[0].Work.FilID != works["A"].FilID || after.Hits[0].Page != 1 {
		t.Errorf("after reading = %+v", after.Hits)
	}

	// Again reads both; one paper by name reads it even if read.
	if res, err := l.ExtractText(ctx, ExtractOptions{Again: true}); err != nil || res.Done != 2 {
		t.Errorf("again = %+v, %v", res, err)
	}
	one, err := l.ExtractOne(ctx, works["B"].FilID, false)
	if err != nil || !one.Already || one.Status != "no-text" {
		t.Errorf("ExtractOne(B) = %+v, %v; want already read, no-text", one, err)
	}
	one, err = l.ExtractOne(ctx, works["B"].FilID, true)
	if err != nil || one.Already || one.Status != "no-text" {
		t.Errorf("ExtractOne(B, again) = %+v, %v", one, err)
	}
	if _, err := l.ExtractOne(ctx, works["C"].FilID, false); !errors.Is(err, ErrNoPDF) {
		t.Errorf("ExtractOne(C), which has no PDF: %v", err)
	}

	// A changed file is read again.
	putPDF(t, l, works["A"].FilID, "My copy.PDF", pdftest.Make(
		"A different paper entirely, now about the history of open access journals and how\n"+
			"their number grew from a handful in the early nineties to many thousands later,\n"+
			"with enough words on the page to pass for ordinary prose."))
	if res, err := l.ExtractText(ctx, ExtractOptions{}); err != nil || res.Done != 1 {
		t.Errorf("after the file changed = %+v, %v", res, err)
	}
	if found, _ := l.Search(ctx, "reproducibility", SearchOptions{}); len(found.Hits) != 0 {
		t.Errorf("the old text is still found: %+v", found.Hits)
	}
}

func TestSearchOptions(t *testing.T) {
	t.Parallel()
	l, _, works := openFetchTest(t)
	ctx := t.Context()
	page := "Open access articles were cited more often than others, in every field\n" +
		"that was studied, and the advantage grew over the years that followed.\n"
	putPDF(t, l, works["A"].FilID, "a.pdf", pdftest.Make(page, page, page, page,
		page+"\nReferences\n1. Harnad S, Brody T. Comparing the impact of open access articles. 2004."))
	putPDF(t, l, works["B"].FilID, "b.pdf", pdftest.Make(page+page))
	if _, err := l.ExtractText(ctx, ExtractOptions{}); err != nil {
		t.Fatal(err)
	}

	all, err := l.Search(ctx, "open access", SearchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	perPaper := map[string]int{}
	for _, h := range all.Hits {
		perPaper[h.Work.FilID]++
	}
	if perPaper[works["A"].FilID] != DefaultPerPaper || perPaper[works["B"].FilID] != 1 {
		t.Errorf("passages per paper = %v; want %d from A and B's one", perPaper, DefaultPerPaper)
	}
	if one, _ := l.Search(ctx, "open access", SearchOptions{Paper: works["A"].FilID}); len(one.Hits) != 5 {
		t.Errorf("only A: %d hits, want its 5 pages", len(one.Hits))
	}
	if refs, _ := l.Search(ctx, "Harnad", SearchOptions{}); len(refs.Hits) != 0 {
		t.Errorf("a reference list searched by default: %+v", refs.Hits)
	}
	if refs, _ := l.Search(ctx, "Harnad", SearchOptions{References: true}); len(refs.Hits) != 1 || refs.Hits[0].Section != text.SectionReferences {
		t.Errorf("with references = %+v", refs.Hits)
	}
	if _, err := l.Search(ctx, " - ", SearchOptions{}); !errors.Is(err, errs.ErrEmptyQuery) {
		t.Errorf("an empty search: %v", err)
	}
}

func TestExtractNeedsAFolder(t *testing.T) {
	t.Parallel()
	l, _ := openTest(t, nil, Options{})
	if _, err := l.ExtractText(t.Context(), ExtractOptions{}); !errors.Is(err, ErrNoFolder) {
		t.Errorf("ExtractText on a database-only library: %v", err)
	}
}
