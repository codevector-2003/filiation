package graph

import (
	"context"
	"errors"
	"fmt"

	"github.com/codevector-2003/filiation/internal/errs"
	"github.com/codevector-2003/filiation/internal/identity"
	"github.com/codevector-2003/filiation/internal/model"
	"github.com/codevector-2003/filiation/internal/sources/openalex"
	"github.com/codevector-2003/filiation/internal/store"
)

// Source is what the graph needs from an index of works. It is declared here,
// where it is consumed, and says only what this package uses — not a guess at
// the shape a second source will one day have. *openalex.Client satisfies it;
// tests satisfy it with a map.
type Source interface {
	Resolve(ctx context.Context, id identity.ID) (model.HydratedWork, error)
	SearchByTitle(ctx context.Context, title string, limit int) ([]openalex.Candidate, error)
	GetWorksBatch(ctx context.Context, ids []string) (openalex.Batch, error)
}

// Graph grows the citation graph: it takes works from a Source and writes them
// to the store. It is the one place data crosses from sources to store, which
// is what keeps the two swappable independently.
type Graph struct {
	db  *store.DB
	src Source
}

// New builds a Graph over an open, migrated library.
func New(db *store.DB, src Source) *Graph {
	return &Graph{db: db, src: src}
}

// Seeded reports what adding a seed did.
type Seeded struct {
	// Work is the seed as stored, read back after the commit — so its depth,
	// flags and provenance are the library's, not the response's.
	Work model.Work

	// WasSeed reports the work was already a seed before this call.
	WasSeed bool

	// Recorded counts what was written for the seed's reference list. Adding a
	// seed that is already in the library records no new stubs or edges, which
	// is how the CLI can say "already there" without a special case.
	Recorded store.Recorded
}

// AddSeed resolves a deterministic identifier and adds the work it names as a
// seed. A title is refused; see SearchTitle and AddSeedWork.
//
// Nothing is written unless OpenAlex has the work: an unresolvable seed wraps
// errs.ErrUnresolved and leaves the library untouched, because there is no node
// to hang the failure on.
func (g *Graph) AddSeed(ctx context.Context, id identity.ID) (Seeded, error) {
	if !id.Deterministic() {
		return Seeded{}, errors.New("graph: AddSeed needs a deterministic identifier; search a title and pass the chosen work to AddSeedWork")
	}
	h, err := g.src.Resolve(ctx, id)
	if err != nil {
		return Seeded{}, fmt.Errorf("resolve %s %s: %w", id.Kind, id.Value, err)
	}
	return g.AddSeedWork(ctx, h)
}

// AddSeedWork adds an already-fetched work as a seed: the work itself, a stub
// for every work it cites, and the edges between them, in one transaction
// (§4). It is how a title-search candidate the user chose gets added without
// fetching it a second time.
//
// The seed is depth 0 and marked as a seed. If it was already in the library —
// reached as a stub from some other seed, or added before — hydration keeps the
// shorter depth and the original source (see store.UpsertWork), so adding it
// again is safe and idempotent.
func (g *Graph) AddSeedWork(ctx context.Context, h model.HydratedWork) (Seeded, error) {
	h.Depth = model.Ptr(0)
	h.IsSeed = true
	h.Source = model.SourceSeed

	var out Seeded
	stored := h.OpenAlexID
	err := g.db.Tx(ctx, func(tx *store.Tx) error {
		// Read inside the transaction that writes, so the answer cannot be
		// stale by the time the write lands. The seed may already be present
		// under its own ID or, as an OpenAlex duplicate, under another ID
		// holding the same DOI.
		wasSeed, err := isSeed(ctx, tx, h.OpenAlexID)
		if err != nil {
			return err
		}
		if h.DOI != nil && !wasSeed {
			if holder, err := tx.WorkIDByDOI(ctx, *h.DOI); err == nil {
				if wasSeed, err = isSeed(ctx, tx, holder); err != nil {
					return err
				}
			} else if !errors.Is(err, errs.ErrNotFound) {
				return err
			}
		}
		out.WasSeed = wasSeed

		res, err := hydrate(ctx, tx, &h)
		if err != nil {
			return err
		}
		if res.Folded {
			// The seed's flags were on the record that was folded away;
			// they belong on the one that holds it now.
			if err := tx.MarkSeed(ctx, res.ID); err != nil {
				return err
			}
		}
		stored, out.Recorded = res.ID, res.Recorded
		return nil
	})
	if err != nil {
		return Seeded{}, fmt.Errorf("add seed %s: %w", h.OpenAlexID, err)
	}

	w, err := g.db.GetWork(ctx, stored)
	if err != nil {
		return Seeded{}, fmt.Errorf("read back seed %s: %w", stored, err)
	}
	out.Work = *w
	return out, nil
}

// isSeed reports whether id is in the library and marked as a seed.
func isSeed(ctx context.Context, tx *store.Tx, id string) (bool, error) {
	w, err := tx.GetWork(ctx, id)
	switch {
	case err == nil:
		return w.IsSeed, nil
	case errors.Is(err, errs.ErrNotFound):
		return false, nil
	default:
		return false, err
	}
}

// SearchTitle returns candidates for a title. It writes nothing: choosing is
// the user's job (ADR-005), and a wrong seed poisons every node grown from it.
func (g *Graph) SearchTitle(ctx context.Context, title string, limit int) ([]openalex.Candidate, error) {
	c, err := g.src.SearchByTitle(ctx, title, limit)
	if err != nil {
		return nil, fmt.Errorf("search title: %w", err)
	}
	return c, nil
}
