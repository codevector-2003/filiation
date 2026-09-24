package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/codevector-2003/filiation/internal/errs"
	"github.com/codevector-2003/filiation/internal/model"
)

// Recorded is what one call to RecordEdgesAndStubs wrote. It feeds the counters
// on model.ExpansionResult, which is a report and not an error type.
type Recorded struct {
	// Refs is how many references were considered, after self-citations and
	// repeated IDs were dropped.
	Refs int

	// Stubs is how many works this call introduced to the library — references
	// pointing at papers nothing had cited yet. The rest were already there,
	// which is deduplication happening rather than deduplication failing.
	Stubs int

	// Edges is how many citation edges were new. Re-running against the same
	// work records none, which is what makes add and expand idempotent.
	Edges int
}

// DeadEnd reports whether this work cannot extend the graph: its references were
// fetched and there were none.
//
// This is not a defect. Reference coverage is a property of the field — 6% dead
// ends in medicine against 84% in arts and humanities (D12) — and it has to be
// surfaced per node from day one, exactly as OA status is, or a graph that stops
// after one hop reads as a broken tool rather than as missing data.
func (r Recorded) DeadEnd() bool { return r.Refs == 0 }

// RecordEdgesAndStubs writes a hydrated work's reference list: one stub per
// referenced work, one edge per reference, and fetched_refs on the citing work.
//
// This is the call the whole ingestion path is built around, and it must be one
// transaction. The edges reference work rows this same call creates, so a
// partial commit leaves either an edge with no target or a work claiming
// fetched_refs it never wrote. Both are states nothing downstream should have to
// handle, and neither is visible to the user until the graph looks wrong.
//
// The citing work must already exist — call UpsertWork first. The fetched_refs
// update doubles as that check and returns errs.ErrNotFound if it matches
// nothing, which is a better failure than the foreign key error the first edge
// would otherwise raise.
//
// Every reference is recorded, including a review article's eight hundred. They
// arrive free inside the response already paid for, and an edge is ground truth
// (hard rule 2) that nothing here is entitled to discard. §4's cap on how many
// of them may compete for the budget belongs to the frontier query in M1, where
// it can be applied per citing work without throwing away what was measured.
func (tx *Tx) RecordEdgesAndStubs(ctx context.Context, h *model.HydratedWork) (Recorded, error) {
	var rec Recorded
	if h == nil {
		return rec, errors.New("store: record edges: nil work")
	}
	if err := checkWorkID(h.OpenAlexID); err != nil {
		return rec, err
	}

	// First, because it is also the existence check — and RETURNING hands back
	// the depth actually stored, rather than trusting whatever the in-flight
	// copy of the work happens to carry.
	var depth *int
	err := tx.tx.QueryRowContext(ctx,
		`UPDATE work SET fetched_refs = 1 WHERE openalex_id = ? RETURNING depth;`,
		h.OpenAlexID).Scan(&depth)
	if errors.Is(err, sql.ErrNoRows) {
		return rec, fmt.Errorf("store: record edges for %s: upsert the work first: %w",
			h.OpenAlexID, errs.ErrNotFound)
	}
	if err != nil {
		return rec, fmt.Errorf("store: record edges for %s: %w", h.OpenAlexID, err)
	}

	// Children sit one hop further from the seed than their parent. A parent
	// with no depth — reached by a route nothing recorded — passes none on,
	// rather than inventing a distance from a seed it cannot see.
	var childDepth *int
	if depth != nil {
		childDepth = model.Ptr(*depth + 1)
	}

	seen := make(map[string]struct{}, len(h.ReferencedWorks))
	for _, ref := range h.ReferencedWorks {
		if ref == h.OpenAlexID {
			// A self-citation is an indexing artefact, and the edge would be a
			// self-loop that every traversal then has to special-case.
			continue
		}
		if _, dup := seen[ref]; dup {
			continue
		}
		seen[ref] = struct{}{}
		rec.Refs++

		created, err := upsertStub(ctx, tx.tx, ref, childDepth, model.SourceExpansion)
		if err != nil {
			return rec, fmt.Errorf("store: record edges for %s: %w", h.OpenAlexID, err)
		}
		if created {
			rec.Stubs++
		}

		n, err := insertEdge(ctx, tx.tx, h.OpenAlexID, ref)
		if err != nil {
			return rec, fmt.Errorf("store: record edges for %s: %w", h.OpenAlexID, err)
		}
		rec.Edges += n
	}
	return rec, nil
}

