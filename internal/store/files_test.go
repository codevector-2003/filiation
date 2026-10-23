package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codevector-2003/filiation/internal/model"
)

// A v2 library, as M3 step 2 left one, upgrades to v3 with its rows intact and
// the tables for papers/ added.
func TestUpgradeV2Library(t *testing.T) {
	t.Parallel()
	db := openTest(t)
	ctx := t.Context()
	ddl, err := os.ReadFile("testdata/schema_v2.sql")
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
	mustExec(t, db.write, `INSERT INTO work (fil_id, openalex_id, title) VALUES ('F7K2M9QXA', 'W1', 'The state of OA');`)
	if v, _ := db.Version(ctx); v != 2 {
		t.Fatalf("fixture is version %d, want 2", v)
	}

	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if v, _ := db.Version(ctx); v != SchemaVersion {
		t.Errorf("version after upgrade = %d", v)
	}
	if w, err := db.GetWork(ctx, "F7K2M9QXA"); err != nil || *w.Title != "The state of OA" {
		t.Errorf("work after upgrade = %+v, %v", w, err)
	}
	err = db.Tx(ctx, func(tx *Tx) error {
		return tx.PutFolder(ctx, Folder{Path: "papers/x--F7K2M9QXA", FilID: "F7K2M9QXA"})
	})
	if err != nil {
		t.Errorf("work_folder after upgrade: %v", err)
	}
}

