package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/codevector-2003/filiation/internal/errs"
	"github.com/codevector-2003/filiation/internal/model"
)

// ZoteroItem is a Zotero item brought in, and the work it became.
type ZoteroItem struct {
	Key     string
	FilID   string
	Version int
	Matched bool // found in OpenAlex; false: kept as a local document
}

// ZoteroItems returns every item brought in from Zotero, by key.
func (db *DB) ZoteroItems(ctx context.Context) (map[string]ZoteroItem, error) {
	rows, err := db.read.QueryContext(ctx, `SELECT item_key, fil_id, version, matched FROM zotero_item;`)
	if err != nil {
		return nil, fmt.Errorf("store: list Zotero items: %w", err)
	}
	defer rows.Close()
	out := map[string]ZoteroItem{}
	for rows.Next() {
		var z ZoteroItem
		if err := rows.Scan(&z.Key, &z.FilID, &z.Version, &z.Matched); err != nil {
			return nil, fmt.Errorf("store: list Zotero items: %w", err)
		}
		out[z.Key] = z
	}
	return out, rows.Err()
}

// PutZoteroItem records that a Zotero item became a work.
func (tx *Tx) PutZoteroItem(ctx context.Context, z ZoteroItem) error {
	if _, err := tx.tx.ExecContext(ctx, `
INSERT INTO zotero_item (item_key, fil_id, version, matched) VALUES (?, ?, ?, ?)
ON CONFLICT (item_key) DO UPDATE SET fil_id = excluded.fil_id, version = excluded.version, matched = excluded.matched;`,
		z.Key, z.FilID, z.Version, z.Matched); err != nil {
		return fmt.Errorf("store: record Zotero item %s: %w", z.Key, err)
	}
	return nil
}

// ZoteroCollection makes sure a Zotero collection has its collection here,
// inside parent (0: at the top), and returns its ID.
//
// One brought in before is found by its key, and takes Zotero's name and
// place again unless that would clash. Otherwise a collection with the same
// name in the same place is adopted: the user's own, or one brought in by the
// other route (a running Zotero or an export), or a namesake, since Zotero
// allows two of a name and this library does not. Failing that one is made.
// A collection keeps the first key it was brought in by.
// Nothing is ever deleted: a collection removed in Zotero stays here.
func (tx *Tx) ZoteroCollection(ctx context.Context, key, name string, parent int64) (int64, error) {
	var id int64
	err := tx.tx.QueryRowContext(ctx, `SELECT id FROM collection WHERE zotero_key = ?;`, key).Scan(&id)
	switch {
	case err == nil:
		if tx.nameFree(ctx, parent, name, id) == nil && tx.parentFor(ctx, parent) == nil {
			if _, err := tx.tx.ExecContext(ctx, `UPDATE collection SET name = ?, parent_id = ? WHERE id = ?;`,
				name, nullID(parent), id); err != nil {
				return 0, fmt.Errorf("store: update collection %q: %w", name, err)
			}
		}
		return id, nil
	case !errors.Is(err, sql.ErrNoRows):
		return 0, fmt.Errorf("store: find Zotero collection %s: %w", key, err)
	}
	err = tx.tx.QueryRowContext(ctx, `
SELECT c.id FROM collection c
WHERE ifnull(c.parent_id, 0) = ? AND c.name = ?
  AND NOT EXISTS (SELECT 1 FROM folder_collection f WHERE f.collection_id = c.id);`, parent, name).Scan(&id)
	switch {
	case err == nil:
		_, err = tx.tx.ExecContext(ctx, `UPDATE collection SET zotero_key = ? WHERE id = ? AND zotero_key IS NULL;`, key, id)
	case errors.Is(err, sql.ErrNoRows):
		if id, err = tx.CreateCollection(ctx, name, parent); err == nil {
			_, err = tx.tx.ExecContext(ctx, `UPDATE collection SET zotero_key = ? WHERE id = ?;`, key, id)
		}
	}
	if err != nil {
		return 0, fmt.Errorf("store: bring in collection %q: %w", name, err)
	}
	return id, nil
}

// LabelNamed finds the label called name, or makes it in colour.
func (tx *Tx) LabelNamed(ctx context.Context, name, colour string) (int64, error) {
	var id int64
	err := tx.tx.QueryRowContext(ctx, `SELECT id FROM label WHERE name = ?;`, name).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return tx.CreateLabel(ctx, name, colour)
	}
	if err != nil {
		return 0, fmt.Errorf("store: find label %q: %w", name, err)
	}
	return id, nil
}

// LocalWorkNamed finds a local document from source with this title and year,
// so the same item in a second export, or one read from Zotero after an
// export, is not added twice.
func (db *DB) LocalWorkNamed(ctx context.Context, title string, year *int, source model.Source) (string, bool, error) {
	var fil string
	err := db.read.QueryRowContext(ctx, `
SELECT fil_id FROM work
WHERE openalex_id IS NULL AND source = ? AND title = ? AND year IS ?
ORDER BY added_at, fil_id LIMIT 1;`, string(source), title, year).Scan(&fil)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", false, nil
	case err != nil:
		return "", false, fmt.Errorf("store: find local document %q: %w", title, err)
	}
	return fil, true, nil
}

// LinkLocal makes the local document fil the work OpenAlex knows as
// openalexID, ready to be hydrated. If the library has that work already,
// fil folds into it, keeping fil as an alias, and its fil ID is returned;
// otherwise fil takes the OpenAlex ID and keeps its own fil ID.
func (tx *Tx) LinkLocal(ctx context.Context, fil, openalexID string) (string, error) {
	if err := checkOpenAlexID(openalexID); err != nil {
		return "", err
	}
	w, err := getWork(ctx, tx.tx, fil)
	if err != nil {
		return "", fmt.Errorf("store: link %s: %w", fil, err)
	}
	if w.OpenAlexID != "" {
		return "", fmt.Errorf("store: link %s: it is already OpenAlex's %s: %w", fil, w.OpenAlexID, errs.ErrInvalidInput)
	}
	have, found, err := filIDOf(ctx, tx.tx, openalexID)
	if err != nil {
		return "", fmt.Errorf("store: link %s: %w", fil, err)
	}
	if found {
		if err := tx.fold(ctx, *w, have); err != nil {
			return "", fmt.Errorf("store: link %s to %s: %w", fil, openalexID, err)
		}
		return have, nil
	}
	if _, err := tx.tx.ExecContext(ctx, `UPDATE work SET openalex_id = ? WHERE fil_id = ?;`, openalexID, fil); err != nil {
		return "", fmt.Errorf("store: link %s to %s: %w", fil, openalexID, err)
	}
	return fil, nil
}

// MarkZoteroMatched records that the Zotero items brought in as fil are now
// found in OpenAlex.
func (tx *Tx) MarkZoteroMatched(ctx context.Context, fil string) error {
	if _, err := tx.tx.ExecContext(ctx, `UPDATE zotero_item SET matched = 1 WHERE fil_id = ?;`, fil); err != nil {
		return fmt.Errorf("store: mark %s matched: %w", fil, err)
	}
	return nil
}
