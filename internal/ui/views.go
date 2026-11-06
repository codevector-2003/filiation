package ui

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/codevector-2003/filiation/internal/library"
)

// Saved views: the Papers list's filters under a name (D33).

type viewJSON struct {
	ID      int64               `json:"id"`
	Name    string              `json:"name"`
	Papers  int                 `json:"papers"`
	Filters library.ViewFilters `json:"filters"`
}

func viewsOut(vs []library.View) []viewJSON {
	out := make([]viewJSON, len(vs))
	for i, v := range vs {
		out[i] = viewJSON{ID: v.ID, Name: v.Name, Papers: v.Papers, Filters: v.Filters}
	}
	return out
}

type viewRequest struct {
	Name    string               `json:"name"`
	Filters *library.ViewFilters `json:"filters"`
}

func (s *server) createView(w http.ResponseWriter, r *http.Request) {
	var req viewRequest
	if !readBody(w, r, &req) {
		return
	}
	var f library.ViewFilters
	if req.Filters != nil {
		f = *req.Filters
	}
	v, err := s.Library.SaveView(r.Context(), req.Name, f)
	change(w, v.ID, err)
}

func (s *server) changeView(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req viewRequest
	if !readBody(w, r, &req) {
		return
	}
	change(w, id, s.Library.ChangeView(r.Context(), id, req.Name, req.Filters))
}

func (s *server) deleteView(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	change(w, id, s.Library.DeleteView(r.Context(), id))
}

// searchWithin reads which papers a search keeps to: a saved view, the
// Papers list's filters, or both, a view's filters narrowed by the rest. Nil
// means the whole library.
func (s *server) searchWithin(r *http.Request, q url.Values) (*library.ListOptions, error) {
	opts, err := listOptions(q)
	if err != nil {
		return nil, err
	}
	if v := q.Get("view"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, err
		}
		view, err := s.Library.ViewNamed(r.Context(), strconv.FormatInt(id, 10))
		if err != nil {
			return nil, err
		}
		base := view.Filters.List()
		if opts.YearFrom > 0 {
			base.YearFrom = opts.YearFrom
		}
		if opts.YearTo > 0 {
			base.YearTo = opts.YearTo
		}
		base.Authors = append(base.Authors, opts.Authors...)
		base.Venues = append(base.Venues, opts.Venues...)
		return &base, nil
	}
	f := library.ViewFilters{Title: opts.Title, CollectionID: opts.CollectionID, Favourites: opts.Favourites,
		Labels: opts.Labels, YearFrom: opts.YearFrom, YearTo: opts.YearTo, Authors: opts.Authors, Venues: opts.Venues}
	if opts.Seeds || opts.WithPDF || opts.WithoutText || !f.Empty() {
		return &opts, nil
	}
	return nil, nil
}

// pathID reads a view's number from the path.
func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	n, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || n <= 0 {
		fail(w, http.StatusBadRequest, errors.New("no such view"))
		return 0, false
	}
	return n, true
}
