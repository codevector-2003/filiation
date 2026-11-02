package ui

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/codevector-2003/filiation/internal/assistants"
	"github.com/codevector-2003/filiation/internal/errs"
	"github.com/codevector-2003/filiation/internal/library"
	"github.com/codevector-2003/filiation/internal/model"
	"github.com/codevector-2003/filiation/internal/opener"
)

//go:embed assets
var assets embed.FS

// listLimit is how many papers the list holds at once.
const listLimit = 500

// Options is what the interface shows and how it reaches the system.
type Options struct {
	// Library is the open library; nil when there is none to show, and
	// Problem then says why, or is empty when none was made yet.
	Library *library.Library
	Name    string
	Problem string

	// OpenPath opens a file or folder with the system: opener.Open, or a
	// stand-in in a test.
	OpenPath func(path string) error

	// PickFiles asks the person for PDFs in the system's file dialog.
	PickFiles func() ([]string, error)

	// Assistants is where to find AI assistants, and CallLog the record of
	// what they asked. A nil Assistants leaves that screen out.
	Assistants *assistants.Env
	CallLog    string
}

type server struct {
	Options
	work     work
	readOnly bool // assistants may read but not grow the library; guarded by work.mu
}

// New returns the handler serving the screens and their data.
func New(opts Options) http.Handler {
	s := &server{Options: opts}
	root, err := fs.Sub(assets, "assets")
	if err != nil {
		panic(err) // the embedded tree always has assets/
	}
	mux := http.NewServeMux()
	mux.Handle("GET /", scripts(http.FileServerFS(root)))
	mux.HandleFunc("GET /api/library", s.library)
	mux.HandleFunc("GET /api/papers", s.needLibrary(s.papers))
	mux.HandleFunc("GET /api/papers/{id}", s.needLibrary(s.paper))
	mux.HandleFunc("POST /api/papers/{id}/open", s.needLibrary(s.openPDF))
	mux.HandleFunc("POST /api/papers/{id}/folder", s.needLibrary(s.openFolder))
	mux.HandleFunc("GET /api/papers/{id}/pages/{n}", s.needLibrary(s.page))
	mux.HandleFunc("GET /api/papers/{id}/pdf", s.needLibrary(s.pdfFile))
	mux.HandleFunc("GET /api/papers/{id}/notes", s.needLibrary(s.notes))
	mux.HandleFunc("POST /api/papers/{id}/notes", s.needLibrary(s.addNote))
	mux.HandleFunc("POST /api/papers/{id}/notes/export", s.needLibrary(s.exportNotes))
	mux.HandleFunc("POST /api/notes/{note}", s.needLibrary(s.changeNote))
	mux.HandleFunc("POST /api/notes/{note}/delete", s.needLibrary(s.deleteNote))
	mux.HandleFunc("GET /api/papers/{id}/cited/{n}", s.needLibrary(s.citedOnPage))
	mux.HandleFunc("GET /api/search", s.needLibrary(s.search))
	mux.HandleFunc("GET /api/map", s.needLibrary(s.mapAround))
	mux.HandleFunc("GET /api/link", s.needLibrary(s.link))
	mux.HandleFunc("GET /api/path", s.needLibrary(s.path))
	mux.HandleFunc("POST /api/export", s.needLibrary(s.export))
	mux.HandleFunc("POST /api/add", s.needLibrary(s.add))
	mux.HandleFunc("GET /api/job", s.jobStatus)
	mux.HandleFunc("POST /api/job/stop", s.jobStop)
	mux.HandleFunc("POST /api/fetch", s.needLibrary(s.fetch))
	mux.HandleFunc("GET /api/inbox", s.needLibrary(s.inboxList))
	mux.HandleFunc("POST /api/inbox/import", s.needLibrary(s.inboxImport))
	mux.HandleFunc("POST /api/inbox/choose", s.needLibrary(s.inboxChoose))
	mux.HandleFunc("POST /api/inbox/folder", s.needLibrary(s.inboxFolder))
	mux.HandleFunc("POST /api/inbox/{id}/file", s.needLibrary(s.inboxFile))
	mux.HandleFunc("POST /api/inbox/{id}/skip", s.needLibrary(s.inboxSkip))
	mux.HandleFunc("GET /api/assistants", s.assistantList)
	mux.HandleFunc("POST /api/assistants/permission", s.assistantPermission)
	mux.HandleFunc("POST /api/assistants/{id}/connect", s.assistantConnect)
	mux.HandleFunc("POST /api/assistants/{id}/disconnect", s.assistantDisconnect)
	return mux
}

