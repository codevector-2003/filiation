package graph

import (
	"context"
	"fmt"

	"github.com/codevector-2003/filiation/internal/errs"
	"github.com/codevector-2003/filiation/internal/model"
	"github.com/codevector-2003/filiation/internal/store"
)

// AroundOptions say what to bring in around one paper.
type AroundOptions struct {
	// References fetches the works the paper cites that are known only by
	// ID: one request per 100.
	References bool
	// Citing brings in up to this many of the works that cite the paper,
	// most cited first: one request per 200.
	Citing int
	// Follow records what each work brought in cites, as Expand would.
	// Without it they come in with their details only, and the graph grows
	// by the citations to and from this one paper and nothing else.
	Follow bool

	// Progress, if set, is told how many works have come in of how many.
	Progress func(done, total int)
}

// BroughtIn is what BringAround did.
type BroughtIn struct {
	References int // works it cites that were fetched
	Citing     int // works citing it that were brought in
	New        int // of those citing it, how many were new to the library
	CitedBy    int // how many cite it in all, by OpenAlex's count
}

// BringAround brings in the works one step from the paper fil, in either
// direction, without growing the rest of the graph: a narrow expansion the
// person asks for by naming the paper. A paper known only by ID is fetched
// first, since its reference list comes with it.
func (g *Graph) BringAround(ctx context.Context, fil string, o AroundOptions) (BroughtIn, error) {
	var out BroughtIn
	w, err := g.db.GetWork(ctx, fil)
	if err != nil {
		return out, err
	}
	if w.OpenAlexID == "" {
		return out, fmt.Errorf("%s is your own document, which OpenAlex does not know: match it first: %w", fil, errs.ErrInvalidInput)
	}
	if o.References && !w.Hydrated {
		if _, err := g.HydrateIDs(ctx, []string{w.OpenAlexID}); err != nil {
			return out, err
		}
	}
	progress := func(done, total int) {
		if o.Progress != nil {
			o.Progress(done, total)
		}
	}

	var refs []string
	if o.References {
		if o.Follow {
			refs, err = g.db.UnfetchedCitedBy(ctx, fil)
		} else {
			refs, err = g.db.StubsCitedBy(ctx, []string{fil})
		}
		if err != nil {
			return out, err
		}
	}
	total := len(refs) + o.Citing
	progress(0, total)
	if len(refs) > 0 {
		if o.Follow {
			res, err := g.HydrateIDs(ctx, refs)
			out.References = res.Hydrated + res.Merged
			if err != nil {
				return out, err
			}
		} else {
			n, err := g.DetailIDs(ctx, refs)
			out.References = n
			if err != nil {
				return out, err
			}
		}
	}
	progress(len(refs), total)
	if o.Citing <= 0 {
		return out, nil
	}

	works, count, err := g.src.Citing(ctx, w.OpenAlexID, o.Citing)
	out.CitedBy = count
	// What arrived is paid for, so it is kept even if the rest failed.
	if len(works) > 0 {
		if cerr := g.putCiting(context.WithoutCancel(ctx), w, works, o.Follow, &out); cerr != nil {
			return out, cerr
		}
	}
	progress(len(refs)+len(works), len(refs)+len(works))
	if err != nil {
		return out, fmt.Errorf("graph: bring in what cites %s: %w", fil, err)
	}
	return out, nil
}

// putCiting writes the works citing w. Followed, they are hydrated as
// Expand hydrates a batch, their references with them; otherwise each is a
// stub with its details and its citation of w.
func (g *Graph) putCiting(ctx context.Context, w *model.Work, works []model.HydratedWork, follow bool, out *BroughtIn) error {
	depth := 1
	if w.Depth != nil {
		depth = *w.Depth + 1
	}
	return g.db.Tx(ctx, func(tx *store.Tx) error {
		out.Citing, out.New = 0, 0
		for i := range works {
			h := works[i]
			if follow {
				h.Depth, h.IsSeed, h.Source = nil, false, model.SourceExpansion
				// hydrate needs the work to exist, as a stub does in Expand.
				created, err := tx.UpsertStub(ctx, h.OpenAlexID, &depth, model.SourceExpansion)
				if err != nil {
					return err
				}
				if _, err := hydrate(ctx, tx, &h); err != nil {
					return err
				}
				if created {
					out.New++
				}
			} else {
				created, err := tx.PutCiting(ctx, &h, w.FilID, &depth)
				if err != nil {
					return err
				}
				if created {
					out.New++
				}
			}
			out.Citing++
		}
		return nil
	})
}
