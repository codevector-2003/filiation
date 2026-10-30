package library

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codevector-2003/filiation/internal/blobs"
	"github.com/codevector-2003/filiation/internal/config"
	"github.com/codevector-2003/filiation/internal/model"
	"github.com/codevector-2003/filiation/internal/store"
	"github.com/codevector-2003/filiation/internal/text"
	"github.com/codevector-2003/filiation/internal/text/pdftest"
)

// openImportTest is an empty library folder answering OpenAlex from routes.
func openImportTest(t *testing.T, routes map[string]string) (*Library, *replay) {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{MaxNodes: 500, MaxDepth: 3, MaxRefsPerWork: 100, FolderCollections: true}
	if err := cfg.UseFolder(filepath.Join(dir, "Lib")); err != nil {
		t.Fatal(err)
	}
	r := &replay{t: t, routes: routes, details: true}
	l, err := Open(t.Context(), cfg, Options{CacheDir: filepath.Join(dir, "cache"), Transport: r})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	return l, r
}

const seedTitle = "The state of OA: a large-scale analysis of the prevalence and impact of Open Access articles"

// seedFront is the M0 seed's first page as a publisher sets it: the DOI in
// the header, the title, the authors, and another paper's DOI in the text.
func seedFront(title string) string {
	return "PeerJ\nDOI 10.7717/peerj.4375\n" + title + "\nHeather Piwowar, Jason Priem, Vincent Lariviere\n" +
		"Despite growing interest in open access, there is an unmet need for large-scale studies\n" +
		"of its prevalence. Earlier work (doi:10.1000/earlier.1) estimated it from one sample only.\n" +
		"We estimate that at least 28 percent of the scholarly literature is open access today."
}

