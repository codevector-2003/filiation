package store

import (
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/codevector-2003/filiation/internal/errs"
)

// organised is a library of four papers, F00000001 to F00000004.
func organised(t *testing.T) *DB {
	t.Helper()
	db := migrated(t)
	for _, id := range []string{"1", "2", "3", "4"} {
		mustExec(t, db.write, `INSERT INTO work (fil_id, openalex_id, title) VALUES (?, ?, ?);`,
			"F0000000"+id, "W"+id, "Paper "+id)
	}
	return db
}

func listed(t *testing.T, db *DB, q ListQuery) []string {
	t.Helper()
	q.Sort = "arrived"
	rows, err := db.List(t.Context(), q)
	if err != nil {
		t.Fatal(err)
	}
	f, err := db.Facets(t.Context(), q, FacetFind{})
	if err != nil {
		t.Fatal(err)
	}
	if f.Total != len(rows) {
		t.Errorf("Facets(%+v).Total = %d, List found %d", q, f.Total, len(rows))
	}
	out := []string{}
	for _, r := range rows {
		out = append(out, r.FilID)
	}
	return out
}

func TestCollectionTree(t *testing.T) {
	t.Parallel()
	db := organised(t)
	ctx := t.Context()
	var thesis, ch2, key, other int64
	err := db.Tx(ctx, func(tx *Tx) error {
		var err error
		if thesis, err = tx.CreateCollection(ctx, "Thesis", 0); err != nil {
			return err
		}
		if ch2, err = tx.CreateCollection(ctx, "Chapter 2", thesis); err != nil {
			return err
		}
		if key, err = tx.CreateCollection(ctx, "Methods", ch2); err != nil {
			return err
		}
		// The same name in another place is another collection.
		if other, err = tx.CreateCollection(ctx, "Methods", 0); err != nil {
			return err
		}
		if err := tx.FileInCollection(ctx, ch2, []string{"F00000001", "F00000002"}, true); err != nil {
			return err
		}
		return tx.FileInCollection(ctx, key, []string{"F00000002", "F00000003"}, true)
	})
	if err != nil {
		t.Fatal(err)
	}

	cs, err := db.Collections(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	works := map[int64]int{}
	for _, c := range cs {
		order = append(order, c.Name)
		works[c.ID] = c.Works
	}
	if want := []string{"Methods", "Thesis", "Chapter 2", "Methods"}; !slices.Equal(order, want) {
		t.Errorf("tree order = %v, want %v", order, want)
	}
	// A paper in two collections inside Thesis is counted once.
	if works[thesis] != 3 || works[ch2] != 3 || works[key] != 2 || works[other] != 0 {
		t.Errorf("counts = %v", works)
	}
	if got := listed(t, db, ListQuery{CollectionID: thesis}); !slices.Equal(got, []string{"F00000001", "F00000002", "F00000003"}) {
		t.Errorf("Thesis lists %v; its subcollections' papers too", got)
	}
	if got := listed(t, db, ListQuery{CollectionID: key}); !slices.Equal(got, []string{"F00000002", "F00000003"}) {
		t.Errorf("Methods lists %v", got)
	}
	if got := listed(t, db, ListQuery{CollectionID: 999}); len(got) != 0 {
		t.Errorf("a collection not there lists %v", got)
	}

	refuse := func(what string, want error, fn func(tx *Tx) error) {
		t.Helper()
		if err := db.Tx(ctx, fn); !errors.Is(err, want) {
			t.Errorf("%s = %v, want %v", what, err, want)
		}
	}
	refuse("moving Thesis into its own subcollection", errs.ErrInvalidInput, func(tx *Tx) error { return tx.MoveCollection(ctx, thesis, key) })
	refuse("moving Thesis into itself", errs.ErrInvalidInput, func(tx *Tx) error { return tx.MoveCollection(ctx, thesis, thesis) })
	refuse("moving Methods beside another Methods", errs.ErrInvalidInput, func(tx *Tx) error { return tx.MoveCollection(ctx, key, 0) })
	refuse("renaming Chapter 2 to a name it sits beside", nil, func(tx *Tx) error { return tx.RenameCollection(ctx, ch2, "Chapter 2") })
	refuse("a second Thesis", errs.ErrInvalidInput, func(tx *Tx) error { _, err := tx.CreateCollection(ctx, "Thesis", 0); return err })
	refuse("filing a paper not there", errs.ErrNotFound, func(tx *Tx) error { return tx.FileInCollection(ctx, ch2, []string{"F99999999"}, true) })
	refuse("moving the other Methods into Thesis", nil, func(tx *Tx) error { return tx.MoveCollection(ctx, other, thesis) })

	// Deleting Thesis takes everything inside it, and no paper.
	if err := db.Tx(ctx, func(tx *Tx) error { return tx.DeleteCollection(ctx, thesis) }); err != nil {
		t.Fatal(err)
	}
	if cs, _ := db.Collections(ctx); len(cs) != 0 {
		t.Errorf("after deleting Thesis: %+v", cs)
	}
	if got := listed(t, db, ListQuery{}); len(got) != 4 {
		t.Errorf("papers after deleting Thesis = %v", got)
	}
	var left int
	db.read.QueryRowContext(ctx, `SELECT count(*) FROM collection_work;`).Scan(&left)
	if left != 0 {
		t.Errorf("%d memberships left behind", left)
	}
}

func TestFolderCollectionsNestAndStayReadOnly(t *testing.T) {
	t.Parallel()
	db := organised(t)
	ctx := t.Context()
	err := db.Tx(ctx, func(tx *Tx) error {
		return tx.SyncFolderCollections(ctx, map[string]FolderCollection{
			"papers/Chapter 2":             {Name: "Chapter 2", Works: []string{"F00000001"}},
			"papers/Chapter 2/Key studies": {Name: "Chapter 2/Key studies", Works: []string{"F00000002"}},
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	cs, err := db.Collections(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 2 || cs[1].Parent != cs[0].ID || cs[1].Short != "Key studies" || cs[0].Works != 2 || !cs[0].FromFolder {
		t.Fatalf("folder collections = %+v", cs)
	}
	for what, fn := range map[string]func(tx *Tx) error{
		"rename": func(tx *Tx) error { return tx.RenameCollection(ctx, cs[0].ID, "Elsewhere") },
		"delete": func(tx *Tx) error { return tx.DeleteCollection(ctx, cs[0].ID) },
		"file":   func(tx *Tx) error { return tx.FileInCollection(ctx, cs[0].ID, []string{"F00000003"}, true) },
		"inside": func(tx *Tx) error { _, err := tx.CreateCollection(ctx, "Mine", cs[0].ID); return err },
	} {
		if err := db.Tx(ctx, fn); !errors.Is(err, errs.ErrInvalidInput) {
			t.Errorf("%s a folder collection = %v; refused", what, err)
		}
	}

	// A collection of the user's that a folder adopted, with one of theirs
	// inside, outlives the folder.
	var mine int64
	err = db.Tx(ctx, func(tx *Tx) error {
		var err error
		if mine, err = tx.CreateCollection(ctx, "Review", 0); err != nil {
			return err
		}
		if _, err := tx.CreateCollection(ctx, "Drafts", mine); err != nil {
			return err
		}
		return tx.SyncFolderCollections(ctx, map[string]FolderCollection{"papers/Review": {Name: "Review", Works: []string{"F00000004"}}})
	})
	if err != nil {
		t.Fatal(err)
	}
	if cs, _ := db.Collections(ctx); len(cs) != 2 || cs[0].ID != mine || !cs[0].FromFolder {
		t.Fatalf("adopted = %+v", cs)
	}
	if err := db.Tx(ctx, func(tx *Tx) error { return tx.SyncFolderCollections(ctx, nil) }); err != nil {
		t.Fatal(err)
	}
	if cs, _ := db.Collections(ctx); len(cs) != 2 || cs[0].ID != mine || cs[0].FromFolder || cs[1].Name != "Drafts" {
		t.Errorf("after the folder went = %+v; Review and Drafts are the user's again", cs)
	}
}

func TestLabelsAndFavourites(t *testing.T) {
	t.Parallel()
	db := organised(t)
	ctx := t.Context()
	var key, read int64
	err := db.Tx(ctx, func(tx *Tx) error {
		var err error
		if key, err = tx.CreateLabel(ctx, "Key paper", "blue"); err != nil {
			return err
		}
		if read, err = tx.CreateLabel(ctx, "To read", "amber"); err != nil {
			return err
		}
		if err := tx.SetLabel(ctx, key, []string{"F00000001", "F00000002"}, true); err != nil {
			return err
		}
		if err := tx.SetLabel(ctx, read, []string{"F00000002", "F00000003"}, true); err != nil {
			return err
		}
		return tx.SetFavourite(ctx, []string{"F00000003", "F00000004"}, true)
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := listed(t, db, ListQuery{Labels: []int64{key, read}}); !slices.Equal(got, []string{"F00000002"}) {
		t.Errorf("both labels = %v; a paper must carry each", got)
	}
	if got := listed(t, db, ListQuery{Favourites: true, Labels: []int64{read}}); !slices.Equal(got, []string{"F00000003"}) {
		t.Errorf("starred and to read = %v", got)
	}
	rows, _ := db.List(ctx, ListQuery{Sort: "arrived"})
	if !slices.Equal(rows[1].Labels, []int64{key, read}) || rows[1].Favourite || !rows[3].Favourite || len(rows[3].Labels) != 0 {
		t.Errorf("rows = %+v", rows)
	}
	if o, err := db.OrganisedOf(ctx, "F00000003"); err != nil || !o.Favourite || !slices.Equal(o.Labels, []int64{read}) || len(o.Collections) != 0 {
		t.Errorf("OrganisedOf = %+v, %v", o, err)
	}
	if n, _ := db.Favourites(ctx); n != 2 {
		t.Errorf("favourites = %d", n)
	}

	if err := db.Tx(ctx, func(tx *Tx) error { return tx.ChangeLabel(ctx, read, "Key paper", "amber") }); !errors.Is(err, errs.ErrInvalidInput) {
		t.Errorf("renaming a label to one taken = %v", err)
	}
	err = db.Tx(ctx, func(tx *Tx) error {
		if err := tx.SetFavourite(ctx, []string{"F00000004"}, false); err != nil {
			return err
		}
		if err := tx.SetLabel(ctx, key, []string{"F00000001"}, false); err != nil {
			return err
		}
		return tx.DeleteLabel(ctx, read)
	})
	if err != nil {
		t.Fatal(err)
	}
	ls, _ := db.Labels(ctx)
	if len(ls) != 1 || ls[0].Name != "Key paper" || ls[0].Works != 1 || ls[0].Colour != "blue" {
		t.Errorf("labels = %+v", ls)
	}
	if got := listed(t, db, ListQuery{Favourites: true}); !slices.Equal(got, []string{"F00000003"}) {
		t.Errorf("favourites = %v", got)
	}
	if got := listed(t, db, ListQuery{}); len(got) != 4 {
		t.Errorf("deleting a label lost papers: %v", got)
	}
}

func TestMergeKeepsLabelsAndStar(t *testing.T) {
	t.Parallel()
	db := organised(t)
	ctx := t.Context()
	err := db.Tx(ctx, func(tx *Tx) error {
		id, err := tx.CreateLabel(ctx, "Key paper", "blue")
		if err != nil {
			return err
		}
		if err := tx.SetLabel(ctx, id, []string{"F00000001"}, true); err != nil {
			return err
		}
		if err := tx.SetFavourite(ctx, []string{"F00000001"}, true); err != nil {
			return err
		}
		return tx.MergeInto(ctx, "W1", "W2")
	})
	if err != nil {
		t.Fatal(err)
	}
	o, err := db.OrganisedOf(ctx, "F00000002")
	if err != nil || !o.Favourite || len(o.Labels) != 1 {
		t.Errorf("survivor = %+v, %v; it keeps the label and the star", o, err)
	}
}

func TestUpgradeV10Library(t *testing.T) {
	t.Parallel()
	db := openTest(t)
	ctx := t.Context()
	ddl, err := os.ReadFile("testdata/schema_v10.sql")
	if err != nil {
		t.Fatal(err)
	}
	tx, _ := db.write.BeginTx(ctx, nil)
	if err := applySchema(ctx, tx, string(ddl)); err != nil {
		t.Fatal(err)
	}
	tx.Commit()
	for _, q := range []string{
		`INSERT INTO work (fil_id, openalex_id) VALUES ('F00000001', 'W1'), ('F00000002', 'W2');`,
		`INSERT INTO collection (id, name) VALUES (4, 'Mine'), (7, 'Chapter 2');`,
		`INSERT INTO collection_work (collection_id, work_id) VALUES (4, 'F00000001'), (7, 'F00000001'), (7, 'F00000002');`,
		`INSERT INTO folder_collection (collection_id, folder) VALUES (7, 'papers/Chapter 2');`,
	} {
		mustExec(t, db.write, q)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if v, _ := db.Version(ctx); v != SchemaVersion {
		t.Errorf("version = %d", v)
	}
	cs, err := db.Collections(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 2 || cs[0].ID != 7 || cs[0].Works != 2 || !cs[0].FromFolder || cs[1].ID != 4 || cs[1].Works != 1 || cs[1].FromFolder {
		t.Errorf("collections after the upgrade = %+v", cs)
	}
	// The old one-name rule is gone: Mine can hold its own Chapter 2.
	err = db.Tx(ctx, func(tx *Tx) error { _, err := tx.CreateCollection(ctx, "Chapter 2", 4); return err })
	if err != nil {
		t.Errorf("a Chapter 2 inside Mine: %v", err)
	}
	// Deleting a collection still takes its memberships with it.
	if err := db.Tx(ctx, func(tx *Tx) error { return tx.DeleteCollection(ctx, 4) }); err != nil {
		t.Fatal(err)
	}
	var left int
	db.read.QueryRowContext(ctx, `SELECT count(*) FROM collection_work WHERE collection_id = 4;`).Scan(&left)
	if left != 0 {
		t.Errorf("%d memberships of a deleted collection left", left)
	}
}
