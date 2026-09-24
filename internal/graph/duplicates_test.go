package graph

import (
	"testing"

	"github.com/codevector-2003/filiation/internal/model"
)

func titled(id, doi, title string, year int, typ model.WorkType, refs ...string) model.HydratedWork {
	w := hydrated(id, doi, refs...)
	w.Title = model.Ptr(title)
	w.Year = model.Ptr(year)
	w.Type = typ
	return w
}

func TestProbableDuplicatesReportsWithoutMerging(t *testing.T) {
	t.Parallel()
	// Shaped on the live run of 24 Sept 2026.
	g, db, _ := seeded(t, []model.HydratedWork{
		hydrated("W1", "10.1/1", "W2", "W3", "W4", "W5", "W6", "W7", "W8", "W9"),
		// One paper, two records, different DOIs.
		titled("W2", "10.1/a", "Free Internet access to traditional journals", 1998, model.TypeArticle),
		titled("W3", "10.1/b", "Free internet access to traditional journals.", 1998, model.TypeArticle),
		// A preprint and its journal version.
		titled("W4", "10.1/c", "Sci-Hub provides access to nearly all scholarly literature", 2017, model.TypePreprint),
		titled("W5", "10.1/d", "Sci-Hub Provides Access to Nearly All Scholarly Literature", 2018, model.TypeArticle),
		// A book and a review of it: same title, different works. Reported —
		// the point is that a person can see it is not a duplicate.
		titled("W6", "10.1/e", "The Access Principle: The Case for Open Access to Research and Scholarship", 2006, model.TypeBook),
		titled("W7", "10.1/f", "The access principle: the case for open access to research and scholarship", 2006, "book-review"),
		// Generic titles are not reported at all.
		titled("W8", "10.1/g", "Editorial", 2010, model.TypeArticle),
		titled("W9", "10.1/h", "Editorial", 2011, model.TypeArticle),
	}, "W1")
	expand(t, g, t.Context(), ExpandOptions{MaxNodes: 20, MaxDepth: 3})
	before, _ := db.Stats(t.Context())

	groups, err := g.ProbableDuplicates(t.Context())
	if err != nil {
		t.Fatalf("ProbableDuplicates: %v", err)
	}
	if len(groups) != 3 {
		for _, gr := range groups {
			t.Logf("group %q: %d works", gr.Title, len(gr.Works))
		}
		t.Fatalf("groups = %d, want 3 — 'Editorial' is too generic to report", len(groups))
	}
	// Sorted: equal sizes, so alphabetical.
	if groups[0].Title != "Free Internet access to traditional journals" {
		t.Errorf("first group = %q", groups[0].Title)
	}
	// The book and its review are listed together, with their types, which
	// is what lets a person see they are not the same work — and why nothing
	// here is merged automatically.
	book := groups[2]
	if book.Works[0].Type != model.TypeBook || book.Works[1].Type != "book-review" {
		t.Errorf("book group types = %q, %q", book.Works[0].Type, book.Works[1].Type)
	}
	sci := groups[1]
	if sci.Works[0].OpenAlexID != "W4" || sci.Works[1].OpenAlexID != "W5" {
		t.Errorf("Sci-Hub group = %s, %s; want the 2017 preprint before the 2018 article",
			sci.Works[0].OpenAlexID, sci.Works[1].OpenAlexID)
	}

	// D14: reporting changes nothing.
	if after, _ := db.Stats(t.Context()); after != before {
		t.Errorf("ProbableDuplicates changed the library: %+v -> %+v", before, after)
	}
}

func TestProbableDuplicatesIgnoresStubs(t *testing.T) {
	t.Parallel()
	g, _, _ := seeded(t, []model.HydratedWork{
		titled("W1", "10.1/1", "A long enough title to be reported here", 2020, model.TypeArticle, "W2"),
	}, "W1")
	groups, err := g.ProbableDuplicates(t.Context())
	if err != nil || len(groups) != 0 {
		t.Errorf("groups = %v, %v; want none", groups, err)
	}
}
