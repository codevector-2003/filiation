package library

import (
	"context"
	"strings"
	"time"

	"github.com/codevector-2003/filiation/internal/model"
	"github.com/codevector-2003/filiation/internal/store"
	"github.com/codevector-2003/filiation/internal/text"
)

// ListOptions filter and bound a listing of the library.
type ListOptions = store.ListQuery

// Listed is one paper in a listing, with its state beside it: a PDF on
// disk, what reading it gave, how many references carry their sentence.
type Listed = store.ListRow

// List lists the library: seeds first, then the most cited within it.
func (l *Library) List(ctx context.Context, opts ListOptions) ([]Listed, error) {
	return l.db.List(ctx, opts)
}

// Paper is everything the library knows about one paper.
type Paper struct {
	Work    model.Work
	Cites   int // works it cites
	CitedBy int // works in this library that cite it

	Folder string      // its folder in papers/, absolute; "" when it has none
	Files  []PaperFile // its files, present ones first

	// PDF is whether a legal PDF link is known, and the links; Asked is when
	// OpenAlex was asked (zero: never).
	PDF      model.PDFLinks
	Asked    time.Time
	Attempts []Attempt // downloads tried, newest first

	// Text is what reading its PDF gave; nil when it was never read.
	Text *PaperText

	// Contexts is how many of its references carry the sentence they are
	// cited in; Sample is the first few, in reading order.
	Contexts int
	Sample   []CitedIn
}

// PaperFile is one of a paper's files.
type PaperFile struct {
	Path     string // absolute
	Version  string
	Origin   string // download | import | found
	Missing  bool
	Mismatch bool // its first pages name another paper or edition (fetch's check)
}

// PaperText is what reading a paper's PDF gave.
type PaperText struct {
	Status string // text.Status
	Reason string
	Pages  int
	File   string // absolute
}

// CitedIn is a reference and one sentence the paper cites it in.
type CitedIn struct {
	Work model.Work
	Mention
}

// showSample is how many citation sentences Show returns.
const showSample = 5

// Show gathers everything the library knows about the paper input names:
// what fil show prints and get_paper returns.
func (l *Library) Show(ctx context.Context, input string) (Paper, error) {
	w, err := l.Find(ctx, input)
	if err != nil {
		return Paper{}, err
	}
	if w, err = l.workWithAuthors(ctx, w.FilID); err != nil {
		return Paper{}, err
	}
	p := Paper{Work: w}
	n, err := l.Neighbours(ctx, w.FilID)
	if err != nil {
		return Paper{}, err
	}
	p.Cites, p.CitedBy = len(n.Cites), len(n.CitedBy)

	if l.files != nil {
		folders, err := l.db.FoldersOf(ctx, w.FilID)
		if err != nil {
			return Paper{}, err
		}
		for _, f := range folders {
			if !f.Missing {
				p.Folder = l.files.Abs(f.Path)
				break
			}
		}
		files, err := l.db.FilesOf(ctx, w.FilID)
		if err != nil {
			return Paper{}, err
		}
		for _, f := range files {
			p.Files = append(p.Files, PaperFile{Path: l.files.Abs(f.Path), Version: f.Version, Origin: f.Origin,
				Missing: f.Missing, Mismatch: f.Identity == store.IdentityMismatch})
		}
	}
	if w.OpenAlexID != "" {
		if p.PDF, p.Asked, err = l.db.PDFLinksOf(ctx, w.FilID); err != nil {
			return Paper{}, err
		}
		if p.Attempts, err = l.db.AttemptsOf(ctx, w.FilID); err != nil {
			return Paper{}, err
		}
	}
	if t, ok, err := l.db.TextOf(ctx, w.FilID); err != nil {
		return Paper{}, err
	} else if ok {
		file := t.Path
		if l.files != nil {
			file = l.files.Abs(t.Path)
		}
		p.Text = &PaperText{Status: t.Status, Reason: t.Reason, Pages: t.Pages, File: file}
	}

	contexts, err := l.CitationContexts(ctx, w.FilID)
	if err != nil {
		return Paper{}, err
	}
	p.Contexts = len(contexts)
	byID := map[string]model.Work{}
	for _, c := range n.Cites {
		byID[c.FilID] = c
	}
	ms, err := l.db.MentionsOf(ctx, w.FilID)
	if err != nil {
		return Paper{}, err
	}
	seen := map[string]bool{}
	for _, m := range ms {
		if len(p.Sample) == showSample {
			break
		}
		if seen[m.ToWork] {
			continue
		}
		seen[m.ToWork] = true
		p.Sample = append(p.Sample, CitedIn{Work: byID[m.ToWork], Mention: Mention{Page: m.Page, Sentence: m.Sentence,
			Section: m.Section, Marker: m.Marker, Method: m.Method, Confidence: m.Confidence}})
	}
	return p, nil
}

// NoText is a paper whose PDF gave no searchable text, and why.
type NoText struct {
	Work   model.Work
	Status string // no-text, gibberish, unreadable, timed-out
	Reason string
	File   string // absolute
}

// WithoutText lists the papers whose PDF gave no searchable text, each with
// the reason, grouped by status: what fil stats --why prints.
func (l *Library) WithoutText(ctx context.Context) ([]NoText, error) {
	ts, err := l.db.WithoutText(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]NoText, 0, len(ts))
	for _, t := range ts {
		w, err := l.workWithAuthors(ctx, t.FilID)
		if err != nil {
			return nil, err
		}
		file := t.Path
		if l.files != nil {
			file = l.files.Abs(t.Path)
		}
		out = append(out, NoText{Work: w, Status: t.Status, Reason: t.Reason, File: file})
	}
	return out, nil
}

// checkIdentity records whether a downloaded PDF's first pages carry its
// paper's title and first author's surname — the check import makes before
// filing anything (ADR-014). A download can be another paper, or another
// edition: a living review's link serves its newest version under every
// record of it. A mismatch is flagged, never deleted; the file may still be
// wanted.
func (l *Library) checkIdentity(ctx context.Context, job store.ToRead, pages []string, w model.Work) error {
	files, err := l.db.FilesOf(ctx, job.FilID)
	if err != nil {
		return err
	}
	for _, f := range files {
		if f.Path != job.Path || f.Origin != store.OriginDownload || w.Title == nil {
			continue
		}
		front := pages[:min(len(pages), 2)]
		surname := ""
		if len(w.Authors) > 0 {
			if fs := strings.Fields(w.Authors[0].Name); len(fs) > 0 {
				surname = fs[len(fs)-1]
			}
		}
		identity := store.IdentityMismatch
		if text.Mentions(front, *w.Title, surname) || text.Resembles(front, *w.Title, surname) {
			identity = store.IdentityMatch
		}
		return l.db.Tx(context.WithoutCancel(ctx), func(tx *store.Tx) error { return tx.SetIdentity(ctx, f.Path, identity) })
	}
	return nil
}
