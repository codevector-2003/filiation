package store

import (
	"os"
	"testing"

	"github.com/codevector-2003/filiation/internal/model"
)

func TestRecordImport(t *testing.T) {
	t.Parallel()
	db := textLibrary(t)
	ctx := t.Context()
	if _, ok, err := db.ImportOf(ctx, "h1"); ok || err != nil {
		t.Fatalf("ImportOf before = %v, %v", ok, err)
	}
	record := func(a ImportAttempt) {
		t.Helper()
		if err := db.Tx(ctx, func(tx *Tx) error { return tx.RecordImport(ctx, a) }); err != nil {
			t.Fatal(err)
		}
	}
	record(ImportAttempt{SHA256: "h1", Path: "Inbox/a.pdf", Outcome: ImportCandidates, Reason: "choose", Searched: true})
	record(ImportAttempt{SHA256: "h1", Path: "Inbox/a.pdf", Outcome: ImportImported, FilID: "F00000001"})
	a, ok, err := db.ImportOf(ctx, "h1")
	if err != nil || !ok || a.Outcome != ImportImported || a.FilID != "F00000001" || a.Reason != "" || a.At.IsZero() {
		t.Errorf("ImportOf = %+v, %v, %v", a, ok, err)
	}
	if !a.Searched {
		t.Error("a title search spent on the file was forgotten")
	}
}

func TestAddLocalWork(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()
	var fil string
	if err := db.Tx(ctx, func(tx *Tx) error {
		var err error
		fil, err = tx.AddLocalWork(ctx, "Field notes on bees", model.Ptr(2025))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	w, err := db.GetWork(ctx, fil)
	if err != nil {
		t.Fatal(err)
	}
	if w.OpenAlexID != "" || w.DisplayTitle() != "Field notes on bees" || !w.IsSeed || w.Source != model.SourceUpload || !w.Hydrated {
		t.Errorf("local document = %+v", w)
	}
	// Never on the expansion frontier: there is nothing to fetch.
	items, err := db.NextFrontier(ctx, 10, 10, 0, nil)
	if err != nil || len(items) != 0 {
		t.Errorf("frontier = %+v, %v", items, err)
	}
}

func TestUpgradeV6Library(t *testing.T) {
	t.Parallel()
	db := openTest(t)
	ctx := t.Context()
	ddl, err := os.ReadFile("testdata/schema_v6.sql")
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
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if v, _ := db.Version(ctx); v != SchemaVersion {
		t.Errorf("version = %d", v)
	}
	if err := db.Tx(ctx, func(tx *Tx) error {
		return tx.RecordImport(ctx, ImportAttempt{SHA256: "h", Path: "x.pdf", Outcome: ImportUnidentified})
	}); err != nil {
		t.Errorf("import_attempt after upgrade: %v", err)
	}
}
