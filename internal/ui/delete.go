package ui

import (
	"errors"
	"net/http"
	"path/filepath"
)

// Deleting papers (D36): first what would go, for the dialog, then the
// deletion itself. The library changes in one transaction; only then are the
// papers' folders moved to the system's bin, one by one, so a folder that
// cannot be moved is reported and left where it was, never half-deleted.

type deleteRequest struct {
	IDs []string `json:"ids"`
}

type deletePlanJSON struct {
	Papers      int `json:"papers"`
	PDFs        int `json:"pdfs"`
	Notes       int `json:"notes"`
	Collections int `json:"collections"`
	Labels      int `json:"labels"`
	Orphans     int `json:"orphans"`
}

type deletedJSON struct {
	Papers  int      `json:"papers"`
	Orphans int      `json:"orphans"`
	Binned  int      `json:"binned"`
	Kept    []string `json:"kept,omitempty"` // folders that could not be moved to the bin, by name
	Problem string   `json:"problem,omitempty"`
}

func readIDs(w http.ResponseWriter, r *http.Request) ([]string, bool) {
	var req deleteRequest
	if !readBody(w, r, &req) {
		return nil, false
	}
	switch {
	case len(req.IDs) == 0:
		fail(w, http.StatusBadRequest, errors.New("choose a paper"))
		return nil, false
	case len(req.IDs) > 1000:
		fail(w, http.StatusBadRequest, errors.New("choose at most 1,000 papers at once"))
		return nil, false
	}
	return req.IDs, true
}

func (s *server) deletePlan(w http.ResponseWriter, r *http.Request) {
	ids, ok := readIDs(w, r)
	if !ok {
		return
	}
	p, err := s.Library.DeletePlan(r.Context(), ids)
	if err != nil {
		failFind(w, err)
		return
	}
	send(w, deletePlanJSON{Papers: p.Papers, PDFs: p.PDFs, Notes: p.Notes, Collections: p.Collections, Labels: p.Labels, Orphans: p.Orphans})
}

func (s *server) deletePapers(w http.ResponseWriter, r *http.Request) {
	ids, ok := readIDs(w, r)
	if !ok {
		return
	}
	// Held as a job is started, so that a job cannot begin halfway through,
	// and refused while one runs: an expansion could bring a paper back as
	// it goes.
	s.work.mu.Lock()
	if s.work.job.Running {
		s.work.mu.Unlock()
		fail(w, http.StatusConflict, errors.New("wait for the running job to finish, or stop it, before deleting papers"))
		return
	}
	d, err := s.Library.DeletePapers(r.Context(), ids)
	s.work.mu.Unlock()
	if err != nil {
		failFind(w, err)
		return
	}
	out := deletedJSON{Papers: d.Papers, Orphans: d.Orphans}
	for _, f := range d.Folders {
		if s.Trash == nil {
			out.Kept = append(out.Kept, filepath.Base(f))
			continue
		}
		if err := s.Trash(f); err != nil {
			out.Kept = append(out.Kept, filepath.Base(f))
			out.Problem = err.Error()
			continue
		}
		out.Binned++
	}
	send(w, out)
}
