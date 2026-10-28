package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/codevector-2003/filiation/internal/model"
	"github.com/codevector-2003/filiation/internal/store"
	"github.com/codevector-2003/filiation/internal/text"
)

const (
	// ExtractTimeout is the longest one PDF may take to read. A 70-page
	// paper takes about two seconds; a file still going at the limit is
	// pathological, and is recorded as timed out rather than holding up
	// the run (ARCHITECTURE_PHASE3.md §7).
	ExtractTimeout = 2 * time.Minute

	// DefaultSearchLimit is how many passages a search returns, and
	// DefaultPerPaper the most from one paper, so one long paper that uses
	// the words throughout does not crowd out the rest.
	DefaultSearchLimit = 20
	DefaultPerPaper    = 3

	readWorkers = 2 // PDFs read at once: PDFium runs two instances
)

// ErrNoPDF means the paper has no PDF in papers/ to read.
var ErrNoPDF = errors.New("no PDF in the library folder")

// MarkStart and MarkEnd surround the matched words in a SearchHit's
// Snippet, for a front door to show as it likes.
const (
	MarkStart = store.MarkStart
	MarkEnd   = store.MarkEnd
)

// ExtractOptions bound one reading of the library's PDFs.
type ExtractOptions struct {
	// Again reads every PDF, or the named paper's, even if already read.
	Again bool
	// Progress, if set, is called after each paper with the running totals.
	Progress func(Extracted)
}

// Extracted is what reading the PDFs did.
type Extracted struct {
	Total    int            // PDFs to read
	Done     int            // PDFs read
	Counts   map[string]int // by text.Status
	Passages int
	Duration time.Duration
	Papers   []ExtractedPaper
}

// ExtractedPaper is what reading one paper's PDF gave.
type ExtractedPaper struct {
	Work     model.Work
	File     string // absolute path of the PDF read
	Status   string // text.Status
	Reason   string // why there is no text, when Status is not ok
	Pages    int
	Passages int
	Took     time.Duration
	Already  bool // read before, from the same file; nothing was done
}

// ExtractText reads the text of every PDF in papers/ not yet read — new,
// changed, or read by an older version of fil — so it can be searched. It
// scans papers/ first, so a PDF you put there yourself is read too.
//
// A paper whose PDF has no usable text is recorded with the reason — a scan,
// a garbled text layer, a damaged file — and not tried again until the file
// changes. Cancelling keeps every paper already read.
func (l *Library) ExtractText(ctx context.Context, opts ExtractOptions) (Extracted, error) {
	if l.files == nil {
		return Extracted{Counts: map[string]int{}}, ErrNoFolder
	}
	if _, err := l.Scan(ctx); err != nil {
		return Extracted{Counts: map[string]int{}}, err
	}
	return l.extractAll(ctx, opts)
}

func (l *Library) extractAll(ctx context.Context, opts ExtractOptions) (Extracted, error) {
	todo, err := l.db.PDFsToRead(ctx, text.ExtractorVersion, opts.Again)
	if err != nil {
		return Extracted{Counts: map[string]int{}}, err
	}
	return l.extract(ctx, todo, opts.Progress)
}

// ExtractOne reads the text of the paper input names. Naming a paper is
// asking for it: with again, it is read even if it already was.
func (l *Library) ExtractOne(ctx context.Context, input string, again bool) (ExtractedPaper, error) {
	if l.files == nil {
		return ExtractedPaper{}, ErrNoFolder
	}
	w, err := l.Find(ctx, input)
	if err != nil {
		return ExtractedPaper{}, err
	}
	if _, err := l.Scan(ctx); err != nil {
		return ExtractedPaper{}, err
	}
	return l.extractWork(ctx, w.FilID, again)
}

