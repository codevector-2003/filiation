package store

import (
	"testing"

	"github.com/codevector-2003/filiation/internal/model"
)

func names(as []model.Author) []string {
	out := make([]string, len(as))
	for i, a := range as {
		out[i] = a.Name
	}
	return out
}

func TestAuthorsRoundTripInBylineOrder(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()

	w := &model.Work{OpenAlexID: "W1", Authors: []model.Author{
		{OpenAlexID: "A3", Name: "Heather Piwowar", ORCID: model.Ptr("0000-0003-1613-5981"), Position: 0},
		{OpenAlexID: "A1", Name: "Jason Priem", Position: 1},
		{OpenAlexID: "A2", Name: "Stefanie Haustein", Position: 2},
		{OpenAlexID: "A1", Name: "Jason Priem", Position: 3}, // listed twice by OpenAlex
	}}
	if err := db.UpsertWork(ctx, w); err != nil {
		t.Fatal(err)
	}
	// A second work shares an author, and has none of its own besides.
	if err := db.UpsertWork(ctx, &model.Work{OpenAlexID: "W2", Authors: []model.Author{{OpenAlexID: "A2", Name: "Stefanie Haustein"}}}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertWork(ctx, &model.Work{OpenAlexID: "W3", Authors: []model.Author{}}); err != nil {
		t.Fatal(err)
	}

	got, err := db.AuthorsOf(ctx, []string{"W1", "W2", "W3", "W404"})
	if err != nil {
		t.Fatal(err)
	}
	if n := names(got["W1"]); len(n) != 3 || n[0] != "Heather Piwowar" || n[1] != "Jason Priem" || n[2] != "Stefanie Haustein" {
		t.Errorf("W1 authors = %v; want Piwowar, Priem, Haustein in byline order, Priem once", n)
	}
	if got["W1"][0].ORCID == nil || *got["W1"][0].ORCID != "0000-0003-1613-5981" {
		t.Errorf("ORCID not kept: %+v", got["W1"][0])
	}
	if len(got["W2"]) != 1 || len(got["W3"]) != 0 || len(got["W404"]) != 0 {
		t.Errorf("AuthorsOf = %v; want one author for W2, none for W3 or an unknown work", got)
	}
}

// A re-fetch replaces the byline; a write without authors loaded leaves it be.
func TestAuthorsAreReplacedOnlyWhenLoaded(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()

	first := &model.Work{OpenAlexID: "W1", Authors: []model.Author{
		{OpenAlexID: "A1", Name: "J. Priem"}, {OpenAlexID: "A2", Name: "S. Haustein", Position: 1}}}
	if err := db.UpsertWork(ctx, first); err != nil {
		t.Fatal(err)
	}
	// Authors nil: this caller did not load them. Nothing may be lost.
	if err := db.UpsertWork(ctx, &model.Work{OpenAlexID: "W1"}); err != nil {
		t.Fatal(err)
	}
	got, _ := db.AuthorsOf(ctx, []string{"W1"})
	if len(got["W1"]) != 2 {
		t.Fatalf("authors after a write without them = %v, want both kept", names(got["W1"]))
	}

	refetched := &model.Work{OpenAlexID: "W1", Authors: []model.Author{{OpenAlexID: "A1", Name: "Jason Priem"}}}
	if err := db.UpsertWork(ctx, refetched); err != nil {
		t.Fatal(err)
	}
	got, _ = db.AuthorsOf(ctx, []string{"W1"})
	if n := names(got["W1"]); len(n) != 1 || n[0] != "Jason Priem" {
		t.Errorf("authors after re-fetch = %v; want only the refreshed name", n)
	}
}
