package store

import (
	"os"
	"testing"
)

func TestListAndIdentity(t *testing.T) {
	t.Parallel()
	db := textLibrary(t) // W2 is the seed; W1 has two PDFs; W3's was deleted
	ctx := t.Context()
	mustExec(t, db.write, `INSERT INTO cites (from_work, to_work) VALUES ('F00000002', 'F00000001');`)
	putText(t, db, Text{FilID: "W2", Path: "papers/b--F00000002/Paper.PDF", SHA256: "b2", Status: "gibberish", Extractor: "e"})

	all, err := db.List(ctx, ListQuery{})
	if err != nil || len(all) != 3 || all[0].FilID != "F00000002" || all[1].FilID != "F00000001" {
		t.Fatalf("List = %+v, %v; want the seed, then the cited work, then the rest", all, err)
	}
	if !all[1].HasPDF || all[1].CitedBy != 1 || all[2].HasPDF {
		t.Errorf("rows = %+v; W1 has a PDF and one citation, W3's PDF was deleted", all)
	}
	for q, want := range map[*ListQuery]int{
		{Seeds: true}: 1, {WithPDF: true}: 2, {WithoutText: true}: 1, {Title: "OPEN access"}: 2, {Title: "growth open"}: 1, {Limit: 2}: 2,
	} {
		if got, _ := db.List(ctx, *q); len(got) != want {
			t.Errorf("List(%+v) = %d rows, want %d", *q, len(got), want)
		}
	}

	// The identity flag follows the file when it moves, and is cleared when
	// its bytes change.
	path := "papers/a--F00000001/published.pdf"
	if err := db.Tx(ctx, func(tx *Tx) error { return tx.SetIdentity(ctx, path, IdentityMismatch) }); err != nil {
		t.Fatal(err)
	}
	if rows, _ := db.List(ctx, ListQuery{WithPDF: true}); !rows[1].Mismatch {
		t.Errorf("mismatch not listed: %+v", rows)
	}
	moved := "papers/Renamed--F00000001/published.pdf"
	if err := db.Tx(ctx, func(tx *Tx) error {
		return tx.MoveFile(ctx, path, File{Path: moved, FilID: "F00000001", SHA256: "a1", Size: 1, Origin: OriginDownload})
	}); err != nil {
		t.Fatal(err)
	}
	identityOf := func(p string) string {
		files, _ := db.FilesOf(ctx, "F00000001")
		for _, f := range files {
			if f.Path == p {
				return f.Identity
			}
		}
		return "?"
	}
	if got := identityOf(moved); got != IdentityMismatch {
		t.Errorf("after a move, identity = %q", got)
	}
	if err := db.Tx(ctx, func(tx *Tx) error {
		return tx.PutFile(ctx, File{Path: moved, FilID: "F00000001", SHA256: "a1-changed", Size: 2, Origin: OriginDownload})
	}); err != nil {
		t.Fatal(err)
	}
	if got := identityOf(moved); got != "" {
		t.Errorf("after the bytes changed, identity = %q, want cleared", got)
	}
}

func TestUpgradeV8Library(t *testing.T) {
	t.Parallel()
	db := openTest(t)
	ctx := t.Context()
	ddl, err := os.ReadFile("testdata/schema_v8.sql")
	if err != nil {
		t.Fatal(err)
	}
	tx, _ := db.write.BeginTx(ctx, nil)
	if err := applySchema(ctx, tx, string(ddl)); err != nil {
		t.Fatal(err)
	}
	tx.Commit()
	mustExec(t, db.write, `INSERT INTO work (fil_id, openalex_id) VALUES ('F00000001', 'W1');`)
	mustExec(t, db.write, `INSERT INTO work_file (path, fil_id, sha256, size, origin) VALUES ('p.pdf', 'F00000001', 's', 1, 'download');`)
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if v, _ := db.Version(ctx); v != SchemaVersion {
		t.Errorf("version = %d", v)
	}
	if err := db.Tx(ctx, func(tx *Tx) error { return tx.SetIdentity(ctx, "p.pdf", IdentityMatch) }); err != nil {
		t.Errorf("identity after upgrade: %v", err)
	}
}