func (l *Library) extractWork(ctx context.Context, filID string, again bool) (ExtractedPaper, error) {
	w, err := l.workWithAuthors(ctx, filID)
	if err != nil {
		return ExtractedPaper{}, err
	}
	r, ok, err := l.db.PDFToRead(ctx, filID)
	if err != nil {
		return ExtractedPaper{}, err
	}
	if !ok {
		return ExtractedPaper{Work: w}, fmt.Errorf("%s: %w", w.FilID, ErrNoPDF)
	}
	if !again {
		t, ok, err := l.db.TextOf(ctx, filID)
		if err != nil {
			return ExtractedPaper{}, err
		}
		if ok && t.SHA256 == r.SHA256 && t.Extractor == text.ExtractorVersion {
			// The file as it is now: a PDF moved or renamed since it was read
			// keeps its hash, and its text, but not its old path.
			return ExtractedPaper{Work: w, File: l.files.Abs(r.Path), Status: t.Status, Reason: t.Reason,
				Pages: t.Pages, Already: true}, nil
		}
	}
	res, err := l.extract(ctx, []store.ToRead{r}, nil)
	if err != nil {
		return ExtractedPaper{Work: w}, err
	}
	if len(res.Papers) == 0 {
		return ExtractedPaper{Work: w}, fmt.Errorf("%s: %w", w.FilID, ErrNoPDF) // gone since the scan
	}
	return res.Papers[0], nil
}

// extract reads PDFs two at a time, each within ExtractTimeout. Writes go
// through the store's one write handle, one transaction per paper.
func (l *Library) extract(ctx context.Context, todo []store.ToRead, progress func(Extracted)) (Extracted, error) {
	start := time.Now()
	res := Extracted{Total: len(todo), Counts: map[string]int{}}
	var mu sync.Mutex
	var firstErr error
	jobs := make(chan store.ToRead)
	var wg sync.WaitGroup
	for range min(readWorkers, len(todo)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				p, ok, err := l.readOne(ctx, job)
				mu.Lock()
				if err != nil && firstErr == nil && !errors.Is(err, context.Canceled) {
					firstErr = err
				}
				if err == nil && ok {
					res.add(p)
					if progress != nil {
						progress(res.snapshot(start))
					}
				}
				mu.Unlock()
			}
		}()
	}
feed:
	for _, job := range todo {
		mu.Lock()
		stop := firstErr != nil
		mu.Unlock()
		if stop {
			break // PDFium itself failed; every paper would fail the same way
		}
		select {
		case jobs <- job:
		case <-ctx.Done():
			break feed
		}
	}
	close(jobs)
	wg.Wait()
	res.Duration = time.Since(start)
	if err := ctx.Err(); err != nil {
		return res, err
	}
	return res, firstErr
}

// readOne reads one PDF and records what it gave. False means the file has
// gone since it was listed — moved or deleted — and there is nothing to
// record; the next scan finds out which.
func (l *Library) readOne(ctx context.Context, job store.ToRead) (ExtractedPaper, bool, error) {
	abs := l.files.Abs(job.Path)
	data, err := os.ReadFile(abs)
	if errors.Is(err, os.ErrNotExist) {
		return ExtractedPaper{}, false, nil
	}
	if err != nil {
		return ExtractedPaper{}, false, fmt.Errorf("read %s: %w", abs, err)
	}

	start := time.Now()
	fctx, cancel := context.WithTimeout(ctx, ExtractTimeout)
	r, err := text.Read(fctx, text.Shared(), data)
	cancel()
	took := time.Since(start)
	if err != nil {
		if ctx.Err() != nil {
			return ExtractedPaper{}, false, ctx.Err()
		}
		return ExtractedPaper{}, false, err
	}

	chunks := make([]store.Chunk, len(r.Chunks))
	for i, c := range r.Chunks {
		chunks[i] = store.Chunk{Page: c.Page, Ordinal: c.Ordinal, Section: c.Section, Start: c.Start, End: c.End, Text: c.Text}
	}
	t := store.Text{FilID: job.FilID, Path: job.Path, SHA256: job.SHA256, Status: string(r.Status), Reason: r.Reason,
		Pages: r.Pages, Chars: r.Chars, Extractor: text.ExtractorVersion, Millis: took.Milliseconds()}
	// Recorded even if the user cancels now: the work is done.
	wctx := context.WithoutCancel(ctx)
	if err := l.db.Tx(wctx, func(tx *store.Tx) error { return tx.PutText(wctx, t, chunks) }); err != nil {
		return ExtractedPaper{}, false, err
	}
	w, err := l.workWithAuthors(wctx, job.FilID)
	if err != nil {
		return ExtractedPaper{}, false, err
	}
	return ExtractedPaper{Work: w, File: abs, Status: t.Status, Reason: t.Reason, Pages: t.Pages,
		Passages: len(chunks), Took: took}, true, nil
}