func TestFoldersAndFilesMoveTogether(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()
	w := &model.Work{OpenAlexID: "W1"}
	if err := db.UpsertWork(ctx, w); err != nil {
		t.Fatal(err)
	}
	fil := w.FilID

	old := "papers/Piwowar-2018-The-state-of-OA--" + fil
	err := db.Tx(ctx, func(tx *Tx) error {
		if err := tx.PutFolder(ctx, Folder{Path: old, FilID: fil, GivenName: filepath.Base(old)}); err != nil {
			return err
		}
		for _, f := range []File{
			{Path: old + "/published.pdf", FilID: fil, SHA256: "AB12", Size: 10, Version: "published", Origin: OriginDownload},
			{Path: old + "/notes/margin.pdf", FilID: fil, SHA256: "cd34", Size: 5, Origin: OriginFound},
			// A sibling whose name merely starts the same way must not move.
			{Path: old + "-copy/x.pdf", FilID: fil, SHA256: "ef56", Size: 1, Origin: OriginFound},
		} {
			if err := tx.PutFile(ctx, f); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	moved := "papers/Chapter 2/Piwowar-2018-The-state-of-OA--" + fil
	err = db.Tx(ctx, func(tx *Tx) error {
		return tx.MoveFolder(ctx, old, Folder{Path: moved, FilID: fil, GivenName: filepath.Base(old)})
	})
	if err != nil {
		t.Fatal(err)
	}
	folders, _ := db.FoldersOf(ctx, fil)
	if len(folders) != 1 || folders[0].Path != moved || folders[0].GivenName != filepath.Base(old) {
		t.Errorf("folders after move = %+v", folders)
	}
	files, _ := db.FilesOf(ctx, fil)
	got := map[string]bool{}
	for _, f := range files {
		got[f.Path] = true
	}
	for _, p := range []string{moved + "/published.pdf", moved + "/notes/margin.pdf", old + "-copy/x.pdf"} {
		if !got[p] {
			t.Errorf("no file at %s after the move: %v", p, got)
		}
	}
	if same, _ := db.FilesWithHash(ctx, "ab12"); len(same) != 1 || same[0].Version != "published" {
		t.Errorf("FilesWithHash = %+v; hashes compare case-insensitively", same)
	}
}

// A fold moves the folded record's folders and files to the survivor, as it
// does notes: nothing a person can see on disk loses its paper.
func TestMergeMovesFoldersAndFiles(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()
	old := &model.Work{OpenAlexID: "W1"}
	if err := db.UpsertWork(ctx, old); err != nil {
		t.Fatal(err)
	}
	err := db.Tx(ctx, func(tx *Tx) error {
		if err := tx.PutFolder(ctx, Folder{Path: "papers/a--" + old.FilID, FilID: old.FilID}); err != nil {
			return err
		}
		return tx.PutFile(ctx, File{Path: "papers/a--" + old.FilID + "/published.pdf", FilID: old.FilID,
			SHA256: "ab", Size: 1, Origin: OriginDownload})
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Tx(ctx, func(tx *Tx) error { return tx.MergeInto(ctx, "W1", "W2") }); err != nil {
		t.Fatal(err)
	}
	survivor, err := db.GetWork(ctx, "W2")
	if err != nil {
		t.Fatal(err)
	}
	if f, _ := db.FoldersOf(ctx, survivor.FilID); len(f) != 1 {
		t.Errorf("survivor's folders = %+v", f)
	}
	if f, _ := db.FilesOf(ctx, survivor.FilID); len(f) != 1 {
		t.Errorf("survivor's files = %+v", f)
	}
	// The old fil ID in the folder's name still resolves, to the survivor.
	ids, err := db.ResolveRefs(ctx, []string{old.FilID, "F0000000Z"})
	if err != nil || ids[old.FilID] != survivor.FilID || len(ids) != 1 {
		t.Errorf("ResolveRefs = %v, %v", ids, err)
	}
}

func TestSyncFolderCollections(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()
	var fils []string
	for _, id := range []string{"W1", "W2", "W3"} {
		w := &model.Work{OpenAlexID: id}
		if err := db.UpsertWork(ctx, w); err != nil {
			t.Fatal(err)
		}
		fils = append(fils, w.FilID)
	}
	mustExec(t, db.write, `INSERT INTO collection (name) VALUES ('Mine');`)
	mustExec(t, db.write, `INSERT INTO collection_work (collection_id, work_id) SELECT id, ? FROM collection WHERE name = 'Mine';`, fils[2])

	sync := func(want map[string]FolderCollection) {
		t.Helper()
		if err := db.Tx(ctx, func(tx *Tx) error { return tx.SyncFolderCollections(ctx, want) }); err != nil {
			t.Fatal(err)
		}
	}
	sizes := func() map[string]int {
		t.Helper()
		cs, err := db.Collections(ctx)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]int{}
		for _, c := range cs {
			out[c.Name] = c.Works
		}
		return out
	}

	sync(map[string]FolderCollection{
		"papers/Chapter 2":             {Name: "Chapter 2", Works: fils[:2]},
		"papers/Chapter 2/Key studies": {Name: "Chapter 2/Key studies", Works: fils[1:2]},
	})
	if got := sizes(); got["Chapter 2"] != 2 || got["Chapter 2/Key studies"] != 1 || got["Mine"] != 1 {
		t.Errorf("collections = %v", got)
	}

	// The folder loses a work and its subfolder goes; the user's own collection
	// is untouched.
	sync(map[string]FolderCollection{"papers/Chapter 2": {Name: "Chapter 2", Works: fils[:1]}})
	if got := sizes(); len(got) != 2 || got["Chapter 2"] != 1 || got["Mine"] != 1 {
		t.Errorf("collections after resync = %v", got)
	}
}

func TestCopyToAndCheck(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()
	if err := db.UpsertWork(ctx, &model.Work{OpenAlexID: "W1"}); err != nil {
		t.Fatal(err)
	}
	want, err := db.Check(ctx)
	if err != nil || want.Works != 1 {
		t.Fatalf("Check = %+v, %v", want, err)
	}
	dst := filepath.Join(t.TempDir(), "copy", "filiation.db")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := db.CopyTo(ctx, dst); err != nil {
		t.Fatal(err)
	}
	cp, err := Open(ctx, dst)
	if err != nil {
		t.Fatal(err)
	}
	defer cp.Close()
	if got, err := cp.Check(ctx); err != nil || got != want {
		t.Errorf("copy Check = %+v, %v; want %+v", got, err, want)
	}
}
