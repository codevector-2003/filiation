package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// upgradeV1ToV2 re-keys a library on fil IDs (D17, ADR-010).
//
// SQLite cannot change a primary key in place, so every table that refers to a
// work is rebuilt. The v1 tables are renamed out of the way, schema.sql creates
// the v2 tables under the real names, the rows are copied across through a map
// from each OpenAlex ID to its new fil ID, and the v1 tables are dropped.
//
// Foreign keys stay on throughout, which SQLite's generic recipe does not
// manage: renaming a table rewrites the references to it in the tables that
// point at it, so the v1 children follow v1_work and the v2 children point at
// the new work; and the v1 children are dropped before v1_work, so dropping the
// parent cascades into nothing. That keeps the upgrade inside Migrate's one
// transaction, where a failure leaves the library untouched.
//
// Two things are preserved that a plain copy would lose:
//
//   - the rowid order of cites. The frontier ranks a work's references by it —
//     the order OpenAlex sent them in — so it is copied explicitly;
//   - every row. The counts are compared table by table before anything is
//     dropped, and an upgrade that would lose data fails instead of committing.
func upgradeV1ToV2(ctx context.Context, db *DB, tx *sql.Tx) error {
	aside := []string{
		// Triggers and named indexes belong to their table and would move with
		// it — and then schema.sql's CREATE ... IF NOT EXISTS would find the
		// names taken and skip them for the new tables.
		`DROP TRIGGER IF EXISTS chunk_ai;`,
		`DROP TRIGGER IF EXISTS chunk_ad;`,
		`DROP TRIGGER IF EXISTS chunk_au;`,
		`DROP INDEX IF EXISTS idx_work_year;`,
		`DROP INDEX IF EXISTS idx_work_doi;`,
		`DROP INDEX IF EXISTS idx_work_refs;`,
		`DROP INDEX IF EXISTS idx_work_frontier;`,
		`DROP INDEX IF EXISTS idx_cites_to;`,
		`DROP INDEX IF EXISTS idx_cites_intent;`,
		`DROP INDEX IF EXISTS idx_chunk_work;`,
	}
	for _, t := range v1Tables {
		aside = append(aside, `ALTER TABLE `+t+` RENAME TO v1_`+t+`;`)
	}
	for _, q := range aside {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("set v1 tables aside: %s: %w", q, err)
		}
	}

	// The v2 tables. author, collection, meta and chunk_fts already exist and
	// are left as they are; chunk_fts names its content table by name, so it
	// follows the new chunk without being touched.
	if err := applySchema(ctx, tx, Schema); err != nil {
		return err
	}

	if err := mapFilIDs(ctx, db, tx); err != nil {
		return err
	}

	copies := []string{
		`INSERT INTO work (fil_id, openalex_id, doi, arxiv_id, pmid, title, abstract, year,
			venue, type, cited_by_count, oa_status, oa_url, hydrated, fetched_refs,
			unresolved, depth, is_seed, source, added_at)
		 SELECT m.fil_id, w.openalex_id, w.doi, w.arxiv_id, w.pmid, w.title, w.abstract, w.year,
			w.venue, w.type, w.cited_by_count, w.oa_status, w.oa_url, w.hydrated, w.fetched_refs,
			w.unresolved, w.depth, w.is_seed, w.source, w.added_at
		 FROM v1_work w JOIN v1_fil_map m ON m.openalex_id = w.openalex_id;`,
		`INSERT INTO cites (rowid, from_work, to_work, intent, context, section, confidence)
		 SELECT c.rowid, f.fil_id, t.fil_id, c.intent, c.context, c.section, c.confidence
		 FROM v1_cites c
		 JOIN v1_fil_map f ON f.openalex_id = c.from_work
		 JOIN v1_fil_map t ON t.openalex_id = c.to_work
		 ORDER BY c.rowid;`,
		`INSERT INTO authorship (work_id, author_id, position)
		 SELECT m.fil_id, a.author_id, a.position
		 FROM v1_authorship a JOIN v1_fil_map m ON m.openalex_id = a.work_id;`,
		// The index still holds the v1 rows under the same rowids; the insert
		// trigger on the new chunk writes them again.
		`INSERT INTO chunk_fts(chunk_fts) VALUES ('delete-all');`,
		`INSERT INTO chunk (id, work_id, section, page, ordinal, text)
		 SELECT c.id, m.fil_id, c.section, c.page, c.ordinal, c.text
		 FROM v1_chunk c JOIN v1_fil_map m ON m.openalex_id = c.work_id;`,
		`INSERT INTO note (id, work_id, body, created_at)
		 SELECT n.id, m.fil_id, n.body, n.created_at
		 FROM v1_note n JOIN v1_fil_map m ON m.openalex_id = n.work_id;`,
		`INSERT INTO collection_work (collection_id, work_id)
		 SELECT cw.collection_id, m.fil_id
		 FROM v1_collection_work cw JOIN v1_fil_map m ON m.openalex_id = cw.work_id;`,
	}
	for _, q := range copies {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("copy into v2: %w", err)
		}
	}

	for _, t := range v1Tables {
		var before, after int
		err := tx.QueryRowContext(ctx,
			`SELECT (SELECT count(*) FROM v1_`+t+`), (SELECT count(*) FROM `+t+`);`).Scan(&before, &after)
		if err != nil {
			return fmt.Errorf("count %s: %w", t, err)
		}
		if before != after {
			return fmt.Errorf("%s had %d rows and would have %d; nothing was changed", t, before, after)
		}
	}

	// Children first, so dropping v1_work cascades into nothing.
	drops := []string{
		`DROP TABLE v1_collection_work;`, `DROP TABLE v1_note;`, `DROP TABLE v1_chunk;`,
		`DROP TABLE v1_authorship;`, `DROP TABLE v1_cites;`, `DROP TABLE v1_work;`,
		`DROP TABLE v1_fil_map;`,
		`UPDATE meta SET value = '2' WHERE key = 'schema_version';`,
	}
	for _, q := range drops {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("drop v1 tables: %s: %w", q, err)
		}
	}
	return nil
}

