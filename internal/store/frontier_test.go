package store

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/codevector-2003/filiation/internal/errs"
	"github.com/codevector-2003/filiation/internal/model"
)

// ids lists the frontier's IDs in order.
func ids(items []model.FrontierItem) string {
	s := make([]string, len(items))
	for i, it := range items {
		s[i] = it.OpenAlexID
	}
	return strings.Join(s, ",")
}

func frontier(t *testing.T, db *DB, limit, maxDepth, refCap int, exclude map[string]bool) []model.FrontierItem {
	t.Helper()
	items, err := db.NextFrontier(t.Context(), limit, maxDepth, refCap, exclude)
	if err != nil {
		t.Fatalf("NextFrontier: %v", err)
	}
	return items
}

func TestNextFrontierIsBestFirst(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	// Three seeds. W20 is cited by all three, W21 by two, the rest by one.
	record(t, db, hydratedWith("W1", 0, "W20", "W21", "W30"))
	record(t, db, hydratedWith("W2", 0, "W20", "W21", "W31"))
	record(t, db, hydratedWith("W3", 0, "W20", "W32"))

	got := frontier(t, db, 10, 5, 0, nil)
	if want := "W20,W21,W30,W31,W32"; ids(got) != want {
		t.Errorf("frontier = %s, want %s — highest in-degree first, then by ID", ids(got), want)
	}
	if got[0].InDegree != 3 || got[1].InDegree != 2 || got[2].InDegree != 1 {
		t.Errorf("in-degrees = %d,%d,%d; want 3,2,1", got[0].InDegree, got[1].InDegree, got[2].InDegree)
	}
	if got[0].Depth != 1 {
		t.Errorf("depth = %d, want 1", got[0].Depth)
	}

	if got := frontier(t, db, 2, 5, 0, nil); ids(got) != "W20,W21" {
		t.Errorf("limit 2 = %s", ids(got))
	}
}

func TestNextFrontierPrefersShallowOnTies(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	record(t, db, hydratedWith("W1", 0, "W9")) // W9 at depth 1
	record(t, db, hydratedWith("W5", 2, "W8")) // W8 at depth 3
	if got := frontier(t, db, 10, 5, 0, nil); ids(got) != "W9,W8" {
		t.Errorf("frontier = %s, want the shallower tie first", ids(got))
	}
}

func TestNextFrontierFilters(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	record(t, db, hydratedWith("W1", 0, "W10", "W11", "W12"))
	record(t, db, hydratedWith("W2", 3, "W40")) // W40 at depth 4

	// W11 is hydrated, W12 unresolved: neither is a candidate.
	if err := db.UpsertWork(t.Context(), &model.Work{OpenAlexID: "W11"}); err != nil {
		t.Fatal(err)
	}
	err := db.Tx(t.Context(), func(tx *Tx) error { return tx.MarkUnresolved(t.Context(), "W12") })
	if err != nil {
		t.Fatal(err)
	}

	if got := frontier(t, db, 10, 3, 0, nil); ids(got) != "W10" {
		t.Errorf("frontier = %s, want W10 only — hydrated, unresolved and too-deep works excluded", ids(got))
	}
	if got := frontier(t, db, 10, 4, 0, nil); ids(got) != "W10,W40" {
		t.Errorf("maxDepth 4 = %s, want W40 admitted", ids(got))
	}
	if got := frontier(t, db, 10, 4, 0, map[string]bool{"W10": true}); ids(got) != "W40" {
		t.Errorf("excluding W10 = %s", ids(got))
	}

	beyond, err := db.StubsBeyond(t.Context(), 3)
	if err != nil || beyond != 1 {
		t.Errorf("StubsBeyond(3) = %d, %v; want 1 (W40)", beyond, err)
	}
}

func TestNextFrontierExcludesStubsWithNoDepth(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	h := hydratedWith("W1", 0, "W2")
	h.Depth = nil
	record(t, db, h)
	if got := frontier(t, db, 10, 5, 0, nil); len(got) != 0 {
		t.Errorf("frontier = %s, want empty — a stub at no known depth grows the graph from nowhere", ids(got))
	}
}

func TestNextFrontierCapsReferencesPerWork(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	// A review citing 20 works, and one ordinary paper citing the review's
	// last reference.
	refs := make([]string, 20)
	for i := range refs {
		refs[i] = fmt.Sprintf("W%d", 100+i)
	}
	record(t, db, hydratedWith("W1", 0, refs...))

	// Capped at 5: only the review's first five are candidates...
	got := frontier(t, db, 100, 5, 5, nil)
	if want := "W100,W101,W102,W103,W104"; ids(got) != want {
		t.Errorf("capped frontier = %s, want %s", ids(got), want)
	}
	// ...but every edge is still recorded.
	if s, _ := db.Stats(t.Context()); s.Edges != 20 {
		t.Errorf("edges = %d, want all 20 recorded", s.Edges)
	}

	// Another work ranks W119 within its own first five: now it qualifies,
	// and with two citers it leads.
	record(t, db, hydratedWith("W2", 0, "W119"))
	got = frontier(t, db, 100, 5, 5, nil)
	if got[0].OpenAlexID != "W119" || got[0].InDegree != 2 {
		t.Errorf("first = %+v, want W119 with in-degree 2", got[0])
	}
	if len(got) != 6 {
		t.Errorf("capped frontier has %d items, want 6", len(got))
	}

	if got := frontier(t, db, 100, 5, 0, nil); len(got) != 20 {
		t.Errorf("uncapped frontier has %d items, want 20", len(got))
	}
}

