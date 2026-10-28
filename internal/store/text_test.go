package store

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/codevector-2003/filiation/internal/errs"
)

// textLibrary has three works. W1 has a published PDF and a preprint, W2 a
// preprint, W3 a PDF the researcher deleted.
func textLibrary(t *testing.T) *DB {
	t.Helper()
	db := migrated(t)
	for _, q := range []string{
		`INSERT INTO work (fil_id, openalex_id, title, year, is_seed) VALUES
			('F00000001', 'W1', 'Open access and citations', 2016, 0),
			('F00000002', 'W2', 'The growth of open access', 2006, 1),
			('F00000003', 'W3', 'Deleted', 2010, 0);`,
		`INSERT INTO work_file (path, fil_id, sha256, size, version, origin) VALUES
			('papers/a--F00000001/preprint.pdf', 'F00000001', 'p1', 1, 'preprint', 'download'),
			('papers/a--F00000001/published.pdf', 'F00000001', 'a1', 1, 'published', 'download'),
			('papers/b--F00000002/Paper.PDF', 'F00000002', 'b2', 1, NULL, 'found');`,
		`INSERT INTO work_file (path, fil_id, sha256, size, origin, missing) VALUES
			('papers/c--F00000003/published.pdf', 'F00000003', 'c3', 1, 'download', 1);`,
	} {
		mustExec(t, db.write, q)
	}
	return db
}

func toReadIDs(t *testing.T, db *DB, extractor string, again bool) map[string]string {
	t.Helper()
	got, err := db.PDFsToRead(t.Context(), extractor, again)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, r := range got {
		out[r.FilID] = r.Path
	}
	return out
}

func putText(t *testing.T, db *DB, txt Text, chunks ...Chunk) {
	t.Helper()
	if err := db.Tx(t.Context(), func(tx *Tx) error { return tx.PutText(t.Context(), txt, chunks) }); err != nil {
		t.Fatal(err)
	}
}

func TestPDFsToRead(t *testing.T) {
	t.Parallel()
	db := textLibrary(t)
	ctx := t.Context()

	// Each paper with a PDF on disk, once, the published version first; the
	// deleted one not at all; a mixed-case .PDF counts.
	got := toReadIDs(t, db, "pdfium/1", false)
	want := map[string]string{
		"F00000001": "papers/a--F00000001/published.pdf",
		"F00000002": "papers/b--F00000002/Paper.PDF",
	}
	if len(got) != len(want) || got["F00000001"] != want["F00000001"] || got["F00000002"] != want["F00000002"] {
		t.Fatalf("PDFsToRead = %v, want %v", got, want)
	}
	first, _ := db.PDFsToRead(ctx, "pdfium/1", false)
	if first[0].FilID != "F00000002" {
		t.Errorf("the seed is not first: %+v", first)
	}

	// Read: nothing left to read.
	putText(t, db, Text{FilID: "F00000001", Path: want["F00000001"], SHA256: "a1", Status: "ok", Extractor: "pdfium/1"})
	putText(t, db, Text{FilID: "F00000002", Path: want["F00000002"], SHA256: "b2", Status: "gibberish", Reason: "x", Extractor: "pdfium/1"})
	if got := toReadIDs(t, db, "pdfium/1", false); len(got) != 0 {
		t.Errorf("after reading = %v", got)
	}
	// A newer extractor reads everything again, and so does again.
	if got := toReadIDs(t, db, "pdfium/2", false); len(got) != 2 {
		t.Errorf("newer extractor = %v", got)
	}
	if got := toReadIDs(t, db, "pdfium/1", true); len(got) != 2 {
		t.Errorf("again = %v", got)
	}
	// A changed file is read again.
	mustExec(t, db.write, `UPDATE work_file SET sha256 = 'a1-new' WHERE sha256 = 'a1';`)
	if got := toReadIDs(t, db, "pdfium/1", false); len(got) != 1 || got["F00000001"] == "" {
		t.Errorf("after the file changed = %v", got)
	}

	if r, ok, err := db.PDFToRead(ctx, "W1"); err != nil || !ok || r.SHA256 != "a1-new" {
		t.Errorf("PDFToRead(W1) = %+v, %v, %v", r, ok, err)
	}
	if _, ok, err := db.PDFToRead(ctx, "W3"); err != nil || ok {
		t.Errorf("PDFToRead(W3), whose PDF was deleted = %v, %v", ok, err)
	}

	counts, err := db.TextCounts(ctx)
	if err != nil || counts["ok"] != 1 || counts["gibberish"] != 1 {
		t.Errorf("TextCounts = %v, %v", counts, err)
	}
	txt, ok, err := db.TextOf(ctx, "W2")
	if err != nil || !ok || txt.Status != "gibberish" || txt.Reason != "x" || txt.ReadAt.IsZero() {
		t.Errorf("TextOf(W2) = %+v, %v, %v", txt, ok, err)
	}
	if _, ok, _ := db.TextOf(ctx, "W3"); ok {
		t.Error("W3 was never read")
	}
}