// scripts names the type of .mjs files itself. Left to the system's own table
// they can come out as plain text, and a page refuses to run those as modules.
func scripts(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".mjs") {
			w.Header().Set("Content-Type", "text/javascript")
		}
		h.ServeHTTP(w, r)
	})
}

func (s *server) needLibrary(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Library == nil {
			fail(w, http.StatusServiceUnavailable, errors.New("no library is open"))
			return
		}
		h(w, r)
	}
}

func send(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func isBadInput(err error) bool { return errors.Is(err, errs.ErrInvalidInput) }

func failFind(w http.ResponseWriter, err error) {
	if errors.Is(err, errs.ErrNotFound) {
		fail(w, http.StatusNotFound, errors.New("this paper is not in the library"))
		return
	}
	fail(w, http.StatusInternalServerError, err)
}

type collectionJSON struct {
	Name  string `json:"name"`
	Works int    `json:"works"`
}

type libraryJSON struct {
	Ready       bool             `json:"ready"`
	Name        string           `json:"name"`
	Problem     string           `json:"problem,omitempty"`
	Folder      string           `json:"folder,omitempty"`
	Works       int              `json:"works"`
	PDFs        int              `json:"pdfs"`
	Searchable  int              `json:"searchable"`
	Inbox       int              `json:"inbox"`
	Collections []collectionJSON `json:"collections"`
}

func (s *server) library(w http.ResponseWriter, r *http.Request) {
	out := libraryJSON{Name: s.Name, Problem: s.Problem, Collections: []collectionJSON{}}
	if out.Name == "" {
		out.Name = "My Library"
	}
	if s.Library == nil {
		send(w, out)
		return
	}
	ctx := r.Context()
	out.Ready, out.Folder = true, s.Library.Folder()
	sum, err := s.Library.Summarise(ctx)
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	out.Works = sum.Works
	if out.PDFs, err = s.Library.FetchedOnDisk(ctx); err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	counts, err := s.Library.TextCounts(ctx)
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	out.Searchable = counts["ok"]
	// A library with no folder has no Inbox, so nothing is waiting in it.
	out.Inbox, _ = s.Library.InboxWaiting()
	cs, err := s.Library.Collections(ctx)
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	for _, c := range cs {
		out.Collections = append(out.Collections, collectionJSON{c.Name, c.Works})
	}
	send(w, out)
}

type rowJSON struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	By         string `json:"by"`
	Seed       bool   `json:"seed"`
	PDF        bool   `json:"pdf"`
	Mismatch   bool   `json:"mismatch,omitempty"`
	Text       string `json:"text,omitempty"` // searchable, or why not
	Searchable bool   `json:"searchable"`
	Sentences  int    `json:"sentences"`
	Stub       bool   `json:"stub"`
}

