package ui

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/codevector-2003/filiation/internal/errs"
)

// The sidebar's collections and labels, and the star. Every change is one
// call into the library, which keeps the rules: a folder's collection is the
// folder's, and nothing here deletes a paper.

type collectionJSON struct {
	ID     int64  `json:"id"`
	Parent int64  `json:"parent"`
	Name   string `json:"name"`  // inside its parent
	Path   string `json:"path"`  // as stored: what fil list --collection takes
	Works  int    `json:"works"` // counting those in collections inside it
	Folder bool   `json:"folder"`
	Zotero bool   `json:"zotero"` // brought in from Zotero
}

type labelJSON struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Colour string `json:"colour"`
	Works  int    `json:"works"`
}

// organised fills in the sidebar's part of the library.
func (s *server) organised(r *http.Request, out *libraryJSON) error {
	ctx := r.Context()
	cs, err := s.Library.Collections(ctx)
	if err != nil {
		return err
	}
	for _, c := range cs {
		out.Collections = append(out.Collections, collectionJSON{ID: c.ID, Parent: c.Parent, Name: c.Short,
			Path: c.Name, Works: c.Works, Folder: c.FromFolder, Zotero: c.Zotero != ""})
	}
	ls, err := s.Library.Labels(ctx)
	if err != nil {
		return err
	}
	for _, l := range ls {
		out.Labels = append(out.Labels, labelJSON{ID: l.ID, Name: l.Name, Colour: l.Colour, Works: l.Works})
	}
	out.Favourites, err = s.Library.Favourites(ctx)
	return err
}

// change answers a change: the new ID when there is one, or why it was
// refused, in words a person can act on.
func change(w http.ResponseWriter, id int64, err error) {
	switch {
	case errors.Is(err, errs.ErrInvalidInput):
		fail(w, http.StatusBadRequest, errors.New(reason(err, errs.ErrInvalidInput)))
	case errors.Is(err, errs.ErrNotFound):
		fail(w, http.StatusNotFound, errors.New(reason(err, errs.ErrNotFound)))
	case err != nil:
		fail(w, http.StatusInternalServerError, err)
	case id != 0:
		send(w, map[string]int64{"id": id})
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// reason is an error's message without the sentinel's name on the end.
func reason(err, sentinel error) string {
	return strings.TrimSuffix(err.Error(), ": "+sentinel.Error())
}

// organiseJSON is any one change's request. Each route reads the fields it
// needs.
type organiseJSON struct {
	Name   string   `json:"name"`
	Parent int64    `json:"parent"`
	Colour string   `json:"colour"`
	Papers []string `json:"papers"`
	On     bool     `json:"on"`
}

// organise reads a change: the {id} in its route, when it has one, and its
// body.
func organise(w http.ResponseWriter, r *http.Request) (int64, organiseJSON, bool) {
	var id int64
	if v := r.PathValue("id"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n <= 0 {
			fail(w, http.StatusBadRequest, errors.New("no such collection or label"))
			return 0, organiseJSON{}, false
		}
		id = n
	}
	var req organiseJSON
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, err)
		return 0, req, false
	}
	return id, req, true
}

func (s *server) createCollection(w http.ResponseWriter, r *http.Request) {
	if _, req, ok := organise(w, r); ok {
		id, err := s.Library.CreateCollection(r.Context(), req.Name, req.Parent)
		change(w, id, err)
	}
}

func (s *server) renameCollection(w http.ResponseWriter, r *http.Request) {
	if id, req, ok := organise(w, r); ok {
		change(w, 0, s.Library.RenameCollection(r.Context(), id, req.Name))
	}
}

func (s *server) moveCollection(w http.ResponseWriter, r *http.Request) {
	if id, req, ok := organise(w, r); ok {
		change(w, 0, s.Library.MoveCollection(r.Context(), id, req.Parent))
	}
}

func (s *server) deleteCollection(w http.ResponseWriter, r *http.Request) {
	if id, _, ok := organise(w, r); ok {
		change(w, 0, s.Library.DeleteCollection(r.Context(), id))
	}
}

func (s *server) fileInCollection(w http.ResponseWriter, r *http.Request) {
	if id, req, ok := organise(w, r); ok {
		change(w, 0, s.Library.FileInCollection(r.Context(), id, req.Papers, req.On))
	}
}

func (s *server) createLabel(w http.ResponseWriter, r *http.Request) {
	if _, req, ok := organise(w, r); ok {
		id, err := s.Library.CreateLabel(r.Context(), req.Name, req.Colour)
		change(w, id, err)
	}
}

func (s *server) changeLabel(w http.ResponseWriter, r *http.Request) {
	if id, req, ok := organise(w, r); ok {
		change(w, 0, s.Library.ChangeLabel(r.Context(), id, req.Name, req.Colour))
	}
}

func (s *server) deleteLabel(w http.ResponseWriter, r *http.Request) {
	if id, _, ok := organise(w, r); ok {
		change(w, 0, s.Library.DeleteLabel(r.Context(), id))
	}
}

func (s *server) setLabel(w http.ResponseWriter, r *http.Request) {
	if id, req, ok := organise(w, r); ok {
		change(w, 0, s.Library.SetLabel(r.Context(), id, req.Papers, req.On))
	}
}

func (s *server) setFavourite(w http.ResponseWriter, r *http.Request) {
	if _, req, ok := organise(w, r); ok {
		change(w, 0, s.Library.SetFavourite(r.Context(), req.Papers, req.On))
	}
}
