package library

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/codevector-2003/filiation/internal/blobs"
	"github.com/codevector-2003/filiation/internal/model"
	"github.com/codevector-2003/filiation/internal/sources/pdfs"
	"github.com/codevector-2003/filiation/internal/store"
)

const (
	// DefaultFetchMax is how many PDFs one fetch downloads unless told
	// otherwise. At spike 9's median sizes that is 25–50 MB and a few
	// minutes; run fetch again to continue. Disk and bandwidth are the
	// user's.
	DefaultFetchMax = 50

	// FetchRetryAfter is how long a link that failed is left alone. A host
	// that refused once will usually refuse again tomorrow.
	FetchRetryAfter = 30 * 24 * time.Hour

	// MaxPDFSize stops a runaway download (ADR-013).
	MaxPDFSize = 100 << 20

	fetchWorkers    = 4 // downloads at once; each host still gets one request a second
	maxLinksPerWork = 3 // links tried for one paper in one run
)

// FetchOptions bound one fetch.
type FetchOptions struct {
	// Max is the most PDFs to download; zero means DefaultFetchMax.
	Max int
	// Progress, if set, is called after each paper with the running totals.
	Progress func(Fetched)
	// ReadProgress, if set, is called after each PDF is read, once the
	// downloads are done.
	ReadProgress func(Extracted)
}

// Fetched is what a fetch did.
type Fetched struct {
	Located    Located // links looked up first, for papers never asked about
	Hydrated   int     // papers known only by ID, fetched first so they have a title
	Tried      int     // papers a download was tried for
	Downloaded int     // papers that now have a PDF
	Bytes      int64
	Duration   time.Duration

	// Outcomes counts the failed attempts by why they failed.
	Outcomes map[string]int

	Papers []FetchedPaper

	// Read is the reading of the PDFs that followed the downloads: every PDF
	// in papers/ not yet read, whether downloaded now or before.
	Read Extracted
}

// Attempt is one download attempt: the link, its host, how it ended.
type Attempt = store.Attempt

// FetchedPaper is what happened for one paper.
type FetchedPaper struct {
	Work     model.Work
	File     string // absolute path of the PDF, when one was saved or was already there
	Already  bool   // the PDF was already in the library; nothing was downloaded
	Attempts []Attempt
	Text     *ExtractedPaper // reading the PDF, when there is one
}

// Fetch downloads the open-access PDFs of up to Max papers into papers/ —
// your own papers first, then the ones your library cites most (ADR-013).
//
// It scans papers/ first, so a PDF you added or moved counts and a paper
// whose PDF you deleted is not downloaded again. It looks up links for
// papers never asked about. Then, four papers at a time, it tries each
// paper's links best first — published, accepted, preprint — until one
// gives a PDF, at most three. Every attempt is recorded; a link that failed
// is left alone for a month. Cancelling keeps every finished download.
//
// Once the downloads are done, it reads the text of every PDF not yet read,
// so a search finds them (ExtractText).
func (l *Library) Fetch(ctx context.Context, opts FetchOptions) (Fetched, error) {
	start := time.Now()
	res := Fetched{Outcomes: map[string]int{}, Read: Extracted{Counts: map[string]int{}}}
	if l.files == nil {
		return res, ErrNoFolder
	}
	if opts.Max == 0 {
		opts.Max = DefaultFetchMax
	}
	if _, err := l.Scan(ctx); err != nil {
		return res, err
	}
	located, err := l.LocatePDFs(ctx, LocateOptions{})
	res.Located = located
	if err != nil {
		return res, err
	}
	todo, err := l.db.WorksToFetch(ctx, opts.Max, FetchRetryAfter)
	if err != nil {
		return res, err
	}
	ids := make([]string, len(todo))
	for i, job := range todo {
		ids[i] = job.FilID
	}
	if res.Hydrated, err = l.hydrateStubs(ctx, ids); err != nil {
		return res, err
	}

	var mu sync.Mutex
	jobs := make(chan store.ToFetch)
	var wg sync.WaitGroup
	var firstErr error
	for range fetchWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				w, err := l.workWithAuthors(ctx, job.FilID)
				var p FetchedPaper
				if err == nil {
					p, err = l.fetchWork(ctx, w, job.Links)
				}
				mu.Lock()
				if err != nil && firstErr == nil && !errors.Is(err, context.Canceled) {
					firstErr = err
				}
				if err == nil {
					res.add(p)
					if opts.Progress != nil {
						opts.Progress(res.snapshot(start))
					}
				}
				mu.Unlock()
			}
		}()
	}
feed:
	for _, job := range todo {
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
	if firstErr != nil {
		return res, firstErr
	}
	read, err := l.extractAll(ctx, ExtractOptions{Progress: opts.ReadProgress})
	res.Read = read
	return res, err
}