func (s *server) papers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	opts := library.ListOptions{Title: q.Get("title"), Collection: q.Get("collection"), Limit: listLimit}
	switch q.Get("show") {
	case "", "all":
	case "seeds":
		opts.Seeds = true
	case "pdf":
		opts.WithPDF = true
	case "notext":
		opts.WithoutText = true
	default:
		fail(w, http.StatusBadRequest, fmt.Errorf("unknown view %q", q.Get("show")))
		return
	}
	rows, err := s.Library.List(r.Context(), opts)
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]rowJSON, len(rows))
	for i, l := range rows {
		out[i] = rowJSON{ID: l.FilID, Title: l.Title, By: rowBy(l), Seed: l.Seed, PDF: l.HasPDF, Mismatch: l.Mismatch,
			Text: textNote(l.TextStatus), Searchable: l.TextStatus == "ok", Sentences: l.Contexts, Stub: !l.Hydrated}
		if out[i].Title == "" {
			out[i].Title = "Not fetched yet: " + l.OpenAlexID
		}
	}
	send(w, map[string]any{"papers": out, "more": len(rows) == listLimit})
}

func rowBy(l library.Listed) string {
	var parts []string
	if f := strings.Fields(l.Author); len(f) > 0 {
		parts = append(parts, f[len(f)-1])
	}
	if l.Year > 0 {
		parts = append(parts, strconv.Itoa(l.Year))
	}
	return strings.Join(parts, " · ")
}

// textNote is a paper's text state in a few words.
func textNote(status string) string {
	switch status {
	case "":
		return ""
	case "ok":
		return "Searchable"
	case "no-text":
		return "No text layer"
	case "gibberish":
		return "Unreadable text"
	case "unreadable":
		return "Could not be read"
	case "timed-out":
		return "Took too long to read"
	}
	return status
}

type sentenceJSON struct {
	Page  int    `json:"page"`
	Text  string `json:"text"`
	Cites string `json:"cites,omitempty"`
}

type paperJSON struct {
	ID        string         `json:"id"`
	Title     string         `json:"title"`
	Authors   string         `json:"authors,omitempty"`
	Meta      string         `json:"meta,omitempty"`
	Origin    string         `json:"origin,omitempty"`
	Seed      bool           `json:"seed"`
	Cites     int            `json:"cites"`
	CitedBy   int            `json:"citedBy"`
	Pages     int            `json:"pages"`
	Sentences int            `json:"sentences"`
	Sample    []sentenceJSON `json:"sample"`
	PDF       bool           `json:"pdf"`
	PDFNote   string         `json:"pdfNote,omitempty"`
	TextNote  string         `json:"textNote,omitempty"`
	Folder    bool           `json:"folder"`
	Web       string         `json:"web,omitempty"`
	Citation  string         `json:"citation,omitempty"`
	File      string         `json:"file,omitempty"` // the name of the PDF the Reader opens
	Abstract  string         `json:"abstract,omitempty"`
}

func (s *server) paper(w http.ResponseWriter, r *http.Request) {
	p, err := s.Library.Show(r.Context(), r.PathValue("id"))
	if err != nil {
		failFind(w, err)
		return
	}
	work := p.Work
	pdf, hasPDF := firstPDF(p)
	out := paperJSON{ID: work.FilID, Title: work.DisplayTitle(), Authors: authors(work), Meta: meta(work),
		Seed: work.IsSeed, Cites: p.Cites, CitedBy: p.CitedBy, Sentences: p.Contexts,
		Sample: []sentenceJSON{}, PDF: hasPDF, Folder: p.Folder != "", Web: p.WebPage(),
		Citation: citation(work), Abstract: work.Abstract}
	out.Origin = map[model.Source]string{model.SourceSeed: "ADDED BY YOU", model.SourceUpload: "IMPORTED BY YOU",
		model.SourceExpansion: "FOUND BY FOLLOWING CITATIONS"}[work.Source]
	out.File = pdfName(pdf)
	if !out.PDF {
		out.PDFNote = pdfNote(p)
	} else if pdf.Mismatch {
		out.PDFNote = "Its first pages do not carry this paper's title and author: it may be another paper or edition."
	}
	if p.Text != nil {
		if p.Text.Status == "ok" {
			out.Pages = p.Text.Pages
		} else {
			out.TextNote = textNote(p.Text.Status)
		}
	}
	for _, c := range p.Sample {
		out.Sample = append(out.Sample, sentenceJSON{Page: c.Page, Text: c.Sentence, Cites: c.Work.DisplayTitle()})
	}
	send(w, out)
}

