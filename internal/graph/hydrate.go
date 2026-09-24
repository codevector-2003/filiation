package graph

import (
	"context"
	"errors"

	"github.com/codevector-2003/filiation/internal/errs"
	"github.com/codevector-2003/filiation/internal/model"
	"github.com/codevector-2003/filiation/internal/store"
)

// written is what writing one fetched work did.
type written struct {
	// ID is the record the work now lives under: its own, or the one it was
	// folded into.
	ID string

	// Folded reports the work was a duplicate — OpenAlex holds the same paper
	// under a second ID with the same DOI — and was folded into the record
	// already in the library instead of being written as a node of its own.
	Folded bool

	Recorded store.Recorded
}

// hydrate writes one fetched work and its references, inside tx.
//
// It is the one place a fetched work enters the library, for seeds and for
// expansion alike, so identity reconciliation happens in one place too
// (graph/doc.go). The case it exists for: OpenAlex does not always merge its
// own duplicates. The same arXiv preprint was seen under W2949614626 and
// W4294576234 with one DOI (24 Sept 2026), and writing the second as a new
// node would trip the UNIQUE index on work.doi — or, without that index, put
// the same paper in the graph twice (hard rule 6).
//
// So a work whose DOI already belongs to another record is folded into that
// record: edges pointing at the newcomer move to the holder, the newcomer's
// references are recorded as the holder's, and the holder's own metadata is
// left alone. The holder stays because it is the node the user already has.
// Both records claim the same DOI, so their reference lists are two accounts
// of one paper's references, and the union is kept.
func hydrate(ctx context.Context, tx *store.Tx, h *model.HydratedWork) (written, error) {
	if h.DOI != nil {
		holder, err := tx.WorkIDByDOI(ctx, *h.DOI)
		switch {
		case err == nil && holder != h.OpenAlexID:
			if err := tx.MergeInto(ctx, h.OpenAlexID, holder); err != nil {
				return written{}, err
			}
			alias := *h
			alias.OpenAlexID = holder
			rec, err := tx.RecordEdgesAndStubs(ctx, &alias)
			if err != nil {
				return written{}, err
			}
			return written{ID: holder, Folded: true, Recorded: rec}, nil
		case err != nil && !errors.Is(err, errs.ErrNotFound):
			return written{}, err
		}
	}

	if err := tx.UpsertWork(ctx, &h.Work); err != nil {
		return written{}, err
	}
	rec, err := tx.RecordEdgesAndStubs(ctx, h)
	if err != nil {
		return written{}, err
	}
	return written{ID: h.OpenAlexID, Recorded: rec}, nil
}
