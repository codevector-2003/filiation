package store

import (
	"context"
	"fmt"

	"github.com/codevector-2003/filiation/internal/model"
)

// EachWork calls fn for every work in the library, in ID order, one row at a
// time — a large library is never held in memory whole. With includeStubs
// false only fetched works are visited: a stub has an ID and nothing else, and
// in a library grown by expansion stubs outnumber fetched works ten to one.
//
// An error from fn stops the walk and is returned.
func (db *DB) EachWork(ctx context.Context, includeStubs bool, fn func(w *model.Work) error) error {
	q := `SELECT ` + workColumns + ` FROM work`
	if !includeStubs {
		q += ` WHERE hydrated = 1`
	}
	rows, err := db.read.QueryContext(ctx, q+` ORDER BY openalex_id;`)
	if err != nil {
		return fmt.Errorf("store: walk works: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		w, err := scanWork(rows)
		if err != nil {
			return fmt.Errorf("store: scan work: %w", err)
		}
		if err := fn(w); err != nil {
			return err
		}
	}
	return rows.Err()
}

// EachEdge calls fn for every citation, ordered by citing then cited work. With
// includeStubs false only edges between two fetched works are visited, so an
// export never refers to a node it did not write. The citing side is always
// fetched — only a fetched work has a known reference list — so the test is on
// the cited side alone.
func (db *DB) EachEdge(ctx context.Context, includeStubs bool, fn func(from, to string) error) error {
	q := `SELECT c.from_work, c.to_work FROM cites c`
	if !includeStubs {
		q += ` JOIN work w ON w.openalex_id = c.to_work AND w.hydrated = 1`
	}
	rows, err := db.read.QueryContext(ctx, q+` ORDER BY c.from_work, c.to_work;`)
	if err != nil {
		return fmt.Errorf("store: walk edges: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var from, to string
		if err := rows.Scan(&from, &to); err != nil {
			return fmt.Errorf("store: scan edge: %w", err)
		}
		if err := fn(from, to); err != nil {
			return err
		}
	}
	return rows.Err()
}
