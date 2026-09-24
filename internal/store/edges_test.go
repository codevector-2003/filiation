package store

import (
	"errors"
	"fmt"
	"testing"

	"github.com/codevector-2003/filiation/internal/errs"
	"github.com/codevector-2003/filiation/internal/model"
)

// hydratedWith is a work as it comes back from OpenAlex, reference list still
// attached.
func hydratedWith(id string, depth int, refs ...string) *model.HydratedWork {
	w := seedWork(id)
	w.DOI = nil // the UNIQUE column; fixtures here are not all the same paper
	w.Depth = model.Ptr(depth)
	w.IsSeed = depth == 0
	return &model.HydratedWork{Work: *w, ReferencedWorks: refs}
}

// record upserts a work and writes its reference list, which is the pair of
// calls the whole ingestion path makes.
func record(t *testing.T, db *DB, h *model.HydratedWork) Recorded {
	t.Helper()
	var rec Recorded
	err := db.Tx(t.Context(), func(tx *Tx) error {
		if err := tx.UpsertWork(t.Context(), &h.Work); err != nil {
			return err
		}
		var err error
		rec, err = tx.RecordEdgesAndStubs(t.Context(), h)
		return err
	})
	if err != nil {
		t.Fatalf("record %s: %v", h.OpenAlexID, err)
	}
	return rec
}

func TestRecordEdgesAndStubs(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()

	rec := record(t, db, hydratedWith("W1", 0, "W2", "W3", "W4"))
	want := Recorded{Refs: 3, Stubs: 3, Edges: 3}
	if rec != want {
		t.Errorf("Recorded = %+v, want %+v", rec, want)
	}
	if rec.DeadEnd() {
		t.Errorf("DeadEnd() = true for a work with three references")
	}

	seed, err := db.GetWork(ctx, "W1")
	if err != nil {
		t.Fatalf("GetWork: %v", err)
	}
	if !seed.FetchedRefs {
		t.Errorf("fetched_refs was not set on the citing work — expansion would fetch it again")
	}

	// Each reference is a stub one hop further from the seed.
	for _, id := range []string{"W2", "W3", "W4"} {
		w, err := db.GetWork(ctx, id)
		if err != nil {
			t.Fatalf("GetWork(%s): %v", id, err)
		}
		if !w.IsStub() {
			t.Errorf("%s is not a stub", id)
		}
		if model.Deref(w.Depth, -1) != 1 {
			t.Errorf("%s Depth = %v, want 1", id, w.Depth)
		}
		if w.Source != model.SourceExpansion {
			t.Errorf("%s Source = %q, want %q", id, w.Source, model.SourceExpansion)
		}
	}

	// The direction of a citation is the opposite of the direction time runs:
	// W1 is the newer paper and it cites W2.
	refs, err := db.References(ctx, "W1")
	if err != nil {
		t.Fatalf("References: %v", err)
	}
	if len(refs) != 3 {
		t.Fatalf("References(W1) = %d edges, want 3", len(refs))
	}
	if refs[0].FromWork != "W1" || refs[0].ToWork != "W2" {
		t.Errorf("first edge = %s -> %s, want W1 -> W2", refs[0].FromWork, refs[0].ToWork)
	}

	citedBy, err := db.CitedBy(ctx, "W2")
	if err != nil {
		t.Fatalf("CitedBy: %v", err)
	}
	if len(citedBy) != 1 || citedBy[0].FromWork != "W1" {
		t.Errorf("CitedBy(W2) = %+v, want one edge from W1", citedBy)
	}
}

func TestRecordEdgesAndStubsIsIdempotent(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()

	h := hydratedWith("W1", 0, "W2", "W3")
	record(t, db, h)
	again := record(t, db, h)

	// Re-running add or expand must write nothing new. This is the property M1's
	// resume depends on, and §8 names it as one of the two tests that will
	// actually catch a regression.
	if again.Stubs != 0 || again.Edges != 0 {
		t.Errorf("second run recorded %+v, want no new stubs and no new edges", again)
	}
	if again.Refs != 2 {
		t.Errorf("Refs = %d, want 2 — the references were still seen", again.Refs)
	}

	s, err := db.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if want := (Stats{Works: 3, Stubs: 2, Edges: 2}); s != want {
		t.Errorf("Stats() = %+v, want %+v", s, want)
	}
}

