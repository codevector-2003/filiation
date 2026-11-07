package library

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/codevector-2003/filiation/internal/errs"
	"github.com/codevector-2003/filiation/internal/model"
	"github.com/codevector-2003/filiation/internal/store"
)

// groupsTest is a library of two topics, six papers each, every paper citing
// the earlier ones of its topic, joined by one citation, plus a reference only
// one paper cites. W1 and W11 are cited most within their topics.
func groupsTest(t *testing.T) *Library {
	t.Helper()
	l, _ := openTest(t, nil, Options{})
	ctx := t.Context()
	topic := func(first int, word string, refs map[int][]string) {
		for i := first; i < first+6; i++ {
			var cites []string
			for j := first; j < i; j++ {
				cites = append(cites, fmt.Sprintf("W%d", j))
			}
			cites = append(cites, refs[i]...)
			h := &model.HydratedWork{Work: model.Work{OpenAlexID: fmt.Sprintf("W%d", i),
				Title: model.Ptr(fmt.Sprintf("On %s, part %d", word, i)), Year: model.Ptr(2000 + i), Hydrated: true, Depth: model.Ptr(0)},
				ReferencedWorks: cites}
			err := l.db.Tx(ctx, func(tx *store.Tx) error {
				if err := tx.UpsertWork(ctx, &h.Work); err != nil {
					return err
				}
				_, err := tx.RecordEdgesAndStubs(ctx, h)
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	topic(1, "hedgerows", map[int][]string{3: {"W99"}})
	topic(11, "moths", map[int][]string{16: {"W1"}})
	return l
}

func TestCitationGroupsSuggestCollections(t *testing.T) {
	t.Parallel()
	l := groupsTest(t)
	ctx := t.Context()

	all, err := l.CitationGroups(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Groups) != 2 || all.Total != 13 || all.Linked != 12 || all.Fringe != 1 || all.Detail != "normal" {
		t.Fatalf("groups = %+v", all)
	}
	var hedgerows CitationGroup
	for _, g := range all.Groups {
		if strings.Contains(g.Members[0].Title, "hedgerows") {
			hedgerows = g
		}
	}
	if hedgerows.Name != "Around On hedgerows, part 1 2001" || len(hedgerows.Members) != 6 || hedgerows.Fetched != 6 ||
		hedgerows.YearFrom != 2001 || hedgerows.YearTo != 2006 || len(hedgerows.Fringe) != 1 {
		t.Fatalf("the hedgerows group = %+v", hedgerows)
	}
	if _, err := l.CitationGroups(ctx, "tiny"); !errors.Is(err, errs.ErrInvalidInput) {
		t.Errorf("an unknown detail = %v", err)
	}

	// Found by its anchor, its name or its place.
	for _, which := range []string{hedgerows.Anchor, strings.ToUpper(hedgerows.Name)} {
		if g, err := l.CitationGroup(ctx, which, "normal"); err != nil || g.Anchor != hedgerows.Anchor {
			t.Errorf("CitationGroup(%q) = %s, %v", which, g.Anchor, err)
		}
	}
	if _, err := l.CitationGroup(ctx, "Around nobody", ""); !errors.Is(err, ErrNoGroup) || !errors.Is(err, errs.ErrNotFound) {
		t.Errorf("a group not found = %v", err)
	}

	// Made a collection, with its fringe: seven papers filed, and remembered.
	id, err := l.MakeGroupCollection(ctx, hedgerows.Anchor, "", "Hedgerows", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if rows, _ := l.List(ctx, ListOptions{CollectionID: id}); len(rows) != 7 {
		t.Errorf("the collection holds %d papers; want 7", len(rows))
	}
	if g, _ := l.CitationGroup(ctx, hedgerows.Anchor, ""); g.CollectionID != id {
		t.Errorf("the group's collection = %d; want %d", g.CollectionID, id)
	}

	// The Papers list keeps to a group's papers.
	if rows, _ := l.List(ctx, ListOptions{Among: hedgerows.IDs(), AmongSet: true, YearFrom: 2004}); len(rows) != 3 {
		t.Errorf("the group from 2004 = %d papers; want 3", len(rows))
	}
	if rows, _ := l.List(ctx, ListOptions{AmongSet: true}); len(rows) != 0 {
		t.Errorf("among no papers = %d; want none", len(rows))
	}

	// Dismissed, and brought back.
	if err := l.DismissGroup(ctx, hedgerows.Anchor, true); err != nil {
		t.Fatal(err)
	}
	if all, _ := l.CitationGroups(ctx, ""); all.Dismissed != 1 {
		t.Errorf("dismissed = %d; want 1", all.Dismissed)
	}
	if err := l.DismissGroup(ctx, hedgerows.Anchor, false); err != nil {
		t.Fatal(err)
	}
	if err := l.DismissGroup(ctx, "F00000000", true); !errors.Is(err, ErrNoGroup) {
		t.Errorf("dismissing a paper not in the library = %v", err)
	}
}
