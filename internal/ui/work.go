package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/codevector-2003/filiation/internal/errs"
	"github.com/codevector-2003/filiation/internal/library"
	"github.com/codevector-2003/filiation/internal/model"
)

// fetchBatch is how many PDFs one press of Fetch PDFs tries for.
const fetchBatch = 25

// work is what the app does that takes a while. One job runs at a time: the
// screens poll it, and the library keeps its single writer either way.
type work struct {
	mu     sync.Mutex
	job    jobJSON
	cancel context.CancelFunc

	inbox   []*inboxItem
	nextID  int
	skipped map[string]bool // files the person passed over

	offered []library.Candidate // what the last Add by title offered
}

type jobJSON struct {
	Kind    string `json:"kind,omitempty"` // fetch | import
	Running bool   `json:"running"`
	Done    int    `json:"done"`
	Total   int    `json:"total"`
	Note    string `json:"note,omitempty"`
	Summary string `json:"summary,omitempty"`
	Error   string `json:"error,omitempty"`
	Run     int    `json:"run"` // counts jobs, so a page can tell one from the last
}

// start runs fn as the job, or answers that one is already running.
func (s *server) start(w http.ResponseWriter, kind string, fn func(ctx context.Context, set func(done, total int, note string)) (string, error)) {
	s.work.mu.Lock()
	defer s.work.mu.Unlock()
	if s.work.job.Running {
		fail(w, http.StatusConflict, errors.New("something is already running: wait for it, or stop it"))
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.work.cancel = cancel
	s.work.job = jobJSON{Kind: kind, Running: true, Run: s.work.job.Run + 1}
	set := func(done, total int, note string) {
		s.work.mu.Lock()
		defer s.work.mu.Unlock()
		s.work.job.Done, s.work.job.Total, s.work.job.Note = done, total, note
	}
	go func() {
		defer cancel()
		summary, err := fn(ctx, set)
		s.work.mu.Lock()
		defer s.work.mu.Unlock()
		s.work.job.Running, s.work.job.Note, s.work.job.Summary = false, "", summary
		switch {
		case errors.Is(err, context.Canceled):
			s.work.job.Summary = "Stopped. What was finished is kept."
		case err != nil:
			s.work.job.Error = err.Error()
		}
	}()
	w.WriteHeader(http.StatusAccepted)
}

func (s *server) jobStatus(w http.ResponseWriter, _ *http.Request) {
	s.work.mu.Lock()
	defer s.work.mu.Unlock()
	send(w, s.work.job)
}

func (s *server) jobStop(w http.ResponseWriter, _ *http.Request) {
	s.work.mu.Lock()
	defer s.work.mu.Unlock()
	if s.work.job.Running {
		s.work.cancel()
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) fetch(w http.ResponseWriter, _ *http.Request) {
	s.start(w, "fetch", func(ctx context.Context, set func(int, int, string)) (string, error) {
		set(0, fetchBatch, "Looking for open-access PDFs")
		f, err := s.Library.Fetch(ctx, library.FetchOptions{Max: fetchBatch,
			Progress:     func(f library.Fetched) { set(f.Tried, fetchBatch, "Downloading open-access PDFs") },
			ReadProgress: func(library.Extracted) { set(0, 0, "Reading the PDFs") }})
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%d of %d downloaded", f.Downloaded, f.Tried), nil
	})
}

// addRequest is a paper to add: what was typed, or which offer was chosen.
type addRequest struct {
	Input     string `json:"input"`
	Candidate *int   `json:"candidate"`
}

type offerJSON struct {
	Index int    `json:"index"`
	Title string `json:"title"`
	By    string `json:"by"`
}

func (s *server) add(w http.ResponseWriter, r *http.Request) {
	var req addRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	var added library.Added
	var err error
	if req.Candidate != nil {
		s.work.mu.Lock()
		offered := s.work.offered
		s.work.mu.Unlock()
		if *req.Candidate < 0 || *req.Candidate >= len(offered) {
			fail(w, http.StatusBadRequest, errors.New("that choice is no longer on offer"))
			return
		}
		added, err = s.Library.AddCandidate(r.Context(), offered[*req.Candidate])
	} else {
		added, err = s.Library.Add(r.Context(), strings.TrimSpace(req.Input), library.AddOptions{})
	}
	var many *library.AmbiguousError
	switch {
	case errors.As(err, &many):
		s.work.mu.Lock()
		s.work.offered = many.Candidates
		s.work.mu.Unlock()
		offers := make([]offerJSON, len(many.Candidates))
		for i, c := range many.Candidates {
			offers[i] = offerJSON{i, c.Work.DisplayTitle(), workBy(c.Work)}
		}
		send(w, map[string]any{"offers": offers})
	case errors.Is(err, errs.ErrUnresolved), errors.Is(err, errs.ErrNotFound):
		fail(w, http.StatusNotFound, errors.New("OpenAlex has no record of that"))
	case errors.Is(err, errs.ErrInvalidInput):
		fail(w, http.StatusBadRequest, err)
	case err != nil:
		fail(w, http.StatusBadGateway, err)
	default:
		send(w, map[string]string{"id": added.Work.FilID, "title": added.Work.DisplayTitle()})
	}
}

// workBy is "Piwowar · 2018", as far as it is known.
func workBy(w model.Work) string {
	var parts []string
	if len(w.Authors) > 0 {
		if f := strings.Fields(w.Authors[0].Name); len(f) > 0 {
			parts = append(parts, f[len(f)-1])
		}
	}
	if w.Year != nil {
		parts = append(parts, strconv.Itoa(*w.Year))
	}
	return strings.Join(parts, " · ")
}

// inboxItem is a PDF an import looked at.
type inboxItem struct {
	id     int
	result library.ImportedFile
}

type candidateJSON struct {
	Index    int    `json:"index"`
	Title    string `json:"title"`
	By       string `json:"by"`
	Year     int    `json:"year,omitempty"`
	Type     string `json:"type,omitempty"`
	Verified bool   `json:"verified"`
}

type inboxJSON struct {
	ID         int             `json:"id"`
	Name       string          `json:"name"`
	State      string          `json:"state"` // filed | choose | unknown
	Reason     string          `json:"reason,omitempty"`
	Clue       string          `json:"clue,omitempty"`
	Title      string          `json:"title,omitempty"`
	PaperID    string          `json:"paperId,omitempty"`
	Candidates []candidateJSON `json:"candidates"`
}

func itemJSON(it *inboxItem) inboxJSON {
	f := it.result
	out := inboxJSON{ID: it.id, Name: filepath.Base(f.File), Reason: f.Reason, Clue: f.Clue, Candidates: []candidateJSON{}}
	switch f.Outcome {
	case library.ImportImported, library.ImportDuplicate:
		out.State, out.PaperID = "filed", f.Work.FilID
		if f.Work.FilID != "" {
			out.Title = f.Work.DisplayTitle()
		}
		if f.Outcome == library.ImportDuplicate {
			out.Reason = "Already in the library"
		}
	case library.ImportUnconfirmed, library.ImportCandidates:
		out.State = "choose"
	default:
		out.State = "unknown"
	}
	for i, c := range f.Candidates {
		j := candidateJSON{Index: i, Title: c.Work.DisplayTitle(), By: workBy(c.Work), Type: string(c.Work.Type), Verified: c.Verified}
		if c.Work.Year != nil {
			j.Year = *c.Work.Year
		}
		out.Candidates = append(out.Candidates, j)
	}
	return out
}

func (s *server) inboxList(w http.ResponseWriter, _ *http.Request) {
	waiting, _ := s.Library.InboxFiles()
	s.work.mu.Lock()
	defer s.work.mu.Unlock()
	items := make([]inboxJSON, 0, len(s.work.inbox))
	seen := map[string]bool{}
	for _, it := range s.work.inbox {
		items = append(items, itemJSON(it))
		// A filed PDF has left, so a new file of that name is a new file. A
		// duplicate stays where it was.
		if it.result.Outcome != library.ImportImported {
			seen[it.result.File] = true
		}
	}
	// Only files not yet looked at are fresh: each look can spend a title search.
	fresh := false
	for _, f := range waiting {
		fresh = fresh || (!seen[f] && !s.work.skipped[f])
	}
	send(w, map[string]any{"items": items, "fresh": fresh})
}

// remember records what an import did to a file, replacing what was known.
func (s *server) remember(f library.ImportedFile) {
	s.work.mu.Lock()
	defer s.work.mu.Unlock()
	for _, it := range s.work.inbox {
		if it.result.File == f.File {
			it.result = f
			return
		}
	}
	s.work.nextID++
	s.work.inbox = append(s.work.inbox, &inboxItem{id: s.work.nextID, result: f})
}

// runImport identifies files, or everything waiting when files is empty.
func (s *server) runImport(w http.ResponseWriter, files []string) {
	s.start(w, "import", func(ctx context.Context, set func(int, int, string)) (string, error) {
		seen := 0
		set(0, len(files), "Identifying PDFs")
		res, err := s.Library.Import(ctx, library.ImportOptions{Files: files, Again: true,
			Progress: func(f library.ImportedFile) {
				s.remember(f)
				seen++
				set(seen, len(files), "Identifying PDFs")
			}})
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%d filed", res.Counts[library.ImportImported]), nil
	})
}

func (s *server) inboxImport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Files []string `json:"files"`
	}
	// No body at all means everything waiting.
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		fail(w, http.StatusBadRequest, err)
		return
	}
	var pdfs []string
	for _, f := range req.Files {
		if isPDF(f) {
			pdfs = append(pdfs, f)
		}
	}
	if len(req.Files) > 0 && len(pdfs) == 0 {
		fail(w, http.StatusBadRequest, errors.New("only PDF files can be added"))
		return
	}
	s.runImport(w, pdfs)
}

