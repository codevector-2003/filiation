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
JOIN ranked r ON r.to_work = w.fil_id
WHERE w.hydrated = 0
  AND w.unresolved = 0
  AND w.openalex_id IS NOT NULL
  AND w.depth IS NOT NULL
  AND w.depth <= ?
GROUP BY w.fil_id, w.openalex_id, w.depth
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

// WorkIDByDOI returns the OpenAlex ID of the work in this library that holds
// doi, or errs.ErrNotFound. The DOI must already be normalised; identity.NormaliseDOI
// is what every write path uses.
//
// OpenAlex does not always merge its own duplicates: the same arXiv preprint
// has been seen under two work IDs with one DOI (W2949614626 and W4294576234,
// 24 Sept 2026). The UNIQUE index on work.doi refuses the second, and this is
// how a caller finds the first to fold it into.
//
// A holder with no OpenAlex ID — a local document the user imported, carrying
// the DOI printed on it — is reported as an error rather than as not found:
// the OpenAlex work arriving with that DOI is the same paper, and reconciling
// the two belongs to import (ARCHITECTURE_PHASE3.md ADR-014), not to a fold.
func (tx *Tx) WorkIDByDOI(ctx context.Context, doi string) (string, error) {
	var id sql.NullString
	err := tx.tx.QueryRowContext(ctx, `SELECT openalex_id FROM work WHERE doi = ?;`, doi).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("store: no work with DOI %s: %w", doi, errs.ErrNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("store: look up DOI %s: %w", doi, err)
	}
	if !id.Valid {
		return "", fmt.Errorf("store: DOI %s belongs to a local document with no OpenAlex ID", doi)
	}
	return id.String, nil
}

// MarkSeed makes id a seed at depth 0. It is for a seed that turned out to be
// a duplicate of a record already in the library: the flags belong on the
// record that survives.
func (tx *Tx) MarkSeed(ctx context.Context, id string) error {
	if err := checkOpenAlexID(id); err != nil {
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
// one paper and now answers for the old ID with the new one. Both are OpenAlex
// IDs.
//
// Without this the same paper sits in the graph twice — once as the stub some
// citing work pointed at, once as the hydrated survivor — which is exactly the
// quiet rot hard rule 6 exists to prevent. The fold:
//
//   - creates newID as a stub if it is not already present, carrying oldID's
//     depth and provenance, and otherwise keeps the shallower depth and a
//     sticky is_seed;
//   - moves every edge, in both directions, from the old record to the
//     survivor. An edge that would already exist, or would become a
//     self-citation, is dropped;
//   - moves what the user attached to the old record — notes, collections,
//     text — so a fold never deletes a person's work;
//   - records the old record's fil ID and OpenAlex ID as aliases of the
//     survivor (D17), and repoints any aliases the old record already had, so
//     everything that named the old record still finds the paper;
//   - deletes the old record.
//
// Call it before hydrating newID in the same transaction. A merged-away record
// can hold a DOI the survivor is about to be written with, and the UNIQUE
// index on work.doi would refuse the survivor while the old row still held it.
//
// Merging an ID into itself, or one not in the library, does nothing.
func (tx *Tx) MergeInto(ctx context.Context, oldID, newID string) error {
	if err := checkOpenAlexID(oldID); err != nil {
		return err
	}
	if err := checkOpenAlexID(newID); err != nil {
		return err
	}
	if oldID == newID {
		return nil
	}

	oldFil, found, err := filIDOf(ctx, tx.tx, oldID)
	if err != nil {
		return fmt.Errorf("store: merge %s: %w", oldID, err)
	}
	if !found {
		return nil
	}
	old, err := getWork(ctx, tx.tx, oldFil)
	if err != nil {
		return fmt.Errorf("store: merge %s: %w", oldID, err)
	}

	newFil, _, err := upsertStub(ctx, tx.tx, tx.newID, newID, old.Depth, old.Source)
	if err != nil {
		return fmt.Errorf("store: merge %s into %s: %w", oldID, newID, err)
	}
	if old.IsSeed {
		if _, err := tx.tx.ExecContext(ctx,
			`UPDATE work SET is_seed = 1 WHERE fil_id = ?;`, newFil); err != nil {
			return fmt.Errorf("store: merge %s into %s: %w", oldID, newID, err)
		}
	}

	moves := []string{
		// Works that cited the old record now cite the survivor.
		`INSERT OR IGNORE INTO cites (from_work, to_work, intent, context, section, confidence)
		 SELECT from_work, ?1, intent, context, section, confidence
		 FROM cites WHERE to_work = ?2 AND from_work <> ?1;`,
		// References the old record made are the survivor's.
		`INSERT OR IGNORE INTO cites (from_work, to_work, intent, context, section, confidence)
		 SELECT ?1, to_work, intent, context, section, confidence
		 FROM cites WHERE from_work = ?2 AND to_work <> ?1;`,
		// What the user attached follows the paper.
		`UPDATE note SET work_id = ?1 WHERE work_id = ?2;`,
		`INSERT OR IGNORE INTO collection_work (collection_id, work_id)
		 SELECT collection_id, ?1 FROM collection_work WHERE work_id = ?2;`,
		`UPDATE chunk SET work_id = ?1 WHERE work_id = ?2;`,
		// Names the old record already answered to now answer for the
		// survivor, and so do its own two.
		`UPDATE work_alias SET fil_id = ?1 WHERE fil_id = ?2;`,
	}
	for _, q := range moves {
		if _, err := tx.tx.ExecContext(ctx, q, newFil, oldFil); err != nil {
			return fmt.Errorf("store: merge %s into %s: %w", oldID, newID, err)
		}
	}

	if _, err := tx.tx.ExecContext(ctx, `DELETE FROM work WHERE fil_id = ?;`, oldFil); err != nil {
		return fmt.Errorf("store: merge %s into %s: delete old record: %w", oldID, newID, err)
	}
	if _, err := tx.tx.ExecContext(ctx,
		`INSERT OR REPLACE INTO work_alias (alias, fil_id) VALUES (?1, ?3), (?2, ?3);`,
		oldFil, oldID, newFil); err != nil {
		return fmt.Errorf("store: merge %s into %s: record aliases: %w", oldID, newID, err)
	}
	return nil
}
