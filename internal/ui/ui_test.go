package ui

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/codevector-2003/filiation/internal/assistants"
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

// A module served as anything but a script does not run, and then no PDF opens.
func TestPDFjsIsServedAsAScript(t *testing.T) {
	t.Parallel()
	h := New(Options{})
	for _, path := range []string{"/pdfjs/pdf.min.mjs", "/pdfjs/pdf.worker.min.mjs"} {
		w := ask(t, h, "GET", path)
		if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/javascript") {
			t.Errorf("GET %s = %d, %q", path, w.Code, w.Header().Get("Content-Type"))
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

// With several editions, the one read is the one whose pages a search names.
func TestTheEditionReadIsTheOneOpened(t *testing.T) {
	t.Parallel()
	files := []library.PaperFile{{Path: "accepted.pdf"}, {Path: "published.pdf"}}
	f, ok := firstPDF(library.Paper{Files: files, Text: &library.PaperText{File: "published.pdf"}})
	if !ok || f.Path != "published.pdf" {
		t.Errorf("firstPDF = %+v, %v", f, ok)
	}
	files[1].Missing = true
	if f, _ := firstPDF(library.Paper{Files: files, Text: &library.PaperText{File: "published.pdf"}}); f.Path != "accepted.pdf" {
		t.Errorf("with the edition read gone, firstPDF = %+v", f)
	}
}

func TestMissingNotesFileIsNotADeletedPDF(t *testing.T) {
	t.Parallel()
	p := library.Paper{Files: []library.PaperFile{{Path: "notes.txt", Missing: true}}}
	if got := pdfNote(p); strings.Contains(got, "deleted") {
		t.Errorf("pdfNote = %q", got)
	}
}

func post(t *testing.T, h http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", path, strings.NewReader(body)))
	return w
}

// waitJob waits for the running job to end.
func waitJob(t *testing.T, h http.Handler) jobJSON {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		j := decode[jobJSON](t, ask(t, h, "GET", "/api/job"))
		if !j.Running {
			return j
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the job did not end")
	return jobJSON{}
}

func TestSearchFindsPassagesWithTheirPage(t *testing.T) {
	t.Parallel()
	h, id, _ := openTest(t)
	type found struct {
		Hits    []hitJSON `json:"hits"`
		Partial bool      `json:"partial"`
	}
	res := decode[found](t, ask(t, h, "GET", "/api/search?q=hedgerow"))
	if len(res.Hits) != 1 || res.Hits[0].ID != id || res.Hits[0].Page != 1 {
		t.Fatalf("search = %+v", res)
	}
	marked := false
	for _, part := range res.Hits[0].Parts {
		if strings.Contains(part.Text, library.MarkStart) || strings.Contains(part.Text, library.MarkEnd) {
			t.Errorf("a marker leaked into %q", part.Text)
		}
		marked = marked || (part.Mark && strings.EqualFold(part.Text, "hedgerow"))
	}
	if !marked {
		t.Errorf("parts = %+v; want the word marked", res.Hits[0].Parts)
	}
	if w := ask(t, h, "GET", "/api/search?q=%20"); w.Code != http.StatusBadRequest {
		t.Errorf("an empty search = %d", w.Code)
	}
}

func TestPageText(t *testing.T) {
	t.Parallel()
	h, id, _ := openTest(t)
	type page struct {
		Page, Pages int
		Text        string
	}
	p := decode[page](t, ask(t, h, "GET", "/api/papers/"+id+"/pages/1"))
	if p.Pages < 1 || !strings.Contains(p.Text, "hedgerow") {
		t.Errorf("page = %+v", p)
	}
	if w := ask(t, h, "GET", "/api/papers/"+id+"/pages/nope"); w.Code != http.StatusBadRequest {
		t.Errorf("a page that is no number = %d", w.Code)
	}
}

func TestMapOfAPaperWithNoCitations(t *testing.T) {
	t.Parallel()
	h, id, _ := openTest(t)
	type mapped struct {
		Center nodeJSON   `json:"center"`
		Nodes  []nodeJSON `json:"nodes"`
	}
	m := decode[mapped](t, ask(t, h, "GET", "/api/map?id="+id+"&steps=2"))
	if m.Center.ID != id || !m.Center.PDF || m.Nodes == nil || len(m.Nodes) != 0 {
		t.Errorf("map = %+v", m)
	}
	if got := decode[mapped](t, ask(t, h, "GET", "/api/map")); got.Center.ID != id {
		t.Errorf("with no paper named, the map centres on %+v", got.Center)
	}
}

func TestExportWritesIntoTheLibrary(t *testing.T) {
	t.Parallel()
	h, _, opened := openTest(t)
	if w := post(t, h, "/api/export", ""); w.Code != http.StatusNoContent {
		t.Fatalf("export = %d: %s", w.Code, w.Body)
	}
	if len(*opened) != 1 {
		t.Fatalf("opened = %q", *opened)
	}
	if _, err := os.Stat(filepath.Join((*opened)[0], exportFileName)); err != nil {
		t.Error(err)
	}
}

func TestInboxReviewAndKeepAsADocument(t *testing.T) {
	t.Parallel()
	h, _, _ := openTest(t)
	lib := decode[libraryJSON](t, ask(t, h, "GET", "/api/library"))
	pdf := pdftest.Make("Notes from a meeting about nothing in particular, kept for the record.\n" +
		"There is no identifier in this file, and nothing a search could use.")
	if err := os.WriteFile(filepath.Join(lib.Folder, "Inbox", "meeting.pdf"), pdf, 0o644); err != nil {
		t.Fatal(err)
	}
	// No body at all: everything waiting.
	if w := post(t, h, "/api/inbox/import", ""); w.Code != http.StatusAccepted {
		t.Fatalf("import = %d: %s", w.Code, w.Body)
	}
	if w := post(t, h, "/api/inbox/import", ""); w.Code != http.StatusAccepted && w.Code != http.StatusConflict {
		t.Errorf("a second import = %d", w.Code)
	}
	waitJob(t, h)
	type box struct {
		Items []inboxJSON `json:"items"`
		Fresh bool        `json:"fresh"`
	}
	first := decode[box](t, ask(t, h, "GET", "/api/inbox"))
	items := first.Items
	if len(items) != 1 || items[0].State == "filed" {
		t.Fatalf("inbox = %+v", items)
	}
	if first.Fresh {
		t.Error("a file already looked at counts as fresh, so each visit would look again")
	}
	if err := os.WriteFile(filepath.Join(lib.Folder, "Inbox", "later.pdf"), pdf, 0o644); err != nil {
		t.Fatal(err)
	}
	if !decode[box](t, ask(t, h, "GET", "/api/inbox")).Fresh {
		t.Error("a file saved since does not count as fresh")
	}
	if err := os.Remove(filepath.Join(lib.Folder, "Inbox", "later.pdf")); err != nil {
		t.Fatal(err)
	}
	path := "/api/inbox/" + strconv.Itoa(items[0].ID)
	if w := post(t, h, path+"/file", `{"title":"Meeting notes"}`); w.Code != http.StatusOK {
		t.Fatalf("file = %d: %s", w.Code, w.Body)
	}
	got := decode[box](t, ask(t, h, "GET", "/api/inbox")).Items
	if len(got) != 1 || got[0].State != "filed" || got[0].PaperID == "" {
		t.Errorf("after filing = %+v", got)
	}
	if w := post(t, h, "/api/inbox/9999/file", `{"title":"x"}`); w.Code != http.StatusNotFound {
		t.Errorf("an unknown item = %d", w.Code)
	}
	if w := post(t, h, path+"/skip", ""); w.Code != http.StatusNoContent {
		t.Errorf("skip = %d", w.Code)
	}
	if got := decode[box](t, ask(t, h, "GET", "/api/inbox")).Items; len(got) != 0 {
		t.Errorf("after skip = %+v", got)
	}
}

// A file passed over stays passed over, and does not hide a new one.
func TestSkippedFileDoesNotHideANewOne(t *testing.T) {
	t.Parallel()
	h, _, _ := openTest(t)
	lib := decode[libraryJSON](t, ask(t, h, "GET", "/api/library"))
	inbox := filepath.Join(lib.Folder, "Inbox")
	pdf := pdftest.Make("Nothing here identifies this file, and nothing a search could use at all.")
	if err := os.WriteFile(filepath.Join(inbox, "first.pdf"), pdf, 0o644); err != nil {
		t.Fatal(err)
	}
	post(t, h, "/api/inbox/import", "")
	waitJob(t, h)
	type box struct {
		Items []inboxJSON `json:"items"`
		Fresh bool        `json:"fresh"`
	}
	got := decode[box](t, ask(t, h, "GET", "/api/inbox"))
	if len(got.Items) != 1 {
		t.Fatalf("inbox = %+v", got)
	}
	post(t, h, "/api/inbox/"+strconv.Itoa(got.Items[0].ID)+"/skip", "")
	if decode[box](t, ask(t, h, "GET", "/api/inbox")).Fresh {
		t.Error("a skipped file came back as fresh")
	}
	if err := os.Remove(filepath.Join(inbox, "first.pdf")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inbox, "second.pdf"), pdf, 0o644); err != nil {
		t.Fatal(err)
	}
	if !decode[box](t, ask(t, h, "GET", "/api/inbox")).Fresh {
		t.Error("a new file went unnoticed after a skipped one left")
	}
}

func TestImportRefusesAnythingButPDFs(t *testing.T) {
	t.Parallel()
	h, _, _ := openTest(t)
	if w := post(t, h, "/api/inbox/import", `{"files":["notes.txt"]}`); w.Code != http.StatusBadRequest {
		t.Errorf("import of a text file = %d", w.Code)
	}
}

func TestAssistantsScreen(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	env := assistants.Env{Home: dir, ConfigDir: dir, Command: "fil",
		LookPath: func(string) (string, error) { return "", errors.New("not here") }}
	log := filepath.Join(dir, "calls.jsonl")
	assistants.Record(log, assistants.Call{At: time.Now(), Tool: "search_library"})
	h := New(Options{Assistants: &env, CallLog: log})

	type screen struct {
		List  []assistants.Status `json:"list"`
		Grow  bool                `json:"grow"`
		Calls []assistants.Call   `json:"calls"`

		Settings string `json:"settings"`
	}
	s := decode[screen](t, ask(t, h, "GET", "/api/assistants"))
	if len(s.List) == 0 || !s.Grow || len(s.Calls) != 1 {
		t.Fatalf("screen = %+v", s)
	}
	if w := post(t, h, "/api/assistants/cursor/connect", ""); w.Code != http.StatusNoContent {
		t.Fatalf("connect = %d: %s", w.Code, w.Body)
	}
	if w := post(t, h, "/api/assistants/permission?grow=false", ""); w.Code != http.StatusNoContent {
		t.Fatalf("permission = %d: %s", w.Code, w.Body)
	}
	s = decode[screen](t, ask(t, h, "GET", "/api/assistants"))
	if s.Grow {
		t.Error("growing still allowed")
	}
	if !strings.Contains(s.Settings, "--read-only") {
		t.Errorf("copied settings = %q; want them read only too", s.Settings)
	}
	for _, a := range s.List {
		if a.ID == "cursor" && (!a.Connected || !a.ReadOnly) {
			t.Errorf("cursor = %+v; the change must reach what is connected", a)
		}
	}
	if err := assistants.Connect(env, "gemini", false); err != nil {
		t.Fatal(err)
	}
	type mixed struct {
		Grow  bool `json:"grow"`
		Mixed bool `json:"mixed"`
	}
	if m := decode[mixed](t, ask(t, h, "GET", "/api/assistants")); m.Grow || !m.Mixed {
		t.Errorf("one reading and one writing = %+v; want off, and said to be mixed", m)
	}
	if w := post(t, h, "/api/assistants/nobody/connect", ""); w.Code != http.StatusConflict {
		t.Errorf("an unknown assistant = %d", w.Code)
	}
}

func TestReaderServesThePDFAndKeepsNotes(t *testing.T) {
	t.Parallel()
	h, id, opened := openTest(t)
	w := ask(t, h, "GET", "/api/papers/"+id+"/pdf")
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Body.String(), "%PDF") {
		t.Fatalf("pdf = %d, %.20q", w.Code, w.Body.String())
	}

	note := post(t, h, "/api/papers/"+id+"/notes",
		`{"page":1,"colour":"yellow","quote":"Nest counts rose","rects":[{"x":0.1,"y":0.2,"w":0.3,"h":0.02}]}`)
	if note.Code != http.StatusOK {
		t.Fatalf("add = %d: %s", note.Code, note.Body)
	}
	added := decode[noteJSON](t, note)
	if w := post(t, h, "/api/notes/"+strconv.FormatInt(added.ID, 10), `{"comment":"Compare with the 2019 count."}`); w.Code != http.StatusNoContent {
		t.Fatalf("change = %d: %s", w.Code, w.Body)
	}
	type list struct {
		Notes []noteJSON `json:"notes"`
	}
	got := decode[list](t, ask(t, h, "GET", "/api/papers/"+id+"/notes")).Notes
	if len(got) != 1 || got[0].Comment != "Compare with the 2019 count." || got[0].Colour != "yellow" || !strings.Contains(string(got[0].Rects), "0.3") {
		t.Fatalf("notes = %+v", got)
	}

	if w := post(t, h, "/api/papers/"+id+"/notes/export", ""); w.Code != http.StatusNoContent {
		t.Fatalf("export = %d: %s", w.Code, w.Body)
	}
	files, err := filepath.Glob(filepath.Join((*opened)[0], "notes-*"+id+".md"))
	if err != nil || len(files) != 1 {
		t.Fatalf("exported = %v, %v", files, err)
	}
	if b, _ := os.ReadFile(files[0]); !strings.Contains(string(b), "> Nest counts rose") || !strings.Contains(string(b), "2019 count") {
		t.Errorf("export = %s", b)
	}

	if w := post(t, h, "/api/notes/"+strconv.FormatInt(added.ID, 10)+"/delete", ""); w.Code != http.StatusNoContent {
		t.Fatalf("delete = %d", w.Code)
	}
	if w := post(t, h, "/api/notes/"+strconv.FormatInt(added.ID, 10)+"/delete", ""); w.Code != http.StatusNotFound {
		t.Errorf("deleting twice = %d", w.Code)
	}
}

func TestNoteRefusesNonsense(t *testing.T) {
	t.Parallel()
	h, id, _ := openTest(t)
	for _, body := range []string{`{"page":1}`, `{"page":1,"quote":"x","colour":"purple"}`, `{"page":-1,"quote":"x"}`} {
		if w := post(t, h, "/api/papers/"+id+"/notes", body); w.Code != http.StatusBadRequest {
			t.Errorf("%s = %d", body, w.Code)
		}
	}
}

func TestPaperWithNoPDFServesNone(t *testing.T) {
	t.Parallel()
	h, id, _ := openTest(t)
	if w := ask(t, h, "GET", "/api/papers/F0000000Z/pdf"); w.Code != http.StatusNotFound {
		t.Errorf("unknown paper = %d", w.Code)
	}
	if w := ask(t, h, "GET", "/api/papers/"+id+"/cited/1"); w.Code != http.StatusOK {
		t.Errorf("cited = %d: %s", w.Code, w.Body)
	}
}
