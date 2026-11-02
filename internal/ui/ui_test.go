package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codevector-2003/filiation/internal/config"
	"github.com/codevector-2003/filiation/internal/library"
	"github.com/codevector-2003/filiation/internal/model"
	"github.com/codevector-2003/filiation/internal/text/pdftest"
)

// openTest makes a library holding one local document, and returns its
// handler, the paper's fil ID and the paths it was asked to open.
func openTest(t *testing.T) (http.Handler, string, *[]string) {
	t.Helper()
	ctx := t.Context()
	dir := t.TempDir()
	cfg := &config.Config{DBPath: filepath.Join(dir, "fil.db"), Folder: dir, Name: "Test",
		MaxNodes: config.DefaultMaxNodes, MaxDepth: config.DefaultMaxDepth, MaxRefsPerWork: config.DefaultMaxRefsPerWork}
	lib, err := library.Open(ctx, cfg, library.Options{NoCache: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lib.Close() })
	file := filepath.Join(dir, "Inbox", "notes.pdf")
	pdf := pdftest.Make("Field notes on solitary bees, kept over three summers in the meadow behind the lab.\n" +
		"Nest counts rose each year after the hedgerow was replanted along the eastern edge.")
	if err := os.WriteFile(file, pdf, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := lib.ImportLocal(ctx, file, "Field notes on solitary bees", model.Ptr(2025))
	if err != nil {
		t.Fatal(err)
	}
	var opened []string
	h := New(Options{Library: lib, Name: "Test", OpenPath: func(p string) error {
		opened = append(opened, p)
		return nil
	}})
	return h, f.Work.FilID, &opened
}

func ask(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	return w
}

func decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("%s: %v", w.Body, err)
	}
	return v
}

func TestScreensAreEmbedded(t *testing.T) {
	t.Parallel()
	h := New(Options{})
	for _, path := range []string{"/", "/app.js", "/app.css", "/fonts/inter-latin-400-normal.woff2"} {
		if w := ask(t, h, "GET", path); w.Code != http.StatusOK {
			t.Errorf("GET %s = %d", path, w.Code)
		}
	}
}

func TestNoLibrarySaysSo(t *testing.T) {
	t.Parallel()
	h := New(Options{Problem: "the settings file is broken"})
	lib := decode[libraryJSON](t, ask(t, h, "GET", "/api/library"))
	if lib.Ready || lib.Problem == "" {
		t.Errorf("library = %+v; want not ready, with the problem", lib)
	}
	if w := ask(t, h, "GET", "/api/papers"); w.Code != http.StatusServiceUnavailable {
		t.Errorf("papers with no library = %d", w.Code)
	}
}

func TestLibraryAndList(t *testing.T) {
	t.Parallel()
	h, id, _ := openTest(t)
	lib := decode[libraryJSON](t, ask(t, h, "GET", "/api/library"))
	if !lib.Ready || lib.Name != "Test" || lib.Works != 1 || lib.PDFs != 1 || lib.Searchable != 1 {
		t.Errorf("library = %+v", lib)
	}
	type list struct {
		Papers []rowJSON `json:"papers"`
		More   bool      `json:"more"`
	}
	all := decode[list](t, ask(t, h, "GET", "/api/papers"))
	if len(all.Papers) != 1 || all.Papers[0].ID != id || !all.Papers[0].PDF || !all.Papers[0].Searchable || all.More {
		t.Fatalf("list = %+v", all)
	}
	if got := decode[list](t, ask(t, h, "GET", "/api/papers?title=solitary&show=seeds")); len(got.Papers) != 1 {
		t.Errorf("a matching filter = %+v", got)
	}
	if got := decode[list](t, ask(t, h, "GET", "/api/papers?title=astronomy")); len(got.Papers) != 0 {
		t.Errorf("a filter matching nothing = %+v", got)
	}
	if w := ask(t, h, "GET", "/api/papers?show=everything"); w.Code != http.StatusBadRequest {
		t.Errorf("an unknown view = %d", w.Code)
	}
}

func TestPaperDetails(t *testing.T) {
	t.Parallel()
	h, id, _ := openTest(t)
	p := decode[paperJSON](t, ask(t, h, "GET", "/api/papers/"+id))
	if p.Title != "Field notes on solitary bees" || !p.PDF || !p.Folder || p.Pages < 1 || p.Origin != "IMPORTED BY YOU" {
		t.Errorf("paper = %+v", p)
	}
	if w := ask(t, h, "GET", "/api/papers/F0000000Z"); w.Code != http.StatusNotFound {
		t.Errorf("a paper not in the library = %d: %s", w.Code, w.Body)
	}
}

// The page names a paper, never a path: what opens is what the library holds.
func TestOpenUsesTheLibrarysOwnPaths(t *testing.T) {
	t.Parallel()
	h, id, opened := openTest(t)
	for _, action := range []string{"open", "folder"} {
		if w := ask(t, h, "POST", "/api/papers/"+id+"/"+action); w.Code != http.StatusNoContent {
			t.Fatalf("%s = %d: %s", action, w.Code, w.Body)
		}
	}
	if len(*opened) != 2 || !strings.HasSuffix((*opened)[0], ".pdf") || strings.HasSuffix((*opened)[1], ".pdf") {
		t.Errorf("opened = %q; want the PDF, then its folder", *opened)
	}
	ask(t, h, "GET", "/api/papers/"+id+"/open")
	if len(*opened) != 2 {
		t.Errorf("opened = %q; opening must not happen on a read", *opened)
	}
}

func TestOnlyAPDFIsOpened(t *testing.T) {
	t.Parallel()
	f, ok := firstPDF(library.Paper{Files: []library.PaperFile{{Path: "notes.txt"}, {Path: "gone.pdf", Missing: true}, {Path: "paper.PDF"}}})
	if !ok || f.Path != "paper.PDF" {
		t.Errorf("firstPDF = %+v, %v", f, ok)
	}
	if _, ok := firstPDF(library.Paper{Files: []library.PaperFile{{Path: "notes.txt"}}}); ok {
		t.Error("notes.txt counted as a PDF")
	}
}

func TestMissingNotesFileIsNotADeletedPDF(t *testing.T) {
	t.Parallel()
	p := library.Paper{Files: []library.PaperFile{{Path: "notes.txt", Missing: true}}}
	if got := pdfNote(p); strings.Contains(got, "deleted") {
		t.Errorf("pdfNote = %q", got)
	}
}
