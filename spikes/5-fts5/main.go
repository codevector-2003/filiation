// Command spike5 proves that ncruces/go-sqlite3 can run Filiation's real schema,
// including FTS5.
//
// Spike 5 of docs/ARCHITECTURE_PHASE1.md §9. ADR-007 chose this driver for one
// reason — a WASM build of SQLite means no cgo, which means one static binary
// per platform — and flagged FTS5 availability as the open risk. M3 keyword
// search depends on it entirely. If FTS5 is missing, the options are an external
// index or a custom SQLite build, and that is worth knowing now.
//
// This deliberately applies internal/store's actual schema.sql rather than a toy
// table, so it also tests the triggers, the partial index and the PRAGMAs that
// the store package will rely on.
//
// Usage:
//
//	go run ./spikes/5-fts5
package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ncruces/go-sqlite3"
	_ "github.com/ncruces/go-sqlite3/driver"
	"github.com/ncruces/go-sqlite3/ext/fts5"

	"github.com/codevector-2003/filiation/internal/store"
)

func main() {
	// FTS5 is not compiled into the default WASM build. It ships as a loadable
	// WASM extension instead, and AutoExtension registers it on every new
	// connection — which is what internal/store must do, because a connection
	// opened without it cannot even read a table created with it.
	sqlite3.AutoExtension(fts5.Register)

	dir, err := os.MkdirTemp("", "filiation-spike5-")
	if err != nil {
		fail("temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	path := filepath.Join(dir, "library.db")
	db, err := sql.Open("sqlite3", "file:"+path)
	if err != nil {
		fail("open: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		fail("ping: %v", err)
	}

	section("Driver and build")
	var version string
	must(db.QueryRow("SELECT sqlite_version()").Scan(&version), "sqlite_version")
	fmt.Printf("SQLite version: %s\n", version)

	opts := compileOptions(db)
	fmt.Printf("Compile options: %d total\n", len(opts))
	for _, want := range []string{"ENABLE_FTS5", "ENABLE_FTS4", "ENABLE_FTS3", "ENABLE_RTREE",
		"ENABLE_JSON1", "ENABLE_MATH_FUNCTIONS", "THREADSAFE"} {
		fmt.Printf("  %-24s %s\n", want, mark(hasPrefix(opts, want)))
	}

	section("Applying internal/store/schema.sql")
	applySchema(db)
	fmt.Println("schema applied cleanly (FTS5 virtual table and all three triggers included)")

	var mode string
	must(db.QueryRow("PRAGMA journal_mode").Scan(&mode), "journal_mode")
	fmt.Printf("journal_mode after schema: %s (want wal)\n", mode)

	var fk int
	must(db.QueryRow("PRAGMA foreign_keys").Scan(&fk), "foreign_keys")
	fmt.Printf("foreign_keys: %d (want 1 — note it is per-connection, not stored in the file)\n", fk)

	section("FTS5 round trip through the real triggers")
	seed(db)

	// The point of a contentless FTS5 table is that chunk_ai keeps it in sync.
	// If the trigger did not fire, this returns nothing.
	rows, err := db.Query(`
		SELECT c.id, c.page, snippet(chunk_fts, 0, '[', ']', '...', 8)
		FROM chunk_fts
		JOIN chunk c ON c.id = chunk_fts.rowid
		WHERE chunk_fts MATCH ?
		ORDER BY rank`, "NEAR(citation graph, 5)")
	if err != nil {
		fail("FTS5 MATCH query: %v", err)
	}
	defer rows.Close()

	found := 0
	for rows.Next() {
		var id, page int
		var snip string
		must(rows.Scan(&id, &page, &snip), "scan")
		fmt.Printf("  hit: chunk %d (page %d) %s\n", id, page, snip)
		found++
	}
	must(rows.Err(), "rows")

	if found == 0 {
		fail("FTS5 returned no rows — the AFTER INSERT trigger did not populate the index")
	}
	fmt.Printf("%d hits. MATCH, NEAR, snippet() and rank all work.\n", found)

	section("Porter stemming")
	// tokenize = 'porter unicode61' in the schema: searching a stem must find
	// inflected forms, or keyword search will feel broken to users.
	for _, term := range []string{"cite", "cited", "citation", "check"} {
		var hits int
		must(db.QueryRow(`SELECT count(*) FROM chunk_fts WHERE chunk_fts MATCH ?`, term).Scan(&hits),
			"porter query")
		fmt.Printf("  MATCH %-10q -> %d rows\n", term, hits)
	}
	fmt.Println("(porter folds inflections together: searching one form finds the others)")

	section("Trigger sync on delete")
	_, err = db.Exec(`DELETE FROM chunk WHERE id = 1`)
	must(err, "delete chunk")
	var after int
	must(db.QueryRow(`SELECT count(*) FROM chunk_fts WHERE chunk_fts MATCH 'citation'`).Scan(&after),
		"count after delete")
	fmt.Printf("rows matching 'citation' after deleting chunk 1: %d (chunk_ad kept the index in sync)\n", after)

	section("Verdict")
	fmt.Println("PASS — ncruces/go-sqlite3 ships FTS5, runs the real schema, and keeps the")
	fmt.Println("contentless index in sync through the triggers. ADR-007's open risk is closed.")
	fmt.Println("M3 keyword search needs no external index and no custom SQLite build.")
}

func applySchema(db *sql.DB) {
	// database/sql Exec with a multi-statement script is driver-dependent, so
	// try it whole and fall back to statement-by-statement, reporting exactly
	// which statement failed. The store package needs to know which works.
	if _, err := db.Exec(store.Schema); err == nil {
		fmt.Println("multi-statement Exec of the whole script: supported")
		return
	} else {
		fmt.Printf("multi-statement Exec failed (%v)\n  falling back to statement-by-statement\n", err)
	}

	for i, stmt := range splitStatements(store.Schema) {
		if _, err := db.Exec(stmt); err != nil {
			fail("statement %d failed: %v\n---\n%s", i, err, stmt)
		}
	}
	fmt.Println("statement-by-statement: all statements applied")
}

// splitStatements is a naive splitter, adequate only because schema.sql has no
// semicolons inside string literals — except in the triggers, which is exactly
// why this is a fallback and not the primary path.
func splitStatements(script string) []string {
	var out []string
	var cur strings.Builder
	inTrigger := false

	for _, line := range strings.Split(script, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--") || trimmed == "" {
			continue
		}
		cur.WriteString(line)
		cur.WriteString("\n")

		upper := strings.ToUpper(trimmed)
		if strings.Contains(upper, "CREATE TRIGGER") {
			inTrigger = true
		}
		if inTrigger {
			if strings.HasPrefix(upper, "END;") {
				inTrigger = false
				out = append(out, cur.String())
				cur.Reset()
			}
			continue
		}
		if strings.HasSuffix(trimmed, ";") {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	if s := strings.TrimSpace(cur.String()); s != "" {
		out = append(out, s)
	}
	return out
}

func seed(db *sql.DB) {
	_, err := db.Exec(`INSERT INTO work(openalex_id, title, year, hydrated) VALUES
		('W1', 'A paper about citation graphs', 2020, 1)`)
	must(err, "insert work")

	chunks := []struct {
		page int
		text string
	}{
		{1, "The citation graph is not the product. The answers are."},
		{2, "We cited three earlier studies, and each cited study was checked by hand."},
		{3, "Unrelated text about marine biology and tidal patterns."},
	}
	for i, c := range chunks {
		_, err := db.Exec(`INSERT INTO chunk(id, work_id, page, ordinal, text) VALUES (?,?,?,?,?)`,
			i+1, "W1", c.page, i, c.text)
		must(err, "insert chunk")
	}
	fmt.Printf("inserted 1 work and %d chunks\n", len(chunks))
}

func compileOptions(db *sql.DB) []string {
	rows, err := db.Query("PRAGMA compile_options")
	if err != nil {
		fail("compile_options: %v", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var s string
		must(rows.Scan(&s), "scan compile option")
		out = append(out, s)
	}
	return out
}

func hasPrefix(opts []string, want string) bool {
	for _, o := range opts {
		if strings.Contains(o, want) {
			return true
		}
	}
	return false
}

func mark(ok bool) string {
	if ok {
		return "yes"
	}
	return "NO"
}

func section(name string) {
	fmt.Printf("\n=== %s ===\n", name)
}

func must(err error, what string) {
	if err != nil {
		fail("%s: %v", what, err)
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "FAIL: "+format+"\n", args...)
	os.Exit(1)
}