func (e *Extracted) add(p ExtractedPaper) {
	e.Done++
	e.Counts[p.Status]++
	e.Passages += p.Passages
	e.Papers = append(e.Papers, p)
}

func (e *Extracted) snapshot(start time.Time) Extracted {
	c := *e
	c.Duration = time.Since(start)
	c.Counts = make(map[string]int, len(e.Counts))
	for k, v := range e.Counts {
		c.Counts[k] = v
	}
	c.Papers = nil
	return c
}

// TextCounts counts the papers whose PDF has been read, by what reading it
// gave: ok, no-text, gibberish, unreadable, timed-out.
func (l *Library) TextCounts(ctx context.Context) (map[string]int, error) {
	return l.db.TextCounts(ctx)
}

// SearchOptions shape one search.
type SearchOptions struct {
	Limit      int    // passages; zero means DefaultSearchLimit
	PerPaper   int    // most passages from one paper; zero means DefaultPerPaper
	Paper      string // only this paper, by any identifier
	References bool   // search reference lists too
}

// SearchHit is one passage that matched.
type SearchHit struct {
	Work    model.Work
	Page    int
	Section string // "references" for a reference list, else ""
	// Snippet is the matching stretch of the passage, with each matched word
	// between MarkStart and MarkEnd.
	Snippet string
	File    string // absolute path of the PDF
}

// Searched is a search's answer.
type Searched struct {
	Hits []SearchHit
	// Unread counts the PDFs the library knows of but has not read, which a
	// search cannot see: fil extract reads them. Search does not scan
	// papers/, so a PDF put there since the last scan is not counted.
	Unread int
}

// Search finds passages in the library's PDFs by keyword: every word must
// appear, "quoted phrases" in order, and a word ending in * matches as a
// prefix. Words match by stem, so "citing" finds "cited". Reference lists are
// left out unless asked for.
func (l *Library) Search(ctx context.Context, words string, opts SearchOptions) (Searched, error) {
	if opts.Limit <= 0 {
		opts.Limit = DefaultSearchLimit
	}
	if opts.PerPaper <= 0 {
		opts.PerPaper = DefaultPerPaper
	}
	q := store.SearchQuery{Words: words, Limit: opts.Limit, PerPaper: opts.PerPaper, References: opts.References}
	if opts.Paper != "" {
		w, err := l.Find(ctx, opts.Paper)
		if err != nil {
			return Searched{}, err
		}
		q.FilID = w.FilID
		q.PerPaper = opts.Limit
	}
	hits, err := l.db.Search(ctx, q)
	if err != nil {
		return Searched{}, err
	}
	var out Searched
	works := map[string]model.Work{}
	for _, h := range hits {
		w, ok := works[h.FilID]
		if !ok {
			if w, err = l.workWithAuthors(ctx, h.FilID); err != nil {
				return Searched{}, err
			}
			works[h.FilID] = w
		}
		file := h.Path
		if l.files != nil {
			file = l.files.Abs(h.Path)
		}
		out.Hits = append(out.Hits, SearchHit{Work: w, Page: h.Page, Section: h.Section, Snippet: h.Snippet, File: file})
	}
	unread, err := l.db.PDFsToRead(ctx, text.ExtractorVersion, false)
	if err != nil {
		return Searched{}, err
	}
	out.Unread = len(unread)
	return out, nil
}
