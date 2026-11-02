package library

import (
	"context"
	"fmt"
	"strings"

	"github.com/codevector-2003/filiation/internal/errs"
	"github.com/codevector-2003/filiation/internal/store"
)

// Note is a highlight or comment on a paper. It lives in the library, so the
// PDF file stays exactly as it was.
type Note = store.Note

// noteColours are the highlight colours the Reader offers.
var noteColours = map[string]bool{"": true, "yellow": true, "green": true, "blue": true, "pink": true}

// AddNote adds a note to the paper input names and returns it with its ID.
func (l *Library) AddNote(ctx context.Context, input string, n Note) (Note, error) {
	w, err := l.Find(ctx, input)
	if err != nil {
		return Note{}, err
	}
	if !noteColours[n.Colour] {
		return Note{}, fmt.Errorf("no highlight colour %q: %w", n.Colour, errs.ErrInvalidInput)
	}
	if n.Page < 0 {
		return Note{}, fmt.Errorf("no page %d: %w", n.Page, errs.ErrInvalidInput)
	}
	if strings.TrimSpace(n.Quote) == "" && strings.TrimSpace(n.Comment) == "" {
		return Note{}, fmt.Errorf("a note needs some text: %w", errs.ErrInvalidInput)
	}
	n.FilID = w.FilID
	err = l.db.Tx(ctx, func(tx *store.Tx) error {
		var err error
		n.ID, err = tx.AddNote(ctx, n)
		return err
	})
	return n, err
}

// SetNoteComment replaces a note's comment.
func (l *Library) SetNoteComment(ctx context.Context, id int64, comment string) error {
	return l.db.Tx(ctx, func(tx *store.Tx) error { return tx.SetNoteComment(ctx, id, comment) })
}

// DeleteNote removes a note.
func (l *Library) DeleteNote(ctx context.Context, id int64) error {
	return l.db.Tx(ctx, func(tx *store.Tx) error { return tx.DeleteNote(ctx, id) })
}

// Notes lists the notes on the paper input names, in reading order.
func (l *Library) Notes(ctx context.Context, input string) ([]Note, error) {
	w, err := l.Find(ctx, input)
	if err != nil {
		return nil, err
	}
	return l.db.Notes(ctx, w.FilID)
}
