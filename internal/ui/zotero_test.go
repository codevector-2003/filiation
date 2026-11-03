package ui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/codevector-2003/filiation/internal/config"
	"github.com/codevector-2003/filiation/internal/library"
)

// zoteroScreen is the interface over an empty library whose Zotero is base.
func zoteroScreen(t *testing.T, base string) http.Handler {
	t.Helper()
	return zoteroScreenWith(t, base, nil)
}

// zoteroScreenWith is zoteroScreen whose file dialog answers with pick.
func zoteroScreenWith(t *testing.T, base string, pick func() (string, error)) http.Handler {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{DBPath: filepath.Join(dir, "fil.db"), Folder: dir, Name: "Test",
		MaxNodes: config.DefaultMaxNodes, MaxDepth: config.DefaultMaxDepth, MaxRefsPerWork: config.DefaultMaxRefsPerWork}
	lib, err := library.Open(t.Context(), cfg, library.Options{NoCache: true, ZoteroBase: base})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lib.Close() })
	return New(Options{Library: lib, Name: "Test", PickExport: pick})
}

func TestZoteroSaysWhyItCannotRead(t *testing.T) {
	t.Parallel()
	gone := httptest.NewServer(http.NotFoundHandler())
	gone.Close()
	refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusForbidden) }))
	defer refusing.Close()
	for base, want := range map[string]string{gone.URL + "/api": "notRunning", refusing.URL + "/api": "notAllowed"} {
		got := decode[zoteroJSON](t, ask(t, zoteroScreen(t, base), "GET", "/api/zotero"))
		if got.State != want {
			t.Errorf("Zotero at %s = %+v, want %s", base, got, want)
		}
	}
}

func TestZoteroImportRunsAsTheJob(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/users/0/collections", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"data":{"key":"THESIS01","name":"Thesis","parentCollection":false}}]`))
	})
	mux.HandleFunc("/api/users/0/items", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"data":{"key":"LEC00001","itemType":"document","title":"Lecture notes, week 4","collections":["THESIS01"]}}]`))
	})
	z := httptest.NewServer(mux)
	defer z.Close()
	h := zoteroScreen(t, z.URL+"/api")

	before := decode[zoteroJSON](t, ask(t, h, "GET", "/api/zotero"))
	if before.State != "ready" || before.Items != 1 || before.NoID != 1 || len(before.Collections) != 1 || before.Last != nil {
		t.Fatalf("preview = %+v", before)
	}
	if w := post(t, h, "/api/zotero/import", `{"refs":true,"pdfs":true}`); w.Code != http.StatusAccepted {
		t.Fatalf("import = %d %s", w.Code, w.Body)
	}
	if j := waitJob(t, h); j.Kind != "zotero" || j.Error != "" {
		t.Fatalf("job = %+v", j)
	}
	after := decode[zoteroJSON](t, ask(t, h, "GET", "/api/zotero"))
	if after.Already != 1 || after.Last == nil || after.Last.Unmatched != 1 || len(after.Last.NotMatched) != 1 {
		t.Errorf("after = %+v", after)
	}
	lib := decode[libraryJSON](t, ask(t, h, "GET", "/api/library"))
	if len(lib.Collections) != 1 || !lib.Collections[0].Zotero || lib.Works != 1 {
		t.Errorf("library = %+v", lib)
	}
}

func TestZoteroFromAnExportFile(t *testing.T) {
	t.Parallel()
	gone := httptest.NewServer(http.NotFoundHandler())
	gone.Close()
	export, err := filepath.Abs(filepath.Join("..", "sources", "zotero", "testdata", "Exported Items", "Exported Items.rdf"))
	if err != nil {
		t.Fatal(err)
	}
	bib := filepath.Join(t.TempDir(), "library.bib")
	os.WriteFile(bib, []byte("@article{x, title={A}}"), 0o644)
	pick := export
	h := zoteroScreenWith(t, gone.URL+"/api", func() (string, error) { return pick, nil })

	got := decode[zoteroJSON](t, post(t, h, "/api/zotero/export", ""))
	if got.State != "ready" || got.From != "Exported Items.rdf" || got.Items != 4 || got.PDFsMissing != 1 || len(got.Collections) != 2 {
		t.Errorf("from the export = %+v", got)
	}
	// It stays chosen until the person goes back to a running Zotero.
	if again := decode[zoteroJSON](t, ask(t, h, "GET", "/api/zotero")); again.From != "Exported Items.rdf" {
		t.Errorf("asked again = %+v", again)
	}
	if live := decode[zoteroJSON](t, post(t, h, "/api/zotero/live", "")); live.State != "notRunning" || live.From != "" {
		t.Errorf("back to Zotero = %+v", live)
	}
	pick = bib
	if wrong := decode[zoteroJSON](t, post(t, h, "/api/zotero/export", "")); wrong.State != "notExport" || wrong.From != "library.bib" {
		t.Errorf("a BibTeX file = %+v", wrong)
	}
	pick = ""
	if w := post(t, h, "/api/zotero/export", ""); w.Code != http.StatusNoContent {
		t.Errorf("the dialog closed = %d", w.Code)
	}
}

func TestLinkingAsksForAnIdentifier(t *testing.T) {
	t.Parallel()
	h := zoteroScreen(t, "")
	if w := post(t, h, "/api/papers/F00000000/link", `{"id":"10.7717/peerj.4375"}`); w.Code != http.StatusNotFound {
		t.Errorf("a paper not here = %d %s", w.Code, w.Body)
	}
	if w := post(t, h, "/api/papers/F00000000/candidates", ""); w.Code != http.StatusNotFound {
		t.Errorf("searching for a paper not here = %d %s", w.Code, w.Body)
	}
}
