package store

import (
	"os"
	"strings"
	"testing"

	"github.com/codevector-2003/filiation/internal/model"
)

func mentionLibrary(t *testing.T) *DB {
	t.Helper()
	db := migrated(t)
	for _, q := range []string{
		`INSERT INTO work (fil_id, openalex_id, title, year, hydrated) VALUES
			('F00000001', 'W1', 'Citing paper', 2020, 1),
			('F00000002', 'W2', NULL, NULL, 0),
			('F00000003', 'W3', 'Cited and fetched', 2010, 1),
			('F00000004', 'W4', NULL, NULL, 0);`,
		`INSERT INTO cites (from_work, to_work) VALUES ('F00000001', 'F00000002'), ('F00000001', 'F00000003');`,
		`UPDATE work SET depth = 1 WHERE hydrated = 0;`,
	} {
		mustExec(t, db.write, q)
	}
	return db
}

func TestPutMentions(t *testing.T) {
	t.Parallel()
	db := mentionLibrary(t)
	ctx := t.Context()
	put := func(ms ...Mention) error {
		return db.Tx(ctx, func(tx *Tx) error { return tx.PutMentions(ctx, "W1", ms) })
	}
	err := put(
		Mention{ToWork: "F00000003", Page: 2, Start: 5, End: 40, Sentence: "Later, on page 2 [3].", Marker: "[3]", Method: "numeric", Confidence: 0.9},
		Mention{ToWork: "F00000003", Page: 1, Start: 10, End: 50, Sentence: "First on page 1 [3].", Section: "introduction", Marker: "[3]", Method: "numeric", Confidence: 0.95},
	)
	if err != nil {
		t.Fatal(err)
	}
	var context, section string
	var conf float64
	db.read.QueryRow(`SELECT context, section, confidence FROM cites WHERE to_work = 'F00000003';`).Scan(&context, &section, &conf)
	if context != "First on page 1 [3]." || section != "introduction" || conf != 0.95 {
		t.Errorf("edge context = %q, %q, %v; want the first mention in reading order", context, section, conf)
	}
	ms, _ := db.MentionsOf(ctx, "W1")
	if len(ms) != 2 || ms[0].Page != 1 {
		t.Errorf("MentionsOf = %+v", ms)
	}

	// A mention never makes an edge (ADR-015): W1 does not cite W4.
	err = put(Mention{ToWork: "F00000004", Page: 1, Sentence: "x", Marker: "[9]", Method: "numeric", Confidence: 0.9})
	if err == nil || !strings.Contains(err.Error(), "never makes one") {
		t.Errorf("a mention for a work not cited: %v", err)
	}
	var edges int
	db.read.QueryRow(`SELECT count(*) FROM cites;`).Scan(&edges)
	if edges != 2 {
		t.Errorf("edges = %d, want 2", edges)
	}

	// Reading again replaces: no mentions, no context left behind.
	if err := put(); err != nil {
		t.Fatal(err)
	}
	var n int
	db.read.QueryRow(`SELECT count(*) FROM mention;`).Scan(&n)
	var ctxNull bool
	db.read.QueryRow(`SELECT context IS NULL FROM cites WHERE to_work = 'F00000003';`).Scan(&ctxNull)
	if n != 0 || !ctxNull {
		t.Errorf("after replacing with none: %d mentions, context cleared %v", n, ctxNull)
	}
}

// Details make a stub recognisable without hydrating it: its own references
// are not recorded, and it stays on the frontier.
func TestPutDetails(t *testing.T) {
	t.Parallel()
	db := mentionLibrary(t)
	ctx := t.Context()
	stubs, err := db.StubsCitedBy(ctx, []string{"F00000001"})
	if err != nil || len(stubs) != 1 || stubs[0] != "W2" {
		t.Fatalf("StubsCitedBy = %v, %v; want W2 alone (W3 is fetched)", stubs, err)
	}
	h := model.HydratedWork{Work: model.Work{OpenAlexID: "W2", Title: model.Ptr("A cited paper"), Year: model.Ptr(2015),
		Authors: []model.Author{{OpenAlexID: "A1", Name: "Heather Piwowar"}}}}
	h.ReferencedWorks = []string{"W99", "W98"}
	if err := db.Tx(ctx, func(tx *Tx) error { return tx.PutDetails(ctx, &h) }); err != nil {
		t.Fatal(err)
	}
	w, _ := db.GetWork(ctx, "W2")
	if w.DisplayTitle() != "A cited paper" || w.Hydrated {
		t.Errorf("W2 = %+v; want a title, still a stub", w)
	}
	refs, _ := db.RefsOf(ctx, "W1")
	for _, r := range refs {
		if r.FilID == "F00000002" && (r.Surname != "Piwowar" || r.Year != 2015) {
			t.Errorf("RefsOf W2 = %+v", r)
		}
	}
	var works, edges int
	db.read.QueryRow(`SELECT count(*) FROM work;`).Scan(&works)
	db.read.QueryRow(`SELECT count(*) FROM cites;`).Scan(&edges)
	if works != 4 || edges != 2 {
		t.Errorf("works %d, edges %d: details grew the graph", works, edges)
	}
	items, _ := db.NextFrontier(ctx, 10, 10, 0, nil)
	onFrontier := false
	for _, it := range items {
		if it.OpenAlexID == "W2" {
			onFrontier = true
		}
	}
	if !onFrontier {
		t.Error("a stub with details left the frontier")
	}
	if stubs, _ := db.StubsCitedBy(ctx, []string{"F00000001"}); len(stubs) != 0 {
		t.Errorf("a stub with details is asked about again: %v", stubs)
	}
}

func TestUpgradeV7Library(t *testing.T) {
	t.Parallel()
	db := openTest(t)
	ctx := t.Context()
	ddl, err := os.ReadFile("testdata/schema_v7.sql")
	if err != nil {
		t.Fatal(err)
	}
	tx, _ := db.write.BeginTx(ctx, nil)
	if err := applySchema(ctx, tx, string(ddl)); err != nil {
		t.Fatal(err)
	}
	tx.Commit()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if v, _ := db.Version(ctx); v != SchemaVersion {
		t.Errorf("version = %d", v)
	}
	mustExec(t, db.write, `INSERT INTO work (fil_id, openalex_id) VALUES ('F00000001', 'W1'), ('F00000002', 'W2');`)
	mustExec(t, db.write, `INSERT INTO cites (from_work, to_work) VALUES ('F00000001', 'F00000002');`)
	if err := db.Tx(ctx, func(tx *Tx) error {
		return tx.PutMentions(ctx, "W1", []Mention{{ToWork: "F00000002", Page: 1, Sentence: "s", Marker: "[1]", Method: "numeric", Confidence: 1}})
	}); err != nil {
		t.Errorf("mention after upgrade: %v", err)
	}
}
