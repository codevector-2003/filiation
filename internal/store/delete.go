package store

import (
	"context"
	"encoding/json"
	"fmt"
)

// DeletePlan is what deleting some papers takes with it (D36), counted before
// anything goes so the person can be told.
type DeletePlan struct {
	Papers      int      // the papers asked for that exist
	PDFs        int      // PDF files of theirs on disk
	Folders     []string // their folders in papers/, relative to the library folder
	Notes       int      // notes and highlights on them
	Collections int      // collections they are filed in
	Labels      int      // labels they carry
	// Orphans are references only these papers cite: never fetched, not the
	// person's, holding nothing of theirs. They exist only because one of
	// these papers cited them, so they go too.
	Orphans []string
}

// DeletePlan counts what deleting the papers with these fil IDs would take.
func (db *DB) DeletePlan(ctx context.Context, ids []string) (DeletePlan, error) {
	return deletePlan(ctx, db.read, ids)
}

// DeleteWorks deletes the papers with these fil IDs and the references only
// they cite, in one transaction. Everything tied to a paper goes with it by
// the schema's cascades: its citations, notes, labels, places in collections,
// files and folders as the library records them. Each deleted paper's
// OpenAlex ID is remembered in deleted_work, so the automatic routes that add
// papers skip it. The folders on disk are the caller's to move: the returned
// plan lists them.
func (tx *Tx) DeleteWorks(ctx context.Context, ids []string) (DeletePlan, error) {
	plan, err := deletePlan(ctx, tx.tx, ids)
	if err != nil {
		return plan, err
	}
	asked, err := json.Marshal(ids)
	if err != nil {
		return plan, err
	}
	orphans, err := json.Marshal(append([]string{}, plan.Orphans...))
	if err != nil {
		return plan, err
	}
	if _, err := tx.tx.ExecContext(ctx, `INSERT OR IGNORE INTO deleted_work (openalex_id, title)
		 SELECT openalex_id, title FROM work
		 WHERE fil_id IN (SELECT value FROM json_each(?)) AND openalex_id IS NOT NULL AND openalex_id <> '';`, string(asked)); err != nil {
		return plan, fmt.Errorf("store: delete papers: %w", err)
	}
	if _, err := tx.tx.ExecContext(ctx, `DELETE FROM work WHERE fil_id IN (SELECT value FROM json_each(?1))
		 OR fil_id IN (SELECT value FROM json_each(?2));`, string(asked), string(orphans)); err != nil {
		return plan, fmt.Errorf("store: delete papers: %w", err)
	}
	return plan, nil
}

// Deleted says whether the person deleted the paper with this OpenAlex ID,
// so that it is not added back without being asked for.
func (tx *Tx) Deleted(ctx context.Context, openAlexID string) (bool, error) {
	var n int
	if err := tx.tx.QueryRowContext(ctx, `SELECT count(*) FROM deleted_work WHERE openalex_id = ?;`, openAlexID).Scan(&n); err != nil {
		return false, fmt.Errorf("store: deleted %s: %w", openAlexID, err)
	}
	return n > 0, nil
}

// deletedAmong is which of these OpenAlex IDs the person deleted.
func deletedAmong(ctx context.Context, q queryer, ids []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(ids) == 0 {
		return out, nil
	}
	b, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	rows, err := q.QueryContext(ctx, `SELECT openalex_id FROM deleted_work WHERE openalex_id IN (SELECT value FROM json_each(?));`, string(b))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

func deletePlan(ctx context.Context, q queryer, ids []string) (DeletePlan, error) {
	var plan DeletePlan
	if len(ids) == 0 {
		return plan, nil
	}
	b, err := json.Marshal(ids)
	if err != nil {
		return plan, err
	}
	asked := string(b)
	const in = `(SELECT value FROM json_each(?1))`
	err = q.QueryRowContext(ctx, `SELECT
  (SELECT count(*) FROM work WHERE fil_id IN `+in+`),
  (SELECT count(*) FROM work_file WHERE fil_id IN `+in+` AND missing = 0 AND lower(path) LIKE '%.pdf'),
  (SELECT count(*) FROM note WHERE work_id IN `+in+`),
  (SELECT count(DISTINCT collection_id) FROM collection_work WHERE work_id IN `+in+`),
  (SELECT count(DISTINCT label_id) FROM work_label WHERE work_id IN `+in+`);`, asked).
		Scan(&plan.Papers, &plan.PDFs, &plan.Notes, &plan.Collections, &plan.Labels)
	if err != nil {
		return plan, fmt.Errorf("store: what deleting takes: %w", err)
	}
	if plan.Folders, err = column(ctx, q, `SELECT path FROM work_folder WHERE fil_id IN `+in+` AND missing = 0 ORDER BY path;`, asked); err != nil {
		return plan, fmt.Errorf("store: what deleting takes: %w", err)
	}
	plan.Orphans, err = column(ctx, q, `
SELECT w.fil_id FROM work w
WHERE w.fil_id IN (SELECT to_work FROM cites WHERE from_work IN `+in+`)
  AND w.fil_id NOT IN `+in+`
  AND w.hydrated = 0 AND ifnull(w.is_seed, 0) = 0
  AND NOT EXISTS (SELECT 1 FROM cites c WHERE c.to_work = w.fil_id AND c.from_work NOT IN `+in+`)
  AND NOT EXISTS (SELECT 1 FROM cites c WHERE c.from_work = w.fil_id)
  AND NOT EXISTS (SELECT 1 FROM work_file f WHERE f.fil_id = w.fil_id)
  AND NOT EXISTS (SELECT 1 FROM work_folder f WHERE f.fil_id = w.fil_id)
  AND NOT EXISTS (SELECT 1 FROM note n WHERE n.work_id = w.fil_id)
  AND NOT EXISTS (SELECT 1 FROM collection_work c WHERE c.work_id = w.fil_id)
  AND NOT EXISTS (SELECT 1 FROM work_label l WHERE l.work_id = w.fil_id)
  AND NOT EXISTS (SELECT 1 FROM favourite f WHERE f.work_id = w.fil_id)
  AND NOT EXISTS (SELECT 1 FROM zotero_item z WHERE z.fil_id = w.fil_id)
ORDER BY w.fil_id;`, asked)
	if err != nil {
		return plan, fmt.Errorf("store: what deleting takes: %w", err)
	}
	return plan, nil
}

// column is one column of text from a query.
func column(ctx context.Context, q queryer, query string, args ...any) ([]string, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
