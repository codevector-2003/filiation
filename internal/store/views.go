package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/codevector-2003/filiation/internal/errs"
)

// SavedView is a name for a set of the Papers list's filters. The filters are
// the library's to read and write, as JSON; the store only keeps them.
type SavedView struct {
	ID      int64
	Name    string
	Filters string
}

// Views returns the saved views, by name.
func (db *DB) Views(ctx context.Context) ([]SavedView, error) {
	rows, err := db.read.QueryContext(ctx, `SELECT id, name, filters FROM saved_view ORDER BY name COLLATE NOCASE, id;`)
	if err != nil {
		return nil, fmt.Errorf("store: list views: %w", err)
	}
	defer rows.Close()
	var out []SavedView
	for rows.Next() {
		var v SavedView
		if err := rows.Scan(&v.ID, &v.Name, &v.Filters); err != nil {
			return nil, fmt.Errorf("store: list views: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// View reads one saved view.
func (db *DB) View(ctx context.Context, id int64) (SavedView, error) {
	v := SavedView{ID: id}
	err := db.read.QueryRowContext(ctx, `SELECT name, filters FROM saved_view WHERE id = ?;`, id).Scan(&v.Name, &v.Filters)
	if errors.Is(err, sql.ErrNoRows) {
		return v, fmt.Errorf("store: view %d: %w", id, errs.ErrNotFound)
	}
	if err != nil {
		return v, fmt.Errorf("store: view %d: %w", id, err)
	}
	return v, nil
}

// viewName refuses an empty name.
func viewName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("give the view a name: %w", errs.ErrInvalidInput)
	}
	return name, nil
}

// uniqueView turns a clash on the name into words a person can act on.
func uniqueView(err error, name string) error {
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return fmt.Errorf("a view is already called %q: %w", name, errs.ErrInvalidInput)
	}
	return err
}

// CreateView saves filters under name and returns its ID.
func (tx *Tx) CreateView(ctx context.Context, name, filters string) (int64, error) {
	name, err := viewName(name)
	if err != nil {
		return 0, err
	}
	res, err := tx.tx.ExecContext(ctx, `INSERT INTO saved_view (name, filters) VALUES (?, ?);`, name, filters)
	if err = uniqueView(err, name); err != nil {
		return 0, fmt.Errorf("store: save view: %w", err)
	}
	return res.LastInsertId()
}

// ChangeView renames a view, gives it new filters, or both; "" leaves either
// as it was.
func (tx *Tx) ChangeView(ctx context.Context, id int64, name, filters string) error {
	if name != "" {
		var err error
		if name, err = viewName(name); err != nil {
			return err
		}
	}
	res, err := tx.tx.ExecContext(ctx, `
UPDATE saved_view SET name = CASE WHEN ?2 = '' THEN name ELSE ?2 END,
                      filters = CASE WHEN ?3 = '' THEN filters ELSE ?3 END
WHERE id = ?1;`, id, name, filters)
	if err = uniqueView(err, name); err != nil {
		return fmt.Errorf("store: change view %d: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("store: view %d: %w", id, errs.ErrNotFound)
	}
	return nil
}

// DeleteView forgets a view. The papers it found are not touched.
func (tx *Tx) DeleteView(ctx context.Context, id int64) error {
	if _, err := tx.tx.ExecContext(ctx, `DELETE FROM saved_view WHERE id = ?;`, id); err != nil {
		return fmt.Errorf("store: delete view %d: %w", id, err)
	}
	return nil
}

// ReadableAmong counts the papers among ids whose text can be searched.
func (db *DB) ReadableAmong(ctx context.Context, ids []string) (int, error) {
	b, err := json.Marshal(append([]string{}, ids...))
	if err != nil {
		return 0, err
	}
	var n int
	err = db.read.QueryRowContext(ctx, `
SELECT count(*) FROM work_text t
WHERE t.status = 'ok' AND t.fil_id IN (SELECT value FROM json_each(?));`, string(b)).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("store: count readable papers: %w", err)
	}
	return n, nil
}