// authors names the first three and counts the rest.
func authors(w model.Work) string {
	const shown = 3
	names := make([]string, 0, shown)
	for _, a := range w.Authors[:min(len(w.Authors), shown)] {
		names = append(names, a.Name)
	}
	s := strings.Join(names, ", ")
	switch more := len(w.Authors) - len(names); {
	case more == 1:
		s += " and 1 other"
	case more > 1:
		s += fmt.Sprintf(" and %d others", more)
	}
	return s
}

func meta(w model.Work) string {
	var parts []string
	if w.Venue != "" {
		parts = append(parts, w.Venue)
	}
	if w.Year != nil {
		parts = append(parts, strconv.Itoa(*w.Year))
	}
	switch {
	case w.OAStatus.IsOpen():
		parts = append(parts, string(w.OAStatus)+" open access")
	case w.OAStatus == model.OAClosed:
		parts = append(parts, "closed access")
	}
	if w.DOI != nil {
		parts = append(parts, "doi:"+*w.DOI)
	}
	return strings.Join(parts, " · ")
}

func isPDF(path string) bool { return strings.EqualFold(filepath.Ext(path), ".pdf") }

// firstPDF is the paper's PDF still on disk, if it has one: the one its text
// was read from when there are several, so that a search's pages are its
// pages. Its folder can hold other files too.
func firstPDF(p library.Paper) (library.PaperFile, bool) {
	if p.Text != nil {
		for _, f := range p.Files {
			if !f.Missing && f.Path == p.Text.File {
				return f, true
			}
		}
	}
	for _, f := range p.Files {
		if !f.Missing && isPDF(f.Path) {
			return f, true
		}
	}
	return library.PaperFile{}, false
}

// pdfNote says why a paper has no PDF, from what the library knows.
func pdfNote(p library.Paper) string {
	for _, f := range p.Files {
		if f.Missing && isPDF(f.Path) {
			return "Its PDF was deleted from the library folder."
		}
	}
	if len(p.Attempts) > 0 {
		return "Every download link failed. Filiation tries again in a month."
	}
	switch p.PDF.Availability {
	case model.PDFLink:
		return "An open-access PDF is known but not downloaded yet."
	case model.PDFOpenNoLink:
		return "Open access, but only as a web page."
	case model.PDFNoOpenCopy:
		return "No open-access copy is known."
	case model.PDFNotInOpenAlex:
		return "Not in OpenAlex."
	}
	return "Not looked up yet."
}

func (s *server) openPDF(w http.ResponseWriter, r *http.Request) {
	p, err := s.Library.Show(r.Context(), r.PathValue("id"))
	if err != nil {
		failFind(w, err)
		return
	}
	pdf, ok := firstPDF(p)
	if !ok {
		fail(w, http.StatusConflict, errors.New("this paper has no PDF on disk"))
		return
	}
	s.open(w, pdf.Path)
}

func (s *server) openFolder(w http.ResponseWriter, r *http.Request) {
	p, err := s.Library.Show(r.Context(), r.PathValue("id"))
	if err != nil {
		failFind(w, err)
		return
	}
	if p.Folder == "" {
		fail(w, http.StatusConflict, errors.New("this paper has no folder"))
		return
	}
	s.open(w, p.Folder)
}

// open only ever gets a path the library gave: the page names a paper, never
// a file.
func (s *server) open(w http.ResponseWriter, path string) {
	root := s.Library.Folder()
	if root == "" {
		fail(w, http.StatusConflict, errors.New("this library has no folder"))
		return
	}
	if err := opener.Within(root, path); err != nil {
		fail(w, http.StatusConflict, err)
		return
	}
	if err := s.OpenPath(path); err != nil {
		fail(w, http.StatusInternalServerError, fmt.Errorf("could not open it: %w", err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
