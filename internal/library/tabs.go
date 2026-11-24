package library

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/codevector-2003/filiation/internal/errs"
	"github.com/codevector-2003/filiation/internal/identity"
	"github.com/codevector-2003/filiation/internal/store"
)

// The desktop app keeps several papers open in the Reader at once, one tab
// each, beside the Library tab that holds every other screen. The tabs are
// kept in the library, so a library opened again shows its own papers where
// they were left, and another library shows its own.

// metaReaderTabs is the meta key the tabs are kept under, as JSON.
const metaReaderTabs = "reader_tabs"

// MaxReaderTabs is how many paper tabs are kept.
const MaxReaderTabs = 50

// ReaderTab is one paper open in the Reader, and where its reader was in it.
type ReaderTab struct {
	ID   string  `json:"id"`             // the paper's fil ID
	Page int     `json:"page"`           // from 1
	Zoom float64 `json:"zoom,omitempty"` // the page's scale; 0 leaves the Reader's own
	Side string  `json:"side,omitempty"` // notes, cited or details
	Find string  `json:"find,omitempty"` // the words in Find in this paper
}

// ReaderTabs is every paper tab, in the order shown, and which tab was in
// front: a paper's fil ID, or "" for the Library tab. Screen is the screen the
// Library tab was showing.
type ReaderTabs struct {
	Tabs   []ReaderTab `json:"tabs"`
	Active string      `json:"active"`
	Screen string      `json:"screen,omitempty"`
}

var readerSides = map[string]bool{"notes": true, "cited": true, "details": true}

// ReaderTabs returns the tabs kept for this library, leaving out any whose
// paper has gone or no longer has a PDF to show. Tabs that cannot be read
// back are forgotten rather than refused: they are a convenience, and the
// app must open without them.
func (l *Library) ReaderTabs(ctx context.Context) (ReaderTabs, error) {
	raw, err := l.db.Meta(ctx, metaReaderTabs)
	if err != nil {
		return ReaderTabs{Tabs: []ReaderTab{}}, err
	}
	var kept ReaderTabs
	if raw == "" || json.Unmarshal([]byte(raw), &kept) != nil {
		return ReaderTabs{Tabs: []ReaderTab{}}, nil
	}
	kept = tidyTabs(kept)
	out := ReaderTabs{Tabs: []ReaderTab{}, Screen: kept.Screen}
	for _, t := range kept.Tabs {
		ok, err := l.HasPDF(ctx, t.ID)
		if err != nil {
			return ReaderTabs{Tabs: []ReaderTab{}}, err
		}
		if ok {
			out.Tabs = append(out.Tabs, t)
			if t.ID == kept.Active {
				out.Active = t.ID
			}
		}
	}
	return out, nil
}

// SaveReaderTabs keeps the tabs for the next time this library is opened.
func (l *Library) SaveReaderTabs(ctx context.Context, tabs ReaderTabs) error {
	if len(tabs.Tabs) > MaxReaderTabs {
		return fmt.Errorf("at most %d papers can be kept open: %w", MaxReaderTabs, errs.ErrInvalidInput)
	}
	for _, t := range tabs.Tabs {
		if _, err := identity.NormaliseFilID(t.ID); err != nil {
			return fmt.Errorf("%q is not a fil ID: %w", t.ID, errs.ErrInvalidInput)
		}
	}
	raw, err := json.Marshal(tidyTabs(tabs))
	if err != nil {
		return err
	}
	return l.db.Tx(ctx, func(tx *store.Tx) error { return tx.SetMeta(ctx, metaReaderTabs, string(raw)) })
}

// tidyTabs puts the tabs in a shape the Reader can always open: one tab per
// paper, a page it can turn to, a side panel it has.
func tidyTabs(in ReaderTabs) ReaderTabs {
	out := ReaderTabs{Tabs: []ReaderTab{}, Screen: shortText(in.Screen, 40)}
	seen := map[string]bool{}
	for _, t := range in.Tabs {
		id, err := identity.NormaliseFilID(t.ID)
		if err != nil || seen[id] || len(out.Tabs) == MaxReaderTabs {
			continue
		}
		seen[id] = true
		t.ID = id
		t.Page = max(t.Page, 1)
		if t.Zoom < 0.25 || t.Zoom > 5 {
			t.Zoom = 0
		}
		if !readerSides[t.Side] {
			t.Side = ""
		}
		t.Find = shortText(t.Find, 200)
		out.Tabs = append(out.Tabs, t)
	}
	if active, err := identity.NormaliseFilID(in.Active); err == nil && seen[active] {
		out.Active = active
	}
	return out
}

// HasPDF reports whether the library holds a PDF of the paper that is still
// on disk, as far as it knows.
func (l *Library) HasPDF(ctx context.Context, filID string) (bool, error) {
	files, err := l.db.FilesOf(ctx, filID)
	if err != nil {
		return false, err
	}
	for _, f := range files {
		if !f.Missing && strings.EqualFold(filepath.Ext(f.Path), ".pdf") {
			return true, nil
		}
	}
	return false, nil
}

// shortText trims s to at most n characters, never cutting one in half.
func shortText(s string, n int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return strings.TrimSpace(string([]rune(s)[:n]))
}
