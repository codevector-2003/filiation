package graph

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/codevector-2003/filiation/internal/errs"
	"github.com/codevector-2003/filiation/internal/model"
	"github.com/codevector-2003/filiation/internal/sources/openalex"
	"github.com/codevector-2003/filiation/internal/store"
)

// maxConsecutiveFailures is how many batches in a row may fail before a run
// gives up. One failed batch is skipped (§7); three in a row is the network
// gone, and the honest answer is to stop and say so — everything fetched so far
// is already committed, so the next run resumes from the frontier.
const maxConsecutiveFailures = 3

// ExpandOptions bound one expansion run.
type ExpandOptions struct {
	// MaxNodes is the budget: the most works this run may try to hydrate.
	// It is the primary control (ADR-002). Depth is only a guard.
	MaxNodes int

	// MaxDepth is the deepest a hydrated work may sit, in hops from the
	// nearest seed. Its references are still recorded — edges are free — but
	// the stubs they create are not fetched in this run.
	MaxDepth int

	// MaxRefsPerWork caps how many of one work's references may compete for
	// the budget (§4). Zero means no cap.
	MaxRefsPerWork int

	// BatchSize is how many works to fetch per request. Zero means
	// openalex.MaxBatch, the most one request may carry.
	BatchSize int

	// Progress, if set, is called after each committed batch with the running
	// totals. The CLI draws its progress line from it.
	Progress func(model.ExpansionResult)
}

// Expand grows the graph from the frontier under a budget, best first.
//
// Each round asks the store for the stubs with the highest in-graph in-degree,
// hydrates them in one request, and commits the batch — works, their
// references as new stubs and edges, merged records folded, unknown IDs marked
// unresolved — in one transaction. Ctrl-C therefore loses at most the batch in
// flight, and never leaves a half-written graph (§4).
//
// There is no queue to save. The frontier is a query over persisted state, so
// a run that stops for any reason resumes by running again, and running again
// on an exhausted frontier makes no requests and writes nothing.
//
// The run ends normally for one of four reasons, reported in StoppedBecause
// with a nil error: the budget is spent, the frontier is empty, the only stubs
// left are past MaxDepth, or ctx was cancelled. An error means something
// actually failed; the result still reports what was committed before it.
func (g *Graph) Expand(ctx context.Context, o ExpandOptions) (model.ExpansionResult, error) {
	start := time.Now()
	var res model.ExpansionResult

	if o.MaxNodes <= 0 {
		return res, fmt.Errorf("graph: expand: budget must be positive, got %d: %w", o.MaxNodes, errs.ErrInvalidConfig)
	}
	if o.MaxDepth < 0 {
		return res, fmt.Errorf("graph: expand: max depth must not be negative, got %d: %w", o.MaxDepth, errs.ErrInvalidConfig)
	}
	batchSize := o.BatchSize
	if batchSize <= 0 || batchSize > openalex.MaxBatch {
		batchSize = openalex.MaxBatch
	}

	finish := func(reason model.StopReason, err error) (model.ExpansionResult, error) {
		res.StoppedBecause = reason
		res.Duration = time.Since(start)
		// Read with a context that survives cancellation: the report of a
		// cancelled run is still owed to the user.
		if s, serr := g.db.Stats(context.WithoutCancel(ctx)); serr == nil {
			res.Stubs = s.Stubs
		}
		return res, err
	}

	skipped := map[string]bool{}
	failures := 0
	spent := 0
	for spent < o.MaxNodes {
		if ctx.Err() != nil {
			return finish(model.StopCancelled, nil)
		}

		items, err := g.db.NextFrontier(ctx, min(batchSize, o.MaxNodes-spent), o.MaxDepth, o.MaxRefsPerWork, skipped)
		if err != nil {
			if ctx.Err() != nil {
				return finish(model.StopCancelled, nil)
			}
			return finish("", err)
		}
		if len(items) == 0 {
			beyond, err := g.db.StubsBeyond(ctx, o.MaxDepth)
			if err != nil {
				return finish("", err)
			}
			if beyond > 0 {
				return finish(model.StopMaxDepth, nil)
			}
			return finish(model.StopFrontierEmpty, nil)
		}

		ids := make([]string, len(items))
		depth := make(map[string]int, len(items))
		for i, it := range items {
			ids[i] = it.OpenAlexID
			depth[it.OpenAlexID] = it.Depth
		}

		batch, fetchErr := g.src.GetWorksBatch(ctx, ids)

		// Whatever arrived is paid for, so it is committed even if the batch
		// then failed or the user pressed Ctrl-C: the commit takes
		// milliseconds, and throwing fetched works away would only make the
		// next run fetch them again.
		if err := g.commit(context.WithoutCancel(ctx), batch, depth, &res); err != nil {
			return finish("", err)
		}
		spent += len(ids)

		switch {
		case fetchErr == nil:
			failures = 0
		case ctx.Err() != nil:
			return finish(model.StopCancelled, nil)
		case errors.Is(fetchErr, errs.ErrTransient):
			// Skip the rest of this batch for the rest of the run, so the
			// frontier cannot hand back the same failing IDs forever. They
			// stay stubs; the next run tries them again.
			done := fetched(batch)
			for _, id := range ids {
				if !done[id] {
					skipped[id] = true
					res.Skipped++
				}
			}
			failures++
			if failures >= maxConsecutiveFailures {
				return finish("", fmt.Errorf("graph: expand: %d batches in a row failed; stopping: %w",
					failures, fetchErr))
			}
		default:
			return finish("", fmt.Errorf("graph: expand: %w", fetchErr))
		}

		if o.Progress != nil {
			snapshot := res
			snapshot.Duration = time.Since(start)
			o.Progress(snapshot)
		}
	}
	return finish(model.StopBudgetExhausted, nil)
}

