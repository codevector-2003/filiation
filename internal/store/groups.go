package store

import (
	"context"
	"database/sql"
	"fmt"
)

// GroupChoice is what the user chose about a suggested citation group, known
// by its anchor paper: not useful, or made into a collection (D34).
type GroupChoice struct {
	Anchor       string
	Dismissed    bool
	CollectionID int64 // 0 when no collection was made, or it was deleted since
}

// GroupChoices returns every choice made, by anchor.
func (db *DB) GroupChoices(ctx context.Context) (map[string]GroupChoice, error) {
	rows, err := db.read.QueryContext(ctx, `SELECT anchor, dismissed, ifnull(collection_id, 0) FROM group_choice;`)
	if err != nil {
		return nil, fmt.Errorf("store: group choices: %w", err)
	}
	defer rows.Close()
	out := map[string]GroupChoice{}
	for rows.Next() {
		var c GroupChoice
		if err := rows.Scan(&c.Anchor, &c.Dismissed, &c.CollectionID); err != nil {
			return nil, fmt.Errorf("store: group choices: %w", err)
		}
		out[c.Anchor] = c
	}
	return out, rows.Err()
}

// DismissGroup marks the group around anchor not useful, or useful again.
func (tx *Tx) DismissGroup(ctx context.Context, anchor string, dismissed bool) error {
	_, err := tx.tx.ExecContext(ctx, `
INSERT INTO group_choice (anchor, dismissed) VALUES (?1, ?2)
ON CONFLICT (anchor) DO UPDATE SET dismissed = ?2, chosen_at = datetime('now');`, anchor, dismissed)
	if err != nil {
		return fmt.Errorf("store: dismiss group %s: %w", anchor, err)
	}
	return nil
}

// GroupMadeCollection records that the group around anchor became collection
// id. A group made into a collection is not dismissed.
func (tx *Tx) GroupMadeCollection(ctx context.Context, anchor string, id int64) error {
	_, err := tx.tx.ExecContext(ctx, `
INSERT INTO group_choice (anchor, dismissed, collection_id) VALUES (?1, 0, ?2)
ON CONFLICT (anchor) DO UPDATE SET dismissed = 0, collection_id = ?2, chosen_at = datetime('now');`, anchor, sql.NullInt64{Int64: id, Valid: id > 0})
	if err != nil {
		return fmt.Errorf("store: record group %s's collection: %w", anchor, err)
	}
	return nil
}
