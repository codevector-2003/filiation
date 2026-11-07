package ui

import (
	"errors"
	"net/http"

	"github.com/codevector-2003/filiation/internal/errs"
	"github.com/codevector-2003/filiation/internal/library"
)

// Citation groups: topics found from the citations alone, suggested as
// collections (D34).

type groupPaperJSON struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	By    string `json:"by"`
}

type groupJSON struct {
	Anchor     string           `json:"anchor"`
	Name       string           `json:"name"`
	Papers     int              `json:"papers"`
	Fetched    int              `json:"fetched"`
	YearFrom   int              `json:"yearFrom,omitempty"`
	YearTo     int              `json:"yearTo,omitempty"`
	Top        []groupPaperJSON `json:"top"`
	Fringe     int              `json:"fringe"`
	Dismissed  bool             `json:"dismissed,omitempty"`
	Collection int64            `json:"collection,omitempty"` // made into this collection
	Members    []string         `json:"members,omitempty"`    // with ?members=1, for the map
}

// groupsTop is how many of a group's papers the Groups screen names.
const groupsTop = 3

func (s *server) groups(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	all, err := s.Library.CitationGroups(r.Context(), q.Get("detail"))
	if err != nil {
		change(w, 0, err)
		return
	}
	out := make([]groupJSON, len(all.Groups))
	for i, g := range all.Groups {
		j := groupJSON{Anchor: g.Anchor, Name: g.Name, Papers: len(g.Members), Fetched: g.Fetched, YearFrom: g.YearFrom,
			YearTo: g.YearTo, Top: []groupPaperJSON{}, Fringe: len(g.Fringe), Dismissed: g.Dismissed, Collection: g.CollectionID}
		// The most cited papers whose titles are known: a bare ID says nothing.
		for _, m := range g.Members {
			if len(j.Top) == groupsTop {
				break
			}
			if m.Title != "" {
				j.Top = append(j.Top, groupPaperJSON{ID: m.FilID, Title: m.Title, By: rowBy(m)})
			}
		}
		if q.Get("members") == "1" {
			j.Members = g.IDs()
		}
		out[i] = j
	}
	send(w, map[string]any{"groups": out, "detail": all.Detail, "total": all.Total, "linked": all.Linked,
		"small": all.Small, "fringe": all.Fringe, "dismissed": all.Dismissed})
}

type groupRequest struct {
	Detail    string `json:"detail"`
	Name      string `json:"name"`
	Parent    int64  `json:"parent"`
	Fringe    bool   `json:"fringe"`
	Dismissed bool   `json:"dismissed"`
}

func (s *server) groupCollection(w http.ResponseWriter, r *http.Request) {
	var req groupRequest
	if !readBody(w, r, &req) {
		return
	}
	id, err := s.Library.MakeGroupCollection(r.Context(), r.PathValue("anchor"), req.Detail, req.Name, req.Parent, req.Fringe)
	change(w, id, err)
}

func (s *server) dismissGroup(w http.ResponseWriter, r *http.Request) {
	var req groupRequest
	if !readBody(w, r, &req) {
		return
	}
	change(w, 0, s.Library.DismissGroup(r.Context(), r.PathValue("anchor"), req.Dismissed))
}

// keepToGroup narrows a listing to one citation group's papers when the
// request names one, with ?group= and ?detail=. It answers the request itself
// and returns false when the group cannot be read.
func (s *server) keepToGroup(w http.ResponseWriter, r *http.Request, opts *library.ListOptions) bool {
	q := r.URL.Query()
	if q.Get("group") == "" {
		return true
	}
	g, err := s.Library.CitationGroup(r.Context(), q.Get("group"), q.Get("detail"))
	switch {
	case errors.Is(err, errs.ErrNotFound):
		fail(w, http.StatusNotFound, errors.New("that group is not in the library any more"))
		return false
	case err != nil:
		change(w, 0, err)
		return false
	}
	opts.Among, opts.AmongSet = g.IDs(), true
	return true
}
