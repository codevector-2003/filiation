package library

import (
	"database/sql"
	"testing"

	"github.com/codevector-2003/filiation/internal/model"
)

// One paper's links are looked up when never asked for, and again once the
// answer is older than LocateMaxAge — as a library-wide lookup would.
func TestPDFLinksLooksUpWhenUncheckedOrStale(t *testing.T) {
	t.Parallel()
	l, r := openTest(t, map[string]string{
		"/works/doi:10.7717/peerj.4375":         "work_W2741809807.json",
		"/works?filter=openalex_id:W2741809807": "locations_W2741809807.json",
	}, Options{NoCache: true})
	ctx := t.Context()
	if _, err := l.Add(ctx, "10.7717/peerj.4375", AddOptions{}); err != nil {
		t.Fatal(err)
	}

	got, err := l.PDFLinks(ctx, "10.7717/peerj.4375")
	if err != nil || got.Availability != model.PDFOpenNoLink || got.CheckedAt.IsZero() {
		t.Fatalf("first look = %+v, %v", got, err)
	}
	if _, err := l.PDFLinks(ctx, "10.7717/peerj.4375"); err != nil || r.count() != 2 {
		t.Errorf("a fresh answer was asked for again: %d requests, %v", r.count(), err)
	}

	// Age the answer past a month, the way time would.
	raw, err := sql.Open("sqlite3", "file:"+l.db.Path()+"?_pragma=busy_timeout(10000)")
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err := raw.Exec(`UPDATE work_pdf SET checked_at = datetime('now', '-40 days');`); err != nil {
		t.Fatal(err)
	}
	got, err = l.PDFLinks(ctx, "10.7717/peerj.4375")
	if err != nil || r.count() != 3 {
		t.Errorf("a stale answer was not asked again: %d requests, %v", r.count(), err)
	}
	if got.CheckedAt.IsZero() || got.Availability != model.PDFOpenNoLink {
		t.Errorf("after refresh = %+v", got)
	}
}
