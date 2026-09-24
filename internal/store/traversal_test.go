package store

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/codevector-2003/filiation/internal/errs"
	"github.com/codevector-2003/filiation/internal/model"
)

// chain renders a path as "W1>W2<W3": > where the left work cites the right,
// < where the right cites the left.
func chain(p Path) string {
	var b strings.Builder
	for i, id := range p.IDs {
		if i > 0 {
			if p.Cites[i-1] {
				b.WriteString(">")
			} else {
				b.WriteString("<")
			}
		}
		b.WriteString(id)
	}
	return b.String()
}

func shortest(t *testing.T, db *DB, from, to string, maxHops int, dir Direction) (string, error) {
	t.Helper()
	p, err := db.ShortestPath(t.Context(), from, to, maxHops, dir)
	if err != nil {
		return "", err
	}
	if len(p.Cites) != len(p.IDs)-1 {
		t.Fatalf("path %v has %d direction flags", p.IDs, len(p.Cites))
	}
	return chain(p), nil
}

// lineage: W1 cites W2 and W3; W2 cites W4; W3 cites W4 and W5; W4 cites W6.
// W7 cites W1 — a later paper building on the seed.
func lineage(t *testing.T) *DB {
	t.Helper()
	db := migrated(t)
	record(t, db, hydratedWith("W1", 0, "W2", "W3"))
	record(t, db, hydratedWith("W2", 1, "W4"))
	record(t, db, hydratedWith("W3", 1, "W4", "W5"))
	record(t, db, hydratedWith("W4", 2, "W6"))
	record(t, db, hydratedWith("W7", 0, "W1"))
	return db
}

func TestShortestPathFollowsReferences(t *testing.T) {
	t.Parallel()
	db := lineage(t)
	tests := []struct {
		from, to string
		dir      Direction
		want     string
	}{
		{"W1", "W6", Backward, "W1>W2>W4>W6"}, // W2 before W3: ties go to the lower ID
		{"W1", "W5", Backward, "W1>W3>W5"},
		{"W7", "W6", Backward, "W7>W1>W2>W4>W6"},
		{"W6", "W1", Forward, "W6<W4<W2<W1"},
		{"W5", "W2", Either, "W5<W3<W1>W2"}, // three steps, as is W5<W3>W4<W2; W1 sorts first
		{"W3", "W3", Backward, "W3"},
	}
	for _, tt := range tests {
		got, err := shortest(t, db, tt.from, tt.to, 10, tt.dir)
		if err != nil || got != tt.want {
			t.Errorf("ShortestPath(%s, %s, %d) = %q, %v; want %q", tt.from, tt.to, tt.dir, got, err, tt.want)
		}
	}
}

func TestShortestPathRespectsDirection(t *testing.T) {
	t.Parallel()
	db := lineage(t)
	// W6 cites nothing, so following references from it reaches nothing.
	if _, err := shortest(t, db, "W6", "W1", 10, Backward); !errors.Is(err, errs.ErrNotFound) {
		t.Errorf("W6 -> W1 following references = %v, want ErrNotFound", err)
	}
	// W5 and W2 are connected only through W3 and W4 in mixed directions.
	if _, err := shortest(t, db, "W5", "W2", 10, Backward); !errors.Is(err, errs.ErrNotFound) {
		t.Errorf("W5 -> W2 following references = %v, want ErrNotFound", err)
	}
}

func TestShortestPathRespectsMaxHops(t *testing.T) {
	t.Parallel()
	db := lineage(t)
	if _, err := shortest(t, db, "W7", "W6", 3, Backward); !errors.Is(err, errs.ErrNotFound) {
		t.Errorf("a 4-step path found within 3 = %v", err)
	}
	if got, err := shortest(t, db, "W7", "W6", 4, Backward); err != nil || got != "W7>W1>W2>W4>W6" {
		t.Errorf("within 4 = %q, %v", got, err)
	}
}

func TestShortestPathSurvivesCycles(t *testing.T) {
	t.Parallel()
	// §8: a hand-built cyclic graph must not hang path finding. A dense ring
	// of 60 works each citing the next five, which is full of cycles, and a
	// target that is not there.
	db := migrated(t)
	const n = 60
	for i := range n {
		refs := make([]string, 5)
		for k := range refs {
			refs[k] = fmt.Sprintf("W%d", 1000+(i+k+1)%n)
		}
		record(t, db, hydratedWith(fmt.Sprintf("W%d", 1000+i), 0, refs...))
	}

	start := time.Now()
	for _, dir := range []Direction{Backward, Forward, Either} {
		if _, err := shortest(t, db, "W1000", "W9999", 50, dir); !errors.Is(err, errs.ErrNotFound) {
			t.Errorf("direction %d: %v, want ErrNotFound", dir, err)
		}
	}
	// And the far side of the ring is found by the short way round.
	if got, err := shortest(t, db, "W1000", "W1030", 50, Backward); err != nil || strings.Count(got, ">") != 6 {
		t.Errorf("W1000 -> W1030 = %q, %v; want 6 steps of 5", got, err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("searching a cyclic graph took %s", elapsed)
	}
}

func TestShortestPathRejectsBadIDs(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	if _, err := db.ShortestPath(t.Context(), "https://openalex.org/W1", "W2", 3, Either); !errors.Is(err, errs.ErrInvalidInput) {
		t.Errorf("error = %v, want ErrInvalidInput", err)
	}
}

func TestNeighbours(t *testing.T) {
	t.Parallel()
	db := lineage(t)
	ctx := t.Context()

	cites, err := db.WorksCitedBy(ctx, "W3")
	if err != nil {
		t.Fatal(err)
	}
	// W4 is fetched, W5 is a stub: fetched first.
	if len(cites) != 2 || cites[0].OpenAlexID != "W4" || cites[1].OpenAlexID != "W5" || !cites[1].IsStub() {
		t.Errorf("W3 cites %+v", cites)
	}

	citing, err := db.WorksCiting(ctx, "W4")
	if err != nil {
		t.Fatal(err)
	}
	if len(citing) != 2 || citing[0].OpenAlexID != "W2" || citing[1].OpenAlexID != "W3" {
		t.Errorf("W4 cited by %+v", citing)
	}

	none, err := db.WorksCitedBy(ctx, "W6")
	if err != nil || none == nil || len(none) != 0 {
		t.Errorf("W6 cites %v, %v; want an empty non-nil list", none, err)
	}
}

func TestFindWork(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()
	w := &model.Work{OpenAlexID: "W1", DOI: model.Ptr("10.1/x"), ArXivID: model.Ptr("1706.03762"), PMID: model.Ptr("29051481")}
	if err := db.UpsertWork(ctx, w); err != nil {
		t.Fatal(err)
	}
	for by, value := range map[Lookup]string{
		ByOpenAlexID: "W1", ByDOI: "10.1/x", ByArXivID: "1706.03762", ByPMID: "29051481",
	} {
		got, err := db.FindWork(ctx, by, value)
		if err != nil || got.OpenAlexID != "W1" {
			t.Errorf("FindWork(%d, %s) = %v, %v", by, value, got, err)
		}
	}
	if _, err := db.FindWork(ctx, ByDOI, "10.1/none"); !errors.Is(err, errs.ErrNotFound) {
		t.Errorf("missing DOI = %v, want ErrNotFound", err)
	}
}
