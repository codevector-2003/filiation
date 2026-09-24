package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"

	"github.com/codevector-2003/filiation/internal/errs"
	"github.com/codevector-2003/filiation/internal/model"
)

// NextFrontier returns up to limit stubs worth hydrating next, best first.
//
// Best is in-graph in-degree: how many works already in this library cite the
// stub (§4). A paper five of your papers cite is a better use of budget than
// one a single paper cites, and the signal is free — it is computed from edges
// already stored, with no API call. Ties go to the shallower work, then to the
// ID, so the order is fully deterministic; resume depends on that.
//
// A stub is a candidate only if it is:
//
//   - not hydrated and not unresolved (OpenAlex has no record — stop spending
//     budget on it, §7);
//   - at a known depth no greater than maxDepth. A stub with no depth was
//     reached by no route this library can measure, and hydrating it would
//     grow the graph from nowhere;
//   - within the first maxRefsPerWork references of at least one work citing
//     it. A review article citing eight hundred papers has all eight hundred
//     edges recorded — they arrived free — but may put only its first
//     maxRefsPerWork forward as candidates, or it would spend the budget alone
//     (§4). References keep the order OpenAlex sent them in, which is insertion
//     order in cites. A stub any other work ranks within the cap stays a
//     candidate.
//
// maxRefsPerWork <= 0 means no cap. exclude lists IDs to leave out even though
// they qualify — the batch that just failed, so one bad batch cannot be retried
// in a loop within a run (§7).
func (db *DB) NextFrontier(ctx context.Context, limit, maxDepth, maxRefsPerWork int, exclude map[string]bool) ([]model.FrontierItem, error) {
	if limit <= 0 {
		return nil, nil
	}
	refCap := maxRefsPerWork
	if refCap <= 0 {
		refCap = math.MaxInt32
	}

	// ROW_NUMBER over cites' rowid ranks each reference within its citing
	// work. Both it and the window function are standard SQL, so this survives
	// the move to Postgres with rowid swapped for a sequence column.
	const q = `
WITH ranked AS (
	SELECT to_work,
	       ROW_NUMBER() OVER (PARTITION BY from_work ORDER BY rowid) AS rank_in_citer
	FROM cites
)
SELECT w.openalex_id, w.depth, count(*) AS in_degree
FROM work w
JOIN ranked r ON r.to_work = w.openalex_id
WHERE w.hydrated = 0
  AND w.unresolved = 0
  AND w.depth IS NOT NULL
  AND w.depth <= ?
GROUP BY w.openalex_id, w.depth
HAVING min(r.rank_in_citer) <= ?
ORDER BY in_degree DESC, w.depth ASC, w.openalex_id ASC
LIMIT ?;`

	// Over-fetch by the exclusion count so excluded IDs cannot starve the
	// batch; they are filtered out below.
	rows, err := db.read.QueryContext(ctx, q, maxDepth, refCap, limit+len(exclude))
	if err != nil {
		return nil, fmt.Errorf("store: next frontier: %w", err)
	}
	defer rows.Close()

	items := make([]model.FrontierItem, 0, limit)
	for rows.Next() && len(items) < limit {
		var it model.FrontierItem
		if err := rows.Scan(&it.OpenAlexID, &it.Depth, &it.InDegree); err != nil {
			return nil, fmt.Errorf("store: scan frontier: %w", err)
		}
		if !exclude[it.OpenAlexID] {
			items = append(items, it)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: next frontier: %w", err)
	}
	return items, nil
}

// StubsBeyond counts stubs deeper than maxDepth that are otherwise
// candidates. When the frontier is empty, it tells "the graph ends here" apart
// from "the depth limit stopped us" — model.StopFrontierEmpty against
// model.StopMaxDepth, which the CLI answers differently.
func (db *DB) StubsBeyond(ctx context.Context, maxDepth int) (int, error) {
	var n int
	err := db.read.QueryRowContext(ctx, `
SELECT count(*) FROM work
WHERE hydrated = 0 AND unresolved = 0 AND depth IS NOT NULL AND depth > ?;`, maxDepth).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("store: count stubs beyond depth %d: %w", maxDepth, err)
	}
	return n, nil
}

