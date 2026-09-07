package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

const (
	// BusyTimeout is how long a connection waits for a lock before giving up.
	//
	// It must be set explicitly. The driver applies a one-minute default only
	// when the DSN carries no _pragma at all — set any other pragma and the
	// default is silently replaced by zero, which turns every moment of
	// contention into an immediate "database is locked" instead of a short
	// wait. Measured against v0.35.3: no pragmas gives 60000, foreign_keys
	// alone gives 0.
	BusyTimeout = 10 * time.Second

	// maxReadConns caps how many reader connections may exist at once.
	//
	// Under WAL readers never block each other or the writer, so this is not a
	// correctness limit. It is a memory one: every connection in this driver is
	// a separate WASM instance, not a socket, so an unbounded pool is a real
	// cost during a long expansion. A starting point to measure, not a law.
	maxReadConns = 4
)

// DB is the open library: one read pool and one write handle.
//
// The split is the single-writer rule made structural (hard rule 5). SQLite
// permits one writer, and Go makes that trivial to violate by accident — any
// goroutine holding a *sql.DB can start a write. Here it cannot: writes go
// through a handle pinned to one connection, so concurrent writers queue on the
// pool instead of racing for the file lock. WAL keeps readers from blocking on
// any of it.
type DB struct {
	read  *sql.DB
	write *sql.DB
	path  string
}

// Open opens the library at path, creating its directory if needed.
//
// It does not apply the schema. Opening and migrating are separate so that a
// caller can inspect an existing library without changing it.
func Open(ctx context.Context, path string) (*DB, error) {
	if path == "" {
		return nil, errors.New("store: open: empty database path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("store: create library directory: %w", err)
	}

	// The write handle first: it creates the file and puts it into WAL before
	// any reader looks at it.
	write, err := sql.Open("sqlite3", writeDSN(path))
	if err != nil {
		return nil, fmt.Errorf("store: open write handle: %w", err)
	}
	// One connection, kept for the life of the process. This is the single
	// writer; recycling it would only pay to rebuild a WASM instance.
	write.SetMaxOpenConns(1)
	write.SetMaxIdleConns(1)
	write.SetConnMaxLifetime(0)
	if err := write.PingContext(ctx); err != nil {
		write.Close()
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}

	read, err := sql.Open("sqlite3", readDSN(path))
	if err != nil {
		write.Close()
		return nil, fmt.Errorf("store: open read pool: %w", err)
	}
	read.SetMaxOpenConns(maxReadConns)
	read.SetMaxIdleConns(maxReadConns)
	read.SetConnMaxLifetime(0)
	if err := read.PingContext(ctx); err != nil {
		read.Close()
		write.Close()
		return nil, fmt.Errorf("store: open %s for reading: %w", path, err)
	}

	return &DB{read: read, write: write, path: path}, nil
}

// Path is where this library lives on disk. The CLI prints it on first run,
// because the default location differs on every OS (ADR-006).
func (db *DB) Path() string { return db.path }

// Close closes both handles, reporting every failure rather than the first.
func (db *DB) Close() error {
	var errs []error
	if db.read != nil {
		if err := db.read.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close read pool: %w", err))
		}
	}
	if db.write != nil {
		if err := db.write.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close write handle: %w", err))
		}
	}
	return errors.Join(errs...)
}

// writeDSN is the connection string for the single writer.
func writeDSN(path string) string {
	return dsn(path, []string{
		// Order matters: the driver documents busy timeout and locking mode as
		// the first pragmas to set.
		fmt.Sprintf("busy_timeout(%d)", BusyTimeout.Milliseconds()),
		// journal_mode is persisted in the file rather than per connection, so
		// setting it here is what puts a brand-new library into WAL before the
		// schema is applied to it.
		"journal_mode(wal)",
		"foreign_keys(on)",
	},
		// BEGIN IMMEDIATE, not BEGIN DEFERRED. A deferred transaction starts as
		// a reader and asks for the write lock only at its first write; if
		// another connection wrote in the meantime, SQLite returns BUSY at that
		// point and busy_timeout cannot help, because retrying mid-transaction
		// would mean reading one snapshot and writing another. Taking the lock
		// up front makes the wait happen where it can be waited on.
		"immediate")
}

// readDSN is the connection string for the read pool.
func readDSN(path string) string {
	return dsn(path, []string{
		fmt.Sprintf("busy_timeout(%d)", BusyTimeout.Milliseconds()),
		// PRAGMA foreign_keys is per connection and is not stored in the file
		// (spike 5). The one in schema.sql bound only the connection that
		// applied it, so every connection in this pool has to set it again —
		// including readers, whose queries would otherwise see a different
		// database than the writer does.
		"foreign_keys(on)",
	}, "deferred")
}

// dsn builds a file: URI.
//
// Built through net/url rather than concatenated, because a real library path
// contains spaces far more often than not — the default on Windows sits under a
// user profile, and people keep libraries in folders with names. A leading
// slash is deliberately not added: file:/C:/... is rejected on Windows, while a
// Unix path supplies its own.
func dsn(path string, pragmas []string, txlock string) string {
	q := make(url.Values, 2)
	for _, p := range pragmas {
		q.Add("_pragma", p)
	}
	q.Set("_txlock", txlock)
	u := url.URL{
		Scheme:   "file",
		Path:     filepath.ToSlash(path),
		OmitHost: true,
		RawQuery: q.Encode(),
	}
	return u.String()
}
