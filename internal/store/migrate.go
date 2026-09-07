package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strconv"

	"github.com/codevector-2003/filiation/internal/errs"
)

// SchemaVersion is the schema this build understands. It must match the
// schema_version row seeded by schema.sql.
//
// Raise it in the same commit that changes the DDL, and add the upgrade step to
// Migrate. A schema change without a version bump is invisible to every guard
// in this file.
const SchemaVersion = 1

// metaSchemaVersion is the meta key holding the version of the library on disk.
const metaSchemaVersion = "schema_version"

// Migrate brings the library up to SchemaVersion, or refuses to touch it.
//
// It is idempotent: every statement in schema.sql is IF NOT EXISTS or OR
// IGNORE, so running it against a current library changes nothing.
//
// The whole thing runs in one transaction on the write handle, so a library is
// either fully migrated or untouched — a half-applied schema is not a state
// anything downstream should have to handle. The version check happens inside
// that transaction and before any DDL, so a refusal writes nothing at all.
func (db *DB) Migrate(ctx context.Context) error {
	tx, err := db.write.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin migration: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is a no-op

	found, err := versionTx(ctx, tx)
	if err != nil {
		return err
	}

	switch {
	case found > SchemaVersion:
		// Refuse, and write nothing. See errs.ErrSchemaTooNew: this build
		// cannot know what state a newer schema maintains, so writes that look
		// valid here could leave the library quietly inconsistent.
		return fmt.Errorf(
			"store: library at %s has schema %d, this build supports %d: %w",
			db.path, found, SchemaVersion, errs.ErrSchemaTooNew)

	case found == SchemaVersion:
		return nil

	case found == 0:
		// A library with no meta table has never been migrated.
		if err := applySchema(ctx, tx, Schema); err != nil {
			return err
		}

	default:
		// Unreachable while SchemaVersion is 1. It becomes the upgrade path the
		// first time the DDL changes, and failing loudly is better than
		// silently leaving an old library half-understood.
		return fmt.Errorf("store: library at %s has schema %d, no upgrade path to %d: %w",
			db.path, found, SchemaVersion, errs.ErrInvalidConfig)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit migration: %w", err)
	}
	return nil
}

// Version reports the schema version recorded in the library, or 0 if it has
// never been migrated.
func (db *DB) Version(ctx context.Context) (int, error) {
	tx, err := db.read.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return 0, fmt.Errorf("store: read schema version: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // read-only
	return versionTx(ctx, tx)
}

// versionTx reads the recorded version, treating "no meta table" and "no
// schema_version row" alike as 0 — both mean a library nothing has migrated.
func versionTx(ctx context.Context, tx *sql.Tx) (int, error) {
	var exists int
	err := tx.QueryRowContext(ctx,
		`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'meta';`,
	).Scan(&exists)
	if err != nil {
		return 0, fmt.Errorf("store: look for meta table: %w", err)
	}
	if exists == 0 {
		return 0, nil
	}

	var raw string
	err = tx.QueryRowContext(ctx,
		`SELECT value FROM meta WHERE key = ?;`, metaSchemaVersion).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("store: read %s: %w", metaSchemaVersion, err)
	}

	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("store: %s is %q, not a number: %w",
			metaSchemaVersion, raw, errs.ErrInvalidConfig)
	}
	return v, nil
}

// pragmaStatement matches a top-level PRAGMA statement in the schema.
//
// They have to come out before the schema is executed, because PRAGMA
// journal_mode cannot run inside a transaction — and the transaction is what
// makes migration all-or-nothing. Nothing is lost by removing them: WAL is set
// by the write DSN before this ever runs, and foreign_keys is per connection,
// so schema.sql could only ever have bound the one connection that applied it
// (spike 5). They stay in the file as a statement of intent; open.go is the
// mechanism.
var pragmaStatement = regexp.MustCompile(`(?im)^\s*PRAGMA\s+[^;]*;\s*$`)

// applySchema executes DDL inside an open transaction. It takes the SQL as an
// argument rather than reading the embedded Schema directly so that a test can
// hand it a broken schema and check nothing survives.
func applySchema(ctx context.Context, tx *sql.Tx, ddl string) error {
	if _, err := tx.ExecContext(ctx, stripPragmas(ddl)); err != nil {
		return fmt.Errorf("store: apply schema: %w", err)
	}
	return nil
}

func stripPragmas(ddl string) string {
	return pragmaStatement.ReplaceAllString(ddl, "")
}
