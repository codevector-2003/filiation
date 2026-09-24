package graph

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/codevector-2003/filiation/internal/errs"
	"github.com/codevector-2003/filiation/internal/model"
	"github.com/codevector-2003/filiation/internal/store"
)

// These are §8's test strategy for the expander. The two it names as the ones
// that will actually catch regressions — idempotency and resume — are here,
// written with the code rather than after it.

// seeded opens a library over the given works and adds seed as a seed.
func seeded(t *testing.T, works []model.HydratedWork, seed string) (*Graph, *store.DB, *fakeSource) {
	t.Helper()
	g, db, src := newGraph(t, works...)
	if _, err := g.AddSeed(t.Context(), id(t, seed)); err != nil {
		t.Fatalf("AddSeed %s: %v", seed, err)
	}
	return g, db, src
}

func expand(t *testing.T, g *Graph, ctx context.Context, o ExpandOptions) model.ExpansionResult {
	t.Helper()
	res, err := g.Expand(ctx, o)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	return res
}

// state is everything the graph holds that a user could see: every work with
// its hydration state and depth, and every edge. Two libraries with the same
// state are the same graph.
func state(t *testing.T, db *store.DB, universeSize int) string {
	t.Helper()
	var lines []string
	for i := 1; i <= universeSize; i++ {
		wid := fmt.Sprintf("W%d", i)
		w, err := db.GetWork(t.Context(), wid)
		if errors.Is(err, errs.ErrNotFound) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		refs, err := db.References(t.Context(), wid)
		if err != nil {
			t.Fatal(err)
		}
		to := make([]string, len(refs))
		for j, e := range refs {
			to[j] = e.ToWork
		}
		sort.Strings(to)
		lines = append(lines, fmt.Sprintf("%s h=%v u=%v d=%v -> %s",
			wid, w.Hydrated, w.Unresolved, model.Deref(w.Depth, -1), strings.Join(to, " ")))
	}
	s, err := db.Stats(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%+v\n%s", s, strings.Join(lines, "\n"))
}

func TestExpandRespectsTheBudget(t *testing.T) {
	t.Parallel()
	// §8: MaxNodes=100 never produces 101 hydrated works.
	g, db, src := seeded(t, universe(400, 6), "W1")

	res := expand(t, g, t.Context(), ExpandOptions{MaxNodes: 100, MaxDepth: 10, BatchSize: 30})
	if res.Hydrated != 100 || res.StoppedBecause != model.StopBudgetExhausted {
		t.Errorf("hydrated %d, stopped %q; want exactly 100, budget exhausted", res.Hydrated, res.StoppedBecause)
	}
	// The first batch is only as big as the seed's reference list — the
	// frontier holds nothing else yet — and the last is cut to what the
	// budget has left.
	total := 0
	var sizes []int
	for _, b := range src.batches {
		sizes = append(sizes, len(b))
		total += len(b)
		if len(b) > 30 {
			t.Errorf("a batch of %d exceeds BatchSize 30", len(b))
		}
	}
	if total != 100 || fmt.Sprint(sizes[1:len(sizes)-1]) != "[30 30 30]" {
		t.Errorf("batch sizes = %v, want 100 in all with full batches between the first and last", sizes)
	}

	s, _ := db.Stats(t.Context())
	if hydratedWorks := s.Works - s.Stubs; hydratedWorks != 101 {
		t.Errorf("hydrated works in the library = %d, want 101 (the seed and 100)", hydratedWorks)
	}
	if res.Stubs != s.Stubs {
		t.Errorf("result reports %d stubs, library has %d", res.Stubs, s.Stubs)
	}
}

func TestExpandGraphMatchesTheSource(t *testing.T) {
	t.Parallel()
	// The golden check, computed independently of the expander: whatever was
	// hydrated, the library must hold exactly those works' references as
	// edges, and every work either hydrated or referenced — no more, no fewer.
	works := universe(300, 5)
	g, db, _ := seeded(t, works, "W1")
	expand(t, g, t.Context(), ExpandOptions{MaxNodes: 80, MaxDepth: 10, BatchSize: 25})

	byID := map[string]model.HydratedWork{}
	for _, w := range works {
		byID[w.OpenAlexID] = w
	}
	wantWorks, wantEdges := map[string]bool{}, 0
	for i := 1; i <= 300; i++ {
		w, err := db.GetWork(t.Context(), fmt.Sprintf("W%d", i))
		if err != nil || w.IsStub() {
			continue
		}
		wantWorks[w.OpenAlexID] = true
		for _, r := range byID[w.OpenAlexID].ReferencedWorks {
			wantWorks[r] = true
			wantEdges++
		}
	}
	s, _ := db.Stats(t.Context())
	if s.Works != len(wantWorks) || s.Edges != wantEdges {
		t.Errorf("library has %d works, %d edges; the hydrated works' references imply %d and %d",
			s.Works, s.Edges, len(wantWorks), wantEdges)
	}
}

func TestExpandIsBestFirst(t *testing.T) {
	t.Parallel()
	// W1 cites W2, W3, W4. W2 and W3 both cite W9; W4 cites W8. After the
	// first hop, W9 has in-degree 2 and W8 has 1, so with a budget of 4 the
	// fourth work fetched is W9, not W8.
	g, db, src := seeded(t, []model.HydratedWork{
		hydrated("W1", "10.1/1", "W2", "W3", "W4"),
		hydrated("W2", "10.1/2", "W9"),
		hydrated("W3", "10.1/3", "W9"),
		hydrated("W4", "10.1/4", "W8"),
		hydrated("W8", "10.1/8"),
		hydrated("W9", "10.1/9"),
	}, "W1")

	expand(t, g, t.Context(), ExpandOptions{MaxNodes: 4, MaxDepth: 5, BatchSize: 3})
	if got := fmt.Sprint(src.batches); got != "[[W2 W3 W4] [W9]]" {
		t.Errorf("batches = %s, want [[W2 W3 W4] [W9]]", got)
	}
	if w, _ := db.GetWork(t.Context(), "W8"); !w.IsStub() {
		t.Errorf("W8 was hydrated ahead of the better-cited W9")
	}
}

func TestExpandTwiceAddsNothing(t *testing.T) {
	t.Parallel()
	// §8: idempotent. Run to exhaustion, then again.
	g, db, src := seeded(t, universe(60, 4), "W1")

	first := expand(t, g, t.Context(), ExpandOptions{MaxNodes: 1000, MaxDepth: 20})
	if first.StoppedBecause != model.StopFrontierEmpty {
		t.Fatalf("first run stopped %q, want frontier-empty", first.StoppedBecause)
	}
	before, calls := state(t, db, 60), src.calls()

	second := expand(t, g, t.Context(), ExpandOptions{MaxNodes: 1000, MaxDepth: 20})
	if second.Hydrated != 0 || second.Edges != 0 || second.StoppedBecause != model.StopFrontierEmpty {
		t.Errorf("second run = %+v, want nothing done", second)
	}
	if src.calls() != calls {
		t.Errorf("second run made %d requests, want none", src.calls()-calls)
	}
	if after := state(t, db, 60); after != before {
		t.Errorf("second run changed the graph:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestExpandResumesToTheSameGraph(t *testing.T) {
	t.Parallel()
	// §8: cancel after two batches, re-run, and the end state equals the
	// uninterrupted run. There is no queue to lose: the frontier is a query.
	const size = 300
	opts := ExpandOptions{MaxNodes: 120, MaxDepth: 10, BatchSize: 20}

	whole, wholeDB, _ := seeded(t, universe(size, 5), "W1")
	expand(t, whole, t.Context(), opts)
	want := state(t, wholeDB, size)

	g, db, src := seeded(t, universe(size, 5), "W1")
	ctx, cancel := context.WithCancel(t.Context())
	src.beforeBatch = func(call int) error {
		if call == 3 {
			cancel() // Ctrl-C while the third request is in flight
			return context.Canceled
		}
		return nil
	}
	interrupted := expand(t, g, ctx, opts)
	committed := len(src.batches[0]) + len(src.batches[1])
	if interrupted.StoppedBecause != model.StopCancelled || interrupted.Hydrated != committed {
		t.Fatalf("interrupted run = %q with %d hydrated, want cancelled with the first two batches, %d",
			interrupted.StoppedBecause, interrupted.Hydrated, committed)
	}

	src.beforeBatch = nil
	rest := opts
	rest.MaxNodes = opts.MaxNodes - interrupted.Hydrated
	expand(t, g, t.Context(), rest)

	if got := state(t, db, size); got != want {
		t.Errorf("resumed graph differs from the uninterrupted one:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestExpandSingleWriterUnderConcurrentReads(t *testing.T) {
	t.Parallel()
	// §8: reads during an expansion never produce "database is locked".
	g, db, _ := seeded(t, universe(500, 6), "W1")

	var stop atomic.Bool
	var wg sync.WaitGroup
	errc := make(chan error, 64)
	for r := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; !stop.Load(); i++ {
				var err error
				switch (i + r) % 3 {
				case 0:
					_, err = db.Stats(t.Context())
				case 1:
					_, err = db.NextFrontier(t.Context(), 10, 10, 0, nil)
				default:
					_, err = db.CitedBy(t.Context(), "W1")
				}
				if err != nil {
					errc <- err
					return
				}
			}
		}()
	}

	_, expandErr := g.Expand(t.Context(), ExpandOptions{MaxNodes: 300, MaxDepth: 10, BatchSize: 10})
	stop.Store(true)
	wg.Wait()
	close(errc)

	if expandErr != nil {
		t.Errorf("Expand under concurrent reads: %v", expandErr)
	}
	for err := range errc {
		t.Errorf("concurrent read failed: %v", err)
	}
}

func TestExpandSurvivesCycles(t *testing.T) {
	t.Parallel()
	// §7: the graph is not a DAG. W1 -> W2 -> W3 -> W1, and W2 <-> W4.
	g, db, _ := seeded(t, []model.HydratedWork{
		hydrated("W1", "10.1/1", "W2"),
		hydrated("W2", "10.1/2", "W3", "W4"),
		hydrated("W3", "10.1/3", "W1"),
		hydrated("W4", "10.1/4", "W2"),
	}, "W1")

	res := expand(t, g, t.Context(), ExpandOptions{MaxNodes: 100, MaxDepth: 10})
	if res.Hydrated != 3 || res.StoppedBecause != model.StopFrontierEmpty {
		t.Errorf("result = %+v, want 3 hydrated and the frontier empty", res)
	}
	if s, _ := db.Stats(t.Context()); s != (store.Stats{Works: 4, Stubs: 0, Edges: 5}) {
		t.Errorf("Stats = %+v", s)
	}
}

func TestExpandStopsAtMaxDepth(t *testing.T) {
	t.Parallel()
	g, db, _ := seeded(t, []model.HydratedWork{
		hydrated("W1", "10.1/1", "W2"),
		hydrated("W2", "10.1/2", "W3"),
		hydrated("W3", "10.1/3", "W4"),
		hydrated("W4", "10.1/4"),
	}, "W1")

	res := expand(t, g, t.Context(), ExpandOptions{MaxNodes: 100, MaxDepth: 1})
	if res.StoppedBecause != model.StopMaxDepth || res.Hydrated != 1 || res.MaxDepthReached != 1 {
		t.Errorf("result = %+v, want max-depth after hydrating W2 at depth 1", res)
	}
	// W2's references are still recorded — edges are free — as a stub at
	// depth 2, waiting for a deeper run.
	w3, err := db.GetWork(t.Context(), "W3")
	if err != nil || !w3.IsStub() || model.Deref(w3.Depth, -1) != 2 {
		t.Errorf("W3 = %+v, %v; want a stub at depth 2", w3, err)
	}

	deeper := expand(t, g, t.Context(), ExpandOptions{MaxNodes: 100, MaxDepth: 5})
	if deeper.Hydrated != 2 || deeper.StoppedBecause != model.StopFrontierEmpty {
		t.Errorf("deeper run = %+v, want W3 and W4 hydrated", deeper)
	}
}

func TestExpandCapsReferencesPerWork(t *testing.T) {
	t.Parallel()
	// A review citing 30 papers may put only 10 forward (§4). All 30 edges
	// are recorded.
	refs := make([]string, 30)
	works := []model.HydratedWork{}
	for i := range refs {
		refs[i] = fmt.Sprintf("W%d", 100+i)
		works = append(works, hydrated(refs[i], fmt.Sprintf("10.1/%d", 100+i)))
	}
	works = append(works, hydrated("W1", "10.1/1", refs...))
	g, db, _ := seeded(t, works, "W1")

	res := expand(t, g, t.Context(), ExpandOptions{MaxNodes: 100, MaxDepth: 5, MaxRefsPerWork: 10})
	if res.Hydrated != 10 {
		t.Errorf("hydrated %d, want 10", res.Hydrated)
	}
	if s, _ := db.Stats(t.Context()); s.Edges != 30 {
		t.Errorf("edges = %d, want all 30", s.Edges)
	}
}

func TestExpandFoldsMergedRecords(t *testing.T) {
	t.Parallel()
	// W1 cites W5; OpenAlex has since merged W5 into W6. Hard rule 6: one node.
	g, db, src := seeded(t, []model.HydratedWork{
		hydrated("W1", "10.1/1", "W5"),
		hydrated("W6", "10.1/6", "W7"),
	}, "W1")
	src.merged["W5"] = "W6"

	res := expand(t, g, t.Context(), ExpandOptions{MaxNodes: 1, MaxDepth: 5})
	if res.Hydrated != 1 {
		t.Fatalf("hydrated %d, want 1", res.Hydrated)
	}
	if _, err := db.GetWork(t.Context(), "W5"); !errors.Is(err, errs.ErrNotFound) {
		t.Errorf("the merged-away W5 is still in the library")
	}
	w6, err := db.GetWork(t.Context(), "W6")
	if err != nil || w6.IsStub() || model.Deref(w6.Depth, -1) != 1 {
		t.Fatalf("W6 = %+v, %v; want hydrated at W5's depth, 1", w6, err)
	}
	if refs, _ := db.References(t.Context(), "W1"); len(refs) != 1 || refs[0].ToWork != "W6" {
		t.Errorf("W1's references = %+v, want the edge moved to W6", refs)
	}
	if w7, err := db.GetWork(t.Context(), "W7"); err != nil || model.Deref(w7.Depth, -1) != 2 {
		t.Errorf("W7 = %+v, %v; want a stub at depth 2", w7, err)
	}
}

func TestExpandMarksMissingWorksUnresolved(t *testing.T) {
	t.Parallel()
	g, db, src := seeded(t, []model.HydratedWork{hydrated("W1", "10.1/1", "W2", "W404")}, "W1")
	src.add(hydrated("W2", "10.1/2"))

	res := expand(t, g, t.Context(), ExpandOptions{MaxNodes: 10, MaxDepth: 5})
	if res.Hydrated != 1 || res.Unresolved != 1 || res.DeadEnds != 1 {
		t.Errorf("result = %+v, want 1 hydrated (a dead end) and 1 unresolved", res)
	}
	w, _ := db.GetWork(t.Context(), "W404")
	if !w.Unresolved {
		t.Errorf("W404 not marked unresolved")
	}
	// The edge stays: it is still true the paper was cited (§7).
	if refs, _ := db.References(t.Context(), "W1"); len(refs) != 2 {
		t.Errorf("W1 has %d references, want 2 — the edge to an unresolved work stays", len(refs))
	}
	// And it is never asked for again.
	calls := src.calls()
	expand(t, g, t.Context(), ExpandOptions{MaxNodes: 10, MaxDepth: 5})
	if src.calls() != calls {
		t.Errorf("an unresolved work was requested again")
	}
}

func TestExpandSkipsAFailedBatchAndCarriesOn(t *testing.T) {
	t.Parallel()
	g, db, src := seeded(t, universe(200, 5), "W1")
	src.beforeBatch = func(call int) error {
		if call == 2 {
			return fmt.Errorf("fake: 503: %w", errs.ErrTransient)
		}
		return nil
	}

	res := expand(t, g, t.Context(), ExpandOptions{MaxNodes: 30, MaxDepth: 10, BatchSize: 10})
	failedSize := len(src.batches[1])
	if res.Skipped != failedSize || res.Hydrated != 30-failedSize || res.StoppedBecause != model.StopBudgetExhausted {
		t.Errorf("result = %+v, want %d skipped, %d hydrated, budget exhausted",
			res, failedSize, 30-failedSize)
	}
	// The failed batch's works were not retried within the run...
	failed := src.batches[1]
	for _, b := range src.batches[2:] {
		for _, id := range b {
			for _, f := range failed {
				if id == f {
					t.Errorf("%s from the failed batch was retried in the same run", id)
				}
			}
		}
	}
	// ...and are still stubs, for the next run to pick up.
	for _, f := range failed {
		if w, _ := db.GetWork(t.Context(), f); !w.IsStub() {
			t.Errorf("%s from the failed batch is not a stub", f)
		}
	}
}

func TestExpandGivesUpWhenTheNetworkIsGone(t *testing.T) {
	t.Parallel()
	g, db, src := seeded(t, universe(200, 5), "W1")
	src.beforeBatch = func(call int) error {
		if call >= 2 {
			return fmt.Errorf("fake: connection refused: %w", errs.ErrTransient)
		}
		return nil
	}

	res, err := g.Expand(t.Context(), ExpandOptions{MaxNodes: 100, MaxDepth: 10, BatchSize: 10})
	if !errors.Is(err, errs.ErrTransient) {
		t.Fatalf("error = %v, want ErrTransient after three failed batches", err)
	}
	if src.calls() != 1+maxConsecutiveFailures {
		t.Errorf("calls = %d, want %d", src.calls(), 1+maxConsecutiveFailures)
	}
	// What the first batch fetched is committed and reported.
	s, _ := db.Stats(t.Context())
	first := len(src.batches[0])
	if res.Hydrated != first || s.Works-s.Stubs != first+1 {
		t.Errorf("hydrated %d in the result, %d in the library; want %d and %d (with the seed)",
			res.Hydrated, s.Works-s.Stubs, first, first+1)
	}
}

func TestExpandReportsProgress(t *testing.T) {
	t.Parallel()
	g, _, src := seeded(t, universe(200, 5), "W1")
	var seen []int
	expand(t, g, t.Context(), ExpandOptions{MaxNodes: 30, MaxDepth: 10, BatchSize: 10,
		Progress: func(r model.ExpansionResult) { seen = append(seen, r.Hydrated) }})
	want, running := []int{}, 0
	for _, b := range src.batches {
		running += len(b)
		want = append(want, running)
	}
	if fmt.Sprint(seen) != fmt.Sprint(want) || seen[len(seen)-1] != 30 {
		t.Errorf("progress = %v, want one report per batch, %v, ending at the budget", seen, want)
	}
}

func TestExpandRejectsBadOptions(t *testing.T) {
	t.Parallel()
	g, _, _ := seeded(t, universe(10, 2), "W1")
	for _, o := range []ExpandOptions{{MaxNodes: 0, MaxDepth: 3}, {MaxNodes: 10, MaxDepth: -1}} {
		if _, err := g.Expand(t.Context(), o); !errors.Is(err, errs.ErrInvalidConfig) {
			t.Errorf("Expand(%+v) = %v, want ErrInvalidConfig", o, err)
		}
	}
}

// dup is a second OpenAlex record for an existing paper: a new ID, the same
// DOI. Seen for real on 24 Sept 2026 — W2949614626 and W4294576234 are one
// arXiv preprint.
func dup(id, doi string, refs ...string) model.HydratedWork {
	return hydrated(id, doi, refs...)
}

func TestExpandFoldsDuplicateDOIsInOneBatch(t *testing.T) {
	t.Parallel()
	// The seed cites both records. They arrive in the same batch.
	g, db, _ := seeded(t, []model.HydratedWork{
		hydrated("W1", "10.1/1", "W2", "W3"),
		hydrated("W2", "10.1/same", "W8"),
		dup("W3", "10.1/same", "W9"),
	}, "W1")

	res := expand(t, g, t.Context(), ExpandOptions{MaxNodes: 2, MaxDepth: 5})
	if res.Hydrated != 1 || res.Merged != 1 {
		t.Errorf("result = %+v, want 1 hydrated and 1 merged", res)
	}
	if _, err := db.GetWork(t.Context(), "W3"); !errors.Is(err, errs.ErrNotFound) {
		t.Errorf("the duplicate W3 is still a node")
	}
	// The seed's two edges collapse to one, and the duplicate's references
	// are kept as the holder's: both records are accounts of one paper.
	if refs, _ := db.References(t.Context(), "W1"); len(refs) != 1 || refs[0].ToWork != "W2" {
		t.Errorf("W1's references = %+v, want W2 only", refs)
	}
	refs, _ := db.References(t.Context(), "W2")
	if len(refs) != 2 {
		t.Errorf("W2's references = %+v, want W8 and W9", refs)
	}
}

func TestExpandFoldsDuplicateDOIsAcrossRuns(t *testing.T) {
	t.Parallel()
	// The holder is hydrated in the first run; the duplicate turns up later,
	// cited by something else.
	g, db, _ := seeded(t, []model.HydratedWork{
		hydrated("W1", "10.1/1", "W2", "W4"),
		hydrated("W2", "10.1/same"),
		hydrated("W4", "10.1/4", "W3"),
		dup("W3", "10.1/same"),
	}, "W1")

	res := expand(t, g, t.Context(), ExpandOptions{MaxNodes: 100, MaxDepth: 5})
	if res.Merged != 1 || res.StoppedBecause != model.StopFrontierEmpty {
		t.Errorf("result = %+v, want the duplicate merged", res)
	}
	citedBy, _ := db.CitedBy(t.Context(), "W2")
	if len(citedBy) != 2 {
		t.Errorf("W2 cited by %d, want 2 — W4's edge to the duplicate moved to the holder", len(citedBy))
	}
	if s, _ := db.Stats(t.Context()); s.Works != 3 {
		t.Errorf("works = %d, want 3", s.Works)
	}
}

func TestAddSeedThatIsADuplicate(t *testing.T) {
	t.Parallel()
	// W2 is in the library, hydrated through expansion. The user adds the
	// same paper by a route that resolves to OpenAlex's duplicate record, W3.
	g, db, _ := seeded(t, []model.HydratedWork{
		hydrated("W1", "10.1/1", "W2"),
		hydrated("W2", "10.1/same"),
	}, "W1")
	expand(t, g, t.Context(), ExpandOptions{MaxNodes: 10, MaxDepth: 5})

	got, err := g.AddSeedWork(t.Context(), dup("W3", "10.1/same", "W7"))
	if err != nil {
		t.Fatalf("AddSeedWork: %v", err)
	}
	if got.Work.OpenAlexID != "W2" || !got.Work.IsSeed || model.Deref(got.Work.Depth, -1) != 0 {
		t.Errorf("seed = %s seed %v depth %v; want W2 as a seed at depth 0",
			got.Work.OpenAlexID, got.Work.IsSeed, got.Work.Depth)
	}
	if got.WasSeed {
		t.Errorf("WasSeed = true; W2 was not a seed")
	}
	if _, err := db.GetWork(t.Context(), "W3"); !errors.Is(err, errs.ErrNotFound) {
		t.Errorf("the duplicate seed became its own node")
	}
}

func TestExpandDoesNotCountARolledBackBatch(t *testing.T) {
	t.Parallel()
	// The second batch's commit fails. It rolls back, so the report must not
	// count it — the first live run over-reported by exactly this.
	works := universe(100, 4)
	g, db, src := seeded(t, works, "W1")
	src.beforeBatch = func(call int) error {
		if call == 2 {
			// Poison one work in this batch: an ID the store refuses.
			src.mu.Lock()
			for _, id := range src.batches[1] {
				w := src.works[id]
				w.OpenAlexID = "not-an-id"
				src.works[id] = w
				break
			}
			src.mu.Unlock()
		}
		return nil
	}

	res, err := g.Expand(t.Context(), ExpandOptions{MaxNodes: 100, MaxDepth: 10, BatchSize: 10})
	if err == nil {
		t.Fatal("Expand succeeded with a batch that cannot be committed")
	}
	first := len(src.batches[0])
	s, _ := db.Stats(t.Context())
	if res.Hydrated != first || s.Works-s.Stubs != first+1 {
		t.Errorf("reported %d hydrated, library holds %d; want both to count only the first batch (%d)",
			res.Hydrated, s.Works-s.Stubs-1, first)
	}
}
