package store

import (
	"strings"
	"testing"

	"github.com/codevector-2003/filiation/internal/model"
)

// filIDs returns a generator that hands out the given IDs in order, then the last
// one forever.
func filIDs(list ...string) func() string {
	i := 0
	return func() string {
		id := list[min(i, len(list)-1)]
		i++
		return id
	}
}

// A fil ID already in use — by a work, or as an alias — is drawn again, never
// stored twice. 40 random bits make a clash rare, not impossible (D17).
func TestNewWorkRedrawsATakenFilID(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()

	db.newID = filIDs("F1111111A")
	record(t, db, hydratedWith("W1", 0))
	mustExec(t, db.write, `INSERT INTO work_alias (alias, fil_id) VALUES ('F2222222B', 'F1111111A');`)

	// The next two draws are taken — one by W1, one as an alias.
	db.newID = filIDs("F1111111A", "F2222222B", "F3333333C")
	err := db.Tx(ctx, func(tx *Tx) error {
		_, err := tx.UpsertStub(ctx, "W2", model.Ptr(1), model.SourceExpansion)
		return err
	})
	if err != nil {
		t.Fatalf("UpsertStub: %v", err)
	}
	if w, err := db.GetWork(ctx, "W2"); err != nil || w.FilID != "F3333333C" {
		t.Errorf("W2 = %+v, %v; want the first free fil ID, F3333333C", w, err)
	}
}

// If every draw is taken, the randomness is broken; the write fails rather than
// looping forever or reusing an ID.
func TestNewWorkGivesUpWhenEveryFilIDIsTaken(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()

	db.newID = filIDs("F1111111A")
	record(t, db, hydratedWith("W1", 0))
	err := db.Tx(ctx, func(tx *Tx) error {
		_, err := tx.UpsertStub(ctx, "W2", nil, model.SourceExpansion)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "no unused fil ID") {
		t.Errorf("UpsertStub with every ID taken = %v, want a refusal", err)
	}
}

// A work keeps its fil ID for ever: hydrating a stub, and hydrating it again,
// never changes it.
func TestFilIDNeverChanges(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()

	record(t, db, hydratedWith("W1", 0, "W2"))
	stub, err := db.GetWork(ctx, "W2")
	if err != nil {
		t.Fatal(err)
	}
	record(t, db, hydratedWith("W2", 1))
	record(t, db, hydratedWith("W2", 1))
	if w, _ := db.GetWork(ctx, "W2"); w.FilID != stub.FilID {
		t.Errorf("W2's fil ID changed on hydration: %s -> %s", stub.FilID, w.FilID)
	}
}

// Folding one record into another keeps every name the old one had, and
// everything the user attached to it (D17).
func TestMergeIntoKeepsAliasesAndUserData(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()

	record(t, db, hydratedWith("W1", 0, "W5"))
	old, _ := db.GetWork(ctx, "W5")
	mustExec(t, db.write, `INSERT INTO note (work_id, body) VALUES (?, 'key paper');`, old.FilID)
	mustExec(t, db.write, `INSERT INTO collection (id, name) VALUES (1, 'Methods');`)
	mustExec(t, db.write, `INSERT INTO collection_work (collection_id, work_id) VALUES (1, ?);`, old.FilID)
	// W5 had itself absorbed an earlier record, whose name must follow too.
	mustExec(t, db.write, `INSERT INTO work_alias (alias, fil_id) VALUES ('W4', ?);`, old.FilID)

	if err := db.Tx(ctx, func(tx *Tx) error { return tx.MergeInto(ctx, "W5", "W6") }); err != nil {
		t.Fatalf("MergeInto: %v", err)
	}
	survivor, err := db.GetWork(ctx, "W6")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{old.FilID, strings.ToLower(old.FilID), "W5", "W4"} {
		w, err := db.FindWork(ctx, ByFilID, name)
		if err != nil || w.FilID != survivor.FilID {
			t.Errorf("finding the folded record by %s = %+v, %v; want the survivor", name, w, err)
		}
	}
	var note, coll string
	db.read.QueryRow(`SELECT work_id FROM note;`).Scan(&note)            //nolint:errcheck
	db.read.QueryRow(`SELECT work_id FROM collection_work;`).Scan(&coll) //nolint:errcheck
	if note != survivor.FilID || coll != survivor.FilID {
		t.Errorf("note on %q and collection on %q after the fold; want both on the survivor %s",
			note, coll, survivor.FilID)
	}

	// An alias is never handed out again as a new work's fil ID.
	db.newID = filIDs(old.FilID, "F9999999Z")
	record(t, db, hydratedWith("W8", 0))
	if w, _ := db.GetWork(ctx, "W8"); w.FilID != "F9999999Z" {
		t.Errorf("W8 got fil ID %s; an alias must not be reused", w.FilID)
	}
}
