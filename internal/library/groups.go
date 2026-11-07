package library

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/codevector-2003/filiation/internal/errs"
	"github.com/codevector-2003/filiation/internal/graph"
	"github.com/codevector-2003/filiation/internal/store"
)

// ErrNoGroup is a citation group asked for that the library does not have, at
// that detail. It is also errs.ErrNotFound.
var ErrNoGroup = fmt.Errorf("no such citation group: %w", errs.ErrNotFound)

// GroupDetails are how fine the citation groups are cut, and the Louvain
// resolution each stands for: higher gives more, smaller groups.
var GroupDetails = map[string]float64{"broad": 0.5, "normal": 1, "fine": 2}

// resolution reads a detail, "" being normal.
func resolution(detail string) (float64, error) {
	if detail == "" {
		detail = "normal"
	}
	r, ok := GroupDetails[strings.ToLower(detail)]
	if !ok {
		return 0, fmt.Errorf("no group detail %q; choose broad, normal or fine: %w", detail, errs.ErrInvalidInput)
	}
	return r, nil
}

// CitationGroup is a cluster of papers that cite one another more than the
// rest of the library, suggested as a collection (D34). It is worked out from
// the citations each time, so a paper added later joins it.
type CitationGroup struct {
	Anchor       string   // the paper the others cite most; the group's key
	Name         string   // "Around Ioannidis 2005"
	Members      []Listed // most cited within the group first
	CitedWithin  map[string]int
	Fringe       []string // linked to this group and nothing else, not grouped
	Fetched      int      // members whose OpenAlex record has been read
	YearFrom     int
	YearTo       int
	Dismissed    bool  // marked not useful
	CollectionID int64 // the collection made from it, if one was and still exists
}

// IDs are the members' fil IDs.
func (g CitationGroup) IDs() []string {
	out := make([]string, len(g.Members))
	for i, m := range g.Members {
		out[i] = m.FilID
	}
	return out
}

// CitationGroups is every group, and the papers left out of them.
type CitationGroups struct {
	Groups    []CitationGroup // largest first; dismissed ones included, marked
	Detail    string
	Total     int // papers in the library
	Linked    int // linked to at least two others, so grouped
	Small     int // of those, in clusters too small to suggest
	Fringe    int // linked to exactly one other
	Dismissed int
}

// CitationGroups clusters the library by its citations at a detail: broad,
// normal ("") or fine.
func (l *Library) CitationGroups(ctx context.Context, detail string) (CitationGroups, error) {
	r, err := resolution(detail)
	if err != nil {
		return CitationGroups{}, err
	}
	if detail == "" {
		detail = "normal"
	}
	rows, err := l.db.List(ctx, store.ListQuery{Sort: "arrived"})
	if err != nil {
		return CitationGroups{}, err
	}
	citations, err := l.Citations(ctx)
	if err != nil {
		return CitationGroups{}, err
	}
	choices, err := l.db.GroupChoices(ctx)
	if err != nil {
		return CitationGroups{}, err
	}
	byID := make(map[string]Listed, len(rows))
	for _, row := range rows {
		byID[row.FilID] = row
	}
	found := graph.FindGroups(citations, r)
	out := CitationGroups{Detail: strings.ToLower(detail), Total: len(rows), Linked: found.Linked, Small: found.Small, Fringe: found.Fringe}
	named := map[string]int{}
	for _, f := range found.Groups {
		g := CitationGroup{Anchor: f.Anchor, CitedWithin: f.CitedWithin, Fringe: f.Fringe}
		for _, id := range f.Members {
			m, ok := byID[id]
			if !ok {
				m = Listed{FilID: id}
			}
			g.Members = append(g.Members, m)
			if m.Hydrated {
				g.Fetched++
			}
			if m.Year > 0 && (g.YearFrom == 0 || m.Year < g.YearFrom) {
				g.YearFrom = m.Year
			}
			g.YearTo = max(g.YearTo, m.Year)
		}
		g.Name = groupName(named1(g.Members))
		named[g.Name]++
		if n := named[g.Name]; n > 1 {
			g.Name += " (" + strconv.Itoa(n) + ")"
		}
		if c, ok := choices[g.Anchor]; ok {
			g.Dismissed, g.CollectionID = c.Dismissed, c.CollectionID
		}
		if g.Dismissed {
			out.Dismissed++
		}
		out.Groups = append(out.Groups, g)
	}
	return out, nil
}

// named1 is the paper a group is named after: its most cited member whose
// title is known, since the anchor itself can be a reference not fetched yet.
func named1(members []Listed) Listed {
	for _, m := range members {
		if m.Title != "" {
			return m
		}
	}
	return members[0]
}

// groupName names a group after a paper: "Around Ioannidis 2005".
func groupName(anchor Listed) string {
	who := ""
	if f := strings.Fields(anchor.Author); len(f) > 0 {
		who = f[len(f)-1]
	} else if f := strings.Fields(anchor.Title); len(f) > 0 {
		who = strings.Join(f[:min(len(f), 5)], " ")
		if len(f) > 5 {
			who += "…"
		}
	} else if anchor.OpenAlexID != "" {
		who = anchor.OpenAlexID
	} else {
		who = anchor.FilID
	}
	if anchor.Year > 0 {
		who += " " + strconv.Itoa(anchor.Year)
	}
	return "Around " + who
}

// CitationGroup finds one group by its anchor's fil ID, its name, or its
// place in the list (1 is the largest).
func (l *Library) CitationGroup(ctx context.Context, which, detail string) (CitationGroup, error) {
	all, err := l.CitationGroups(ctx, detail)
	if err != nil {
		return CitationGroup{}, err
	}
	which = strings.TrimSpace(which)
	for _, g := range all.Groups {
		if g.Anchor == which || strings.EqualFold(g.Name, which) {
			return g, nil
		}
	}
	if n, err := strconv.Atoi(which); err == nil && n >= 1 && n <= len(all.Groups) {
		return all.Groups[n-1], nil
	}
	return CitationGroup{}, fmt.Errorf("%q: %w", which, ErrNoGroup)
}

// MakeGroupCollection files a group's papers in a new collection inside
// parent (0 for the top), with its fringe too when asked, and remembers the
// group was made one. It returns the collection's ID.
func (l *Library) MakeGroupCollection(ctx context.Context, which, detail, name string, parent int64, fringe bool) (int64, error) {
	g, err := l.CitationGroup(ctx, which, detail)
	if err != nil {
		return 0, err
	}
	if strings.TrimSpace(name) == "" {
		name = g.Name
	}
	name, err = cleanName(name)
	if err != nil {
		return 0, err
	}
	papers := g.IDs()
	if fringe {
		papers = append(papers, g.Fringe...)
	}
	var id int64
	err = l.db.Tx(ctx, func(tx *store.Tx) error {
		if id, err = tx.CreateCollection(ctx, name, parent); err != nil {
			return err
		}
		if err := tx.FileInCollection(ctx, id, papers, true); err != nil {
			return err
		}
		return tx.GroupMadeCollection(ctx, g.Anchor, id)
	})
	return id, err
}

// DismissGroup marks the group around anchor not useful, so it is no longer
// suggested, or brings it back.
func (l *Library) DismissGroup(ctx context.Context, anchor string, dismissed bool) error {
	if _, err := l.getWork(ctx, anchor); err != nil {
		return fmt.Errorf("%q: %w", anchor, ErrNoGroup)
	}
	return l.db.Tx(ctx, func(tx *store.Tx) error { return tx.DismissGroup(ctx, anchor, dismissed) })
}
