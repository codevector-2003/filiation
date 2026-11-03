package library

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/codevector-2003/filiation/internal/errs"
	"github.com/codevector-2003/filiation/internal/store"
)

// Collection is one collection, where it sits in the tree, and how many papers
// it holds, counting those in the collections inside it.
type Collection = store.Collection

// Collections lists the library's collections, each after the one it sits in.
func (l *Library) Collections(ctx context.Context) ([]Collection, error) {
	return l.db.Collections(ctx)
}

// Label is a label the user puts on papers.
type Label = store.Label

// Labels lists the library's labels, in name order.
func (l *Library) Labels(ctx context.Context) ([]Label, error) {
	return l.db.Labels(ctx)
}

// LabelColours are the colours a label can take. The interface always shows a
// label's name beside its colour, so the colour never carries the meaning alone.
var LabelColours = []string{"blue", "amber", "green", "pink", "purple", "grey"}

// Favourites counts the starred papers.
func (l *Library) Favourites(ctx context.Context) (int, error) {
	return l.db.Favourites(ctx)
}

// nameMax is the longest name a collection or label may have, in characters.
const nameMax = 120

// cleanName trims a collection or label name and checks there is one.
func cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return "", fmt.Errorf("a name is needed: %w", errs.ErrInvalidInput)
	case len([]rune(name)) > nameMax:
		return "", fmt.Errorf("a name can be at most %d characters: %w", nameMax, errs.ErrInvalidInput)
	case strings.ContainsFunc(name, unicode.IsControl):
		return "", fmt.Errorf("a name cannot hold a line break or tab: %w", errs.ErrInvalidInput)
	}
	return name, nil
}

func checkColour(colour string) error {
	for _, c := range LabelColours {
		if c == colour {
			return nil
		}
	}
	return fmt.Errorf("no label colour %q: %w", colour, errs.ErrInvalidInput)
}

// CreateCollection makes a collection inside parent, or at the top when parent
// is 0, and returns its ID. A collection made from a folder in papers/ cannot
// hold one: its contents are the folder's.
func (l *Library) CreateCollection(ctx context.Context, name string, parent int64) (int64, error) {
	name, err := cleanName(name)
	if err != nil {
		return 0, err
	}
	var id int64
	err = l.db.Tx(ctx, func(tx *store.Tx) error {
		id, err = tx.CreateCollection(ctx, name, parent)
		return err
	})
	return id, err
}

// RenameCollection renames one of the user's collections.
func (l *Library) RenameCollection(ctx context.Context, id int64, name string) error {
	name, err := cleanName(name)
	if err != nil {
		return err
	}
	return l.db.Tx(ctx, func(tx *store.Tx) error { return tx.RenameCollection(ctx, id, name) })
}

// MoveCollection puts one of the user's collections inside another, or at
// the top when parent is 0.
func (l *Library) MoveCollection(ctx context.Context, id, parent int64) error {
	return l.db.Tx(ctx, func(tx *store.Tx) error { return tx.MoveCollection(ctx, id, parent) })
}

// DeleteCollection deletes one of the user's collections and those inside it.
// Its papers, and their PDFs, stay in the library.
func (l *Library) DeleteCollection(ctx context.Context, id int64) error {
	return l.db.Tx(ctx, func(tx *store.Tx) error { return tx.DeleteCollection(ctx, id) })
}

// FileInCollection adds papers, by fil ID, to one of the user's collections,
// or takes them out when in is false.
func (l *Library) FileInCollection(ctx context.Context, id int64, papers []string, in bool) error {
	return l.db.Tx(ctx, func(tx *store.Tx) error { return tx.FileInCollection(ctx, id, papers, in) })
}

// CreateLabel makes a label and returns its ID.
func (l *Library) CreateLabel(ctx context.Context, name, colour string) (int64, error) {
	name, err := cleanName(name)
	if err != nil {
		return 0, err
	}
	if err := checkColour(colour); err != nil {
		return 0, err
	}
	var id int64
	err = l.db.Tx(ctx, func(tx *store.Tx) error {
		id, err = tx.CreateLabel(ctx, name, colour)
		return err
	})
	return id, err
}

// ChangeLabel renames a label and sets its colour.
func (l *Library) ChangeLabel(ctx context.Context, id int64, name, colour string) error {
	name, err := cleanName(name)
	if err != nil {
		return err
	}
	if err := checkColour(colour); err != nil {
		return err
	}
	return l.db.Tx(ctx, func(tx *store.Tx) error { return tx.ChangeLabel(ctx, id, name, colour) })
}

// DeleteLabel deletes a label. The papers it was on stay in the library.
func (l *Library) DeleteLabel(ctx context.Context, id int64) error {
	return l.db.Tx(ctx, func(tx *store.Tx) error { return tx.DeleteLabel(ctx, id) })
}

// SetLabel puts a label on papers, by fil ID, or takes it off.
func (l *Library) SetLabel(ctx context.Context, id int64, papers []string, on bool) error {
	return l.db.Tx(ctx, func(tx *store.Tx) error { return tx.SetLabel(ctx, id, papers, on) })
}

// SetFavourite stars papers, by fil ID, or unstars them.
func (l *Library) SetFavourite(ctx context.Context, papers []string, on bool) error {
	return l.db.Tx(ctx, func(tx *store.Tx) error { return tx.SetFavourite(ctx, papers, on) })
}