func (s *server) inboxChoose(w http.ResponseWriter, r *http.Request) {
	if s.PickFiles == nil {
		fail(w, http.StatusNotImplemented, errors.New("choosing files needs the desktop app"))
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
	s.runImport(w, files)
}

func (s *server) inboxFolder(w http.ResponseWriter, _ *http.Request) {
	root := s.Library.Folder()
	if root == "" {
		fail(w, http.StatusConflict, errors.New("this library has no folder"))
		return
	}
	s.open(w, filepath.Join(root, "Inbox"))
}

func (s *server) item(id string) *inboxItem {
	n, err := strconv.Atoi(id)
	if err != nil {
		return nil
	}
	s.work.mu.Lock()
	defer s.work.mu.Unlock()
	for _, it := range s.work.inbox {
		if it.id == n {
			return it
		}
	}
	return nil
}

// fileRequest says what an Inbox PDF is: one of the candidates, a paper named
// by its identifier, or a local document with a title.
type fileRequest struct {
	Candidate *int   `json:"candidate"`
	Input     string `json:"input"`
	Title     string `json:"title"`
}

func (s *server) inboxFile(w http.ResponseWriter, r *http.Request) {
	it := s.item(r.PathValue("id"))
	if it == nil {
		fail(w, http.StatusNotFound, errors.New("that file is no longer in the list"))
		return
	}
	var req fileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	var f library.ImportedFile
	var err error
	file := it.result.File
	switch {
	case req.Candidate != nil:
		if *req.Candidate < 0 || *req.Candidate >= len(it.result.Candidates) {
			fail(w, http.StatusBadRequest, errors.New("that choice is no longer on offer"))
			return
		}
		f, err = s.Library.ImportChosen(r.Context(), file, it.result.Candidates[*req.Candidate])
	case strings.TrimSpace(req.Input) != "":
		f, err = s.Library.ImportAs(r.Context(), file, strings.TrimSpace(req.Input), false)
	default:
		f, err = s.Library.ImportLocal(r.Context(), file, req.Title, nil)
	}
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	f.File = file // the list keeps its place, wherever the PDF went
	s.remember(f)
	send(w, itemJSON(it))
}

func (s *server) inboxSkip(w http.ResponseWriter, r *http.Request) {
	it := s.item(r.PathValue("id"))
	if it != nil {
		s.work.mu.Lock()
		for i, o := range s.work.inbox {
			if o == it {
				s.work.inbox = append(s.work.inbox[:i], s.work.inbox[i+1:]...)
				if s.work.skipped == nil {
					s.work.skipped = map[string]bool{}
				}
				s.work.skipped[it.result.File] = true
				break
			}
		}
		s.work.mu.Unlock()
	}
	w.WriteHeader(http.StatusNoContent)
}
