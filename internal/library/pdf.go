package library

import (
	"context"
	"time"

	"github.com/codevector-2003/filiation/internal/graph"
	"github.com/codevector-2003/filiation/internal/model"
)

const (
	// DefaultLocateMax is how many works one lookup of PDF links covers
	// unless told otherwise: 20 requests, 20 credits of 1,000 a day (D11).
	DefaultLocateMax = 2000

	// LocateMaxAge is how old a PDF answer may be before it is asked again.
	// Repositories catch up — a preprint is posted, an embargo ends — so
	// "no open copy" is an answer for a month, not for ever.
	LocateMaxAge = 30 * 24 * time.Hour
)

// LocateOptions bound one lookup of PDF links.
type LocateOptions struct {
	// Max is the most works to look up; zero means DefaultLocateMax.
	Max int
	// Progress, if set, is called after each batch of 100.
	Progress func(Located)
}

// Located is what a lookup of PDF links found.
type Located = graph.Located

// LocatePDFs looks up the open-access PDF links of works whose links are not
// known yet, or were looked up more than a month ago — your own papers first,
// then the ones your library cites most (ADR-013). Nothing is downloaded.
func (l *Library) LocatePDFs(ctx context.Context, opts LocateOptions) (Located, error) {
	if opts.Max == 0 {
		opts.Max = DefaultLocateMax
	}
	return l.graph.Locate(ctx, graph.LocateOptions{Max: opts.Max, MaxAge: LocateMaxAge, Progress: opts.Progress})
}

// PDFLinks is what is known about one work's open-access PDF.
type PDFLinks struct {
	Work model.Work
	model.PDFLinks
	CheckedAt time.Time // zero when never looked up
}

// PDFLinks returns what is known about the PDF of the work input names,
// looking it up first — one request — if it never has been, or if the answer
// is older than LocateMaxAge, as a library-wide lookup would.
func (l *Library) PDFLinks(ctx context.Context, input string) (PDFLinks, error) {
	w, err := l.Find(ctx, input)
	if err != nil {
		return PDFLinks{}, err
	}
	links, at, err := l.db.PDFLinksOf(ctx, w.FilID)
	if err != nil {
		return PDFLinks{}, err
	}
	stale := links.Availability == model.PDFUnchecked || (!at.IsZero() && time.Since(at) > LocateMaxAge)
	if stale && w.OpenAlexID != "" && !w.Unresolved {
		if _, err := l.graph.LocateIDs(ctx, []string{w.OpenAlexID}, nil); err != nil {
			return PDFLinks{}, err
		}
		if links, at, err = l.db.PDFLinksOf(ctx, w.FilID); err != nil {
			return PDFLinks{}, err
		}
	}
	return PDFLinks{Work: w, PDFLinks: links, CheckedAt: at}, nil
}

// PDFCounts counts the library's works by what is known about their PDF;
// works never looked up count under model.PDFUnchecked.
func (l *Library) PDFCounts(ctx context.Context) (map[model.PDFAvailability]int, error) {
	return l.db.PDFCounts(ctx)
}