// WorkIDByDOI returns the ID of the work in this library that holds doi, or
// errs.ErrNotFound. The DOI must already be normalised; identity.NormaliseDOI
// is what every write path uses.
//
// OpenAlex does not always merge its own duplicates: the same arXiv preprint
// has been seen under two work IDs with one DOI (W2949614626 and W4294576234,
// 24 Sept 2026). The UNIQUE index on work.doi refuses the second, and this is
// how a caller finds the first to fold it into.
func (tx *Tx) WorkIDByDOI(ctx context.Context, doi string) (string, error) {
	var id string
	err := tx.tx.QueryRowContext(ctx, `SELECT openalex_id FROM work WHERE doi = ?;`, doi).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("store: no work with DOI %s: %w", doi, errs.ErrNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("store: look up DOI %s: %w", doi, err)
	}
	return id, nil
}

// MarkSeed makes id a seed at depth 0. It is for a seed that turned out to be
// a duplicate of a record already in the library: the flags belong on the
// record that survives.
func (tx *Tx) MarkSeed(ctx context.Context, id string) error {
	if err := checkWorkID(id); err != nil {
		return err
	}
	res, err := tx.tx.ExecContext(ctx,
		`UPDATE work SET is_seed = 1, depth = 0 WHERE openalex_id = ?;`, id)
	if err != nil {
		return fmt.Errorf("store: mark %s as a seed: %w", id, err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return fmt.Errorf("store: mark %s as a seed: %w", id, errs.ErrNotFound)
	}
	return nil
}

// MergeInto folds the work oldID into newID: OpenAlex merged two records for
// one paper and now answers for the old ID with the new one.
//
// Without this the same paper sits in the graph twice — once as the stub some
// citing work pointed at, once as the hydrated survivor — which is exactly the
// quiet rot hard rule 6 exists to prevent. The fold:
//
//   - creates newID as a stub if it is not already present, carrying oldID's
//     depth and provenance, and otherwise keeps the shallower depth and a
//     sticky is_seed;
//   - moves every edge, in both directions, from oldID to newID. An edge that
//     would already exist, or would become a self-citation, is dropped;
//   - deletes oldID, which cascades to anything still attached to it.
//
// Call it before hydrating newID in the same transaction. A merged-away record
// can hold a DOI the survivor is about to be written with, and the UNIQUE
// index on work.doi would refuse the survivor while the old row still held it.
//
// Merging an ID into itself, or one not in the library, does nothing.
func (tx *Tx) MergeInto(ctx context.Context, oldID, newID string) error {
	if err := checkWorkID(oldID); err != nil {
		return err
	}
	if err := checkWorkID(newID); err != nil {
		return err
	}
	if oldID == newID {
		return nil
	}

	old, err := getWork(ctx, tx.tx, oldID)
	if errors.Is(err, errs.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("store: merge %s: %w", oldID, err)
	}

	if _, err := upsertStub(ctx, tx.tx, newID, old.Depth, old.Source); err != nil {
		return fmt.Errorf("store: merge %s into %s: %w", oldID, newID, err)
	}
	if old.IsSeed {
		if _, err := tx.tx.ExecContext(ctx,
			`UPDATE work SET is_seed = 1 WHERE openalex_id = ?;`, newID); err != nil {
			return fmt.Errorf("store: merge %s into %s: %w", oldID, newID, err)
		}
	}

	moves := []string{
		// Works that cited the old record now cite the survivor.
		`INSERT OR IGNORE INTO cites (from_work, to_work, intent, context, section, confidence)
		 SELECT from_work, ?, intent, context, section, confidence
		 FROM cites WHERE to_work = ? AND from_work <> ?;`,
		// References the old record made are the survivor's.
		`INSERT OR IGNORE INTO cites (from_work, to_work, intent, context, section, confidence)
		 SELECT ?, to_work, intent, context, section, confidence
		 FROM cites WHERE from_work = ? AND to_work <> ?;`,
	}
	for _, q := range moves {
		if _, err := tx.tx.ExecContext(ctx, q, newID, oldID, newID); err != nil {
			return fmt.Errorf("store: merge %s into %s: move edges: %w", oldID, newID, err)
		}
	}

	if _, err := tx.tx.ExecContext(ctx, `DELETE FROM work WHERE openalex_id = ?;`, oldID); err != nil {
		return fmt.Errorf("store: merge %s into %s: delete old record: %w", oldID, newID, err)
	}
	return nil
}
