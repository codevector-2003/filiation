package text

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/requests"
)

// Front is what the opening of a PDF says about which paper it is: the
// clues import works from (ADR-014). Nothing in it is trusted until an
// identifier found here resolves to a work whose title is in Pages.
type Front struct {
	// Pages is the cleaned text of the first pages, at most frontPages.
	Pages []string
	// Meta is the document information dictionary, where present: Title,
	// Subject, Keywords, and the doi key some publishers add. Templates
	// reuse old values, so it is a clue, never an answer.
	Meta map[string]string
	// Title is the largest text near the top of page 1 — the title, usually,
	// but a journal's name or a running head sometimes. A candidate only.
	Title string
}

// frontPages is how far into a PDF its identity is looked for. The arXiv
// stamp, the DOI and the title are on page 1; page 2 catches a cover sheet.
const frontPages = 2

// metaKeys are the document information keys read. Custom keys are allowed
// by the PDF format; doi is the one publishers use.
var metaKeys = []string{"Title", "Subject", "Keywords", "doi", "DOI"}

// FrontReader reads a PDF's front matter. PDFium is one.
type FrontReader interface {
	Front(ctx context.Context, pdf []byte) (Front, error)
}

// Front reads the opening of a PDF. Like Extract, it stops at ctx's end and
// returns ErrUnreadable for a file PDFium cannot open.
func (p *PDFium) Front(ctx context.Context, pdf []byte) (Front, error) {
	var f Front
	err := p.run(ctx, func(inst pdfium.Pdfium) (err error) {
		f, err = readFront(inst, pdf)
		return err
	})
	return f, err
}

func readFront(inst pdfium.Pdfium, pdf []byte) (Front, error) {
	doc, err := inst.OpenDocument(&requests.OpenDocument{File: &pdf})
	if err != nil {
		return Front{}, fmt.Errorf("%w: %s", ErrUnreadable, reason(err))
	}
	defer inst.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: doc.Document}) //nolint:errcheck // nothing to do about it

	f := Front{Meta: map[string]string{}}
	for _, k := range metaKeys {
		m, err := inst.FPDF_GetMetaText(&requests.FPDF_GetMetaText{Document: doc.Document, Tag: k})
		if err == nil && strings.TrimSpace(m.Value) != "" {
			f.Meta[k] = strings.TrimSpace(m.Value)
		}
	}

	count, err := inst.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: doc.Document})
	if err != nil {
		return Front{}, fmt.Errorf("%w: %s", ErrUnreadable, reason(err))
	}
	for i := range min(count.PageCount, frontPages) {
		page := requests.Page{ByIndex: &requests.PageByIndex{Document: doc.Document, Index: i}}
		t, err := inst.GetPageText(&requests.GetPageText{Page: page})
		if err != nil {
			return Front{}, fmt.Errorf("%w: page %d: %s", ErrUnreadable, i+1, reason(err))
		}
		f.Pages = append(f.Pages, Clean(t.Text))
		if i == 0 {
			s, err := inst.GetPageTextStructured(&requests.GetPageTextStructured{
				Page: page, Mode: requests.GetPageTextStructuredModeRects, CollectFontInformation: true})
			if err == nil {
				runs := make([]run, 0, len(s.Rects))
				for _, r := range s.Rects {
					size := 0.0
					if r.FontInformation != nil {
						size = r.FontInformation.RenderedSize
						if size == 0 {
							size = r.FontInformation.Size
						}
					}
					runs = append(runs, run{text: r.Text, size: size, top: r.PointPosition.Top, left: r.PointPosition.Left})
				}
				f.Title = largestText(runs)
			}
		}
	}
	return f, nil
}

// run is a stretch of text set in one font on one line.
type run struct {
	text      string
	size      float64 // rendered size in points
	top, left float64 // position, in points from the page's bottom-left
}

// largestText returns the text set in the largest size that has enough of
// it to be a title — a drop capital or a logo letter does not — in reading
// order. Sizes within half a point are the same size.
//
// arXiv's margin stamp is often the largest text on the page, and is left
// out: it is a clue of its own (Clues), not a title.
func largestText(all []run) string {
	runs := all[:0:0]
	for _, r := range all {
		if !arXivStamp.MatchString(r.text) {
			runs = append(runs, r)
		}
	}
	letters := map[float64]int{}
	for _, r := range runs {
		letters[bucket(r.size)] += len(strings.Fields(r.text))
	}
	var sizes []float64
	for s, words := range letters {
		if words >= 3 && s > 0 {
			sizes = append(sizes, s)
		}
	}
	if len(sizes) == 0 {
		return ""
	}
	sort.Float64s(sizes)
	biggest := sizes[len(sizes)-1]

	var picked []run
	for _, r := range runs {
		if bucket(r.size) == biggest && strings.TrimSpace(r.text) != "" {
			picked = append(picked, r)
		}
	}
	// Top of the page first (larger top is higher), then left to right.
	sort.SliceStable(picked, func(i, j int) bool {
		if math.Abs(picked[i].top-picked[j].top) > biggest/2 {
			return picked[i].top > picked[j].top
		}
		return picked[i].left < picked[j].left
	})
	parts := make([]string, len(picked))
	for i, r := range picked {
		parts[i] = r.text
	}
	title := Clean(strings.Join(parts, " "))
	// An author line set in the title's size runs on into it: stop where
	// the names begin — initials, a surname, a comma or "and".
	if loc := authorList.FindStringIndex(title); loc != nil && loc[0] > 0 {
		title = title[:loc[0]]
	}
	words := strings.Fields(title)
	return strings.Join(words[:min(len(words), maxTitleWords)], " ")
}

// authorList is the start of a list of authors: "K. S. Novoselov ,",
// "S.V. Morozov,", "A. Smith and".
var authorList = regexp.MustCompile(`\b[A-Z]\.\s?(?:[A-Z]\.\s?)*[A-Z][a-z]+\s?(?:,|\band\b|&)`)

// maxTitleWords caps a title candidate: an author line set in the title's
// size would otherwise run on into it.
const maxTitleWords = 30

// arXivStamp is the identifier arXiv prints down the margin of every
// preprint: "arXiv:1706.03762v5 [cs.CL] 6 Dec 2017", or an old-style
// "arXiv:cond-mat/0604601v1".
var arXivStamp = regexp.MustCompile(`(?i)\barXiv:\s*((?:\d{4}\.\d{4,5})|(?:[a-z-]+(?:\.[A-Z]{2})?/\d{7}))(v\d+)?`)

func bucket(size float64) float64 { return math.Round(size*2) / 2 }
