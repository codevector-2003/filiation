package store

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/codevector-2003/filiation/internal/errs"
)

func TestSortFilterAndFacets(t *testing.T) {
	t.Parallel()
	db := textLibrary(t)
	ctx := t.Context()
	mustExec(t, db.write, `
UPDATE work SET year = 2001, venue = 'Nature', title = 'Bees' WHERE fil_id = 'F00000001';
UPDATE work SET year = 2010, venue = 'Science', title = 'ants' WHERE fil_id = 'F00000002';
UPDATE work SET year = NULL, venue = NULL, title = 'Cats' WHERE fil_id = 'F00000003';
INSERT INTO author (openalex_id, name) VALUES ('A1', 'Ada Lovelace'), ('A2', 'Alan Turing');
INSERT INTO authorship (work_id, author_id, position) VALUES ('F00000001', 'A1', 0), ('F00000002', 'A1', 0), ('F00000002', 'A2', 1);`)

	for sort, want := range map[string]string{
		"newest": "F00000002,F00000001,F00000003",
		"oldest": "F00000001,F00000002,F00000003",
		"title":  "F00000002,F00000001,F00000003",
	} {
		rows, err := db.List(ctx, ListQuery{Sort: sort})
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, r := range rows {
			ids = append(ids, r.FilID)
		}
		if got := strings.Join(ids, ","); got != want {
			t.Errorf("sort %s = %s, want %s", sort, got, want)
		}
	}
	if _, err := db.List(ctx, ListQuery{Sort: "by mood"}); !errors.Is(err, errs.ErrInvalidInput) {
		t.Errorf("an unknown order = %v", err)
	}

	for q, want := range map[*ListQuery]int{
		{YearFrom: 2005}: 1, {YearTo: 2005}: 1, {YearFrom: 2000, YearTo: 2020}: 2,
		{Authors: []string{"Alan Turing"}}: 1, {Authors: []string{"Alan Turing", "Ada Lovelace"}}: 2,
		{Venues: []string{"Nature"}}: 1, {Venues: []string{"Nature"}, Authors: []string{"Alan Turing"}}: 0,
	} {
		rows, err := db.List(ctx, *q)
		if err != nil || len(rows) != want {
			t.Errorf("List(%+v) = %d rows, %v; want %d", *q, len(rows), err, want)
		}
		if f, err := db.Facets(ctx, *q, FacetFind{}); err != nil || f.Total != want {
			t.Errorf("Facets(%+v).Total = %d, %v; want %d", *q, f.Total, err, want)
		}
	}

	// A filter's options ignore that filter, but not the others, and a work
	// with no year still counts until a year range is set.
	f, err := db.Facets(ctx, ListQuery{Authors: []string{"Alan Turing"}, Title: "ants"}, FacetFind{})
	if err != nil {
		t.Fatal(err)
	}
	if f.Total != 1 || len(f.Authors) != 2 || f.Authors[0] != (FacetCount{"Ada Lovelace", 1}) || len(f.Venues) != 1 || f.Venues[0].Name != "Science" {
		t.Errorf("facets = %+v", f)
	}
	if f, _ := db.Facets(ctx, ListQuery{}, FacetFind{}); f.Total != 3 || len(f.Years) != 2 || f.Years[0].Year != 2001 {
		t.Errorf("years = %+v", f)
	}

	// Finding narrows what is offered, and what is chosen stays on show.
	f, _ = db.Facets(ctx, ListQuery{Venues: []string{"Nature"}}, FacetFind{Author: "lovelace", Venue: "science"})
	if len(f.Authors) != 1 || len(f.Venues) != 2 {
		t.Errorf("found facets = %+v", f)
	}
}

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

	// Titles match Unicode-aware, accents or not (found in review, PR #12).
	mustExec(t, db.write, `UPDATE work SET title = 'Étude des accès ouverts' WHERE fil_id = 'F00000003';`)
	for _, q := range []string{"étude", "ÉTUDE", "etude acces"} {
		if got, _ := db.List(ctx, ListQuery{Title: q}); len(got) != 1 || got[0].FilID != "F00000003" {
			t.Errorf("List(title %q) = %+v", q, got)
		}
	}
	if got, _ := db.List(ctx, ListQuery{Title: "open", Limit: 1}); len(got) != 1 {
		t.Errorf("a title filter with a limit = %d rows", len(got))
	}

	// Paging skips the same rows with a title filter as without one.
	if got, _ := db.List(ctx, ListQuery{Title: "open", Offset: 1}); len(got) != 1 {
		t.Errorf("a title filter with an offset = %d rows", len(got))
	}
	if got, _ := db.List(ctx, ListQuery{Limit: 2, Offset: 2}); len(got) != 1 {
		t.Errorf("the last page = %d rows", len(got))
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
