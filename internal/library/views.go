package library

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/codevector-2003/filiation/internal/errs"
	"github.com/codevector-2003/filiation/internal/store"
)

// ErrNoView is a view named that is not saved. It is also errs.ErrNotFound.
var ErrNoView = fmt.Errorf("no such view: %w", errs.ErrNotFound)

// ViewFilters are what a saved view keeps: the Papers list's filters and its
// order. A view is a question asked again each time it is opened, so a paper
// that matches later is found by it; nothing is filed anywhere (D33).
type ViewFilters struct {
	Show         string   `json:"show,omitempty"` // "", seeds, pdf, notext: the Papers list's tabs
	Sort         string   `json:"sort,omitempty"`
	Title        string   `json:"title,omitempty"`
	CollectionID int64    `json:"collection,omitempty"`
	Favourites   bool     `json:"favourites,omitempty"`
	Labels       []int64  `json:"labels,omitempty"`
	YearFrom     int      `json:"yearFrom,omitempty"`
	YearTo       int      `json:"yearTo,omitempty"`
	Authors      []string `json:"authors,omitempty"`
	Venues       []string `json:"venues,omitempty"`
}

// List is the listing the filters ask for.
func (f ViewFilters) List() ListOptions {
	return ListOptions{Seeds: f.Show == "seeds", WithPDF: f.Show == "pdf", WithoutText: f.Show == "notext",
		Sort: f.Sort, Title: f.Title, CollectionID: f.CollectionID, Favourites: f.Favourites, Labels: f.Labels,
		YearFrom: f.YearFrom, YearTo: f.YearTo, Authors: f.Authors, Venues: f.Venues}
}

// Empty reports filters that keep every paper, which make no view.
func (f ViewFilters) Empty() bool {
	return f.Show == "" && f.Title == "" && f.CollectionID == 0 && !f.Favourites && len(f.Labels) == 0 &&
		f.YearFrom == 0 && f.YearTo == 0 && len(f.Authors) == 0 && len(f.Venues) == 0
}

// View is a saved view, and how many papers it finds now.
type View struct {
	ID      int64
	Name    string
	Filters ViewFilters
	Papers  int
}

// Views returns the saved views, by name, each with what it finds now.
func (l *Library) Views(ctx context.Context) ([]View, error) {
	saved, err := l.db.Views(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]View, 0, len(saved))
	for _, s := range saved {
		v, err := l.counted(ctx, s)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// ViewNamed finds a view by its name or its number.
func (l *Library) ViewNamed(ctx context.Context, nameOrID string) (View, error) {
	if id, err := strconv.ParseInt(nameOrID, 10, 64); err == nil {
		s, err := l.db.View(ctx, id)
		if err == nil {
			return l.counted(ctx, s)
		}
	}
	saved, err := l.db.Views(ctx)
	if err != nil {
		return View{}, err
	}
	for _, s := range saved {
		if strings.EqualFold(s.Name, strings.TrimSpace(nameOrID)) {
			return l.counted(ctx, s)
		}
	}
	return View{}, fmt.Errorf("%q: %w", nameOrID, ErrNoView)
}

func (l *Library) counted(ctx context.Context, s store.SavedView) (View, error) {
	v := View{ID: s.ID, Name: s.Name}
	if err := json.Unmarshal([]byte(s.Filters), &v.Filters); err != nil {
		return View{}, fmt.Errorf("view %q: %w", s.Name, err)
	}
	f, err := l.db.Facets(ctx, v.Filters.List(), store.FacetFind{})
	if err != nil {
		return View{}, err
	}
	v.Papers = f.Total
	return v, nil
}

// SaveView saves filters under name.
func (l *Library) SaveView(ctx context.Context, name string, f ViewFilters) (View, error) {
	if f.Empty() {
		return View{}, fmt.Errorf("these filters keep every paper; choose some first: %w", errs.ErrInvalidInput)
	}
	raw, err := json.Marshal(f)
	if err != nil {
		return View{}, err
	}
	var id int64
	err = l.db.Tx(ctx, func(tx *store.Tx) error {
		id, err = tx.CreateView(ctx, name, string(raw))
		return err
	})
	if err != nil {
		return View{}, err
	}
	return l.counted(ctx, store.SavedView{ID: id, Name: strings.TrimSpace(name), Filters: string(raw)})
}

// ChangeView renames a view when name is not "", and gives it new filters
// when f is not nil.
func (l *Library) ChangeView(ctx context.Context, id int64, name string, f *ViewFilters) error {
	raw := ""
	if f != nil {
		if f.Empty() {
			return fmt.Errorf("these filters keep every paper; choose some first: %w", errs.ErrInvalidInput)
		}
		b, err := json.Marshal(*f)
		if err != nil {
			return err
		}
		raw = string(b)
	}
	return l.db.Tx(ctx, func(tx *store.Tx) error { return tx.ChangeView(ctx, id, name, raw) })
}

// DeleteView forgets a view; its papers are not touched.
func (l *Library) DeleteView(ctx context.Context, id int64) error {
	return l.db.Tx(ctx, func(tx *store.Tx) error { return tx.DeleteView(ctx, id) })
}
