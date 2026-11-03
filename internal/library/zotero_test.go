package library

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/codevector-2003/filiation/internal/config"
	"github.com/codevector-2003/filiation/internal/text/pdftest"
)

// zoteroTest is an empty library folder, OpenAlex answering from routes, and
// a stand-in Zotero holding three items: the M0 seed with its PDF, a DOI
// OpenAlex does not know, and a lecture with no identifier.
func zoteroTest(t *testing.T) (*Library, string) {
	t.Helper()
	dir := t.TempDir()
	pdf := filepath.Join(dir, "Zotero", "storage", "PDF00001", "Piwowar 2018.pdf")
	if err := os.MkdirAll(filepath.Dir(pdf), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pdf, pdftest.Make(seedFront(seedTitle)), 0o644); err != nil {
		t.Fatal(err)
	}
	items := []map[string]any{
		{"key": "ART00001", "data": map[string]any{"key": "ART00001", "version": 3, "itemType": "journalArticle",
			"title": seedTitle, "date": "2018", "DOI": "10.7717/peerj.4375", "collections": []string{"CHAPTER2"},
			"tags": []map[string]any{{"tag": "key paper"}, {"tag": "Open Access", "type": 1}}}},
		{"key": "PDF00001", "data": map[string]any{"key": "PDF00001", "itemType": "attachment", "parentItem": "ART00001",
			"linkMode": "imported_file", "contentType": "application/pdf"}},
		{"key": "PDF00002", "data": map[string]any{"key": "PDF00002", "itemType": "attachment", "parentItem": "ART00001",
			"linkMode": "imported_file", "contentType": "application/pdf"}},
		{"key": "ODD00001", "data": map[string]any{"key": "ODD00001", "itemType": "journalArticle",
			"title": "A paper OpenAlex lost", "DOI": "10.1000/missing", "collections": []string{"TEACHING"}}},
		{"key": "LEC00001", "data": map[string]any{"key": "LEC00001", "itemType": "document",
			"title": "Lecture notes, week 4", "date": "2019", "collections": []string{"THESIS01"},
			"creators": []map[string]any{{"firstName": "Ada", "lastName": "Lovelace"}}}},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/users/0/collections", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[
 {"data":{"key":"CHAPTER2","name":"Chapter 2","parentCollection":"THESIS01"}},
 {"data":{"key":"THESIS01","name":"Thesis","parentCollection":false}},
 {"data":{"key":"TEACHING","name":"Teaching","parentCollection":false}}]`))
	})
	mux.HandleFunc("/api/users/0/items", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(items)
	})
	mux.HandleFunc("/api/users/0/items/PDF00001/file/view/url", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte((&url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(pdf)}).String()))
	})
	mux.HandleFunc("/api/users/0/items/PDF00002/file/view/url", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte((&url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(filepath.Join(dir, "gone.pdf"))}).String()))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cfg := &config.Config{MaxNodes: 500, MaxDepth: 3, MaxRefsPerWork: 100}
	if err := cfg.UseFolder(filepath.Join(dir, "Lib")); err != nil {
		t.Fatal(err)
	}
	routes := maps.Clone(peerj)
	routes["/works/doi:10.1000/missing"] = "404"
	l, err := Open(t.Context(), cfg, Options{CacheDir: filepath.Join(dir, "cache"),
		Transport: &replay{t: t, routes: routes, details: true}, ZoteroBase: srv.URL + "/api"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	return l, pdf
}

func TestZoteroPreviewAndImport(t *testing.T) {
	t.Parallel()
	l, pdf := zoteroTest(t)
	ctx := t.Context()
	before, _ := os.ReadFile(pdf)

	p, err := l.ZoteroPreview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if p.Items != 3 || p.WithDOI != 2 || p.NoID != 1 || p.PDFs != 1 || p.Tags != 1 || p.Already != 0 ||
		len(p.Collections) != 3 || p.Collections[0].Name != "Teaching" && p.Collections[0].Name != "Thesis" {
		t.Errorf("preview = %+v", p)
	}
	if n, _ := l.Summarise(ctx); n.Works != 0 {
		t.Errorf("the preview wrote %d works", n.Works)
	}

	r, err := l.ZoteroImport(ctx, ZoteroOptions{Refs: true, PDFs: true, Tags: true})
	if err != nil {
		t.Fatal(err)
	}
	if r.Items != 3 || r.Matched != 1 || r.Unmatched != 2 || r.PDFs != 1 || r.PDFsMissing != 1 || r.Collections != 3 || len(r.NotMatched) != 2 {
		t.Errorf("report = %+v", r)
	}
	cs, _ := l.Collections(ctx)
	names := map[string]Collection{}
	for _, c := range cs {
		names[c.Name] = c
	}
	if names["Chapter 2"].Parent != names["Thesis"].ID || names["Thesis"].Works != 2 || names["Thesis"].Zotero != "THESIS01" {
		t.Errorf("collections = %+v", cs)
	}
	ls, _ := l.Labels(ctx)
	if len(ls) != 1 || ls[0].Name != "key paper" || ls[0].Works != 1 {
		t.Errorf("labels = %+v; the user's tag, not the automatic one", ls)
	}
	rows, _ := l.List(ctx, ListOptions{})
	zot := map[string]int{}
	for _, row := range rows {
		zot[row.Zotero]++
		if row.Zotero == "matched" && (!row.HasPDF || row.CitedBy != 0 || !row.Seed) {
			t.Errorf("the seed = %+v; a seed with its PDF", row)
		}
	}
	if zot["matched"] != 1 || zot["unmatched"] != 2 || zot[""] != 54 {
		t.Errorf("rows by Zotero = %v; the seed's 54 references come in as stubs", zot)
	}
	if after, _ := os.ReadFile(pdf); string(after) != string(before) {
		t.Error("Zotero's own PDF changed")
	}

	// Again: nothing new, and no paper twice.
	again, err := l.ZoteroImport(ctx, ZoteroOptions{Refs: true, PDFs: true, Tags: true})
	if err != nil {
		t.Fatal(err)
	}
	if again.Already != 3 || again.Matched+again.Unmatched != 0 || again.PDFs != 0 {
		t.Errorf("second import = %+v", again)
	}
	if n, _ := l.Summarise(ctx); n.Works != 57 {
		t.Errorf("works after importing twice = %d, want 57", n.Works)
	}
}

func TestZoteroImportLeavesSkippedCollectionsOut(t *testing.T) {
	t.Parallel()
	l, _ := zoteroTest(t)
	ctx := t.Context()
	r, err := l.ZoteroImport(ctx, ZoteroOptions{Skip: []string{"THESIS01", "TEACHING"}})
	if err != nil {
		t.Fatal(err)
	}
	// Leaving Thesis out leaves Chapter 2, inside it, out too.
	if r.Items != 0 || r.Collections != 0 {
		t.Errorf("report = %+v", r)
	}
	r, err = l.ZoteroImport(ctx, ZoteroOptions{Skip: []string{"TEACHING"}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Items != 2 || r.Matched != 1 || r.PDFs != 0 {
		t.Errorf("report = %+v; PDFs were not asked for", r)
	}
	rows, _ := l.List(ctx, ListOptions{Seeds: true})
	for _, row := range rows {
		if row.Zotero == "matched" && row.RefsKnown == false {
			t.Errorf("seed %+v: references are recorded unless asked not to", row)
		}
	}
}
