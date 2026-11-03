package ui

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/codevector-2003/filiation/internal/library"
)

// One paper's actions: downloading its PDF, and bringing in the papers one
// citation step from it.

// maxCiting is the most citing papers one Bring In asks for: five requests.
const maxCiting = 1000

type surroundingsJSON struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	By        string `json:"by"`
	Local     bool   `json:"local"`
	Fetched   bool   `json:"fetched"` // its own reference list is here
	Cites     int    `json:"cites"`
	Unfetched int    `json:"unfetched"`
	Untitled  int    `json:"untitled"`
	CitedBy   int    `json:"citedBy"`
	Here      int    `json:"here"`
}

func (s *server) surroundings(w http.ResponseWriter, r *http.Request) {
	a, err := s.Library.Surroundings(r.Context(), r.PathValue("id"))
	if err != nil {
		failFind(w, err)
		return
	}
	send(w, surroundingsJSON{ID: a.Work.FilID, Title: a.Work.DisplayTitle(), By: workBy(a.Work),
		Local: a.Work.OpenAlexID == "", Fetched: a.Work.Hydrated, Cites: a.Cites, Unfetched: a.Unfetched,
		Untitled: a.Untitled, CitedBy: a.CitedBy, Here: a.Here})
}

func (s *server) bringAround(w http.ResponseWriter, r *http.Request) {
	var req struct {
		References bool `json:"references"`
		Citing     int  `json:"citing"`
		Follow     bool `json:"follow"`
	}
	if !readBody(w, r, &req) {
		return
	}
	if req.Citing < 0 || req.Citing > maxCiting {
		fail(w, http.StatusBadRequest, fmt.Errorf("choose up to %d papers that cite it", maxCiting))
		return
	}
	if !req.References && req.Citing == 0 {
		fail(w, http.StatusBadRequest, errors.New("choose what to bring in"))
		return
	}
	id := r.PathValue("id")
	s.start(w, "around", func(ctx context.Context, set func(int, int, string)) (string, error) {
		got, err := s.Library.BringAround(ctx, id, library.AroundOptions{References: req.References,
			Citing: req.Citing, Follow: req.Follow,
			Progress: func(done, total int) { set(done, total, "Bringing in papers") }})
		if err != nil {
			return "", err
		}
		var parts []string
		if req.References {
			parts = append(parts, fmt.Sprintf("%d it cites", got.References))
		}
		if req.Citing > 0 {
			parts = append(parts, fmt.Sprintf("%d that cite it", got.Citing))
		}
		return fmt.Sprintf("Brought in %s: %s", count(got.References+got.Citing, "paper"), strings.Join(parts, ", ")), nil
	})
}

// downloadPDFs downloads the PDFs of the papers named, one after another, as
// fil fetch <paper> does: naming a paper is asking for it, so every link is
// tried again.
func (s *server) downloadPDFs(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if !readBody(w, r, &req) {
		return
	}
	if len(req.IDs) == 0 {
		fail(w, http.StatusBadRequest, errors.New("choose a paper"))
		return
	}
	if len(req.IDs) > 200 {
		fail(w, http.StatusBadRequest, errors.New("choose at most 200 papers at once"))
		return
	}
	ids := append([]string(nil), req.IDs...)
	s.start(w, "download", func(ctx context.Context, set func(int, int, string)) (string, error) {
		got, had := 0, 0
		var none []string
		for i, id := range ids {
			set(i, len(ids), "Downloading PDFs")
			p, err := s.Library.FetchOne(ctx, id)
			switch {
			case ctx.Err() != nil:
				return "", ctx.Err()
			case errors.Is(err, library.ErrNoFolder):
				return "", errors.New("this library has no folder for PDFs")
			case err != nil:
				none = append(none, err.Error())
			case p.Already:
				had++
			case p.File != "":
				got++
			default:
				none = append(none, s.whyNoPDF(ctx, id))
			}
		}
		set(len(ids), len(ids), "Downloading PDFs")
		if len(ids) == 1 {
			switch {
			case got == 1:
				return "Downloaded", nil
			case had == 1:
				return "Its PDF was already here", nil
			}
			return "", errors.New(none[0])
		}
		out := fmt.Sprintf("%d of %d downloaded", got, len(ids))
		if had > 0 {
			out += fmt.Sprintf(", %d already here", had)
		}
		if len(none) > 0 {
			out += fmt.Sprintf(", %d with no legal copy", len(none))
		}
		return out, nil
	})
}

// whyNoPDF is why a paper has no PDF after asking for one.
func (s *server) whyNoPDF(ctx context.Context, id string) string {
	p, err := s.Library.Show(ctx, id)
	if err != nil {
		return "No PDF could be downloaded."
	}
	return pdfNote(p)
}

type attachedJSON struct {
	State  string `json:"state"` // added | unconfirmed | duplicate
	File   string `json:"file,omitempty"`
	Name   string `json:"name,omitempty"`
	Reason string `json:"reason,omitempty"`
	Pages  int    `json:"pages,omitempty"` // searchable, once read
}

// attachPDF files a PDF the person has under the paper: the file named, or
// one they choose. A file that does not carry the paper's title is sent back
// to be asked about; force says they have seen that and want it anyway.
func (s *server) attachPDF(w http.ResponseWriter, r *http.Request) {
	var req struct {
		File  string `json:"file"`
		Force bool   `json:"force"`
	}
	if !readBody(w, r, &req) {
		return
	}
	file := req.File
	if file == "" {
		if s.PickFiles == nil {
			fail(w, http.StatusNotImplemented, errors.New("choosing a file needs the desktop app"))
			return
		}
		files, err := s.PickFiles()
		if err != nil {
			fail(w, http.StatusInternalServerError, err)
			return
		}
		if len(files) == 0 {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		file = files[0]
	}
	if !isPDF(file) {
		fail(w, http.StatusBadRequest, errors.New("only a PDF can be added"))
		return
	}
	got, err := s.Library.AttachPDF(r.Context(), r.PathValue("id"), file, req.Force)
	switch {
	case errors.Is(err, library.ErrNoFolder):
		fail(w, http.StatusConflict, errors.New("this library has no folder for PDFs"))
		return
	case err != nil:
		failFind(w, err)
		return
	}
	out := attachedJSON{File: got.File, Name: filepath.Base(got.File), Reason: got.Reason}
	switch got.Outcome {
	case library.ImportUnconfirmed:
		out.State = "unconfirmed"
	case library.ImportDuplicate:
		out.State = "duplicate"
	default:
		out.State = "added"
		if got.Text != nil && got.Text.Status == "ok" {
			out.Pages = got.Text.Pages
		}
	}
	send(w, out)
}
