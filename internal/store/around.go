package store

import (
	"context"
	"fmt"

	"github.com/codevector-2003/filiation/internal/model"
)

// Around is what the library holds around one paper: how many works it
// cites, how many of those are known only by ID, and how many of those have
// no title either.
type Around struct {
	Cites    int
	Stubs    int
	Untitled int
}

// AroundOf counts the works fil cites, by what is known of them.
func (db *DB) AroundOf(ctx context.Context, fil string) (Around, error) {
	var a Around
	err := db.read.QueryRowContext(ctx, `
SELECT count(*),
       coalesce(sum(w.hydrated = 0 AND w.unresolved = 0), 0),
       coalesce(sum(w.hydrated = 0 AND w.unresolved = 0 AND w.title IS NULL), 0)
FROM cites c JOIN work w ON w.fil_id = c.to_work
WHERE c.from_work = ?;`, fil).Scan(&a.Cites, &a.Stubs, &a.Untitled)
	if err != nil {
		return Around{}, fmt.Errorf("store: count around %s: %w", fil, err)
	}
	return a, nil
}

// UnfetchedCitedBy returns the OpenAlex IDs of the works fil cites that are
// still stubs, titled or not: what hydrating its references would fetch.
func (db *DB) UnfetchedCitedBy(ctx context.Context, fil string) ([]string, error) {
	rows, err := db.read.QueryContext(ctx, `
SELECT w.openalex_id
FROM cites c JOIN work w ON w.fil_id = c.to_work
WHERE c.from_work = ? AND w.hydrated = 0 AND w.unresolved = 0 AND w.openalex_id IS NOT NULL
ORDER BY w.openalex_id;`, fil)
	if err != nil {
		return nil, fmt.Errorf("store: list unfetched references: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("store: list unfetched references: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// PutCiting records that h cites the work cited, without hydrating h: h
// becomes a stub with its details, as PutDetails gives one, so the graph
// gains the citation and not h's own references. It reports whether h was new
// to the library.
func (tx *Tx) PutCiting(ctx context.Context, h *model.HydratedWork, cited string, depth *int) (bool, error) {
	fil, created, err := upsertStub(ctx, tx.tx, tx.newID, h.OpenAlexID, depth, model.SourceExpansion)
	if err != nil {
		return false, err
	}
	if err := tx.PutDetails(ctx, h); err != nil {
		return false, err
	}
	if err := tx.UpsertEdge(ctx, model.Edge{FromWork: fil, ToWork: cited}); err != nil {
		return false, err
	}
	return created, nil
}