func TestMergeInto(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()
	// W1 cites W5, which OpenAlex has since merged into W6. W2 cites both.
	record(t, db, hydratedWith("W1", 0, "W5"))
	record(t, db, hydratedWith("W2", 0, "W5", "W6"))

	err := db.Tx(ctx, func(tx *Tx) error { return tx.MergeInto(ctx, "W5", "W6") })
	if err != nil {
		t.Fatalf("MergeInto: %v", err)
	}

	if _, err := db.GetWork(ctx, "W5"); err == nil {
		t.Errorf("W5 survived the merge")
	}
	citedBy, err := db.CitedBy(ctx, "W6")
	if err != nil {
		t.Fatal(err)
	}
	var from []string
	for _, e := range citedBy {
		from = append(from, e.FromWork)
	}
	if strings.Join(from, ",") != "W1,W2" {
		t.Errorf("W6 cited by %v, want W1 and W2 — W2's duplicate edge collapsed", from)
	}
	if s, _ := db.Stats(ctx); s != (Stats{Works: 3, Stubs: 1, Edges: 2}) {
		t.Errorf("Stats = %+v, want 3 works, 1 stub, 2 edges", s)
	}
}

func TestMergeIntoMovesReferencesAndKeepsProvenance(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()
	// The old record is a hydrated seed that cites W9; the survivor is new.
	record(t, db, hydratedWith("W5", 0, "W9", "W6"))

	err := db.Tx(ctx, func(tx *Tx) error { return tx.MergeInto(ctx, "W5", "W6") })
	if err != nil {
		t.Fatalf("MergeInto: %v", err)
	}

	w, err := db.GetWork(ctx, "W6")
	if err != nil {
		t.Fatal(err)
	}
	if !w.IsSeed || model.Deref(w.Depth, -1) != 0 {
		t.Errorf("survivor seed %v depth %v; want the old record's seed flag and depth 0", w.IsSeed, w.Depth)
	}
	refs, _ := db.References(ctx, "W6")
	if len(refs) != 1 || refs[0].ToWork != "W9" {
		t.Errorf("survivor's references = %+v, want W9 only — W5 -> W6 would be a self-citation", refs)
	}
}

func TestMergeIntoIsANoOpWhenNothingToMerge(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()
	record(t, db, hydratedWith("W1", 0, "W2"))
	err := db.Tx(ctx, func(tx *Tx) error {
		if err := tx.MergeInto(ctx, "W2", "W2"); err != nil {
			return err
		}
		return tx.MergeInto(ctx, "W404", "W2")
	})
	if err != nil {
		t.Fatalf("MergeInto: %v", err)
	}
	if s, _ := db.Stats(ctx); s != (Stats{Works: 2, Stubs: 1, Edges: 1}) {
		t.Errorf("Stats = %+v, want unchanged", s)
	}
}

func TestWorkIDByDOIAndMarkSeed(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()
	record(t, db, hydratedWith("W1", 0, "W2"))
	if err := db.UpsertWork(ctx, &model.Work{OpenAlexID: "W2", DOI: model.Ptr("10.1/two")}); err != nil {
		t.Fatal(err)
	}

	err := db.Tx(ctx, func(tx *Tx) error {
		id, err := tx.WorkIDByDOI(ctx, "10.1/two")
		if err != nil || id != "W2" {
			t.Errorf("WorkIDByDOI = %q, %v; want W2", id, err)
		}
		if _, err := tx.WorkIDByDOI(ctx, "10.1/none"); !errors.Is(err, errs.ErrNotFound) {
			t.Errorf("unknown DOI = %v, want ErrNotFound", err)
		}
		if err := tx.MarkSeed(ctx, "W2"); err != nil {
			return err
		}
		if err := tx.MarkSeed(ctx, "W404"); !errors.Is(err, errs.ErrNotFound) {
			t.Errorf("MarkSeed on an absent work = %v, want ErrNotFound", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	w, _ := db.GetWork(ctx, "W2")
	if !w.IsSeed || model.Deref(w.Depth, -1) != 0 {
		t.Errorf("W2 seed %v depth %v; want a seed at depth 0", w.IsSeed, w.Depth)
	}
}

func TestSummary(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()
	if s, err := db.Summary(ctx); err != nil || s != (Summary{}) {
		t.Fatalf("empty Summary = %+v, %v", s, err)
	}

	record(t, db, hydratedWith("W1", 0, "W2", "W3", "W4")) // a seed citing three
	record(t, db, hydratedWith("W2", 1))                   // hydrated, cites nothing: a dead end
	err := db.Tx(ctx, func(tx *Tx) error { return tx.MarkUnresolved(ctx, "W4") })
	if err != nil {
		t.Fatal(err)
	}

	got, err := db.Summary(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := Summary{Works: 4, Hydrated: 2, Stubs: 2, Unresolved: 1, Seeds: 1, Edges: 3, DeadEnds: 1}
	if got != want {
		t.Errorf("Summary = %+v, want %+v", got, want)
	}

	titled, err := db.TitledWorks(ctx)
	if err != nil || len(titled) != 2 || titled[0].OpenAlexID != "W1" {
		t.Errorf("TitledWorks = %d works, %v; want W1 and W2 — stubs have no titles", len(titled), err)
	}
}
