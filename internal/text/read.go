package text

import (
	"context"
	"errors"
	"strings"
	"unicode"
)

// Result is what reading one PDF gave.
type Result struct {
	Verdict
	Pages  int
	Chars  int     // visible characters, all pages
	Chunks []Chunk // only when Status is ok: gibberish would pollute search
	// Text is the cleaned pages, when Status is ok: what citation context is
	// found in (Contexts), while the PDF is read.
	Text []string
}

// Read extracts, cleans, judges and chunks one PDF.
//
// A PDF that cannot be read is a Result, not an error: an unreadable file is
// StatusUnreadable, and one still being read when ctx's deadline passed is
// StatusTimedOut. The error is for everything else — the user cancelling,
// or PDFium failing to start — and means the run should stop.
func Read(ctx context.Context, ex Extractor, pdf []byte) (Result, error) {
	raw, err := ex.Extract(ctx, pdf)
	switch {
	case errors.Is(err, ErrUnreadable):
		return Result{Verdict: Verdict{StatusUnreadable, strings.TrimPrefix(err.Error(), ErrUnreadable.Error()+": ")}}, nil
	case errors.Is(err, context.DeadlineExceeded):
		return Result{Verdict: Verdict{StatusTimedOut, "still being read at the time limit"}}, nil
	case err != nil:
		return Result{}, err
	}

	pages := make([]string, len(raw))
	chars := 0
	for i, p := range raw {
		pages[i] = Clean(p)
		for _, r := range pages[i] {
			if !unicode.IsSpace(r) {
				chars++
			}
		}
	}
	res := Result{Verdict: Judge(pages), Pages: len(pages), Chars: chars}
	if res.Status == StatusOK {
		res.Chunks = Chunks(pages)
		res.Text = pages
	}
	return res, nil
}
