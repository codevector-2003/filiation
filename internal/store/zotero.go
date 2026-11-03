package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
// place again unless that would clash. Otherwise one of the user's own with
// the same name in the same place is adopted, and failing that one is made.
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
WHERE ifnull(c.parent_id, 0) = ? AND c.name = ? AND c.zotero_key IS NULL
  AND NOT EXISTS (SELECT 1 FROM folder_collection f WHERE f.collection_id = c.id);`, parent, name).Scan(&id)
	switch {
	case err == nil:
		_, err = tx.tx.ExecContext(ctx, `UPDATE collection SET zotero_key = ? WHERE id = ?;`, key, id)
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