func TestSearch(t *testing.T) {
	t.Parallel()
	db := textLibrary(t)
	ctx := t.Context()
	putText(t, db, Text{FilID: "W1", Path: "papers/a--F00000001/published.pdf", SHA256: "a1", Status: "ok", Extractor: "e"},
		Chunk{Page: 1, Ordinal: 0, Text: "Articles that were openly accessible were cited more often."},
		Chunk{Page: 2, Ordinal: 1, Text: "The citation advantage of open access holds in physics."},
		Chunk{Page: 3, Ordinal: 2, Text: "Open access to articles is cited by many."},
		Chunk{Page: 9, Ordinal: 3, Section: "references", Text: "Harnad S. Open access citation advantage. 2004."},
	)
	putText(t, db, Text{FilID: "W2", Path: "papers/b--F00000002/Paper.PDF", SHA256: "b2", Status: "ok", Extractor: "e"},
		Chunk{Page: 4, Ordinal: 0, Text: "Growth of open access across ten disciplines, and the citation advantage."},
	)

	hits, err := db.Search(ctx, SearchQuery{Words: "citation advantage", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("got %d hits, want 2 (the reference list left out): %+v", len(hits), hits)
	}
	for _, h := range hits {
		if !strings.Contains(h.Snippet, MarkStart+"citation"+MarkEnd) || h.Path == "" || h.Section != "" {
			t.Errorf("hit = %+v", h)
		}
	}
	if hits[0].FilID == "F00000001" && hits[0].Page != 2 {
		t.Errorf("W1's hit is on page %d, want 2", hits[0].Page)
	}

	// Stemming: "cite" finds "cited" and "citation"? Porter stems cited and
	// cite alike.
	if hits, _ := db.Search(ctx, SearchQuery{Words: "cite", Limit: 10}); len(hits) < 2 {
		t.Errorf("cite found %d passages, want cited ones too", len(hits))
	}
	// References on request.
	if hits, _ := db.Search(ctx, SearchQuery{Words: "Harnad", Limit: 10}); len(hits) != 0 {
		t.Errorf("a reference list was searched by default: %+v", hits)
	}
	if hits, _ := db.Search(ctx, SearchQuery{Words: "Harnad", Limit: 10, References: true}); len(hits) != 1 || hits[0].Section != "references" {
		t.Errorf("with references = %+v", hits)
	}
	// One paper, and at most so many passages from each.
	if hits, _ := db.Search(ctx, SearchQuery{Words: "open access", Limit: 10, FilID: "W2"}); len(hits) != 1 || hits[0].FilID != "F00000002" {
		t.Errorf("only W2 = %+v", hits)
	}
	if hits, _ := db.Search(ctx, SearchQuery{Words: "open access", Limit: 10, PerPaper: 1}); len(hits) != 2 {
		t.Errorf("one per paper = %+v", hits)
	}
	if hits, _ := db.Search(ctx, SearchQuery{Words: "open access", Limit: 2}); len(hits) != 2 {
		t.Errorf("limit 2 = %d hits", len(hits))
	}
	// Any: a passage with some of the words, the one with more first.
	if hits, _ := db.Search(ctx, SearchQuery{Words: "physics zyxwv", Limit: 10}); len(hits) != 0 {
		t.Errorf("all words, one absent = %+v", hits)
	}
	if hits, _ := db.Search(ctx, SearchQuery{Words: "physics advantage zyxwv", Limit: 10, Any: true}); len(hits) < 2 ||
		hits[0].Page != 2 {
		t.Errorf("any word = %+v; want W1's page 2, which has two of them, first", hits)
	}
	// A phrase must be in order.
	if hits, _ := db.Search(ctx, SearchQuery{Words: `"access open"`, Limit: 10}); len(hits) != 0 {
		t.Errorf("a phrase matched out of order: %+v", hits)
	}
	// A prefix.
	if hits, _ := db.Search(ctx, SearchQuery{Words: "discipl*", Limit: 10}); len(hits) != 1 {
		t.Errorf("prefix = %+v", hits)
	}

	// Reading a paper again replaces its passages, leaving nothing stale in
	// the index.
	putText(t, db, Text{FilID: "W1", Path: "papers/a--F00000001/published.pdf", SHA256: "a1", Status: "ok", Extractor: "e2"},
		Chunk{Page: 1, Ordinal: 0, Text: "Entirely different words now."})
	if hits, _ := db.Search(ctx, SearchQuery{Words: "physics", Limit: 10}); len(hits) != 0 {
		t.Errorf("an old passage is still found: %+v", hits)
	}
	if hits, _ := db.Search(ctx, SearchQuery{Words: "entirely different", Limit: 10}); len(hits) != 1 {
		t.Errorf("the new passage is not found: %+v", hits)
	}
	var n int
	db.read.QueryRow(`SELECT count(*) FROM chunk WHERE work_id = 'F00000001';`).Scan(&n)
	if n != 1 {
		t.Errorf("W1 has %d chunks, want 1", n)
	}
}

func TestMatchQuery(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"open access", `"open" "access"`},
		{`"open access" citation`, `"open access" "citation"`},
		{"C++ - & :", `"C++"`},
		{"citat*", `"citat"*`},
		{`say "hi`, `"say" "hi"`},
		{`a"b`, `"a" "b"`},
		{"NOT OR AND NEAR(", `"NOT" "OR" "AND" "NEAR("`},
		{"col:umn", `"col:umn"`},
		{"a\x00b", `"a" "b"`},
		{`""""`, ""},
		{"   ", ""},
		{"- + *", ""},
	}
	for _, tt := range tests {
		got, err := MatchQuery(tt.in)
		if tt.want == "" {
			if !errors.Is(err, errs.ErrEmptyQuery) {
				t.Errorf("MatchQuery(%q) = %q, %v; want ErrEmptyQuery", tt.in, got, err)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("MatchQuery(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
		}
	}
}

// TestSearchHostileInput runs every query a person might type through FTS5:
// none may be a syntax error.
func TestSearchHostileInput(t *testing.T) {
	t.Parallel()
	db := textLibrary(t)
	putText(t, db, Text{FilID: "W1", Path: "p", SHA256: "a1", Status: "ok", Extractor: "e"},
		Chunk{Page: 1, Text: "C++ and Go: a comparison (2020) of NEAR-identical results."})
	for _, q := range []string{
		"C++", "NEAR(a b)", "a OR", "NOT", "(", ")", "*", "^start", "col:umn", `"unclosed`,
		"a AND", "-x", "'quoted'", "naïve café", "x*y", "{a b}", "a\x00b",
	} {
		if _, err := db.Search(t.Context(), SearchQuery{Words: q, Limit: 5}); err != nil && !errors.Is(err, errs.ErrEmptyQuery) {
			t.Errorf("Search(%q): %v", q, err)
		}
	}
}

func TestMergeKeepsOneText(t *testing.T) {
	t.Parallel()
	db := textLibrary(t)
	ctx := t.Context()
	putText(t, db, Text{FilID: "W1", Path: "p1", SHA256: "a1", Status: "ok", Extractor: "e"},
		Chunk{Page: 1, Text: "from the first record"})
	putText(t, db, Text{FilID: "W2", Path: "p2", SHA256: "b2", Status: "ok", Extractor: "e"},
		Chunk{Page: 1, Text: "from the second record"})
	if err := db.Tx(ctx, func(tx *Tx) error { return tx.MergeInto(ctx, "W1", "W2") }); err != nil {
		t.Fatal(err)
	}
	hits, _ := db.Search(ctx, SearchQuery{Words: "record", Limit: 10})
	if len(hits) != 1 || !strings.Contains(hits[0].Snippet, "second") {
		t.Errorf("after the merge = %+v; want the survivor's text alone", hits)
	}

	// A survivor with no text takes the merged record's.
	putText(t, db, Text{FilID: "W3", Path: "p3", SHA256: "c3", Status: "ok", Extractor: "e"},
		Chunk{Page: 1, Text: "the third record's words"})
	if err := db.Tx(ctx, func(tx *Tx) error { return tx.MergeInto(ctx, "W3", "W9") }); err != nil {
		t.Fatal(err)
	}
	if txt, ok, _ := db.TextOf(ctx, "W9"); !ok || txt.Path != "p3" {
		t.Errorf("W9's text = %+v, %v", txt, ok)
	}
	if hits, _ := db.Search(ctx, SearchQuery{Words: "third", Limit: 10}); len(hits) != 1 {
		t.Errorf("the moved text is not found: %+v", hits)
	}
}

func TestUpgradeV5Library(t *testing.T) {
	t.Parallel()
	db := openTest(t)
	ctx := t.Context()
	ddl, err := os.ReadFile("testdata/schema_v5.sql")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.write.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := applySchema(ctx, tx, string(ddl)); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db.write, `INSERT INTO work (fil_id, openalex_id) VALUES ('F7K2M9QXA', 'W1');`)
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if v, _ := db.Version(ctx); v != SchemaVersion {
		t.Errorf("version = %d", v)
	}
	putText(t, db, Text{FilID: "W1", Path: "p", SHA256: "s", Status: "ok", Extractor: "e"},
		Chunk{Page: 2, Start: 10, End: 30, Text: "upgraded and searchable"})
	hits, err := db.Search(ctx, SearchQuery{Words: "upgraded", Limit: 1})
	if err != nil || len(hits) != 1 || hits[0].Page != 2 {
		t.Errorf("search after upgrade = %+v, %v", hits, err)
	}
	var start, end int
	db.read.QueryRow(`SELECT char_start, char_end FROM chunk;`).Scan(&start, &end)
	if start != 10 || end != 30 {
		t.Errorf("offsets = %d, %d", start, end)
	}
}
