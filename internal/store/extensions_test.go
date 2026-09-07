package store

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

// openRaw is a bare database/sql handle. The real Open, with its two handles
// and PRAGMAs, is step 4.3 onwards; these tests are about one thing only —
// whether the extension is present on connections this package causes to exist.
func openRaw(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", "file:"+path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Ping(); err != nil {
		t.Fatalf("ping %s: %v", path, err)
	}
	return db
}

// The driver must be registered under the name the rest of the package uses.
// Importing store is what registers it, so this fails loudly if the blank
// import in extensions.go is ever tidied away.
func TestSQLiteDriverIsRegistered(t *testing.T) {
	t.Parallel()
	for _, name := range sql.Drivers() {
		if name == "sqlite3" {
			return
		}
	}
	t.Fatalf("sqlite3 driver not registered; have %v", sql.Drivers())
}

// FTS5 must be usable without any caller having asked for it.
func TestFTS5IsAvailable(t *testing.T) {
	t.Parallel()
	db := openRaw(t, filepath.Join(t.TempDir(), "library.db"))

	if _, err := db.Exec(`CREATE VIRTUAL TABLE t USING fts5(body);`); err != nil {
		t.Fatalf("create fts5 table: %v — extension not registered before the connection opened", err)
	}
	if _, err := db.Exec(`INSERT INTO t(body) VALUES ('citation graph filiation');`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	var got string
	err := db.QueryRow(`SELECT body FROM t WHERE t MATCH 'filiation';`).Scan(&got)
	if err != nil {
		t.Fatalf("match query: %v", err)
	}
	if !strings.Contains(got, "filiation") {
		t.Errorf("got %q, want the indexed row", got)
	}
}

// The failure spike 5 warned about, reproduced as an assertion: a table created
// with FTS5 must still be readable from a connection opened later, in a
// different pool. This is the test that would catch a registration moved out of
// init and into Open.
func TestFTS5SurvivesReopen(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "library.db")

	first := openRaw(t, path)
	if _, err := first.Exec(`CREATE VIRTUAL TABLE t USING fts5(body);`); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := first.Exec(`INSERT INTO t(body) VALUES ('reference list');`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	second := openRaw(t, path)
	var n int
	if err := second.QueryRow(`SELECT count(*) FROM t;`).Scan(&n); err != nil {
		t.Fatalf("read fts5 table on a new connection: %v", err)
	}
	if n != 1 {
		t.Errorf("count = %d, want 1", n)
	}
}

// The whole embedded schema uses FTS5 and three sync triggers. Applying it is
// the real proof that registration happens early enough, since it is what every
// first open will do.
func TestSchemaAppliesWithExtensions(t *testing.T) {
	t.Parallel()
	db := openRaw(t, filepath.Join(t.TempDir(), "library.db"))

	if _, err := db.Exec(Schema); err != nil {
		t.Fatalf("apply schema.sql: %v", err)
	}
	for _, table := range []string{"work", "cites", "chunk", "chunk_fts", "meta"} {
		var name string
		err := db.QueryRow(
			`SELECT name FROM sqlite_master WHERE name = ?;`, table).Scan(&name)
		if err != nil {
			t.Errorf("table %s missing after schema apply: %v", table, err)
		}
	}
}

// Registering twice would run the extension twice on every new connection.
// init has already called this; calling it again must be a no-op.
func TestRegisterExtensionsIsIdempotent(t *testing.T) {
	t.Parallel()
	registerExtensions()
	registerExtensions()

	db := openRaw(t, filepath.Join(t.TempDir(), "library.db"))
	if _, err := db.Exec(`CREATE VIRTUAL TABLE t USING fts5(body);`); err != nil {
		t.Fatalf("fts5 unusable after repeated registration: %v", err)
	}
}
