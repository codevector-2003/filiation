package store

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/codevector-2003/filiation/internal/identity"
)

// v1Library builds a library exactly as v0.1 and v0.2 left one: the v1 schema,
// keyed on openalex_id, and filled by v1 SQL.
func v1Library(t *testing.T) *DB {
	t.Helper()
	db := openTest(t)
	ddl, err := os.ReadFile("testdata/schema_v1.sql")
	if err != nil {
		t.Fatalf("read v1 schema: %v", err)
	}
	tx, err := db.write.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := applySchema(t.Context(), tx, string(ddl)); err != nil {
		t.Fatalf("apply v1 schema: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if v, err := db.Version(t.Context()); err != nil || v != 1 {
		t.Fatalf("v1 library reports version %d, %v", v, err)
	}

	// A seed, W1, citing W9, W3 and W7 in that order — deliberately not ID
	// order, because the frontier ranks references by the order they were
	// recorded in, and the upgrade must keep it. W3 is hydrated and cites W9.
	for _, q := range []string{
		`INSERT INTO work (openalex_id, doi, title, year, hydrated, fetched_refs, depth, is_seed, source)
		 VALUES ('W1', '10.7717/peerj.4375', 'The state of OA', 2018, 1, 1, 0, 1, 'seed');`,
		`INSERT INTO work (openalex_id, depth, source) VALUES ('W9', 1, 'expansion');`,
		`INSERT INTO work (openalex_id, title, hydrated, fetched_refs, depth, source)
		 VALUES ('W3', 'A cited paper', 1, 1, 1, 'expansion');`,
		`INSERT INTO work (openalex_id, depth, source, unresolved) VALUES ('W7', 1, 'expansion', 1);`,
		`INSERT INTO cites (from_work, to_work) VALUES ('W1', 'W9');`,
		`INSERT INTO cites (from_work, to_work, context) VALUES ('W1', 'W3', 'as shown by [2]');`,
		`INSERT INTO cites (from_work, to_work) VALUES ('W1', 'W7');`,
		`INSERT INTO cites (from_work, to_work) VALUES ('W3', 'W9');`,
		`INSERT INTO author (openalex_id, name) VALUES ('A1', 'Heather Piwowar'), ('A2', 'Jason Priem');`,
		`INSERT INTO authorship (work_id, author_id, position) VALUES ('W1', 'A1', 0), ('W1', 'A2', 1);`,
		`INSERT INTO chunk (id, work_id, section, page, ordinal, text)
		 VALUES (7, 'W1', 'intro', 1, 0, 'Unpaywall finds legal open copies of papers');`,
		`INSERT INTO note (id, work_id, body) VALUES (3, 'W1', 'read this first');`,
		`INSERT INTO collection (id, name) VALUES (1, 'Chapter 2');`,
		`INSERT INTO collection_work (collection_id, work_id) VALUES (1, 'W3');`,
	} {
		mustExec(t, db.write, q)
	}
	return db
}

func TestUpgradeV1Library(t *testing.T) {
	t.Parallel()
	db := v1Library(t)
	ctx := t.Context()

	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if v, err := db.Version(ctx); err != nil || v != SchemaVersion {
		t.Fatalf("Version after upgrade = %d, %v; want %d", v, err, SchemaVersion)
	}

	// Every work kept, each with its own new fil ID.
	fils := map[string]string{}
	for _, id := range []string{"W1", "W3", "W7", "W9"} {
		w, err := db.GetWork(ctx, id)
		if err != nil {
			t.Fatalf("GetWork(%s) after upgrade: %v", id, err)
		}
		if !identity.IsFilID(w.FilID) {
			t.Errorf("%s has fil ID %q", id, w.FilID)
		}
		if other, dup := fils[w.FilID]; dup {
			t.Errorf("%s and %s share fil ID %s", id, other, w.FilID)
		}
		fils[w.FilID] = id
	}
	seed, _ := db.GetWork(ctx, "W1")
	if *seed.Title != "The state of OA" || *seed.DOI != "10.7717/peerj.4375" || !seed.IsSeed ||
		*seed.Depth != 0 || *seed.Year != 2018 || !seed.FetchedRefs {
		t.Errorf("seed after upgrade = %+v; metadata and flags must survive", seed)
	}
	if w7, _ := db.GetWork(ctx, "W7"); !w7.Unresolved || w7.Hydrated {
		t.Errorf("W7 after upgrade = %+v, want still an unresolved stub", w7)
	}

	// The order W1's references were recorded in survives.
	rows, err := db.read.QueryContext(ctx, `
SELECT t.openalex_id FROM cites c JOIN work t ON t.fil_id = c.to_work
WHERE c.from_work = ? ORDER BY c.rowid;`, seed.FilID)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for rows.Next() {
		var id string
		rows.Scan(&id) //nolint:errcheck // checked by the comparison
		order = append(order, id)
	}
	rows.Close()
	if strings.Join(order, ",") != "W9,W3,W7" {
		t.Errorf("W1's references in recorded order = %v, want W9,W3,W7", order)
	}
	if s, _ := db.Stats(ctx); s != (Stats{Works: 4, Stubs: 2, Edges: 4}) {
		t.Errorf("Stats after upgrade = %+v, want 4 works, 2 stubs, 4 edges", s)
	}
	refs, _ := db.References(ctx, "W1")
	for _, e := range refs {
		if oa(t, db, e.ToWork)[0] == "W3" && e.Context != "as shown by [2]" {
			t.Errorf("edge W1 -> W3 context = %q, want it kept", e.Context)
		}
	}

	// Authors, text, notes and collections follow their works.
	authors, err := db.AuthorsOf(ctx, []string{seed.FilID})
	if err != nil || len(authors[seed.FilID]) != 2 || authors[seed.FilID][0].Name != "Heather Piwowar" {
		t.Errorf("authors of W1 after upgrade = %v, %v", authors, err)
	}
	var hit int
	if err := db.read.QueryRow(
		`SELECT rowid FROM chunk_fts WHERE chunk_fts MATCH 'unpaywall';`).Scan(&hit); err != nil || hit != 7 {
		t.Errorf("keyword search after upgrade = chunk %d, %v; want chunk 7", hit, err)
	}
	var chunkWork, noteWork, collWork string
	db.read.QueryRow(`SELECT work_id FROM chunk WHERE id = 7;`).Scan(&chunkWork)                     //nolint:errcheck
	db.read.QueryRow(`SELECT work_id FROM note WHERE id = 3;`).Scan(&noteWork)                       //nolint:errcheck
	db.read.QueryRow(`SELECT work_id FROM collection_work WHERE collection_id = 1;`).Scan(&collWork) //nolint:errcheck
	if chunkWork != seed.FilID || noteWork != seed.FilID || fils[collWork] != "W3" {
		t.Errorf("chunk, note, collection point at %q, %q, %q; want W1, W1, W3 by fil ID",
			chunkWork, noteWork, collWork)
	}

	// The v2 machinery works on the upgraded library: new chunks are
	// indexed, the frontier still finds stubs, and nothing of v1 is left.
	mustExec(t, db.write, `INSERT INTO chunk (work_id, text) VALUES (?, 'citation genealogy');`, seed.FilID)
	var n int
	if err := db.read.QueryRow(
		`SELECT count(*) FROM chunk_fts WHERE chunk_fts MATCH 'genealogy';`).Scan(&n); err != nil || n != 1 {
		t.Errorf("a chunk added after the upgrade is not searchable (%d, %v)", n, err)
	}
	front, err := db.NextFrontier(ctx, 10, 5, 0, nil)
	if err != nil || len(front) != 1 || front[0].OpenAlexID != "W9" || front[0].InDegree != 2 {
		t.Errorf("frontier after upgrade = %+v, %v; want W9 cited twice", front, err)
	}
	for _, table := range []string{"v1_work", "v1_cites", "v1_authorship", "v1_chunk", "v1_note",
		"v1_collection_work", "v1_fil_map"} {
		if tableExists(t, db, table) {
			t.Errorf("%s left behind after the upgrade", table)
		}
	}
	for _, index := range []string{"idx_work_frontier", "idx_cites_to", "idx_chunk_work", "idx_work_alias_fil"} {
		if !tableExists(t, db, index) {
			t.Errorf("index %s missing after the upgrade", index)
		}
	}

	// A second run changes nothing.
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	if again, _ := db.GetWork(ctx, "W1"); again.FilID != seed.FilID {
		t.Errorf("fil ID changed on a second Migrate: %s -> %s", seed.FilID, again.FilID)
	}
	var fk int
	if err := db.write.QueryRow(`PRAGMA foreign_keys;`).Scan(&fk); err != nil || fk != 1 {
		t.Errorf("foreign_keys on the write handle after the upgrade = %d, %v; want 1", fk, err)
	}
}

// An upgrade that would lose rows must not commit. The v1 library here holds
// an edge to a work that does not exist — possible only with foreign keys off
// — and the copy, which joins through the ID map, cannot carry it.
func TestUpgradeThatWouldLoseRowsChangesNothing(t *testing.T) {
	t.Parallel()
	db := v1Library(t)
	ctx := t.Context()
	mustExec(t, db.write, `PRAGMA foreign_keys = OFF;`)
	mustExec(t, db.write, `INSERT INTO cites (from_work, to_work) VALUES ('W1', 'W404');`)
	mustExec(t, db.write, `PRAGMA foreign_keys = ON;`)

	err := db.Migrate(ctx)
	if err == nil || !strings.Contains(err.Error(), "cites had 5 rows") {
		t.Fatalf("Migrate = %v, want a refusal naming the lost cites row", err)
	}
	if v, _ := db.Version(ctx); v != 1 {
		t.Errorf("Version after a failed upgrade = %d, want the untouched 1", v)
	}
	var cols []string
	rows, err := db.read.QueryContext(ctx, `SELECT name FROM pragma_table_info('work');`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var c string
		rows.Scan(&c) //nolint:errcheck
		cols = append(cols, c)
	}
	rows.Close()
	if slices.Contains(cols, "fil_id") || !slices.Contains(cols, "pdf_sha256") {
		t.Errorf("work columns after a failed upgrade = %v, want the v1 table untouched", cols)
	}
	if tableExists(t, db, "v1_work") || tableExists(t, db, "work_alias") {
		t.Error("a failed upgrade left tables behind")
	}
}

// A version with no upgrade path is refused before anything is written.
func TestUpgradeWithNoPathIsRefused(t *testing.T) {
	t.Parallel()
	db := openTest(t)
	mustExec(t, db.write, `CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT);`)
	mustExec(t, db.write, `INSERT INTO meta (key, value) VALUES ('schema_version', '-3');`)
	if err := db.Migrate(context.Background()); err == nil || !strings.Contains(err.Error(), "no upgrade path") {
		t.Errorf("Migrate of schema -3 = %v, want no upgrade path", err)
	}
}