// FetchOne downloads the PDF of the paper input names. Naming a paper is
// asking for it: every link is tried, including ones that failed before, and
// a PDF you deleted is downloaded again.
func (l *Library) FetchOne(ctx context.Context, input string) (FetchedPaper, error) {
	if l.files == nil {
		return FetchedPaper{}, ErrNoFolder
	}
	p, err := l.PDFLinks(ctx, input) // finds the work, and looks its links up if need be
	if err != nil {
		return FetchedPaper{}, err
	}
	if _, err := l.Scan(ctx); err != nil {
		return FetchedPaper{}, err
	}
	if _, err := l.hydrateStubs(ctx, []string{p.Work.FilID}); err != nil {
		return FetchedPaper{}, err
	}
	w, err := l.workWithAuthors(ctx, p.Work.FilID)
	if err != nil {
		return FetchedPaper{}, err
	}
	files, err := l.db.FilesOf(ctx, w.FilID)
	if err != nil {
		return FetchedPaper{}, err
	}
	for _, f := range files {
		if !f.Missing && isPDFPath(f.Path) {
			return l.withText(ctx, FetchedPaper{Work: w, File: l.files.Abs(f.Path), Already: true})
		}
	}
	links, err := l.db.LinksToTry(ctx, w.FilID, 0)
	if err != nil {
		return FetchedPaper{}, err
	}
	got, err := l.fetchWork(ctx, w, links)
	if err != nil || got.File == "" {
		return got, err
	}
	return l.withText(ctx, got)
}

// withText reads the paper's PDF, unless it has been read already.
func (l *Library) withText(ctx context.Context, p FetchedPaper) (FetchedPaper, error) {
	t, err := l.extractWork(ctx, p.Work.FilID, false)
	if err != nil {
		return p, err
	}
	p.Text = &t
	return p, nil
}

// hydrateStubs fetches the metadata of the stubs among the papers about to be
// downloaded — a reference fil has a PDF link for but no title yet — so
// each gets a folder named for it and a title in search, and its own
// references for citation context. One request per 100 papers.
//
// A failure to hydrate does not stop the downloads: the folder is named
// "Unknown" for now and renamed when the paper is fetched later. Only
// cancelling is an error.
func (l *Library) hydrateStubs(ctx context.Context, filIDs []string) (int, error) {
	var stubs []string
	for _, id := range filIDs {
		w, err := l.getWork(ctx, id)
		if err != nil {
			return 0, err
		}
		if w.IsStub() && !w.Unresolved && w.OpenAlexID != "" {
			stubs = append(stubs, w.OpenAlexID)
		}
	}
	if len(stubs) == 0 {
		return 0, nil
	}
	res, err := l.graph.HydrateIDs(ctx, stubs)
	if err != nil && ctx.Err() != nil {
		return res.Hydrated, ctx.Err()
	}
	return res.Hydrated, nil
}

func (l *Library) workWithAuthors(ctx context.Context, filID string) (model.Work, error) {
	w, err := l.getWork(ctx, filID)
	if err != nil {
		return model.Work{}, err
	}
	ws := []model.Work{w}
	if err := l.withAuthors(ctx, ws); err != nil {
		return model.Work{}, err
	}
	return ws[0], nil
}

// fetchWork tries a paper's links in order until one gives a PDF.
func (l *Library) fetchWork(ctx context.Context, w model.Work, links []model.Location) (FetchedPaper, error) {
	out := FetchedPaper{Work: w}
	for i, link := range links {
		if i == maxLinksPerWork {
			break
		}
		a, staged, err := l.download(ctx, link)
		if err != nil {
			return out, err // cancelled, or the library folder cannot be written
		}
		if staged != nil {
			saved, dup, err := l.keep(ctx, w, link, staged, &a)
			staged.Discard() // after a Commit, nothing to do
			if err != nil {
				return out, err
			}
			if !dup {
				out.File = saved
			}
		}
		if err := l.db.Tx(ctx, func(tx *store.Tx) error { return tx.RecordAttempt(ctx, w.FilID, a) }); err != nil {
			return out, err
		}
		out.Attempts = append(out.Attempts, a)
		if out.File != "" {
			return out, nil
		}
	}
	return out, nil
}

