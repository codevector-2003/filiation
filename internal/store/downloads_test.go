package store

import (
	"os"
	"testing"
	"time"
)

// fetchLibrary has five works with PDF links: W2 a seed, W3 cited twice, W1
// once; W4 already has its PDF on disk and W5 had one the researcher deleted.
func fetchLibrary(t *testing.T) *DB {
	t.Helper()
	db := migrated(t)
	for _, q := range []string{
		`INSERT INTO work (fil_id, openalex_id, is_seed) VALUES ('F00000001', 'W1', 0), ('F00000002', 'W2', 1),
			('F00000003', 'W3', 0), ('F00000004', 'W4', 0), ('F00000005', 'W5', 0), ('F00000006', 'W6', 0);`,
		`INSERT INTO cites (from_work, to_work) VALUES ('F00000002', 'F00000003'), ('F00000004', 'F00000003'), ('F00000002', 'F00000001');`,
		`INSERT INTO work_pdf (fil_id, availability) VALUES ('F00000001', 'pdf-link'), ('F00000002', 'pdf-link'),
			('F00000003', 'pdf-link'), ('F00000004', 'pdf-link'), ('F00000005', 'pdf-link'), ('F00000006', 'no-open-copy');`,
		`INSERT INTO work_location (fil_id, rank, pdf_url, version, host, source) VALUES
			('F00000001', 0, 'https://a.example/1.pdf', 'published', 'a.example', 'openalex'),
			('F00000002', 0, 'https://a.example/2.pdf', 'published', 'a.example', 'openalex'),
			('F00000002', 1, 'https://arxiv.org/pdf/2', 'preprint', 'arxiv.org', 'openalex'),
			('F00000003', 0, 'https://a.example/3.pdf', 'published', 'a.example', 'openalex'),
			('F00000004', 0, 'https://a.example/4.pdf', 'published', 'a.example', 'openalex'),
			('F00000005', 0, 'https://a.example/5.pdf', 'published', 'a.example', 'openalex');`,
		`INSERT INTO work_file (path, fil_id, sha256, size, origin) VALUES ('papers/x--F00000004/published.pdf', 'F00000004', 'aa', 1, 'download');`,
		`INSERT INTO work_file (path, fil_id, sha256, size, origin, missing) VALUES ('papers/y--F00000005/published.pdf', 'F00000005', 'bb', 1, 'download', 1);`,
	} {
		mustExec(t, db.write, q)
	}
	return db
}

func fetchIDs(t *testing.T, db *DB, limit int) []string {
	t.Helper()
	got, err := db.WorksToFetch(t.Context(), limit, 30*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, f := range got {
		ids = append(ids, f.FilID)
	}
	return ids
}

func TestWorksToFetch(t *testing.T) {
	t.Parallel()
	db := fetchLibrary(t)
	ctx := t.Context()

	// Seed first, then by citations; W4 has its PDF, W5's was deleted, W6
	// has no link.
	if got := fetchIDs(t, db, 10); len(got) != 3 || got[0] != "F00000002" || got[1] != "F00000003" || got[2] != "F00000001" {
		t.Fatalf("WorksToFetch = %v", got)
	}
	all, _ := db.WorksToFetch(ctx, 1, 30*24*time.Hour)
	if len(all) != 1 || len(all[0].Links) != 2 || all[0].Links[0].Host != "a.example" {
		t.Errorf("seed's links = %+v; want both, published first", all)
	}

	// The seed's publisher refuses: its preprint is still to try. W3's only
	// link fails: it drops out for a month.
	err := db.Tx(ctx, func(tx *Tx) error {
		if err := tx.RecordAttempt(ctx, "W2", Attempt{URL: "https://a.example/2.pdf", Host: "a.example", Outcome: "refused", Status: 403}); err != nil {
			return err
		}
		return tx.RecordAttempt(ctx, "W3", Attempt{URL: "https://a.example/3.pdf", Host: "a.example", Outcome: "not-a-pdf", Status: 200, Millis: 120})
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := fetchIDs(t, db, 10); len(got) != 2 || got[0] != "F00000002" || got[1] != "F00000001" {
		t.Errorf("after failures = %v; want W2 (preprint left) and W1", got)
	}
	links, _ := db.LinksToTry(ctx, "W2", 30*24*time.Hour)
	if len(links) != 1 || links[0].Host != "arxiv.org" {
		t.Errorf("W2's links to try = %+v", links)
	}
	// Named by a person, every link is tried again.
	if links, _ := db.LinksToTry(ctx, "W3", 0); len(links) != 1 {
		t.Errorf("W3 asked for by name = %+v", links)
	}
	// After the month, W3 is tried again.
	mustExec(t, db.write, `UPDATE download_attempt SET at = datetime('now', '-31 days');`)
	if got := fetchIDs(t, db, 10); len(got) != 3 {
		t.Errorf("a month on = %v", got)
	}

	attempts, err := db.AttemptsOf(ctx, "W3")
	if err != nil || len(attempts) != 1 || attempts[0].Outcome != "not-a-pdf" || attempts[0].Millis != 120 || attempts[0].At.IsZero() {
		t.Errorf("AttemptsOf = %+v, %v", attempts, err)
	}
	if n, err := db.PDFsOnDisk(ctx); err != nil || n != 1 {
		t.Errorf("PDFsOnDisk = %d, %v; the deleted one does not count", n, err)
	}
}

func TestUpgradeV4Library(t *testing.T) {
	t.Parallel()
	db := openTest(t)
	ctx := t.Context()
	ddl, err := os.ReadFile("testdata/schema_v4.sql")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.write.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := applySchema(ctx, tx, string(ddl)); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db.write, `INSERT INTO work (fil_id, openalex_id) VALUES ('F7K2M9QXA', 'W1');`)
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if v, _ := db.Version(ctx); v != SchemaVersion {
		t.Errorf("version = %d", v)
	}
	if err := db.Tx(ctx, func(tx *Tx) error {
		return tx.RecordAttempt(ctx, "W1", Attempt{URL: "https://x/y.pdf", Host: "x", Outcome: "ok"})
	}); err != nil {
		t.Errorf("download_attempt after upgrade: %v", err)
	}
}