func putInInbox(t *testing.T, l *Library, name string, pdf []byte) string {
	t.Helper()
	p := filepath.Join(l.Folder(), blobs.InboxDir, name)
	if err := os.WriteFile(p, pdf, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// M3 step 8, done when: a PDF dropped into Inbox/ is identified, verified
// and filed — and, with D21, this is how the seed's PDF reaches papers/.
func TestImportFromInbox(t *testing.T) {
	t.Parallel()
	l, _ := openImportTest(t, maps.Clone(peerj))
	ctx := t.Context()
	dropped := putInInbox(t, l, "download (3).pdf", pdftest.Make(seedFront(seedTitle)))

	res, err := l.Import(ctx, ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) != 1 || res.Files[0].Outcome != store.ImportImported {
		t.Fatalf("Import = %+v", res)
	}
	f := res.Files[0]
	if f.Work.OpenAlexID != "W2741809807" || !f.Work.IsSeed || f.Work.Source != model.SourceUpload {
		t.Errorf("work = %+v; want the seed, as an upload", f.Work)
	}
	if !strings.Contains(f.Clue, "header") {
		t.Errorf("clue = %q; want the header DOI, not the cited one", f.Clue)
	}
	if _, err := os.Stat(dropped); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the file is still in Inbox/: %v", err)
	}
	if !strings.Contains(f.Saved, filepath.Join("papers", "Piwowar-2018-")) || filepath.Base(f.Saved) != "download (3).pdf" {
		t.Errorf("saved at %q; want the paper's folder, under its own name", f.Saved)
	}
	if f.Text == nil || f.Text.Status != "ok" {
		t.Errorf("text = %+v; want it read", f.Text)
	}
	if found, _ := l.Search(ctx, "28 percent", SearchOptions{}); len(found.Hits) != 1 {
		t.Errorf("search after import = %+v", found.Hits)
	}
	if _, err := os.Stat(filepath.Join(l.Folder(), blobs.InboxDir, blobs.ReportName)); !errors.Is(err, os.ErrNotExist) {
		t.Error("a report was left for an empty Inbox/")
	}

	// The same bytes again, from outside: reported, not stored twice.
	again := filepath.Join(t.TempDir(), "copy.pdf")
	os.WriteFile(again, pdftest.Make(seedFront(seedTitle)), 0o644)
	dup, err := l.Import(ctx, ImportOptions{Files: []string{again}})
	if err != nil || dup.Files[0].Outcome != store.ImportDuplicate {
		t.Errorf("the same bytes again = %+v, %v", dup, err)
	}
}

// ADR-014's own test: a wrong DOI on page 1 is caught by verification.
func TestImportCatchesAWrongDOI(t *testing.T) {
	t.Parallel()
	routes := maps.Clone(peerj)
	routes["/works/doi:10.1000/earlier.1"] = "404" // the cited DOI is tried next, and is unknown
	l, _ := openImportTest(t, routes)
	ctx := t.Context()
	other := pdftest.Make(seedFront("A field guide to the solitary bees of northern Europe"))
	dropped := putInInbox(t, l, "bees.pdf", other)

	res, err := l.Import(ctx, ImportOptions{TitleSearches: -1})
	if err != nil {
		t.Fatal(err)
	}
	f := res.Files[0]
	if f.Outcome != store.ImportUnconfirmed || len(f.Candidates) != 1 || f.Candidates[0].Verified {
		t.Fatalf("a wrong DOI = %+v; want unconfirmed, the seed as an unverified candidate", f)
	}
	if _, err := os.Stat(dropped); err != nil {
		t.Errorf("an unconfirmed file left Inbox/: %v", err)
	}
	report, err := os.ReadFile(filepath.Join(l.Folder(), blobs.InboxDir, blobs.ReportName))
	if err != nil || !strings.Contains(string(report), "bees.pdf") || !strings.Contains(string(report), "title is not in the PDF") {
		t.Errorf("report = %q, %v", report, err)
	}

	// Named by the researcher: still checked, and still unconfirmed —
	// until they say yes.
	as, err := l.ImportAs(ctx, dropped, "10.7717/peerj.4375", false)
	if err != nil || as.Outcome != store.ImportUnconfirmed {
		t.Errorf("--as with the wrong title = %+v, %v", as, err)
	}
	forced, err := l.ImportAs(ctx, dropped, "10.7717/peerj.4375", true)
	if err != nil || forced.Outcome != store.ImportImported {
		t.Fatalf("--as, confirmed = %+v, %v", forced, err)
	}
	if _, err := os.Stat(filepath.Join(l.Folder(), blobs.InboxDir, blobs.ReportName)); !errors.Is(err, os.ErrNotExist) {
		t.Error("the report outlived the last file in Inbox/")
	}
}

func TestImportAScanByName(t *testing.T) {
	t.Parallel()
	l, _ := openImportTest(t, maps.Clone(peerj))
	ctx := t.Context()
	scan := putInInbox(t, l, "scan.pdf", pdftest.Make("", ""))

	res, err := l.Import(ctx, ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if f := res.Files[0]; f.Outcome != store.ImportUnidentified || f.Reason == "" {
		t.Fatalf("a scan = %+v; want unidentified, with a reason", f)
	}
	// Unchanged, it is not looked at again unless asked.
	if res, _ := l.Import(ctx, ImportOptions{Files: []string{scan}}); !res.Files[0].Skipped {
		t.Errorf("second try = %+v; want skipped", res.Files[0])
	}
	// Named: there is no text to check it against, so it is filed as named.
	f, err := l.ImportAs(ctx, scan, "doi:10.7717/peerj.4375", false)
	if err != nil || f.Outcome != store.ImportImported {
		t.Errorf("--as for a scan = %+v, %v", f, err)
	}
}

func TestImportLeavesPapersAndCopiesOutside(t *testing.T) {
	t.Parallel()
	l, _ := openImportTest(t, maps.Clone(peerj))
	ctx := t.Context()

	// Filed by the researcher in a folder of their own: identified, left
	// where it is.
	mine := filepath.Join(l.Folder(), blobs.PapersDir, "My reading", "oa.pdf")
	os.MkdirAll(filepath.Dir(mine), 0o755)
	os.WriteFile(mine, pdftest.Make(seedFront(seedTitle)), 0o644)
	res, err := l.Import(ctx, ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) != 1 || res.Files[0].Outcome != store.ImportImported || res.Files[0].Saved != mine {
		t.Fatalf("a PDF in papers/ = %+v; want imported where it is", res.Files)
	}

	// From outside the library: copied in, the original untouched. Another
	// copy of the same paper — annotated, say — is kept beside the first.
	outside := filepath.Join(t.TempDir(), "annotated.pdf")
	os.WriteFile(outside, pdftest.Make(seedFront(seedTitle)+"\nMy notes: check the 28 percent figure."), 0o644)
	got, err := l.Import(ctx, ImportOptions{Files: []string{outside}})
	if err != nil || got.Files[0].Outcome != store.ImportImported {
		t.Fatalf("from outside = %+v, %v", got, err)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Errorf("the original was moved or deleted: %v", err)
	}
	if !strings.HasPrefix(got.Files[0].Saved, filepath.Join(l.Folder(), blobs.PapersDir)) {
		t.Errorf("copied to %q", got.Files[0].Saved)
	}
	files, _ := l.db.FilesOf(ctx, got.Files[0].Work.FilID)
	if len(files) != 2 {
		t.Errorf("the seed has %d files, want both copies", len(files))
	}
}

func TestImportByTitleSearch(t *testing.T) {
	t.Parallel()
	routes := maps.Clone(peerj)
	routes["/works?filter=title.search:Attention is all you need"] = "search_attention.json"
	l, _ := openImportTest(t, routes)
	ctx := t.Context()
	pdf := pdftest.Doc{Title: "Attention is all you need",
		Pages: []string{"Ashish Vaswani, Noam Shazeer, Niki Parmar\nThe dominant sequence transduction models are based on\n" +
			"complex recurrent or convolutional neural networks that include an encoder and a decoder."}}.Bytes()
	file := putInInbox(t, l, "transformer.pdf", pdf)

	res, err := l.Import(ctx, ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	f := res.Files[0]
	if f.Outcome != store.ImportCandidates || len(f.Candidates) == 0 || !f.Candidates[0].Verified ||
		f.Candidates[0].Work.OpenAlexID != "W2626778328" || res.Searches != 1 {
		t.Fatalf("title only = %+v (searches %d); want candidates, the right one first and verified", f, res.Searches)
	}
	for _, c := range f.Candidates[1:] {
		if c.Verified {
			t.Errorf("%s verified, but its title is not in the PDF", c.Work.DisplayTitle())
		}
	}
	chosen, err := l.ImportChosen(ctx, file, f.Candidates[0])
	if err != nil || chosen.Outcome != store.ImportImported || chosen.Work.OpenAlexID != "W2626778328" {
		t.Errorf("chosen = %+v, %v", chosen, err)
	}
}

// Found in the M3 validation run: OpenAlex refused one PDF's title search
// (a footnote mark read as a wildcard), and the whole import stopped. A
// refused search is that file's problem; the rest are still filed.
func TestImportGoesOnWhenASearchIsRefused(t *testing.T) {
	t.Parallel()
	routes := maps.Clone(peerj)
	pdf := pdftest.Doc{Title: "The Structure of Ill Structured Problems",
		Pages: []string{"Herbert A. Simon\nThe boundary between well structured and ill structured problems\n" +
			"is vague, fluid and not susceptible to formalization in any simple way."}}.Bytes()
	front, err := text.Shared().Front(t.Context(), pdf)
	if err != nil {
		t.Fatal(err)
	}
	routes["/works?filter=title.search:"+titleQuery(front)] = "400"
	l, _ := openImportTest(t, routes)
	ctx := t.Context()
	putInInbox(t, l, "a-simon.pdf", pdf)
	putInInbox(t, l, "b-seed.pdf", pdftest.Make(seedFront(seedTitle)))

	res, err := l.Import(ctx, ImportOptions{})
	if err != nil {
		t.Fatalf("Import stopped: %v", err)
	}
	if len(res.Files) != 2 {
		t.Fatalf("files = %+v; want both looked at", res.Files)
	}
	for _, f := range res.Files {
		switch filepath.Base(f.File) {
		case "a-simon.pdf":
			if f.Outcome != store.ImportUnidentified || !strings.Contains(f.Reason, "refused") {
				t.Errorf("refused search = %+v; want unidentified, saying why", f)
			}
		case "b-seed.pdf":
			if f.Outcome != store.ImportImported {
				t.Errorf("the seed = %+v; want it filed despite the other file", f)
			}
		}
	}
}

// A refusal of access is not about one file's title: it stops the import
// rather than leaving every title-only PDF marked unidentified (PR #13 review).
func TestImportStopsWhenAccessIsRefused(t *testing.T) {
	t.Parallel()
	routes := maps.Clone(peerj)
	pdf := pdftest.Doc{Title: "The Structure of Ill Structured Problems",
		Pages: []string{"Herbert A. Simon\nThe boundary between well structured and ill structured problems\n" +
			"is vague, fluid and not susceptible to formalization in any simple way."}}.Bytes()
	front, err := text.Shared().Front(t.Context(), pdf)
	if err != nil {
		t.Fatal(err)
	}
	routes["/works?filter=title.search:"+titleQuery(front)] = "403"
	l, _ := openImportTest(t, routes)
	putInInbox(t, l, "a-simon.pdf", pdf)

	if _, err := l.Import(t.Context(), ImportOptions{}); err == nil {
		t.Error("Import succeeded; want the 403 to stop it")
	}
}

// Found in the M3 validation run: a work the PDF's identifier named, whose
// title is not in the PDF, was offered twice when the title search found it
// too.
func TestImportOffersACandidateOnce(t *testing.T) {
	t.Parallel()
	routes := maps.Clone(peerj)
	routes["/works/doi:10.1000/earlier.1"] = "404"
	seedJSON, err := os.ReadFile(filepath.Join(fixtureDir, "work_W2741809807.json"))
	if err != nil {
		t.Fatal(err)
	}
	pdf := pdftest.Doc{Title: "A field guide to the solitary bees of northern Europe",
		Pages: []string{"PeerJ\nDOI 10.7717/peerj.4375\nHeather Piwowar, Jason Priem\n" +
			"Solitary bees nest alone, in the ground or in hollow stems, and most fly for a few weeks a year."}}.Bytes()
	front, err := text.Shared().Front(t.Context(), pdf)
	if err != nil {
		t.Fatal(err)
	}
	routes["/works?filter=title.search:"+titleQuery(front)] = `{"results":[` + string(seedJSON) + `]}`
	l, _ := openImportTest(t, routes)
	putInInbox(t, l, "bees.pdf", pdf)

	res, err := l.Import(t.Context(), ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	f := res.Files[0]
	if f.Outcome != store.ImportCandidates || len(f.Candidates) != 1 || f.Candidates[0].Work.OpenAlexID != "W2741809807" {
		t.Errorf("candidates = %+v (%s); want the seed, once", f.Candidates, f.Outcome)
	}
}

func TestImportLocalDocument(t *testing.T) {
	t.Parallel()
	l, _ := openImportTest(t, maps.Clone(peerj))
	ctx := t.Context()
	file := putInInbox(t, l, "notes.pdf", pdftest.Make(
		"Field notes on solitary bees, kept over three summers in the meadow behind the lab.\n"+
			"Nest counts rose each year after the hedgerow was replanted along the eastern edge."))
	f, err := l.ImportLocal(ctx, file, "Field notes on solitary bees", model.Ptr(2025))
	if err != nil || f.Outcome != store.ImportImported {
		t.Fatalf("ImportLocal = %+v, %v", f, err)
	}
	if f.Work.OpenAlexID != "" || f.Work.FilID == "" || !f.Work.IsSeed {
		t.Errorf("local document = %+v; want a fil ID and no OpenAlex ID", f.Work)
	}
	if found, _ := l.Search(ctx, "hedgerow", SearchOptions{}); len(found.Hits) != 1 {
		t.Errorf("a local document is not searchable: %+v", found.Hits)
	}
	if n, err := l.Neighbours(ctx, f.Work.FilID); err != nil || len(n.Cites) != 0 {
		t.Errorf("Neighbours of a local document = %+v, %v", n, err)
	}
	if _, err := l.Stats(ctx); err != nil {
		t.Errorf("Stats with a local document: %v", err)
	}
}

// Dropping a PDF in Inbox/ is enough: extract (and fetch) file what
// identifies itself, free, before reading.
func TestExtractPicksUpTheInbox(t *testing.T) {
	t.Parallel()
	l, _ := openImportTest(t, maps.Clone(peerj))
	ctx := t.Context()
	putInInbox(t, l, "oa.pdf", pdftest.Make(seedFront(seedTitle)))
	putInInbox(t, l, "scan.pdf", pdftest.Make(""))

	res, err := l.ExtractText(ctx, ExtractOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Imported.Counts[store.ImportImported] != 1 || res.Imported.Searches != 0 {
		t.Errorf("Imported = %+v; want the seed filed, no title search", res.Imported)
	}
	if found, _ := l.Search(ctx, "28 percent", SearchOptions{}); len(found.Hits) != 1 {
		t.Errorf("not searchable after extract: %+v", found.Hits)
	}
	inbox, _ := l.files.Inbox()
	if len(inbox) != 1 || !strings.HasSuffix(inbox[0].Path, "scan.pdf") {
		t.Errorf("Inbox/ = %+v; want the scan alone", inbox)
	}
}

// A PDF the researcher deleted from the library and then imports again is
// that paper's PDF again: its record is restored, not reported as a
// duplicate of a file that is gone (found in review, PR #10).
func TestImportRestoresADeletedCopy(t *testing.T) {
	t.Parallel()
	l, _ := openImportTest(t, maps.Clone(peerj))
	ctx := t.Context()
	pdf := pdftest.Make(seedFront(seedTitle))
	putInInbox(t, l, "oa.pdf", pdf)
	first, err := l.Import(ctx, ImportOptions{})
	if err != nil || first.Files[0].Outcome != store.ImportImported {
		t.Fatalf("first import = %+v, %v", first, err)
	}
	if err := os.Remove(first.Files[0].Saved); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Scan(ctx); err != nil {
		t.Fatal(err)
	}

	putInInbox(t, l, "oa again.pdf", pdf)
	again, err := l.Import(ctx, ImportOptions{})
	if err != nil || again.Files[0].Outcome != store.ImportImported {
		t.Fatalf("the deleted copy imported again = %+v, %v; want imported", again.Files, err)
	}
	files, _ := l.db.FilesOf(ctx, first.Files[0].Work.FilID)
	if len(files) != 1 || files[0].Missing || !strings.HasSuffix(files[0].Path, "oa again.pdf") {
		t.Errorf("files = %+v; want one, present, at the new path", files)
	}
}

// Bytes the library already holds are reported before anything is made for
// them: --local and --as must not leave a paper with no PDF (found in
// review, PR #10).
func TestImportDuplicateMakesNoPaper(t *testing.T) {
	t.Parallel()
	l, _ := openImportTest(t, maps.Clone(peerj))
	ctx := t.Context()
	pdf := pdftest.Make(seedFront(seedTitle))
	putInInbox(t, l, "oa.pdf", pdf)
	if _, err := l.Import(ctx, ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	before, _ := l.Stats(ctx)

	copyOf := filepath.Join(t.TempDir(), "copy.pdf")
	os.WriteFile(copyOf, pdf, 0o644)
	local, err := l.ImportLocal(ctx, copyOf, "A title for the same bytes", nil)
	if err != nil || local.Outcome != store.ImportDuplicate {
		t.Errorf("--local on a duplicate = %+v, %v", local, err)
	}
	as, err := l.ImportAs(ctx, copyOf, "10.7717/peerj.4375", true)
	if err != nil || as.Outcome != store.ImportDuplicate {
		t.Errorf("--as on a duplicate = %+v, %v", as, err)
	}
	after, _ := l.Stats(ctx)
	if after.Works != before.Works {
		t.Errorf("works %d → %d: a duplicate made a paper", before.Works, after.Works)
	}
}