// download fetches one link into the temporary directory. A failure is
// returned as an attempt, not an error; the error is only for cancelling.
func (l *Library) download(ctx context.Context, link model.Location) (store.Attempt, *blobs.Staged, error) {
	a := store.Attempt{URL: link.PDFURL, Host: link.Host}
	start := time.Now()

	d, err := l.pdfs.Get(ctx, link.PDFURL)
	if err != nil {
		f := pdfs.Classify(ctx, err)
		if f.Outcome == pdfs.Cancelled {
			return a, nil, ctx.Err()
		}
		a.Outcome, a.Status, a.Millis = string(f.Outcome), f.Status, time.Since(start).Milliseconds()
		return a, nil, nil
	}
	body := &readErr{r: d.Body}
	staged, err := l.files.Stage(body, blobs.WriteOptions{PDF: true, MaxSize: MaxPDFSize})
	d.Body.Close()
	a.Millis = time.Since(start).Milliseconds()
	switch {
	case err == nil:
		a.Outcome, a.Bytes = store.OutcomeOK, staged.Size
		return a, staged, nil
	case errors.Is(err, blobs.ErrTooLarge):
		a.Outcome = string(pdfs.TooLarge)
	case errors.Is(err, blobs.ErrNotPDF):
		a.Outcome = string(pdfs.NotPDF)
	case body.err != nil:
		f := pdfs.Classify(ctx, body.err)
		if f.Outcome == pdfs.Cancelled {
			return a, nil, ctx.Err()
		}
		a.Outcome = string(f.Outcome)
	default:
		// Not the link's fault: the library folder could not be written —
		// full, or not writable. Recording the link as failed would leave it
		// alone for a month after the disk was fixed, so stop instead.
		return a, nil, fmt.Errorf("save a download in %s: %w", l.files.Root(), err)
	}
	return a, nil, nil
}

// readErr remembers the error reading a download, so a failed save can be
// told apart: the network's fault, or the disk's.
type readErr struct {
	r   io.Reader
	err error
}

func (e *readErr) Read(p []byte) (int, error) {
	n, err := e.r.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		e.err = err
	}
	return n, err
}

// keep puts a downloaded PDF in the paper's folder and records it — unless
// the library already holds those exact bytes for another paper, which marks
// the link as serving something else (a publisher's notice, typically) and
// the attempt as a duplicate.
func (l *Library) keep(ctx context.Context, w model.Work, link model.Location, s *blobs.Staged, a *store.Attempt) (string, bool, error) {
	// Checking the hash and recording the file are one step. Downloads run
	// four at a time, and two of them can bring the same bytes: without the
	// lock both would look before either had recorded, and both would keep a
	// copy.
	l.keepMu.Lock()
	defer l.keepMu.Unlock()

	same, err := l.db.FilesWithHash(ctx, s.SHA256)
	if err != nil {
		return "", false, err
	}
	for _, f := range same {
		if f.FilID != w.FilID {
			a.Outcome = store.OutcomeDuplicate
			return "", true, nil
		}
	}
	folder, err := l.ensureFolder(ctx, w)
	if err != nil {
		return "", false, err
	}
	rel, err := l.files.Rel(folder.Path)
	if err != nil {
		return "", false, err
	}
	base := fileBase(link.Version)
	var saved blobs.File
	for n := 1; ; n++ {
		name := base + ".pdf"
		if n > 1 {
			name = fmt.Sprintf("%s-%d.pdf", base, n)
		}
		saved, err = s.Commit(rel + "/" + name)
		if !errors.Is(err, fs.ErrExist) || n == 20 {
			break
		}
	}
	if err != nil {
		return "", false, err
	}
	abs := l.files.Abs(saved.Path)
	var mtime int64
	if info, err := os.Stat(abs); err == nil {
		mtime = info.ModTime().Unix()
	}
	err = l.db.Tx(ctx, func(tx *store.Tx) error {
		return tx.PutFile(ctx, store.File{Path: saved.Path, FilID: w.FilID, SHA256: saved.SHA256, Size: saved.Size,
			ModTime: mtime, Version: link.Version, Origin: store.OriginDownload})
	})
	if err != nil {
		return "", false, err // the file stays; the next scan records it as found
	}
	return abs, false, nil
}

// fileBase names a downloaded PDF by its version (ADR-012).
func fileBase(version string) string {
	if version == "" {
		return "paper"
	}
	return version
}

func isPDFPath(p string) bool { return strings.EqualFold(filepath.Ext(p), ".pdf") }

func (f *Fetched) add(p FetchedPaper) {
	f.Tried++
	f.Papers = append(f.Papers, p)
	if p.File != "" {
		f.Downloaded++
	}
	for _, a := range p.Attempts {
		if a.Outcome == store.OutcomeOK {
			f.Bytes += a.Bytes
		} else {
			f.Outcomes[a.Outcome]++
		}
	}
}

func (f *Fetched) snapshot(start time.Time) Fetched {
	c := *f
	c.Duration = time.Since(start)
	c.Outcomes = make(map[string]int, len(f.Outcomes))
	for k, v := range f.Outcomes {
		c.Outcomes[k] = v
	}
	c.Papers = nil
	return c
}

// FetchedOnDisk counts the papers that have a PDF in papers/.
func (l *Library) FetchedOnDisk(ctx context.Context) (int, error) { return l.db.PDFsOnDisk(ctx) }

// Attempts returns the download attempts for the paper input names, newest
// first, so a paper without a PDF can say exactly why.
func (l *Library) Attempts(ctx context.Context, input string) ([]Attempt, error) {
	w, err := l.Find(ctx, input)
	if err != nil {
		return nil, err
	}
	return l.db.AttemptsOf(ctx, w.FilID)
}