// insertEdge writes a bare citation edge and reports whether it was new. The
// columns that make this project worth building — context, intent, section —
// stay NULL until there is a PDF to read them out of.
func insertEdge(ctx context.Context, e execer, from, to string) (int, error) {
	res, err := e.ExecContext(ctx,
		`INSERT OR IGNORE INTO cites (from_work, to_work) VALUES (?, ?);`, from, to)
	if err != nil {
		return 0, fmt.Errorf("edge %s -> %s: %w", from, to, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("edge %s -> %s: %w", from, to, err)
	}
	return int(n), nil
}

// UpsertEdge writes one edge with whatever has been learned about it, and
// enriches an edge already present rather than replacing it.
//
// An edge is recorded long before anything is known about why it was made: the
// row is written during expansion from OpenAlex's resolved reference list, and
// the context sentence arrives in M3 once the citing paper's PDF has been read,
// the intent in M6. So a second write must fill in what it knows and leave
// everything else alone — a call carrying only a context sentence must not erase
// an intent something else took the trouble to classify.
//
// Both works must already exist. Edges point at real nodes because the stub
// exists the moment the edge does (§4), and the foreign keys are enforced on
// every connection (spike 5) to keep that true.
func (tx *Tx) UpsertEdge(ctx context.Context, e model.Edge) error {
	if err := checkWorkID(e.FromWork); err != nil {
		return err
	}
	if err := checkWorkID(e.ToWork); err != nil {
		return err
	}
	if e.FromWork == e.ToWork {
		return fmt.Errorf("store: upsert edge: %s cites itself: %w",
			e.FromWork, errs.ErrInvalidInput)
	}

	const q = `
INSERT INTO cites (from_work, to_work, intent, context, section, confidence)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(from_work, to_work) DO UPDATE SET
	intent     = ifnull(excluded.intent,     cites.intent),
	context    = ifnull(excluded.context,    cites.context),
	section    = ifnull(excluded.section,    cites.section),
	confidence = ifnull(excluded.confidence, cites.confidence);`

	_, err := tx.tx.ExecContext(ctx, q, e.FromWork, e.ToWork,
		nullString(string(e.Intent)), nullString(e.Context),
		nullString(string(e.Section)), e.Confidence)
	if err != nil {
		return fmt.Errorf("store: upsert edge %s -> %s: %w", e.FromWork, e.ToWork, err)
	}
	return nil
}

// References lists the works this one cites, nearest thing first in ID order so
// the result is stable between runs. It returns the edges, not the works: the
// targets are mostly stubs, and a caller that wants them can ask.
func (db *DB) References(ctx context.Context, id string) ([]model.Edge, error) {
	return edgesBy(ctx, db.read, id,
		`SELECT from_work, to_work, intent, context, section, confidence
		 FROM cites WHERE from_work = ? ORDER BY to_work;`)
}

// CitedBy lists the works in this library that cite the given one.
//
// It is the reverse lookup, and it is as common as the forward one — "who built
// on this?" is the question a researcher actually asks. The idx_cites_to index
// exists for exactly this query.
func (db *DB) CitedBy(ctx context.Context, id string) ([]model.Edge, error) {
	return edgesBy(ctx, db.read, id,
		`SELECT from_work, to_work, intent, context, section, confidence
		 FROM cites WHERE to_work = ? ORDER BY from_work;`)
}

func edgesBy(ctx context.Context, q queryer, id, query string) ([]model.Edge, error) {
	if err := checkWorkID(id); err != nil {
		return nil, err
	}
	rows, err := q.QueryContext(ctx, query, id)
	if err != nil {
		return nil, fmt.Errorf("store: list edges for %s: %w", id, err)
	}
	defer rows.Close()

	// Empty, not nil: an honest answer for a work whose references were fetched
	// and turned out to be none. See Recorded.DeadEnd.
	edges := []model.Edge{}
	for rows.Next() {
		var (
			e       model.Edge
			intent  sql.NullString
			context sql.NullString
			section sql.NullString
		)
		if err := rows.Scan(&e.FromWork, &e.ToWork, &intent, &context, &section,
			&e.Confidence); err != nil {
			return nil, fmt.Errorf("store: scan edge for %s: %w", id, err)
		}
		e.Intent = model.Intent(intent.String)
		e.Context = context.String
		e.Section = model.Section(section.String)
		edges = append(edges, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list edges for %s: %w", id, err)
	}
	return edges, nil
}
