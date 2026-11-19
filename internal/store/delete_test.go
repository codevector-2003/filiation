package store

import (
	"slices"
	"testing"
)

func TestDeleteWorks(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()
	// W1, a seed, cites W2, W3 and W4. W5, another seed, also cites W3. W4
	// has a PDF on disk. So deleting W1 takes W2 with it, and nothing else.
	record(t, db, hydratedWith("W1", 0, "W2", "W3", "W4"))
	record(t, db, hydratedWith("W5", 0, "W3"))
	fil := func(oa string) string {
		t.Helper()
		id, ok, err := filIDOf(ctx, db.read, oa)
		if err != nil || !ok {
			t.Fatalf("fil ID of %s: %v, %v", oa, ok, err)
		}
		return id
	}
	w1, w2, w4 := fil("W1"), fil("W2"), fil("W4")
	mustExec(t, db.write, `INSERT INTO work_folder (path, fil_id) VALUES ('papers/a--x', ?), ('papers/d--x', ?);`, w1, w4)
	mustExec(t, db.write, `INSERT INTO work_file (path, fil_id, sha256, size, origin) VALUES ('papers/a--x/a.pdf', ?, 'a', 1, 'download'), ('papers/d--x/d.pdf', ?, 'd', 1, 'download');`, w1, w4)
	mustExec(t, db.write, `INSERT INTO note (work_id, body) VALUES (?, 'one'), (?, 'two');`, w1, w1)

	plan, err := db.DeletePlan(ctx, []string{w1})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Papers != 1 || plan.PDFs != 1 || plan.Notes != 2 || !slices.Equal(plan.Folders, []string{"papers/a--x"}) {
		t.Errorf("plan = %+v", plan)
	}
	if !slices.Equal(plan.Orphans, []string{w2}) {
		t.Errorf("orphans = %v, want only W2: W3 is cited by W5, W4 has a PDF", plan.Orphans)
	}

	if err := db.Tx(ctx, func(tx *Tx) error { _, err := tx.DeleteWorks(ctx, []string{w1}); return err }); err != nil {
		t.Fatal(err)
	}
	for oa, want := range map[string]bool{"W1": false, "W2": false, "W3": true, "W4": true, "W5": true} {
		if _, ok, _ := filIDOf(ctx, db.read, oa); ok != want {
			t.Errorf("%s still here = %v, want %v", oa, ok, want)
		}
	}
	var notes, edges, gone int
	db.read.QueryRow(`SELECT count(*) FROM note;`).Scan(&notes)
	db.read.QueryRow(`SELECT count(*) FROM cites WHERE from_work = ? OR to_work = ?;`, w1, w1).Scan(&edges)
	db.read.QueryRow(`SELECT count(*) FROM deleted_work WHERE openalex_id = 'W1';`).Scan(&gone)
	if notes != 0 || edges != 0 || gone != 1 {
		t.Errorf("notes %d, citations %d left; W1 remembered %d", notes, edges, gone)
	}

	// Following references from a paper that cites W1 does not bring it back.
	rec := record(t, db, hydratedWith("W6", 1, "W1", "W3"))
	if _, ok, _ := filIDOf(ctx, db.read, "W1"); ok || rec.Refs != 1 {
		t.Errorf("W1 came back (%v), or its citation was counted: %+v", ok, rec)
	}
	if err := db.Tx(ctx, func(tx *Tx) error {
		gone, err := tx.Deleted(ctx, "W1")
		if err != nil || !gone {
			t.Errorf("Deleted(W1) = %v, %v", gone, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Adding it again, as a person does, clears the memory.
	record(t, db, hydratedWith("W1", 0))
	db.read.QueryRow(`SELECT count(*) FROM deleted_work;`).Scan(&gone)
	if gone != 0 {
		t.Errorf("adding W1 again left it remembered as deleted")
	}
}

func TestDeletePlanOfNothing(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	plan, err := db.DeletePlan(t.Context(), []string{"F00000000"})
	if err != nil || plan.Papers != 0 || len(plan.Orphans) != 0 {
		t.Errorf("plan of an unknown paper = %+v, %v", plan, err)
	}
}
