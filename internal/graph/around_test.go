package graph

import (
	"testing"

	"github.com/codevector-2003/filiation/internal/store"
)

// aroundGraph is a seed W1 citing W2 and W3, cited by W4, W5 and W6, each of
// which cites a work of its own.
func aroundGraph(t *testing.T) (*Graph, *store.DB, *fakeSource, string) {
	t.Helper()
	w4 := hydrated("W4", "10.1000/4", "W1", "W40")
	w4.CitedByCount = 50
	w5 := hydrated("W5", "10.1000/5", "W1", "W50")
	w5.CitedByCount = 900
	w6 := hydrated("W6", "10.1000/6", "W1", "W60")
	w6.CitedByCount = 7
	g, db, src := newGraph(t, hydrated("W1", "10.1000/1", "W2", "W3"),
		hydrated("W2", "10.1000/2", "W20"), hydrated("W3", "10.1000/3"), w4, w5, w6)
	seeded, err := g.AddSeed(t.Context(), id(t, "W1"))
	if err != nil {
		t.Fatal(err)
	}
	return g, db, src, seeded.Work.FilID
}

func TestBringAroundWithDetailsOnly(t *testing.T) {
	t.Parallel()
	g, db, src, fil := aroundGraph(t)
	got, err := g.BringAround(t.Context(), fil, AroundOptions{References: true, Citing: 2})
	if err != nil {
		t.Fatalf("BringAround: %v", err)
	}
	if got.References != 2 || got.Citing != 2 || got.New != 2 || got.CitedBy != 3 {
		t.Fatalf("brought in %+v", got)
	}
	if src.citing != 1 {
		t.Fatalf("asked who cites it %d times", src.citing)
	}
	// The two most cited of the three, as stubs with their details.
	for _, oa := range []string{"W5", "W4"} {
		w, err := db.GetWork(t.Context(), oa)
		if err != nil {
			t.Fatalf("%s is not in the library: %v", oa, err)
		}
		if w.Hydrated || w.Title == nil || w.CitedByCount == 0 {
			t.Errorf("%s came in as %+v, want a titled stub with its citation count", oa, w)
		}
	}
	if _, err := db.GetWork(t.Context(), "W6"); err == nil {
		t.Error("W6, the least cited, came in past the limit")
	}
	// Nothing past one step: what the newcomers cite is not recorded.
	for _, oa := range []string{"W20", "W40", "W50"} {
		if _, err := db.GetWork(t.Context(), oa); err == nil {
			t.Errorf("%s, two steps away, came in", oa)
		}
	}
	citing, err := db.WorksCiting(t.Context(), fil)
	if err != nil {
		t.Fatal(err)
	}
	if len(citing) != 2 {
		t.Fatalf("%d works cite the paper here, want 2", len(citing))
	}
	// Again: the references have their details, and the citers are here.
	again, err := g.BringAround(t.Context(), fil, AroundOptions{References: true, Citing: 2})
	if err != nil {
		t.Fatal(err)
	}
	if again.References != 0 || again.New != 0 {
		t.Fatalf("a second run brought in %+v", again)
	}
}

func TestBringAroundFollowing(t *testing.T) {
	t.Parallel()
	g, db, _, fil := aroundGraph(t)
	got, err := g.BringAround(t.Context(), fil, AroundOptions{References: true, Citing: 10, Follow: true})
	if err != nil {
		t.Fatalf("BringAround: %v", err)
	}
	if got.References != 2 || got.Citing != 3 {
		t.Fatalf("brought in %+v", got)
	}
	for _, oa := range []string{"W2", "W4"} {
		w, err := db.GetWork(t.Context(), oa)
		if err != nil || !w.Hydrated {
			t.Errorf("%s was not fetched: %+v %v", oa, w, err)
		}
	}
	// Followed, what they cite is recorded, as stubs.
	for _, oa := range []string{"W20", "W40"} {
		w, err := db.GetWork(t.Context(), oa)
		if err != nil || w.Hydrated {
			t.Errorf("%s should be a stub now: %+v %v", oa, w, err)
		}
	}
}

func TestBringAroundRefusesALocalDocument(t *testing.T) {
	t.Parallel()
	g, db, _ := newGraph(t)
	var fil string
	err := db.Tx(t.Context(), func(tx *store.Tx) error {
		var err error
		fil, err = tx.AddLocalWork(t.Context(), "My notes", nil)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.BringAround(t.Context(), fil, AroundOptions{References: true, Citing: 5}); err == nil {
		t.Fatal("a local document was sent to OpenAlex")
	}
}