func TestRecordEdgesDeduplicatesAcrossCitingWorks(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()

	// Two papers citing the same third one is the case that makes the whole
	// graph worth building: the target must be one node, not two.
	record(t, db, hydratedWith("W1", 0, "W3"))
	rec := record(t, db, hydratedWith("W2", 0, "W3"))

	if rec.Stubs != 0 {
		t.Errorf("Stubs = %d, want 0 — W3 was already in the library", rec.Stubs)
	}
	if rec.Edges != 1 {
		t.Errorf("Edges = %d, want 1 — a second citation of the same work is a new edge", rec.Edges)
	}

	citedBy, err := db.CitedBy(ctx, "W3")
	if err != nil {
		t.Fatalf("CitedBy: %v", err)
	}
	if len(citedBy) != 2 {
		t.Errorf("CitedBy(W3) = %d edges, want 2 — in-degree is the frontier's ranking signal",
			len(citedBy))
	}
}

func TestRecordEdgesSkipsSelfCitationsAndRepeats(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()

	rec := record(t, db, hydratedWith("W1", 0, "W2", "W2", "W1", "W3"))
	// Refs counts what was left to record: the repeat and the self-citation are
	// gone before it is incremented, so it is the number a dead-end check can
	// be built on and not the length of the list OpenAlex sent.
	want := Recorded{Refs: 2, Stubs: 2, Edges: 2}
	if rec != want {
		t.Errorf("Recorded = %+v, want %+v", rec, want)
	}

	// A self-loop makes every traversal special-case it.
	refs, err := db.References(ctx, "W1")
	if err != nil {
		t.Fatalf("References: %v", err)
	}
	for _, e := range refs {
		if e.ToWork == "W1" {
			t.Errorf("a self-citation was recorded as an edge")
		}
	}
}

func TestRecordEdgesDeadEnd(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()

	// 84% of the frontier in arts and humanities (D12). It is missing data, not
	// a failure, so it must come back as a report rather than an error.
	rec := record(t, db, hydratedWith("W1", 0))
	if !rec.DeadEnd() {
		t.Errorf("DeadEnd() = false for a work with no references")
	}
	if rec != (Recorded{}) {
		t.Errorf("Recorded = %+v, want zero", rec)
	}

	w, err := db.GetWork(ctx, "W1")
	if err != nil {
		t.Fatalf("GetWork: %v", err)
	}
	if !w.FetchedRefs {
		t.Errorf("fetched_refs was not set — a dead end would be asked again every run")
	}

	refs, err := db.References(ctx, "W1")
	if err != nil {
		t.Fatalf("References: %v", err)
	}
	if refs == nil || len(refs) != 0 {
		t.Errorf("References = %v, want an empty non-nil slice", refs)
	}
}

func TestRecordEdgesRequiresTheCitingWork(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()

	err := db.Tx(ctx, func(tx *Tx) error {
		_, err := tx.RecordEdgesAndStubs(ctx, hydratedWith("W1", 0, "W2"))
		return err
	})
	if !errors.Is(err, errs.ErrNotFound) {
		t.Errorf("RecordEdgesAndStubs before the work exists = %v, want ErrNotFound", err)
	}

	if s, _ := db.Stats(ctx); s != (Stats{}) {
		t.Errorf("Stats() = %+v after a refused call, want a zero Stats", s)
	}
}

func TestRecordEdgesCarriesNoDepthFromAParentWithNone(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()

	h := hydratedWith("W1", 0, "W2")
	h.Depth = nil
	record(t, db, h)

	w, err := db.GetWork(ctx, "W2")
	if err != nil {
		t.Fatalf("GetWork: %v", err)
	}
	if w.Depth != nil {
		t.Errorf("Depth = %v, want nil — a parent with no depth invents none for its children", w.Depth)
	}
}

func TestRecordEdgesIsAllOrNothing(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()

	// One bad identifier partway through a reference list must not leave the
	// citing work claiming fetched_refs it never wrote.
	h := hydratedWith("W1", 0, "W2", "https://openalex.org/W3", "W4")
	err := db.Tx(ctx, func(tx *Tx) error {
		if err := tx.UpsertWork(ctx, &h.Work); err != nil {
			return err
		}
		_, err := tx.RecordEdgesAndStubs(ctx, h)
		return err
	})
	if err == nil {
		t.Fatal("RecordEdgesAndStubs accepted a reference that is not a bare OpenAlex ID")
	}

	if s, _ := db.Stats(ctx); s != (Stats{}) {
		t.Errorf("Stats() = %+v, want a zero Stats — the transaction did not roll back", s)
	}
}

