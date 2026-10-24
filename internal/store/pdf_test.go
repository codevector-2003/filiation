package store

import (
	"os"
	"testing"
	"time"

	"github.com/codevector-2003/filiation/internal/model"
)

func TestPDFLinksRoundTrip(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()
	if err := db.UpsertWork(ctx, &model.Work{OpenAlexID: "W1"}); err != nil {
		t.Fatal(err)
	}

	links, at, err := db.PDFLinksOf(ctx, "W1")
	if err != nil || links.Availability != model.PDFUnchecked || !at.IsZero() {
		t.Fatalf("before lookup = %+v, %v, %v", links, at, err)
	}

	want := model.PDFLinks{Availability: model.PDFLink, Links: []model.Location{
		{PDFURL: "https://journals.plos.org/x.pdf", LandingURL: "https://doi.org/10.1/x", Version: "published", License: "cc-by", Host: "journals.plos.org"},
		{PDFURL: "https://arxiv.org/pdf/1", Version: "preprint", Host: "arxiv.org"},
	}}
	if err := db.Tx(ctx, func(tx *Tx) error { return tx.PutPDFLinks(ctx, "W1", want) }); err != nil {
		t.Fatal(err)
	}
	got, at, err := db.PDFLinksOf(ctx, "W1")
	if err != nil || got.Availability != model.PDFLink || len(got.Links) != 2 || got.Links[0] != want.Links[0] || got.Links[1] != want.Links[1] {
		t.Errorf("after = %+v, %v", got, err)
	}
	if time.Since(at) > time.Minute {
		t.Errorf("checked at %v", at)
	}

	// A later lookup replaces the links, here with none.
	if err := db.Tx(ctx, func(tx *Tx) error {
		return tx.PutPDFLinks(ctx, "W1", model.PDFLinks{Availability: model.PDFNoOpenCopy})
	}); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := db.PDFLinksOf(ctx, "W1"); got.Availability != model.PDFNoOpenCopy || len(got.Links) != 0 {
		t.Errorf("after replacing = %+v", got)
	}
}

// Seeds first, then the most cited; fresh answers and works OpenAlex does not
// know are left alone.
func TestToLocateOrderAndCounts(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()
	for _, q := range []string{
		`INSERT INTO work (fil_id, openalex_id, is_seed) VALUES ('F00000001', 'W1', 0), ('F00000002', 'W2', 1),
			('F00000003', 'W3', 0), ('F00000004', 'W4', 0), ('F00000005', 'W5', 0);`,
		`UPDATE work SET unresolved = 1 WHERE openalex_id = 'W5';`,
		// W3 is cited twice, W1 once.
		`INSERT INTO cites (from_work, to_work) VALUES ('F00000002', 'F00000003'), ('F00000004', 'F00000003'), ('F00000002', 'F00000001');`,
		`INSERT INTO work_pdf (fil_id, availability, checked_at) VALUES ('F00000004', 'no-open-copy', datetime('now', '-2 days'));`,
	} {
		mustExec(t, db.write, q)
	}
	got, err := db.ToLocate(ctx, 10, 30*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0] != "W2" || got[1] != "W3" || got[2] != "W1" {
		t.Errorf("ToLocate = %v, want W2 (seed), W3 (cited twice), W1", got)
	}
	// A stale answer is asked again.
	if got, _ := db.ToLocate(ctx, 10, 24*time.Hour); len(got) != 4 {
		t.Errorf("ToLocate with maxAge 24h = %v, want W4's two-day-old answer asked again", got)
	}
	if got, _ := db.ToLocate(ctx, 1, time.Hour); len(got) != 1 {
		t.Errorf("limit ignored: %v", got)
	}

	counts, err := db.PDFCounts(ctx)
	// W5 is unresolved: not in OpenAlex, without a lookup.
	if err != nil || counts[model.PDFUnchecked] != 3 || counts[model.PDFNoOpenCopy] != 1 || counts[model.PDFNotInOpenAlex] != 1 {
		t.Errorf("PDFCounts = %v, %v", counts, err)
	}
	if l, at, err := db.PDFLinksOf(ctx, "W5"); err != nil || l.Availability != model.PDFNotInOpenAlex || !at.IsZero() {
		t.Errorf("unresolved W5 = %+v, %v, %v", l, at, err)
	}
}

func TestUpgradeV3Library(t *testing.T) {
	t.Parallel()
	db := openTest(t)
	ctx := t.Context()
	ddl, err := os.ReadFile("testdata/schema_v3.sql")
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
	mustExec(t, db.write, `INSERT INTO work (fil_id, openalex_id) VALUES ('F7K2M9QXA', 'W1');`)
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if v, _ := db.Version(ctx); v != SchemaVersion {
		t.Errorf("version = %d", v)
	}
	if err := db.Tx(ctx, func(tx *Tx) error {
		return tx.PutPDFLinks(ctx, "F7K2M9QXA", model.PDFLinks{Availability: model.PDFOpenNoLink})
	}); err != nil {
		t.Errorf("work_pdf after upgrade: %v", err)
	}
}