// v1Tables are the v1 tables that refer to a work, parent first.
var v1Tables = []string{"work", "cites", "authorship", "chunk", "note", "collection_work"}

// mapFilIDs gives every v1 work a fresh fil ID, recorded in v1_fil_map. IDs
// are checked against each other as they are drawn; a v1 library has no
// aliases and no fil IDs for them to clash with.
func mapFilIDs(ctx context.Context, db *DB, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx,
		`CREATE TABLE v1_fil_map (openalex_id TEXT PRIMARY KEY, fil_id TEXT NOT NULL UNIQUE);`); err != nil {
		return fmt.Errorf("create fil ID map: %w", err)
	}

	rows, err := tx.QueryContext(ctx, `SELECT openalex_id FROM v1_work ORDER BY openalex_id;`)
	if err != nil {
		return fmt.Errorf("list v1 works: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("scan v1 work: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("list v1 works: %w", err)
	}

	stmt, err := tx.PrepareContext(ctx, `INSERT INTO v1_fil_map (openalex_id, fil_id) VALUES (?, ?);`)
	if err != nil {
		return fmt.Errorf("prepare fil ID map: %w", err)
	}
	defer stmt.Close()

	used := make(map[string]bool, len(ids))
	for _, id := range ids {
		fil := db.newID()
		for used[fil] {
			fil = db.newID()
		}
		used[fil] = true
		if _, err := stmt.ExecContext(ctx, id, fil); err != nil {
			return fmt.Errorf("map %s to a fil ID: %w", id, err)
		}
	}
	return nil
}

// checkForeignKeys fails if any row points at a row that does not exist. It
// runs before an upgrade commits: a rebuild that left a dangling reference has
// broken the graph, and must not be kept.
func checkForeignKeys(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `PRAGMA foreign_key_check;`)
	if err != nil {
		return fmt.Errorf("check foreign keys: %w", err)
	}
	defer rows.Close()
	var bad []string
	for rows.Next() {
		var (
			table, parent string
			rowid         sql.NullInt64
			fkid          int
		)
		if err := rows.Scan(&table, &rowid, &parent, &fkid); err != nil {
			return fmt.Errorf("check foreign keys: %w", err)
		}
		bad = append(bad, fmt.Sprintf("%s row %d → %s", table, rowid.Int64, parent))
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("check foreign keys: %w", err)
	}
	if len(bad) > 0 {
		return fmt.Errorf("%d rows point at nothing, first %s", len(bad), strings.Join(bad[:min(3, len(bad))], ", "))
	}
	return nil
}
