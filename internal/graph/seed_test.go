package graph

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/codevector-2003/filiation/internal/errs"
	"github.com/codevector-2003/filiation/internal/identity"
	"github.com/codevector-2003/filiation/internal/model"
	"github.com/codevector-2003/filiation/internal/sources/openalex"
	"github.com/codevector-2003/filiation/internal/store"
)

// fakeSource resolves from a map, so these tests are about what the graph
// writes, not about OpenAlex. sources/openalex has its own tests against
// recorded responses.
type fakeSource struct {
	works    map[string]model.HydratedWork // keyed by identity Value
	resolved int
}

func (f *fakeSource) Resolve(_ context.Context, id identity.ID) (model.HydratedWork, error) {
	f.resolved++
	h, ok := f.works[id.Value]
	if !ok {
		return model.HydratedWork{}, fmt.Errorf("fake: %s: %w", id.Value, errs.ErrUnresolved)
	}
	return h, nil
}

func (f *fakeSource) SearchByTitle(context.Context, string, int) ([]openalex.Candidate, error) {
	return nil, nil
}

func hydrated(id, doi string, refs ...string) model.HydratedWork {
	return model.HydratedWork{
		Work: model.Work{
			OpenAlexID:  id,
			DOI:         model.Ptr(doi),
			Title:       model.Ptr("Title of " + id),
			Hydrated:    true,
			FetchedRefs: true,
		},
		ReferencedWorks: refs,
	}
}

func newGraph(t *testing.T, works ...model.HydratedWork) (*Graph, *store.DB, *fakeSource) {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	src := &fakeSource{works: map[string]model.HydratedWork{}}
	for _, w := range works {
		src.works[w.OpenAlexID] = w
		src.works[*w.DOI] = w
	}
	return New(db, src), db, src
}

func id(t *testing.T, s string) identity.ID {
	t.Helper()
	v, err := identity.Parse(s)
	if err != nil {
		t.Fatalf("identity.Parse(%q): %v", s, err)
	}
	return v
}

func TestAddSeed(t *testing.T) {
	t.Parallel()
	g, db, _ := newGraph(t, hydrated("W1", "10.1000/one", "W2", "W3", "W4"))

	got, err := g.AddSeed(t.Context(), id(t, "https://doi.org/10.1000/one"))
	if err != nil {
		t.Fatalf("AddSeed: %v", err)
	}
	if got.Work.OpenAlexID != "W1" || !got.Work.IsSeed || got.Work.Source != model.SourceSeed {
		t.Errorf("seed = %s, seed %v, source %q", got.Work.OpenAlexID, got.Work.IsSeed, got.Work.Source)
	}
	if model.Deref(got.Work.Depth, -1) != 0 {
		t.Errorf("seed depth = %v, want 0", got.Work.Depth)
	}
	if want := (store.Recorded{Refs: 3, Stubs: 3, Edges: 3}); got.Recorded != want {
		t.Errorf("Recorded = %+v, want %+v", got.Recorded, want)
	}

	s, err := db.Stats(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if want := (store.Stats{Works: 4, Stubs: 3, Edges: 3}); s != want {
		t.Errorf("Stats = %+v, want %+v", s, want)
	}
}

func TestAddSeedTwiceAddsNothing(t *testing.T) {
	t.Parallel()
	// M0's definition of done: running it twice adds nothing the second time.
	g, db, _ := newGraph(t, hydrated("W1", "10.1000/one", "W2", "W3"))

	if _, err := g.AddSeed(t.Context(), id(t, "W1")); err != nil {
		t.Fatalf("first AddSeed: %v", err)
	}
	again, err := g.AddSeed(t.Context(), id(t, "W1"))
	if err != nil {
		t.Fatalf("second AddSeed: %v", err)
	}
	if !again.WasSeed {
		t.Errorf("WasSeed = false on the second add")
	}
	if again.Recorded.Stubs != 0 || again.Recorded.Edges != 0 {
		t.Errorf("second add recorded %+v, want nothing new", again.Recorded)
	}
	if s, _ := db.Stats(t.Context()); s != (store.Stats{Works: 3, Stubs: 2, Edges: 2}) {
		t.Errorf("Stats = %+v", s)
	}
}

func TestAddSeedThatWasAlreadyAStub(t *testing.T) {
	t.Parallel()
	// W2 enters as a reference of W1, at depth 1. Adding it as a seed of its
	// own hydrates it in place — one node, now a seed at depth 0.
	g, db, _ := newGraph(t,
		hydrated("W1", "10.1000/one", "W2"),
		hydrated("W2", "10.1000/two", "W5"),
	)
	if _, err := g.AddSeed(t.Context(), id(t, "W1")); err != nil {
		t.Fatal(err)
	}
	got, err := g.AddSeed(t.Context(), id(t, "W2"))
	if err != nil {
		t.Fatalf("AddSeed W2: %v", err)
	}
	if got.WasSeed {
		t.Errorf("WasSeed = true for a work that was only a stub")
	}
	if got.Work.IsStub() || !got.Work.IsSeed || model.Deref(got.Work.Depth, -1) != 0 {
		t.Errorf("W2 = stub %v, seed %v, depth %v; want hydrated seed at depth 0",
			got.Work.IsStub(), got.Work.IsSeed, got.Work.Depth)
	}
	// It entered through expansion, and that history is kept.
	if got.Work.Source != model.SourceExpansion {
		t.Errorf("Source = %q, want %q — source records how a work first entered",
			got.Work.Source, model.SourceExpansion)
	}
	if got.Recorded.Stubs != 1 {
		t.Errorf("new stubs = %d, want 1 (W5)", got.Recorded.Stubs)
	}
	if s, _ := db.Stats(t.Context()); s.Works != 3 {
		t.Errorf("Works = %d, want 3 — W2 was duplicated", s.Works)
	}
}

func TestAddSeedUnresolvedWritesNothing(t *testing.T) {
	t.Parallel()
	g, db, _ := newGraph(t)
	_, err := g.AddSeed(t.Context(), id(t, "10.1000/nowhere"))
	if !errors.Is(err, errs.ErrUnresolved) {
		t.Fatalf("error = %v, want ErrUnresolved", err)
	}
	if s, _ := db.Stats(t.Context()); s != (store.Stats{}) {
		t.Errorf("Stats = %+v after an unresolved seed, want empty", s)
	}
}

func TestAddSeedRefusesTitles(t *testing.T) {
	t.Parallel()
	g, _, src := newGraph(t)
	if _, err := g.AddSeed(t.Context(), id(t, "Attention Is All You Need")); err == nil {
		t.Error("AddSeed accepted a title")
	}
	if src.resolved != 0 {
		t.Errorf("a title reached the source")
	}
}

func TestAddSeedDeadEnd(t *testing.T) {
	t.Parallel()
	// A work with no references still becomes a seed; the report says it is a
	// dead end (D12) rather than failing.
	g, _, _ := newGraph(t, hydrated("W1", "10.1000/one"))
	got, err := g.AddSeed(t.Context(), id(t, "W1"))
	if err != nil {
		t.Fatalf("AddSeed: %v", err)
	}
	if !got.Recorded.DeadEnd() || !got.Work.FetchedRefs {
		t.Errorf("DeadEnd = %v, FetchedRefs = %v; want both true", got.Recorded.DeadEnd(), got.Work.FetchedRefs)
	}
}
