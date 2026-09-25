package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/codevector-2003/filiation/internal/model"
)

// writeAuthors replaces a work's authorship rows with authors, in the caller's
// transaction, so a work and its byline are written or rolled back together.
//
// Replacing rather than merging is deliberate: the fetched list is the whole
// truth about who wrote the work, in order, and a re-fetch that drops an author
// OpenAlex had wrongly attached must drop the row too. The author rows
// themselves are shared between works and only ever updated.
func writeAuthors(ctx context.Context, e execer, workID string, authors []model.Author) error {
	if _, err := e.ExecContext(ctx, `DELETE FROM authorship WHERE work_id = ?;`, workID); err != nil {
		return fmt.Errorf("store: clear authors of %s: %w", workID, err)
	}
	for _, a := range authors {
		if a.OpenAlexID == "" || a.Name == "" {
			continue
		}
		if _, err := e.ExecContext(ctx, `
INSERT INTO author (openalex_id, name, orcid) VALUES (?, ?, ?)
ON CONFLICT(openalex_id) DO UPDATE SET
	name  = excluded.name,
	orcid = ifnull(excluded.orcid, author.orcid);`,
			a.OpenAlexID, a.Name, a.ORCID); err != nil {
			return fmt.Errorf("store: upsert author %s: %w", a.OpenAlexID, err)
		}
		// OpenAlex occasionally lists one author twice; the first position wins.
		if _, err := e.ExecContext(ctx, `
INSERT INTO authorship (work_id, author_id, position) VALUES (?, ?, ?)
ON CONFLICT(work_id, author_id) DO NOTHING;`,
			workID, a.OpenAlexID, a.Position); err != nil {
			return fmt.Errorf("store: record author %s of %s: %w", a.OpenAlexID, workID, err)
		}
	}
	return nil
}

// AuthorsOf returns the authors of each of the given works, in byline order, in
// one query however many works are asked about. A work with no authorship rows
// is absent from the map: either it has none, or it was fetched before authors
// were stored (v0.1), and only the caller knows which question it is asking.
func (db *DB) AuthorsOf(ctx context.Context, workIDs []string) (map[string][]model.Author, error) {
	out := make(map[string][]model.Author)
	if len(workIDs) == 0 {
		return out, nil
	}
	ids, err := json.Marshal(workIDs)
	if err != nil {
		return nil, err
	}
	rows, err := db.read.QueryContext(ctx, `
SELECT s.work_id, a.openalex_id, a.name, a.orcid, ifnull(s.position, 0)
FROM authorship s JOIN author a ON a.openalex_id = s.author_id
WHERE s.work_id IN (SELECT value FROM json_each(?1))
ORDER BY s.work_id, s.position, a.openalex_id;`, string(ids))
	if err != nil {
		return nil, fmt.Errorf("store: authors of %d works: %w", len(workIDs), err)
	}
	defer rows.Close()
	for rows.Next() {
		var work string
		var a model.Author
		if err := rows.Scan(&work, &a.OpenAlexID, &a.Name, &a.ORCID, &a.Position); err != nil {
			return nil, fmt.Errorf("store: scan author: %w", err)
		}
		out[work] = append(out[work], a)
	}
	return out, rows.Err()
}
