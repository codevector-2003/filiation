package ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/codevector-2003/filiation/internal/library"
	"github.com/codevector-2003/filiation/internal/model"
	"github.com/codevector-2003/filiation/internal/opener"
)

// The Reader: a paper's own PDF, shown by the page with PDF.js, and the notes
// kept beside it in the library. The file itself is never changed.

// pdfFile serves the paper's PDF to the page. The page names a paper, never a
// file, and what is served must really be inside the library.
func (s *server) pdfFile(w http.ResponseWriter, r *http.Request) {
	p, err := s.Library.Show(r.Context(), r.PathValue("id"))
	if err != nil {
		failFind(w, err)
		return
	}
	pdf, ok := firstPDF(p)
	// Search's pages belong to the edition its text came from; another
	// edition would put them in the wrong place.
	if r.URL.Query().Get("indexed") == "true" && !isIndexed(p, pdf) {
		ok = false
	}
	if !ok {
		fail(w, http.StatusNotFound, errors.New("this paper has no PDF on disk"))
		return
	}
	if err := opener.Within(s.Library.Folder(), pdf.Path); err != nil {
		fail(w, http.StatusConflict, err)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	http.ServeFile(w, r, pdf.Path)
}

// pdfName is what a note remembers of the PDF it was made on: its place in the
// library, because a paper's editions can share a name in different folders.
func (s *server) pdfName(f library.PaperFile) string {
	if f.Path == "" {
		return ""
	}
	rel, err := filepath.Rel(s.Library.Folder(), f.Path)
	if err != nil {
		return filepath.ToSlash(f.Path)
	}
	return filepath.ToSlash(rel)
}

// isIndexed reports whether f is the file the paper's text was read from.
func isIndexed(p library.Paper, f library.PaperFile) bool {
	return p.Text == nil || p.Text.File == f.Path
}

type noteJSON struct {
	ID      int64           `json:"id"`
	Page    int             `json:"page"`
	Colour  string          `json:"colour"`
	Quote   string          `json:"quote"`
	Rects   json.RawMessage `json:"rects"`
	File    string          `json:"file"`
	Comment string          `json:"comment"`
	Created string          `json:"created"`
}

func toNoteJSON(n library.Note) noteJSON {
	rects := json.RawMessage(n.Rects)
	if !json.Valid(rects) {
		rects = json.RawMessage("[]")
	}
	return noteJSON{ID: n.ID, Page: n.Page, Colour: n.Colour, Quote: n.Quote, Rects: rects, File: n.File, Comment: n.Comment, Created: n.Created}
}

func (s *server) notes(w http.ResponseWriter, r *http.Request) {
	notes, err := s.Library.Notes(r.Context(), r.PathValue("id"))
	if err != nil {
		failFind(w, err)
		return
	}
	out := make([]noteJSON, len(notes))
	for i, n := range notes {
		out[i] = toNoteJSON(n)
	}
	send(w, map[string]any{"notes": out})
}

func (s *server) addNote(w http.ResponseWriter, r *http.Request) {
	var req noteJSON
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	p, err := s.Library.Show(r.Context(), r.PathValue("id"))
	if err != nil {
		failFind(w, err)
		return
	}
	pdf, _ := firstPDF(p)
	n, err := s.Library.AddNote(r.Context(), r.PathValue("id"), library.Note{Page: req.Page, Colour: req.Colour,
		Quote: req.Quote, Rects: string(req.Rects), File: s.pdfName(pdf), Comment: req.Comment})
	if err != nil {
		failNote(w, err)
		return
	}
	send(w, toNoteJSON(n))
}

func failNote(w http.ResponseWriter, err error) {
	if isBadInput(err) {
		fail(w, http.StatusBadRequest, err)
		return
	}
	failFind(w, err)
}

func noteID(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("note"), 10, 64)
}

func (s *server) changeNote(w http.ResponseWriter, r *http.Request) {
	id, err := noteID(r)
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	var req struct {
		Comment string `json:"comment"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if err := s.Library.SetNoteComment(r.Context(), id, req.Comment); err != nil {
		failFind(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) deleteNote(w http.ResponseWriter, r *http.Request) {
	id, err := noteID(r)
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if err := s.Library.DeleteNote(r.Context(), id); err != nil {
		failFind(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// citedOnPage lists what a paper cites on one page, with the sentence.
func (s *server) citedOnPage(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil {
		fail(w, http.StatusBadRequest, errors.New("no such page"))
		return
	}
	id := r.PathValue("id")
	hood, err := s.Library.Neighbours(r.Context(), id)
	if err != nil {
		failFind(w, err)
		return
	}
	contexts, err := s.Library.CitationContexts(r.Context(), id)
	if err != nil {
		failFind(w, err)
		return
	}
	type citedJSON struct {
		ID       string `json:"id"`
		Title    string `json:"title"`
		By       string `json:"by"`
		Sentence string `json:"sentence"`
	}
	out := []citedJSON{}
	for _, wk := range hood.Cites {
		for _, m := range contexts[wk.FilID] {
			if m.Page == n {
				out = append(out, citedJSON{wk.FilID, wk.DisplayTitle(), workBy(wk), m.Sentence})
				break
			}
		}
	}
	send(w, map[string]any{"cited": out})
}

// citation is how a paper is cited after a quotation: "Piwowar et al., 2018".
func citation(w model.Work) string {
	var who string
	if len(w.Authors) > 0 {
		if f := strings.Fields(w.Authors[0].Name); len(f) > 0 {
			who = f[len(f)-1]
		}
		switch {
		case len(w.Authors) == 2:
			if f := strings.Fields(w.Authors[1].Name); len(f) > 0 {
				who += " and " + f[len(f)-1]
			}
		case len(w.Authors) > 2:
			who += " et al."
		}
	}
	if w.Year != nil {
		if who != "" {
			who += ", "
		}
		who += strconv.Itoa(*w.Year)
	}
	return who
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9-]+`)

// exportNotes writes a paper's notes as Markdown into exports/.
func (s *server) exportNotes(w http.ResponseWriter, r *http.Request) {
	root := s.Library.Folder()
	if root == "" {
		fail(w, http.StatusConflict, errors.New("this library has no folder"))
		return
	}
	id := r.PathValue("id")
	p, err := s.Library.Show(r.Context(), id)
	if err != nil {
		failFind(w, err)
		return
	}
	notes, err := s.Library.Notes(r.Context(), id)
	if err != nil {
		failFind(w, err)
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", p.Work.DisplayTitle())
	if c := citation(p.Work); c != "" {
		fmt.Fprintf(&b, "%s\n\n", c)
	}
	for _, n := range notes {
		fmt.Fprintf(&b, "## Page %d\n\n", n.Page)
		if n.Quote != "" {
			fmt.Fprintf(&b, "> %s\n\n", strings.Join(strings.Fields(n.Quote), " "))
		}
		if n.Comment != "" {
			fmt.Fprintf(&b, "%s\n\n", n.Comment)
		}
	}
	name := "notes-" + strings.Trim(unsafeName.ReplaceAllString(citation(p.Work), "-"), "-") + "-" + p.Work.FilID + ".md"
	dir := filepath.Join(root, "exports")
	if err := os.WriteFile(filepath.Join(dir, name), []byte(b.String()), 0o644); err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	s.open(w, dir)
}
