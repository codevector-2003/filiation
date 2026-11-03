package library

import (
	"encoding/json"
	"errors"
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

	p, err := l.ZoteroPreview(ctx, "")
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

// exportOf writes a Zotero RDF export, as Zotero's translator does, of the
// stand-in library's seed (its DOI on its journal, its PDF under files/),
// the lecture, and one PDF the export names but left out.
func exportOf(t *testing.T, dir string) string {
	t.Helper()
	ex := filepath.Join(dir, "Exported Items")
	if err := os.MkdirAll(filepath.Join(ex, "files", "13"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ex, "files", "13", "Piwowar 2018.pdf"), pdftest.Make(seedFront(seedTitle)), 0o644); err != nil {
		t.Fatal(err)
	}
	rdf := `<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns:z="http://www.zotero.org/namespaces/export#"
 xmlns:dcterms="http://purl.org/dc/terms/" xmlns:bib="http://purl.org/net/biblio#" xmlns:foaf="http://xmlns.com/foaf/0.1/"
 xmlns:link="http://purl.org/rss/1.0/modules/link/" xmlns:dc="http://purl.org/dc/elements/1.1/">
 <bib:Article rdf:about="#item_12">
  <z:itemType>journalArticle</z:itemType>
  <dcterms:isPartOf><bib:Journal><dc:title>PeerJ</dc:title><dc:identifier>DOI 10.7717/peerj.4375</dc:identifier></bib:Journal></dcterms:isPartOf>
  <link:link rdf:resource="#item_13"/>
  <dc:title>` + seedTitle + `</dc:title>
  <dc:date>2018</dc:date>
 </bib:Article>
 <z:Attachment rdf:about="#item_13"><z:itemType>attachment</z:itemType>
  <rdf:resource rdf:resource="files/13/Piwowar%202018.pdf"/><link:type>application/pdf</link:type></z:Attachment>
 <bib:Document rdf:about="#item_20">
  <z:itemType>document</z:itemType>
  <bib:authors><rdf:Seq><rdf:li><foaf:Person><foaf:surname>Lovelace</foaf:surname><foaf:givenName>Ada</foaf:givenName></foaf:Person></rdf:li></rdf:Seq></bib:authors>
  <link:link rdf:resource="#item_21"/>
  <dc:title>Lecture notes, week 4</dc:title>
  <dc:date>2019</dc:date>
 </bib:Document>
 <z:Attachment rdf:about="#item_21"><z:itemType>attachment</z:itemType>
  <z:path rdf:resource="files/21/week4.pdf"/><link:type>application/pdf</link:type></z:Attachment>
 <z:Collection rdf:about="#collection_1"><dc:title>Thesis</dc:title>
  <dcterms:hasPart rdf:resource="#collection_2"/><dcterms:hasPart rdf:resource="#item_20"/></z:Collection>
 <z:Collection rdf:about="#collection_2"><dc:title>Chapter 2</dc:title>
  <dcterms:hasPart rdf:resource="#item_12"/></z:Collection>
</rdf:RDF>`
	file := filepath.Join(ex, "Exported Items.rdf")
	if err := os.WriteFile(file, []byte(rdf), 0o644); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestZoteroFromAnExport(t *testing.T) {
	t.Parallel()
	l, _ := zoteroTest(t)
	ctx := t.Context()
	file := exportOf(t, t.TempDir())

	p, err := l.ZoteroPreview(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	if p.Items != 2 || p.WithDOI != 1 || p.NoID != 1 || p.PDFs != 2 || p.PDFsMissing != 1 || len(p.Collections) != 2 {
		t.Errorf("preview = %+v", p)
	}
	r, err := l.ZoteroImport(ctx, ZoteroOptions{Refs: true, PDFs: true, From: file})
	if err != nil {
		t.Fatal(err)
	}
	if r.Matched != 1 || r.Unmatched != 1 || r.PDFs != 1 || r.PDFsMissing != 1 || r.Collections != 2 {
		t.Errorf("report = %+v", r)
	}

	// The same library read from the running Zotero: the seed is the same
	// paper, the lecture the same document, the collections the same ones.
	again, err := l.ZoteroImport(ctx, ZoteroOptions{Refs: true})
	if err != nil {
		t.Fatal(err)
	}
	if again.Matched != 1 || again.Unmatched != 1 || again.Already != 1 {
		t.Errorf("from Zotero after the export = %+v; the lecture is in already", again)
	}
	if n, _ := l.Summarise(ctx); n.Works != 57 {
		t.Errorf("works = %d, want 57: the seed, its 54 references, the lecture and the lost paper", n.Works)
	}
	cs, _ := l.Collections(ctx)
	if len(cs) != 3 {
		t.Errorf("collections = %+v; Thesis and Chapter 2 once each, and Teaching", cs)
	}
	// And the export once more adds nothing.
	third, err := l.ZoteroImport(ctx, ZoteroOptions{Refs: true, From: file})
	if err != nil || third.Already != 2 || third.Matched+third.Unmatched != 0 {
		t.Errorf("the export again = %+v, %v", third, err)
	}

	if _, err := l.ZoteroPreview(ctx, filepath.Join(filepath.Dir(file), "files", "13", "Piwowar 2018.pdf")); !errors.Is(err, ErrZoteroNotExport) {
		t.Errorf("a PDF as the export: %v", err)
	}
}
