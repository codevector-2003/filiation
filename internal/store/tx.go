package store

import (
	"context"
	"database/sql"
	"fmt"
)

// Tx is an open write transaction, and the only way anything writes to a
// library.
//
// It exists so the single-writer rule (hard rule 5) is structural rather than
// remembered. The write handle is unexported and pinned to one connection, so a
// caller cannot start a write without coming through DB.Tx, and two goroutines
// that try queue on the pool instead of racing for the file lock.
//
// The unit of work is deliberately the caller's, not the statement's. Hydrating
// a work, writing its edges and creating the stubs they point at is one
// transaction: the citation edges reference work rows that the same call
// creates, so a partial commit would mean either an edge with no target or a
// work claiming fetched_refs it never wrote. §4 sizes that unit at one batch —
// Ctrl-C loses at most one batch of progress and never a half-written graph.
type Tx struct {
	tx *sql.Tx
}

// Tx runs fn inside one write transaction, committing if it returns nil and
// rolling back otherwise.
//
// The transaction begins as BEGIN IMMEDIATE (see writeDSN), so it waits for the
// write lock up front, where busy_timeout can wait on it, rather than
// discovering it is unavailable partway through.
//
// An error from fn is returned unwrapped. Callers branch on the sentinels in
// internal/errs, and adding a "store: transaction" prefix here would bury the
// message that names what actually failed without telling anyone anything new.
func (db *DB) Tx(ctx context.Context, fn func(*Tx) error) error {
	tx, err := db.write.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin transaction: %w", err)
	}
	// Also the panic path: a rollback after a successful commit is a no-op, so
	// this is safe on every exit.
	defer tx.Rollback() //nolint:errcheck // no-op after commit

	if err := fn(&Tx{tx: tx}); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit transaction: %w", err)
	}
	return nil
}

// queryer is the read half of database/sql, satisfied by both *sql.DB and
// *sql.Tx.
//
// Every query in this package is written once against these two interfaces and
// then exposed twice: on *DB, where it runs against the read pool, and on *Tx,
// where it sees the uncommitted state of the transaction it is part of. A
// hydration path that had to commit before it could read back what it wrote
// would be a bug waiting to happen.
type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// execer adds writes. Only *sql.Tx is ever passed as one: the read pool has no
// business executing a statement that changes the library.
type execer interface {
	queryer
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}