func TestEdgesRequireBothWorks(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()

	if err := db.UpsertWork(ctx, seedWork("W1")); err != nil {
		t.Fatalf("UpsertWork: %v", err)
	}
	// Foreign keys are per connection and not stored in the file (spike 5), so
	// this is as much a test of the DSN as of the edge.
	err := db.Tx(ctx, func(tx *Tx) error {
		return tx.UpsertEdge(ctx, model.Edge{FromWork: "W1", ToWork: "W404"})
	})
	if err == nil {
		t.Error("an edge to a work with no row was accepted")
	}
}

func TestUpsertEdgeEnrichesRatherThanReplaces(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()

	record(t, db, hydratedWith("W1", 0, "W2"))

	// M3 arrives with the context sentence.
	err := db.Tx(ctx, func(tx *Tx) error {
		return tx.UpsertEdge(ctx, model.Edge{
			FromWork: "W1", ToWork: "W2",
			Context: "as shown by Vaswani et al., attention alone suffices",
			Section: model.SectionRelatedWork,
		})
	})
	if err != nil {
		t.Fatalf("UpsertEdge: %v", err)
	}

	// M6 arrives later with the intent, and must not erase the sentence.
	err = db.Tx(ctx, func(tx *Tx) error {
		return tx.UpsertEdge(ctx, model.Edge{
			FromWork: "W1", ToWork: "W2",
			Intent:     model.IntentMethod,
			Confidence: model.Ptr(0.82),
		})
	})
	if err != nil {
		t.Fatalf("UpsertEdge: %v", err)
	}

	refs, err := db.References(ctx, "W1")
	if err != nil {
		t.Fatalf("References: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("References = %d edges, want 1 — enriching created a second edge", len(refs))
	}
	got := refs[0]
	if got.Context == "" {
		t.Errorf("Context was erased by a later write that did not carry one")
	}
	if got.Section != model.SectionRelatedWork {
		t.Errorf("Section = %q, want %q", got.Section, model.SectionRelatedWork)
	}
	if got.Intent != model.IntentMethod {
		t.Errorf("Intent = %q, want %q", got.Intent, model.IntentMethod)
	}
	if model.Deref(got.Confidence, 0) != 0.82 {
		t.Errorf("Confidence = %v, want 0.82", got.Confidence)
	}
}

func TestUpsertEdgeRejectsSelfCitation(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()

	if err := db.UpsertWork(ctx, seedWork("W1")); err != nil {
		t.Fatalf("UpsertWork: %v", err)
	}
	err := db.Tx(ctx, func(tx *Tx) error {
		return tx.UpsertEdge(ctx, model.Edge{FromWork: "W1", ToWork: "W1"})
	})
	if err == nil {
		t.Error("a self-loop was accepted")
	}
}

func TestReferencesOfAWorkWithNoRow(t *testing.T) {
	t.Parallel()
	db := migrated(t)

	// A work nothing has cited and nothing has added is not an error to ask
	// about; it simply has no edges.
	refs, err := db.References(t.Context(), "W404")
	if err != nil {
		t.Fatalf("References: %v", err)
	}
	if len(refs) != 0 {
		t.Errorf("References = %v, want none", refs)
	}
}

func TestRecordEdgesOfAReviewArticle(t *testing.T) {
	t.Parallel()
	db := migrated(t)

	// A review can cite eight hundred papers. Every edge is recorded: they
	// arrive free inside a response already paid for, and an edge is ground
	// truth. Capping how many of them may compete for the budget is the
	// frontier query's job in M1.
	refs := make([]string, 0, 800)
	for i := range 800 {
		refs = append(refs, fmt.Sprintf("W%d", 1000+i))
	}
	rec := record(t, db, hydratedWith("W1", 0, refs...))

	if rec.Edges != 800 || rec.Stubs != 800 {
		t.Errorf("Recorded = %+v, want 800 edges and 800 stubs", rec)
	}
}
