package ui

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/codevector-2003/filiation/internal/config"
	"github.com/codevector-2003/filiation/internal/library"
)

// zoteroScreen is the interface over an empty library whose Zotero is base.
func zoteroScreen(t *testing.T, base string) http.Handler {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{DBPath: filepath.Join(dir, "fil.db"), Folder: dir, Name: "Test",
		MaxNodes: config.DefaultMaxNodes, MaxDepth: config.DefaultMaxDepth, MaxRefsPerWork: config.DefaultMaxRefsPerWork}
	lib, err := library.Open(t.Context(), cfg, library.Options{NoCache: true, ZoteroBase: base})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lib.Close() })
	return New(Options{Library: lib, Name: "Test"})
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
