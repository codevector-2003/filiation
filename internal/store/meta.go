package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// The meta table keeps the library's bookkeeping, one value per key: the
// schema version, and small things the screens remember, such as which papers
// were open in the Reader. A value is the caller's to read; the store only
// keeps it.

// Meta reads the value kept under key, "" when there is none.
func (db *DB) Meta(ctx context.Context, key string) (string, error) {
	var v sql.NullString
	err := db.read.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?;`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("store: read %s: %w", key, err)
	}
	return v.String, nil
}

// SetMeta keeps value under key, replacing what was there. The schema version
// is the migrations' alone to change.
func (tx *Tx) SetMeta(ctx context.Context, key, value string) error {
	if key == metaSchemaVersion {
		return fmt.Errorf("store: %s is set by migration only", key)
	}
	if _, err := tx.tx.ExecContext(ctx,
		`INSERT INTO meta(key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value;`,
		key, value); err != nil {
		return fmt.Errorf("store: write %s: %w", key, err)
	}
	return nil
}
