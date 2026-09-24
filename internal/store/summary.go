package store

import (
	"context"
	"fmt"

	"github.com/codevector-2003/filiation/internal/model"
)

// Summary is the library at a glance: what `fil stats` prints.
type Summary struct {
	Works      int // every row in work
	Hydrated   int // metadata fetched
	Stubs      int // known only by ID, still to fetch
	Unresolved int // of the stubs, those OpenAlex has no record of
	Seeds      int // works the user added
	Edges      int // citations

	// DeadEnds counts hydrated works whose references were fetched and turned
	// out to be none: the graph cannot grow through them. It is the per-library
	// view of D12's reference coverage, computed from the edges already stored.
	DeadEnds int
}

// Summary counts the library in one query.
func (db *DB) Summary(ctx context.Context) (Summary, error) {
	var s Summary
	err := db.read.QueryRowContext(ctx, `
SELECT count(*),
       coalesce(sum(hydrated), 0),
       coalesce(sum(hydrated = 0), 0),
       coalesce(sum(hydrated = 0 AND unresolved = 1), 0),
       coalesce(sum(is_seed), 0),
       (SELECT count(*) FROM cites),
       (SELECT count(*) FROM work w
         WHERE w.hydrated = 1 AND w.fetched_refs = 1
           AND NOT EXISTS (SELECT 1 FROM cites c WHERE c.from_work = w.openalex_id))
FROM work;`).Scan(&s.Works, &s.Hydrated, &s.Stubs, &s.Unresolved, &s.Seeds, &s.Edges, &s.DeadEnds)
	if err != nil {
		return Summary{}, fmt.Errorf("store: summarise library: %w", err)
	}
	return s, nil
}

// TitledWorks returns every hydrated work that has a title, with only the
// columns needed to tell one record from another: ID, title, year, type, DOI.
//
// It exists for duplicate detection, which is policy and so lives in
// internal/graph (D14). This package only fetches the rows.
func (db *DB) TitledWorks(ctx context.Context) ([]model.Work, error) {
	rows, err := db.read.QueryContext(ctx, `
SELECT openalex_id, title, year, type, doi
FROM work
WHERE hydrated = 1 AND title IS NOT NULL
ORDER BY openalex_id;`)
	if err != nil {
		return nil, fmt.Errorf("store: list titled works: %w", err)
	}
	defer rows.Close()

	var works []model.Work
	for rows.Next() {
		var (
			w        model.Work
			workType *string
		)
		if err := rows.Scan(&w.OpenAlexID, &w.Title, &w.Year, &workType, &w.DOI); err != nil {
			return nil, fmt.Errorf("store: scan titled work: %w", err)
		}
		if workType != nil {
			w.Type = model.WorkType(*workType)
		}
		w.Hydrated = true
		works = append(works, w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list titled works: %w", err)
	}
	return works, nil
}
