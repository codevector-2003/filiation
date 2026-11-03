package library

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/codevector-2003/filiation/internal/errs"
	"github.com/codevector-2003/filiation/internal/identity"
	"github.com/codevector-2003/filiation/internal/model"
	"github.com/codevector-2003/filiation/internal/store"
	"github.com/codevector-2003/filiation/internal/text"
)

// Matching a local document, one OpenAlex did not know when it came in, to a
// work OpenAlex does know: by an identifier the user gives, which is free, or
// by choosing from a title search, which costs 10 credits and is never run
// without being asked (D29).

// TitleCandidate is a work a title search found for a local document.
type TitleCandidate struct {
	Work model.Work // with its authors

	// TitleMatch: its title is the document's, allowing for case,
	// punctuation and a typo. Never grounds to link: two works can share a
	// title (ADR-005).
	TitleMatch bool
	// PDFMatches: its title and first author are on the first pages of the
	// document's PDF.
	PDFMatches bool
	// InLibrary is the fil ID it already has here, or "".
	InLibrary string
}

// errLinked refuses a work OpenAlex already knows.
func errLinked(w model.Work) error {
	return fmt.Errorf("%s is already OpenAlex's %s: %w", w.FilID, w.OpenAlexID, errs.ErrInvalidInput)
}

// TitleCandidates searches OpenAlex for the title of the local document
// input names. The search costs 10 of the day's credits; the same search
// again is answered from the cache.
func (l *Library) TitleCandidates(ctx context.Context, input string) ([]TitleCandidate, error) {
	w, err := l.getWork(ctx, input)
	if err != nil {
		return nil, err
	}
	if w.OpenAlexID != "" {
		return nil, errLinked(w)
	}
	title := model.Deref(w.Title, "")
	if title == "" {
		return nil, fmt.Errorf("%s has no title to search for: %w", w.FilID, errs.ErrInvalidInput)
	}
	found, err := l.graph.SearchTitle(ctx, title, 5)
	if err != nil {
		return nil, err
	}
	front, readable := l.frontOf(ctx, w.FilID)
	out := make([]TitleCandidate, 0, len(found))
	for _, c := range found {
		tc := TitleCandidate{Work: c.Work.Work, TitleMatch: c.TitleMatch, PDFMatches: readable && verifies(front, c.Work)}
		if tc.Work.Authors == nil {
			tc.Work.Authors = []model.Author{}
		}
		if have, err := l.db.GetWork(ctx, c.Work.OpenAlexID); err == nil {
			tc.InLibrary = have.FilID
		} else if !errors.Is(err, errs.ErrNotFound) {
			return nil, err
		}
		out = append(out, tc)
	}
	return out, nil
}

// frontOf reads the first pages of a work's PDF, if it has one with text.
func (l *Library) frontOf(ctx context.Context, fil string) (text.Front, bool) {
	if l.files == nil {
		return text.Front{}, false
	}
	t, ok, err := l.db.TextOf(ctx, fil)
	if err != nil || !ok || t.Path == "" {
		return text.Front{}, false
	}
	data, err := os.ReadFile(l.files.Abs(t.Path))
	if err != nil {
		return text.Front{}, false
	}
	fctx, cancel := context.WithTimeout(ctx, ExtractTimeout)
	defer cancel()
	f, err := text.Shared().Front(fctx, data)
	if err != nil || !hasText(f) {
		return text.Front{}, false
	}
	return f, true
}

// Linked is what linking a local document did.
type Linked struct {
	Work     model.Work // as stored now
	Folded   bool       // the library had the work already; the document became part of it
	Recorded store.Recorded
}

// LinkLocal links the local document input names to the work id names: a
// DOI, arXiv ID, PubMed ID or OpenAlex ID. Looking it up is free. The
// document keeps its fil ID, PDF, collections, labels and notes; if the
// library has the work already, the two become one and the document's fil ID
// is kept as an alias. With refs, what the work cites is recorded, as fil
// add does.
func (l *Library) LinkLocal(ctx context.Context, input, id string, refs bool) (Linked, error) {
	w, err := l.getWork(ctx, input)
	if err != nil {
		return Linked{}, err
	}
	if w.OpenAlexID != "" {
		return Linked{}, errLinked(w)
	}
	parsed, err := identity.Parse(id)
	if err != nil {
		return Linked{}, err
	}
	if !parsed.Deterministic() {
		return Linked{}, fmt.Errorf("%q is a title; give a DOI, arXiv ID, PubMed ID or OpenAlex ID: %w", id, errs.ErrInvalidInput)
	}
	h, err := l.graph.Resolve(ctx, parsed)
	if err != nil {
		return Linked{}, err
	}
	h.Source = w.Source
	if !refs {
		h.FetchedRefs, h.ReferencedWorks = false, nil
	}

	var into string
	if err := l.db.Tx(ctx, func(tx *store.Tx) error {
		into, err = tx.LinkLocal(ctx, w.FilID, h.OpenAlexID)
		return err
	}); err != nil {
		return Linked{}, err
	}
	s, err := l.graph.AddSeedWork(ctx, h)
	if err != nil {
		return Linked{}, err
	}
	if err := l.db.Tx(ctx, func(tx *store.Tx) error { return tx.MarkZoteroMatched(ctx, s.Work.FilID) }); err != nil {
		return Linked{}, err
	}
	return Linked{Work: s.Work, Folded: into != w.FilID || s.Work.FilID != w.FilID, Recorded: s.Recorded}, nil
}