// commit writes one batch in one transaction and adds it to res.
//
// The counts are gathered separately and added to res only once the
// transaction has committed. A batch that rolls back wrote nothing, and the
// report must not say otherwise.
func (g *Graph) commit(ctx context.Context, b openalex.Batch, depth map[string]int, res *model.ExpansionResult) error {
	if len(b.Works) == 0 && len(b.Aliases) == 0 && len(b.Missing) == 0 {
		return nil
	}
	var d model.ExpansionResult
	err := g.db.Tx(ctx, func(tx *store.Tx) error {
		d = model.ExpansionResult{}
		// Merged records first: the old stub may hold a DOI the survivor is
		// about to be written with. Sorted, so a run is reproducible.
		olds := make([]string, 0, len(b.Aliases))
		for old := range b.Aliases {
			olds = append(olds, old)
		}
		sort.Strings(olds)
		for _, old := range olds {
			if err := tx.MergeInto(ctx, old, b.Aliases[old]); err != nil {
				return err
			}
			if d, ok := depth[old]; ok {
				depth[b.Aliases[old]] = d
			}
		}

		for i := range b.Works {
			h := b.Works[i]
			// Provenance is the store's to keep: the stub already has its
			// depth and source, and UpsertWork keeps both.
			h.Depth, h.IsSeed, h.Source = nil, false, model.SourceExpansion
			w, err := hydrate(ctx, tx, &h)
			if err != nil {
				return err
			}
			if w.Folded {
				d.Merged++
			} else {
				d.Hydrated++
				if w.Recorded.DeadEnd() {
					d.DeadEnds++
				}
			}
			d.Edges += w.Recorded.Edges
			if dep := depth[h.OpenAlexID]; dep > d.MaxDepthReached {
				d.MaxDepthReached = dep
			}
		}

		for _, id := range b.Missing {
			if err := tx.MarkUnresolved(ctx, id); err != nil {
				return err
			}
			d.Unresolved++
		}
		return nil
	})
	if err != nil {
		return err
	}
	res.Hydrated += d.Hydrated
	res.Merged += d.Merged
	res.Edges += d.Edges
	res.DeadEnds += d.DeadEnds
	res.Unresolved += d.Unresolved
	res.MaxDepthReached = max(res.MaxDepthReached, d.MaxDepthReached)
	return nil
}

func fetched(b openalex.Batch) map[string]bool {
	done := make(map[string]bool, len(b.Works)+len(b.Aliases)+len(b.Missing))
	for _, w := range b.Works {
		done[w.OpenAlexID] = true
	}
	for old := range b.Aliases {
		done[old] = true
	}
	for _, id := range b.Missing {
		done[id] = true
	}
	return done
}
