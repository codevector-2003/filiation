package store

import (
	"errors"
	"strings"
	"testing"

	"github.com/codevector-2003/filiation/internal/errs"
)

// tableExists asks the database rather than the migration code, so a broken
// Migrate cannot report its own success.
func tableExists(t *testing.T, db *DB, name string) bool {
	t.Helper()
	var n int
	err := db.read.QueryRow(
		`SELECT count(*) FROM sqlite_master WHERE name = ?;`, name).Scan(&n)
	if err != nil {
		t.Fatalf("look for table %s: %v", name, err)
	}
	return n > 0
}

func TestMigrateFreshLibrary(t *testing.T) {
	t.Parallel()
	db := openTest(t)
	ctx := t.Context()

	if v, err := db.Version(ctx); err != nil || v != 0 {
		t.Fatalf("Version() before migrate = %d, %v; want 0, nil", v, err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	v, err := db.Version(ctx)
	if err != nil {
		t.Fatalf("Version() error = %v", err)
	}
	if v != SchemaVersion {
		t.Errorf("Version() = %d, want %d", v, SchemaVersion)
	}
	for _, table := range []string{"work", "author", "authorship", "cites", "chunk", "chunk_fts", "note", "collection", "meta"} {
		if !tableExists(t, db, table) {
			t.Errorf("table %s missing after Migrate", table)
		}
	}
	if !tableExists(t, db, "idx_work_frontier") {
		t.Error("idx_work_frontier missing; frontier selection depends on it")
	}
}

// Running twice must change nothing. Re-running is what happens every time the
// user opens their library.
func TestMigrateIsIdempotent(t *testing.T) {
	t.Parallel()
	db := openTest(t)
	ctx := t.Context()

	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	if _, err := db.write.ExecContext(ctx,
		`INSERT INTO work (openalex_id, is_seed) VALUES ('W1', 1);`); err != nil {
		t.Fatalf("insert: %v", err)
	}

	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("third Migrate: %v", err)
	}

	var n int
	if err := db.read.QueryRow(`SELECT count(*) FROM work;`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("work rows = %d, want 1 — migration must not disturb data", n)
	}
	if v, _ := db.Version(ctx); v != SchemaVersion {
		t.Errorf("Version() = %d, want %d", v, SchemaVersion)
	}
}

// The guard this whole version scheme exists for. A library from a newer build
// must be refused, and refused without writing anything at all — a check that
// writes first has already lost.
func TestMigrateRefusesNewerSchema(t *testing.T) {
	t.Parallel()
	db := openTest(t)
	ctx := t.Context()

	// A library as a future fil might leave it: a meta table saying 2, and
	// nothing this build recognises.
	mustExec(t, db.write, `CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT);`)
	mustExec(t, db.write, `INSERT INTO meta (key, value) VALUES ('schema_version', '2');`)

	err := db.Migrate(ctx)
	if err == nil {
		t.Fatal("Migrate() error = nil, want a refusal")
	}
	if !errors.Is(err, errs.ErrSchemaTooNew) {
		t.Errorf("Migrate() error = %v, want it to wrap errs.ErrSchemaTooNew", err)
	}
	// The message has to be actionable: which library, and which versions.
	for _, want := range []string{db.Path(), "2", "1"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}

	if tableExists(t, db, "work") {
		t.Error("Migrate created tables in a library it refused; the refusal must write nothing")
	}
	if v, _ := db.Version(ctx); v != 2 {
		t.Errorf("Version() = %d, want the untouched 2", v)
	}
}

// A version that is neither current nor absent has no upgrade path yet. It must
// fail loudly rather than leave an old library half-understood.
func TestMigrateRefusesUnknownOlderSchema(t *testing.T) {
	t.Parallel()
	db := openTest(t)
	ctx := t.Context()

	mustExec(t, db.write, `CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT);`)
	mustExec(t, db.write, `INSERT INTO meta (key, value) VALUES ('schema_version', 'banana');`)

	err := db.Migrate(ctx)
	if err == nil {
		t.Fatal("Migrate() with a non-numeric version error = nil, want an error")
	}
	if !errors.Is(err, errs.ErrInvalidConfig) {
		t.Errorf("error = %v, want it to wrap errs.ErrInvalidConfig", err)
	}
	if tableExists(t, db, "work") {
		t.Error("Migrate wrote to a library it could not understand")
	}
}

// Migration is one transaction, so a schema that fails partway must leave no
// trace. applySchema takes its SQL as an argument precisely so this is testable.
func TestMigrationIsAllOrNothing(t *testing.T) {
	t.Parallel()
	db := openTest(t)
	ctx := t.Context()

	broken := `
CREATE TABLE first_table (id TEXT PRIMARY KEY);
CREATE TABLE second_table (id TEXT PRIMARY KEY);
THIS IS NOT SQL;
`
	tx, err := db.write.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := applySchema(ctx, tx, broken); err == nil {
		t.Fatal("applySchema with broken SQL error = nil, want an error")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	if tableExists(t, db, "first_table") {
		t.Error("first_table survived a failed migration; it is not all-or-nothing")
	}
	if tableExists(t, db, "second_table") {
		t.Error("second_table survived a failed migration")
	}
}

// PRAGMA journal_mode cannot run inside a transaction, and the transaction is
// what makes migration atomic — so the pragmas have to come out first.
func TestStripPragmas(t *testing.T) {
	t.Parallel()

	// Match executable statements, not the word: schema.sql discusses PRAGMAs in
	// its comments, and stripping must leave those alone.
	if !pragmaStatement.MatchString(Schema) {
		t.Fatal("schema.sql has no PRAGMA statement; this test and stripPragmas are now pointless")
	}
	stripped := stripPragmas(Schema)
	if got := pragmaStatement.FindString(stripped); got != "" {
		t.Errorf("stripped schema still contains a PRAGMA statement: %q", got)
	}
	if !strings.Contains(stripped, "PRAGMA") {
		t.Error("stripping removed the explanatory comments too; only statements should go")
	}
	// Stripping must take the pragmas and nothing else.
	for _, want := range []string{"CREATE TABLE IF NOT EXISTS work", "chunk_fts", "idx_work_frontier", "schema_version"} {
		if !strings.Contains(stripped, want) {
			t.Errorf("stripped schema lost %q", want)
		}
	}
}

// The pragmas are set on the connection, so stripping them from the DDL must
// not leave the migrated library without them.
func TestMigratedLibraryStillHasPragmas(t *testing.T) {
	t.Parallel()
	db := openTest(t)

	if err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	var mode string
	if err := db.write.QueryRow(`PRAGMA journal_mode;`).Scan(&mode); err != nil {
		t.Fatalf("journal_mode: %v", err)
	}
	if !strings.EqualFold(mode, "wal") {
		t.Errorf("journal_mode = %q, want wal", mode)
	}
	var fk int
	if err := db.read.QueryRow(`PRAGMA foreign_keys;`).Scan(&fk); err != nil {
		t.Fatalf("foreign_keys: %v", err)
	}
	if fk != 1 {
		t.Errorf("foreign_keys = %d, want 1", fk)
	}
}

// Migrate must survive the round trip that every real run makes: open, migrate,
// close, open again.
func TestMigratePersistsAcrossReopen(t *testing.T) {
	t.Parallel()
	db := openTest(t)
	ctx := t.Context()

	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	mustExec(t, db.write, `INSERT INTO work (openalex_id) VALUES ('W1');`)
	path := db.Path()
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	again, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer again.Close()

	if err := again.Migrate(ctx); err != nil {
		t.Fatalf("Migrate after reopen: %v", err)
	}
	var id string
	if err := again.read.QueryRow(`SELECT openalex_id FROM work;`).Scan(&id); err != nil {
		t.Fatalf("read: %v", err)
	}
	if id != "W1" {
		t.Errorf("openalex_id = %q, want W1", id)
	}
}

// A stub is a work with an ID and nothing else (ADR-003). If title were ever
// made NOT NULL again, the entire expansion design would break — so the
// migrated schema is checked for it directly.
func TestMigratedSchemaAllowsStubs(t *testing.T) {
	t.Parallel()
	db := openTest(t)
	ctx := t.Context()

	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if _, err := db.write.ExecContext(ctx,
		`INSERT INTO work (openalex_id) VALUES ('W2741809807');`); err != nil {
		t.Fatalf("insert a stub: %v — schema must allow a work with an ID and nothing else", err)
	}

	var hydrated int
	if err := db.read.QueryRow(
		`SELECT hydrated FROM work WHERE openalex_id = 'W2741809807';`).Scan(&hydrated); err != nil {
		t.Fatalf("read stub: %v", err)
	}
	if hydrated != 0 {
		t.Errorf("hydrated = %d, want 0 by default", hydrated)
	}
}
