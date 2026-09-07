package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// openTest opens a library under a directory whose name contains a space, since
// the real default location usually does.
func openTest(t *testing.T) *DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "My Library", "library.db")
	db, err := Open(t.Context(), path)
	if err != nil {
		t.Fatalf("Open(%q) error = %v", path, err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestOpenCreatesDirectoryAndFile(t *testing.T) {
	t.Parallel()
	db := openTest(t)

	if _, err := os.Stat(db.Path()); err != nil {
		t.Errorf("library file not created: %v", err)
	}
	if !filepath.IsAbs(db.Path()) {
		t.Errorf("Path() = %q, want absolute", db.Path())
	}
}

func TestOpenRejectsEmptyPath(t *testing.T) {
	t.Parallel()
	if _, err := Open(t.Context(), ""); err == nil {
		t.Fatal("Open(\"\") error = nil, want an error")
	}
}

// The single-writer rule, asserted structurally rather than trusted (hard rule
// 5). If this cap is ever lifted, concurrent writers race for the file lock.
func TestWriteHandleIsPinnedToOneConnection(t *testing.T) {
	t.Parallel()
	db := openTest(t)

	if got := db.write.Stats().MaxOpenConnections; got != 1 {
		t.Errorf("write MaxOpenConnections = %d, want 1", got)
	}
	if got := db.read.Stats().MaxOpenConnections; got != maxReadConns {
		t.Errorf("read MaxOpenConnections = %d, want %d", got, maxReadConns)
	}
}

// busy_timeout must be set on both handles, and must not be zero.
//
// Zero is the failure this test exists for: the driver applies its own
// one-minute default only when the DSN carries no _pragma at all, so setting
// foreign_keys without also setting busy_timeout silently produces 0, and every
// moment of contention becomes an immediate error instead of a short wait.
func TestBusyTimeoutIsSetOnBothHandles(t *testing.T) {
	t.Parallel()
	db := openTest(t)

	want := int(BusyTimeout.Milliseconds())
	for _, h := range []struct {
		name string
		db   *sql.DB
	}{{"read", db.read}, {"write", db.write}} {
		var got int
		if err := h.db.QueryRow(`PRAGMA busy_timeout;`).Scan(&got); err != nil {
			t.Fatalf("%s: query busy_timeout: %v", h.name, err)
		}
		if got == 0 {
			t.Errorf("%s handle busy_timeout = 0; contention will fail instantly instead of waiting", h.name)
		}
		if got != want {
			t.Errorf("%s handle busy_timeout = %d, want %d", h.name, got, want)
		}
	}
}

// PRAGMA foreign_keys is per connection and is not stored in the file (spike
// 5). Checking one connection proves nothing, so this holds every connection in
// the read pool open at once and checks each of them.
func TestForeignKeysOnEveryConnection(t *testing.T) {
	t.Parallel()
	db := openTest(t)
	ctx := t.Context()

	conns := make([]*sql.Conn, 0, maxReadConns)
	defer func() {
		for _, c := range conns {
			c.Close()
		}
	}()
	for i := range maxReadConns {
		c, err := db.read.Conn(ctx)
		if err != nil {
			t.Fatalf("read conn %d: %v", i, err)
		}
		conns = append(conns, c)

		var fk int
		if err := c.QueryRowContext(ctx, `PRAGMA foreign_keys;`).Scan(&fk); err != nil {
			t.Fatalf("read conn %d: query foreign_keys: %v", i, err)
		}
		if fk != 1 {
			t.Errorf("read conn %d: foreign_keys = %d, want 1", i, fk)
		}
	}

	var fk int
	if err := db.write.QueryRowContext(ctx, `PRAGMA foreign_keys;`).Scan(&fk); err != nil {
		t.Fatalf("write: query foreign_keys: %v", err)
	}
	if fk != 1 {
		t.Errorf("write handle: foreign_keys = %d, want 1", fk)
	}
}

// Reporting the pragma is not the same as enforcing it. This is the behaviour
// the setting is for.
func TestForeignKeysAreEnforced(t *testing.T) {
	t.Parallel()
	db := openTest(t)
	ctx := t.Context()

	mustExec(t, db.write, `CREATE TABLE parent (id TEXT PRIMARY KEY);`)
	mustExec(t, db.write, `CREATE TABLE child (
		id     TEXT PRIMARY KEY,
		parent TEXT NOT NULL REFERENCES parent(id) ON DELETE CASCADE
	);`)

	_, err := db.write.ExecContext(ctx,
		`INSERT INTO child (id, parent) VALUES ('c1', 'nonexistent');`)
	if err == nil {
		t.Fatal("insert with a dangling foreign key succeeded; foreign keys are not enforced")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "foreign key") {
		t.Errorf("error = %v, want a foreign key violation", err)
	}
}

// A brand-new library must be in WAL before the schema is applied, or readers
// block on the writer.
func TestJournalModeIsWAL(t *testing.T) {
	t.Parallel()
	db := openTest(t)

	var mode string
	if err := db.write.QueryRow(`PRAGMA journal_mode;`).Scan(&mode); err != nil {
		t.Fatalf("query journal_mode: %v", err)
	}
	if !strings.EqualFold(mode, "wal") {
		t.Errorf("journal_mode = %q, want wal", mode)
	}
}

// The reason for the whole split: reads must keep working while writes are in
// flight, and concurrent writes must queue rather than collide. A failure here
// shows up as "database is locked".
func TestConcurrentReadsDuringWrites(t *testing.T) {
	t.Parallel()
	db := openTest(t)
	ctx := t.Context()

	mustExec(t, db.write, `CREATE TABLE work (id TEXT PRIMARY KEY, title TEXT);`)

	const writers, readers, each = 4, 4, 25
	errCh := make(chan error, (writers+readers)*each)
	var wg sync.WaitGroup

	for w := range writers {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := range each {
				_, err := db.write.ExecContext(ctx,
					`INSERT INTO work (id, title) VALUES (?, ?);`,
					fmt.Sprintf("W%d-%d", w, i), "a title")
				if err != nil {
					errCh <- fmt.Errorf("write %d/%d: %w", w, i, err)
					return
				}
			}
		}(w)
	}
	for r := range readers {
		wg.Add(1)
		go func(r int) {
			defer wg.Done()
			for i := range each {
				var n int
				if err := db.read.QueryRowContext(ctx, `SELECT count(*) FROM work;`).Scan(&n); err != nil {
					errCh <- fmt.Errorf("read %d/%d: %w", r, i, err)
					return
				}
			}
		}(r)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}

	var n int
	if err := db.read.QueryRow(`SELECT count(*) FROM work;`).Scan(&n); err != nil {
		t.Fatalf("final count: %v", err)
	}
	if want := writers * each; n != want {
		t.Errorf("rows = %d, want %d", n, want)
	}
}

// Reopening an existing library must not disturb it.
func TestOpenExistingLibrary(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "My Library", "library.db")

	first, err := Open(t.Context(), path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	mustExec(t, first.write, `CREATE TABLE work (id TEXT PRIMARY KEY);`)
	mustExec(t, first.write, `INSERT INTO work (id) VALUES ('W1');`)
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	second, err := Open(t.Context(), path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer second.Close()

	var id string
	if err := second.read.QueryRow(`SELECT id FROM work;`).Scan(&id); err != nil {
		t.Fatalf("read after reopen: %v", err)
	}
	if id != "W1" {
		t.Errorf("id = %q, want W1", id)
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	t.Parallel()
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

// A path with a space must survive DSN construction, because the default
// library location on Windows generally has one.
func TestDSNEscapesSpaces(t *testing.T) {
	t.Parallel()
	got := dsn(`C:\Users\a b\library.db`, []string{"busy_timeout(10000)"}, "immediate")

	if strings.Contains(got, " ") {
		t.Errorf("dsn = %q, want the space escaped", got)
	}
	if !strings.Contains(got, "C:/Users/a%20b/library.db") {
		t.Errorf("dsn = %q, want a forward-slashed, escaped path", got)
	}
	if strings.Contains(got, "file:/C:") {
		t.Errorf("dsn = %q, want no leading slash — Windows rejects file:/C:/...", got)
	}
	if !strings.Contains(got, "_txlock=immediate") {
		t.Errorf("dsn = %q, want _txlock=immediate", got)
	}
}

// The writer takes its lock up front; the readers do not.
func TestTxLockModes(t *testing.T) {
	t.Parallel()
	if w := writeDSN("/tmp/library.db"); !strings.Contains(w, "_txlock=immediate") {
		t.Errorf("writeDSN = %q, want _txlock=immediate", w)
	}
	r := readDSN("/tmp/library.db")
	if !strings.Contains(r, "_txlock=deferred") {
		t.Errorf("readDSN = %q, want _txlock=deferred", r)
	}
	// journal_mode is persisted in the file, so only the writer sets it.
	if strings.Contains(r, "journal_mode") {
		t.Errorf("readDSN = %q, want no journal_mode on readers", r)
	}
}

func mustExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("exec %q: %v", firstLine(q), err)
	}
}

func firstLine(s string) string {
	if head, _, found := strings.Cut(s, "\n"); found {
		return head + "..."
	}
	return s
}
