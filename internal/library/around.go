package library

import (
	"context"

	"github.com/codevector-2003/filiation/internal/graph"
	"github.com/codevector-2003/filiation/internal/model"
)

// Surroundings is what is known around one paper before anything is fetched:
// what a front door shows when offering to bring it in.
type Surroundings struct {
	Work      model.Work
	Cites     int // works it cites, as recorded here
	Unfetched int // of those, known only by ID
	Untitled  int // of those, with no title either
	CitedBy   int // works citing it, by OpenAlex's count
	Here      int // works citing it that are in the library
}

// Surroundings counts what is around the paper input names.
func (l *Library) Surroundings(ctx context.Context, input string) (Surroundings, error) {
	found, err := l.Find(ctx, input)
	if err != nil {
		return Surroundings{}, err
	}
	w, err := l.workWithAuthors(ctx, found.FilID)
	if err != nil {
		return Surroundings{}, err
	}
	a, err := l.db.AroundOf(ctx, w.FilID)
	if err != nil {
		return Surroundings{}, err
	}
	here, err := l.db.WorksCiting(ctx, w.FilID)
	if err != nil {
		return Surroundings{}, err
	}
	return Surroundings{Work: w, Cites: a.Cites, Unfetched: a.Stubs, Untitled: a.Untitled,
		CitedBy: w.CitedByCount, Here: len(here)}, nil
}

// AroundOptions say what BringAround brings in.
type AroundOptions = graph.AroundOptions

// BroughtIn is what BringAround did.
type BroughtIn = graph.BroughtIn

// BringAround brings in the papers one citation step from the paper input
// names, what it cites and what cites it, without expanding the rest of the
// library. It is the narrow expansion a person asks for by naming a paper.
func (l *Library) BringAround(ctx context.Context, input string, o AroundOptions) (BroughtIn, error) {
	w, err := l.Find(ctx, input)
	if err != nil {
		return BroughtIn{}, err
	}
	return l.graph.BringAround(ctx, w.FilID, o)
}
