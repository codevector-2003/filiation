package store

import (
	"os"
	"testing"
)

func TestZoteroCollectionAdoptsAndFollows(t *testing.T) {
	t.Parallel()
	db := organised(t)
	ctx := t.Context()
	var mine, chapter, again int64
	err := db.Tx(ctx, func(tx *Tx) error {
		var err error
		if mine, err = tx.CreateCollection(ctx, "Thesis", 0); err != nil {
			return err
		}
		// The user's own Thesis is the Zotero one.
		thesis, err := tx.ZoteroCollection(ctx, "THESIS01", "Thesis", 0)
		if err != nil || thesis != mine {
			t.Errorf("Thesis = %d, %v; want the user's own, %d", thesis, err, mine)
		}
		if chapter, err = tx.ZoteroCollection(ctx, "CHAPTER2", "Chapter 2", thesis); err != nil {
			return err
		}
		// Renamed in Zotero and moved to the top: it follows.
		again, err = tx.ZoteroCollection(ctx, "CHAPTER2", "Chapter Two", 0)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	cs, _ := db.Collections(ctx)
	if again != chapter || len(cs) != 2 || cs[0].Name != "Chapter Two" || cs[0].Parent != 0 || cs[1].Zotero != "THESIS01" {
		t.Errorf("collections = %+v", cs)
	}
}

func TestUpgradeV11Library(t *testing.T) {
	t.Parallel()
	db := openTest(t)
	ctx := t.Context()
	ddl, err := os.ReadFile("testdata/schema_v11.sql")
	if err != nil {
		t.Fatal(err)
	}
	tx, _ := db.write.BeginTx(ctx, nil)
	if err := applySchema(ctx, tx, string(ddl)); err != nil {
		t.Fatal(err)
	}
	tx.Commit()
	mustExec(t, db.write, `INSERT INTO work (fil_id, openalex_id) VALUES ('F00000001', 'W1');`)
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if v, _ := db.Version(ctx); v != SchemaVersion {
		t.Errorf("version = %d", v)
	}
	err = db.Tx(ctx, func(tx *Tx) error {
		return tx.PutZoteroItem(ctx, ZoteroItem{Key: "ART00001", FilID: "F00000001", Matched: true})
	})
	if items, _ := db.ZoteroItems(ctx); err != nil || len(items) != 1 {
		t.Errorf("Zotero items after the upgrade = %v, %v", items, err)
	}
}
