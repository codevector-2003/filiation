package library

import (
	"maps"
	"strings"
	"testing"

	"github.com/codevector-2003/filiation/internal/model"
	"github.com/codevector-2003/filiation/internal/store"
)

// A downloaded PDF is checked against its paper once read (M3 step 10): the
// seed's link here serves another paper — Piwowar and Vision's data-reuse
// study — and is flagged, kept; D's PDF carries its title and author and
// passes. List, Show and get_paper all say so.
func TestFetchChecksADownloadIsItsPaper(t *testing.T) {
	t.Parallel()
	routes := maps.Clone(peerj)
	l, _, works := openFetchTestWith(t, routes)
	ctx := t.Context()
	d := works["D"]
	routes["/works?filter=openalex_id:"+d.OpenAlexID] = `{"meta":{"count":1},"results":[{
		"id":"https://openalex.org/` + d.OpenAlexID + `","title":"Measuring the stub","publication_year":2017,"type":"article",
		"authorships":[{"author":{"id":"https://openalex.org/A5000000001","display_name":"Ada Lovelace"}}],"referenced_works":[]}]}`
	if err := l.db.Tx(ctx, func(tx *store.Tx) error {
		return tx.PutPDFLinks(ctx, d.OpenAlexID, model.PDFLinks{Availability: model.PDFLink,
			Links: []model.Location{link("pub.test", "/titled.pdf", "published")}})
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Fetch(ctx, FetchOptions{}); err != nil {
		t.Fatal(err)
	}

	identity := map[string]bool{} // fil ID -> mismatch
	rows, err := l.List(ctx, ListOptions{WithPDF: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		identity[r.FilID] = r.Mismatch
	}
	seedOrC := works["seed"].FilID
	if _, ok := identity[seedOrC]; !ok {
		seedOrC = works["C"].FilID // the two share bytes; whichever downloaded first keeps them
	}
	if !identity[seedOrC] {
		t.Errorf("the seed's PDF is another paper and was not flagged: %v", identity)
	}
	if m, ok := identity[d.FilID]; !ok || m {
		t.Errorf("D's PDF carries its title and author; flagged %v (listed %v)", m, ok)
	}

	p, err := l.Show(ctx, seedOrC)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Files) == 0 || !p.Files[0].Mismatch || p.Text == nil || p.Text.Status != "ok" {
		t.Errorf("Show = files %+v, text %+v", p.Files, p.Text)
	}
}

func TestShowAndList(t *testing.T) {
	t.Parallel()
	l, _, works := openFetchTest(t)
	ctx := t.Context()
	if _, err := l.Fetch(ctx, FetchOptions{}); err != nil {
		t.Fatal(err)
	}

	// B's only link is a landing page: no PDF, and why.
	b, err := l.Show(ctx, works["B"].FilID)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Files) != 0 || len(b.Attempts) != 1 || b.Attempts[0].Outcome != "not-a-pdf" || b.Text != nil {
		t.Errorf("Show(B) = files %+v, attempts %+v, text %+v", b.Files, b.Attempts, b.Text)
	}
	// A's preprint is a PDF header and nothing else: unreadable, and why.
	none, err := l.WithoutText(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 1 || none[0].Work.FilID != works["A"].FilID || none[0].Status != "unreadable" || none[0].Reason == "" {
		t.Errorf("WithoutText = %+v", none)
	}

	seeds, _ := l.List(ctx, ListOptions{Seeds: true})
	if len(seeds) != 1 || seeds[0].FilID != works["seed"].FilID || !seeds[0].Seed {
		t.Errorf("List seeds = %+v", seeds)
	}
	all, _ := l.List(ctx, ListOptions{})
	if len(all) != 55 || all[0].FilID != works["seed"].FilID {
		t.Errorf("List = %d rows, first %+v; want 55, the seed first", len(all), all[0])
	}
	if got, _ := l.List(ctx, ListOptions{WithoutText: true}); len(got) != 1 || got[0].TextStatus != "unreadable" {
		t.Errorf("List without text = %+v", got)
	}
	if got, _ := l.List(ctx, ListOptions{Title: "reference b"}); len(got) != 1 || !strings.EqualFold(got[0].Title, "Reference B") {
		t.Errorf("List by title = %+v", got)
	}
	if got, _ := l.List(ctx, ListOptions{Limit: 3}); len(got) != 3 {
		t.Errorf("List limit 3 = %d rows", len(got))
	}
}
