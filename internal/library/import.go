package library

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/codevector-2003/filiation/internal/blobs"
	"github.com/codevector-2003/filiation/internal/errs"
	"github.com/codevector-2003/filiation/internal/httpx"
	"github.com/codevector-2003/filiation/internal/identity"
	"github.com/codevector-2003/filiation/internal/model"
	"github.com/codevector-2003/filiation/internal/store"
	"github.com/codevector-2003/filiation/internal/text"
)

// DefaultTitleSearches is how many title searches one import may spend.
// Each costs 10 of OpenAlex's 1,000 free daily credits (D11); finding an
// identifier in the PDF costs nothing.
const DefaultTitleSearches = 20

// shortQuery is how many of a title's first words a second search uses.
const shortQuery = 6

// Import outcomes, as ImportedFile.Outcome reports them. Only Imported files
// a PDF; the rest say why not.
const (
	ImportImported      = store.ImportImported
	ImportDuplicate     = store.ImportDuplicate
	ImportUnconfirmed   = store.ImportUnconfirmed
	ImportCandidates    = store.ImportCandidates
	ImportUnidentified  = store.ImportUnidentified
	ImportNotInOpenAlex = store.ImportNotInOpenAlex
	ImportUnreadable    = store.ImportUnreadable
)

// ImportOptions shape one import.
type ImportOptions struct {
	// Files are the PDFs to import. Empty means everything in Inbox/ and any
	// PDF in papers/ that is not yet any paper's.
	Files []string
	// Again tries files an earlier import could not identify, unchanged
	// since. fil import does; fetch and extract, picking up Inbox/, do not.
	Again bool
	// TitleSearches is the most title searches to spend; zero means
	// DefaultTitleSearches, and a negative number none.
	TitleSearches int
	// AcceptFirst files a title search's best candidate without asking —
	// but only one whose title and first author are in the PDF.
	AcceptFirst bool
	// Progress, if set, is called after each file.
	Progress func(ImportedFile)
}

// Imported is what an import did.
type Imported struct {
	Files    []ImportedFile
	Counts   map[string]int // by outcome
	Searches int            // title searches spent
	Duration time.Duration
}

// ImportedFile is what happened to one PDF.
type ImportedFile struct {
	File    string // where it was, absolute
	Saved   string // where it is now, absolute, when imported
	Outcome string // Import*
	Reason  string // why, when it was not imported
	Work    model.Work
	Clue    string // where its identifier was found, when one was used
	Skipped bool   // an earlier import found nothing, and the file has not changed

	// Candidates are the works the user may choose from, when Outcome is
	// unconfirmed or candidates. Verified ones have their title and first
	// author in the PDF.
	Candidates []ImportCandidate
	Text       *ExtractedPaper // reading the PDF, once filed
}

// ImportCandidate is a work a PDF might be.
type ImportCandidate struct {
	Work     model.Work
	Verified bool // its title and first author are in the PDF
	hydrated model.HydratedWork
}

// Import identifies and files PDFs the researcher already has (ADR-014).
//
// For each PDF it collects clues — the arXiv stamp, the file name, a DOI in
// the page header, the PDF's metadata — and looks each up, free. A clue is
// trusted only if the work it names has its title and first author's
// surname in the PDF's first pages: then the paper is added as a seed you
// uploaded, the file goes into its folder in papers/, and its text is read.
// A PDF with no clue that checks out gets a title search, within the budget;
// a search never files anything on its own (ADR-005) — the candidates come
// back for you to choose from, unless AcceptFirst.
//
// Where a file goes depends on where it was: from Inbox/ it moves into the
// paper's folder; one already in papers/ stays where you put it; one from
// anywhere else is copied, and the original is never touched. Bytes the
// library already holds are reported, not stored twice.
func (l *Library) Import(ctx context.Context, opts ImportOptions) (Imported, error) {
	start := time.Now()
	res := Imported{Counts: map[string]int{}}
	if l.files == nil {
		return res, ErrNoFolder
	}
	searches := opts.TitleSearches
	if searches == 0 {
		searches = DefaultTitleSearches
	}
	scan, err := l.Scan(ctx)
	if err != nil {
		return res, err
	}
	files := opts.Files
	if len(files) == 0 {
		inbox, err := l.files.Inbox()
		if err != nil {
			return res, err
		}
		for _, e := range inbox {
			files = append(files, l.files.Abs(e.Path))
		}
		files = append(files, scan.Loose...)
	}

	for _, f := range files {
		if err := ctx.Err(); err != nil {
			res.Duration = time.Since(start)
			return res, err
		}
		p, spent, err := l.importFile(ctx, f, opts, searches-res.Searches)
		res.Searches += spent
		if err != nil {
			res.Duration = time.Since(start)
			return res, err
		}
		res.Files = append(res.Files, p)
		res.Counts[p.Outcome]++
		if opts.Progress != nil {
			opts.Progress(p)
		}
	}
	res.Duration = time.Since(start)
	return res, l.writeInboxReport(ctx)
}

// source is where a file being imported lives.
type source int

const (
	fromInbox   source = iota // Inbox/: fil files it
	fromPapers                // papers/: the researcher filed it; it stays
	fromOutside               // elsewhere: copied in, the original untouched
)

func (l *Library) sourceOf(abs string) (source, string) {
	rel, err := l.files.Rel(abs)
	if err != nil {
		return fromOutside, ""
	}
	switch {
	case strings.HasPrefix(rel, blobs.InboxDir+"/"):
		return fromInbox, rel
	case strings.HasPrefix(rel, blobs.PapersDir+"/"):
		return fromPapers, rel
	}
	return fromOutside, ""
}

// importFile identifies one PDF and files it when a clue checks out. It
// returns the title searches it spent. The error is for cancelling and for
// what stops the run — the network gone, the disk full — never for a file
// that cannot be identified.
func (l *Library) importFile(ctx context.Context, file string, opts ImportOptions, searches int) (ImportedFile, int, error) {
	abs, err := filepath.Abs(file)
	if err != nil {
		return ImportedFile{}, 0, err
	}
	out := ImportedFile{File: abs}
	data, err := os.ReadFile(abs)
	if err != nil {
		return ImportedFile{}, 0, fmt.Errorf("import %s: %w", abs, err)
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	attempt := store.ImportAttempt{SHA256: hash, Path: l.displayPath(abs)}

	// Bytes the library already holds.
	if dup, ok, err := l.duplicate(ctx, abs, hash, out); err != nil || ok {
		return dup, 0, err
	}
	// The library had these bytes once, and they were deleted: it is that
	// paper's PDF again, and the record is restored rather than asked about.
	if _, gone, err := l.sameBytes(ctx, abs, hash); err != nil {
		return out, 0, err
	} else if gone != nil {
		w, err := l.workWithAuthors(ctx, gone.FilID)
		if err != nil {
			return out, 0, err
		}
		out.Clue = "the same file the library had before"
		filed, err := l.fileWork(ctx, abs, data, hash, w, out)
		return filed, 0, err
	}

	// Not identified before, and unchanged since: say so without asking
	// OpenAlex again, unless told to.
	if prev, ok, err := l.db.ImportOf(ctx, hash); err != nil {
		return out, 0, err
	} else if ok && prev.Outcome != store.ImportImported && prev.Outcome != store.ImportDuplicate && !opts.Again {
		out.Outcome, out.Reason, out.Skipped = prev.Outcome, prev.Reason, true
		return out, 0, nil
	}

	fctx, cancel := context.WithTimeout(ctx, ExtractTimeout)
	front, err := text.Shared().Front(fctx, data)
	cancel()
	switch {
	case ctx.Err() != nil:
		return out, 0, ctx.Err()
	case errors.Is(err, text.ErrUnreadable), errors.Is(err, context.DeadlineExceeded):
		out.Outcome, out.Reason = store.ImportUnreadable, "PDFium could not read it: "+err.Error()
		return out, 0, l.recordImport(ctx, attempt, out)
	case err != nil:
		return out, 0, err
	}

	// Clues, looked up one by one, free. The first whose work is in the
	// text is the paper.
	var unconfirmed []ImportCandidate
	for _, c := range text.Clues(abs, front) {
		h, err := l.graph.Resolve(ctx, c.ID)
		switch {
		case errors.Is(err, errs.ErrUnresolved), errors.Is(err, errs.ErrNotFound), errors.Is(err, errs.ErrInvalidInput):
			continue
		case err != nil:
			if ctx.Err() != nil {
				return out, 0, ctx.Err()
			}
			return out, 0, err
		}
		cand := ImportCandidate{Work: h.Work, hydrated: h, Verified: verifies(front, h)}
		// An identifier from the PDF itself is strong evidence, so a close
		// match of the title will do — a preprint's title is often not the
		// published one (Resembles).
		if cand.Verified || resembles(front, h) {
			out.Clue = c.Where
			filed, err := l.fileAs(ctx, abs, data, hash, h, out)
			if err != nil {
				return out, 0, err
			}
			return filed, 0, nil
		}
		unconfirmed = append(unconfirmed, cand)
	}

	if !hasText(front) {
		out.Outcome = store.ImportUnidentified
		out.Reason = "no identifier and no text to check one against — a scan? Name it with fil import <file> --as <DOI>"
		out.Candidates = unconfirmed
		return out, 0, l.recordImport(ctx, attempt, out)
	}

	// A title search, within the budget.
	spent := 0
	query := titleQuery(front)
	if searches > 0 && query != "" {
		spent = 1
		attempt.Searched = true
		found, err := l.graph.SearchTitle(ctx, query, 5)
		if err == nil && len(found) == 0 && searches > 1 {
			// Nothing: a word broken by the PDF's spacing ("d evices") or a
			// symbol read as a letter ("2π" as "2 S") can sink the whole
			// query. Once more, on the first words alone.
			if words := strings.Fields(query); len(words) > shortQuery {
				query = strings.Join(words[:shortQuery], " ")
				spent++
				found, err = l.graph.SearchTitle(ctx, query, 5)
			}
		}
		switch {
		case err != nil && ctx.Err() != nil:
			return out, spent, ctx.Err()
		case refused(err):
			// OpenAlex refused this one query — something in the title read
			// off the PDF. The file waits; the rest of the import goes on.
			out.Outcome, out.Candidates = store.ImportUnidentified, unconfirmed
			out.Reason = fmt.Sprintf("no identifier, and OpenAlex refused a search for the title %q — name it with fil import <file> --as <DOI>", query)
			return out, spent, l.recordImport(ctx, attempt, out)
		case err != nil:
			return out, spent, err
		}
		var cands []ImportCandidate
		named := map[string]bool{}
		for _, c := range found {
			cands = append(cands, ImportCandidate{Work: c.Work.Work, hydrated: c.Work, Verified: verifies(front, c.Work)})
			named[c.Work.Work.OpenAlexID] = true
		}
		// A work the PDF's identifier named and the search found too is one
		// candidate, not two.
		unconfirmed = slices.DeleteFunc(unconfirmed, func(c ImportCandidate) bool { return named[c.Work.OpenAlexID] })
		// Verified candidates first; OpenAlex's order otherwise.
		sort.SliceStable(cands, func(i, j int) bool { return cands[i].Verified && !cands[j].Verified })
		if opts.AcceptFirst && len(cands) > 0 && cands[0].Verified {
			out.Clue = "a title search, accepted"
			filed, err := l.fileAs(ctx, abs, data, hash, cands[0].hydrated, out)
			return filed, spent, err
		}
		out.Candidates = append(cands, unconfirmed...)
		switch {
		case len(cands) > 0:
			out.Outcome = store.ImportCandidates
			out.Reason = fmt.Sprintf("no identifier checked out; a title search found %d candidate(s) to choose from", len(cands))
		case len(unconfirmed) > 0:
			out.Outcome = store.ImportUnconfirmed
			out.Reason = "its identifier names a work whose title is not in the PDF"
		default:
			out.Outcome = store.ImportNotInOpenAlex
			out.Reason = fmt.Sprintf("no identifier, and OpenAlex has nothing titled %q — fil import <file> --local keeps it as a local document", query)
		}
		return out, spent, l.recordImport(ctx, attempt, out)
	}

	out.Candidates = unconfirmed
	switch {
	case len(unconfirmed) > 0:
		out.Outcome = store.ImportUnconfirmed
		out.Reason = "its identifier names a work whose title is not in the PDF"
	case query == "":
		out.Outcome = store.ImportUnidentified
		out.Reason = "no identifier, and no title found on page 1"
	default:
		out.Outcome = store.ImportUnidentified
		out.Reason = "no identifier; a title search was not spent on it — run fil import"
	}
	return out, spent, l.recordImport(ctx, attempt, out)
}

// ImportAs files the PDF at file as the work input names — a DOI, arXiv
// ID, OpenAlex ID or PMID. It is still checked against the PDF: a work
// whose title is not in it is returned unconfirmed, with the work as its
// one candidate, unless force — the researcher has seen the mismatch and
// said yes. A PDF with no text cannot be checked, and is filed as named.
func (l *Library) ImportAs(ctx context.Context, file, input string, force bool) (ImportedFile, error) {
	if l.files == nil {
		return ImportedFile{}, ErrNoFolder
	}
	id, err := identity.Parse(input)
	if err != nil {
		return ImportedFile{}, err
	}
	if !id.Deterministic() {
		return ImportedFile{}, fmt.Errorf("%q is a title; give a DOI, arXiv ID, OpenAlex ID or PMID: %w", input, errs.ErrInvalidInput)
	}
	abs, data, hash, err := l.readImport(file)
	if err != nil {
		return ImportedFile{}, err
	}
	out := ImportedFile{File: abs, Clue: "named by you"}
	if _, err := l.Scan(ctx); err != nil {
		return out, err
	}
	h, err := l.graph.Resolve(ctx, id)
	if err != nil {
		return out, err
	}
	fctx, cancel := context.WithTimeout(ctx, ExtractTimeout)
	front, ferr := text.Shared().Front(fctx, data)
	cancel()
	if ctx.Err() != nil {
		return out, ctx.Err()
	}
	if !force && ferr == nil && hasText(front) && !verifies(front, h) && !resembles(front, h) {
		out.Outcome = store.ImportUnconfirmed
		out.Reason = "the title of " + id.Value + " is not in the PDF"
		out.Candidates = []ImportCandidate{{Work: h.Work, hydrated: h}}
		return out, l.recordImport(ctx, store.ImportAttempt{SHA256: hash, Path: l.displayPath(abs)}, out)
	}
	return l.fileAs(ctx, abs, data, hash, h, out)
}

// ImportChosen files the PDF at file as a candidate the researcher chose
// from an earlier import's answer. No further check: they chose.
func (l *Library) ImportChosen(ctx context.Context, file string, c ImportCandidate) (ImportedFile, error) {
	if c.hydrated.OpenAlexID == "" {
		return ImportedFile{}, errors.New("library: candidate did not come from Import")
	}
	abs, data, hash, err := l.readImport(file)
	if err != nil {
		return ImportedFile{}, err
	}
	return l.fileAs(ctx, abs, data, hash, c.hydrated, ImportedFile{File: abs, Clue: "chosen by you"})
}

// ImportLocal files the PDF at file as a local document: a paper OpenAlex
// does not know, with the title the researcher gives it, a fil ID, its text
// searchable, and no citations (ADR-014).
func (l *Library) ImportLocal(ctx context.Context, file, title string, year *int) (ImportedFile, error) {
	if l.files == nil {
		return ImportedFile{}, ErrNoFolder
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return ImportedFile{}, fmt.Errorf("a local document needs a title: %w", errs.ErrInvalidInput)
	}
	abs, data, hash, err := l.readImport(file)
	if err != nil {
		return ImportedFile{}, err
	}
	if dup, ok, err := l.duplicate(ctx, abs, hash, ImportedFile{File: abs}); err != nil || ok {
		return dup, err
	}
	var fil string
	if err := l.db.Tx(ctx, func(tx *store.Tx) error {
		var err error
		fil, err = tx.AddLocalWork(ctx, title, year)
		return err
	}); err != nil {
		return ImportedFile{}, err
	}
	w, err := l.workWithAuthors(ctx, fil)
	if err != nil {
		return ImportedFile{}, err
	}
	return l.fileWork(ctx, abs, data, hash, w, ImportedFile{File: abs, Clue: "a local document"})
}

func (l *Library) readImport(file string) (string, []byte, string, error) {
	abs, err := filepath.Abs(file)
	if err != nil {
		return "", nil, "", err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", nil, "", fmt.Errorf("import %s: %w", abs, err)
	}
	sum := sha256.Sum256(data)
	return abs, data, hex.EncodeToString(sum[:]), nil
}

// sameBytes finds the library's records of these bytes, other than abs
// itself: one still on disk, and one the researcher deleted.
func (l *Library) sameBytes(ctx context.Context, abs, hash string) (present, gone *store.File, err error) {
	same, err := l.db.FilesWithHash(ctx, hash)
	if err != nil {
		return nil, nil, err
	}
	src, rel := l.sourceOf(abs)
	for i := range same {
		f := &same[i]
		switch {
		case src == fromPapers && f.Path == rel:
			// This very file, already recorded.
		case f.Missing:
			if gone == nil {
				gone = f
			}
		case present == nil:
			present = f
		}
	}
	return present, gone, nil
}

// duplicate reports, and records, a file whose bytes the library already
// holds on disk. It is checked before anything is created for the file:
// a duplicate must not leave a new paper behind with no PDF.
func (l *Library) duplicate(ctx context.Context, abs, hash string, out ImportedFile) (ImportedFile, bool, error) {
	present, _, err := l.sameBytes(ctx, abs, hash)
	if err != nil || present == nil {
		return out, false, err
	}
	w, err := l.workWithAuthors(ctx, present.FilID)
	if err != nil {
		return out, false, err
	}
	out.Outcome, out.Work = store.ImportDuplicate, w
	out.Reason = "the library already has this file, for " + w.FilID + " (" + l.files.Abs(present.Path) + ")"
	return out, true, l.recordImport(ctx, store.ImportAttempt{SHA256: hash, Path: l.displayPath(abs)}, out)
}

// fileAs adds h as a seed the researcher uploaded, then files the PDF under
// it.
func (l *Library) fileAs(ctx context.Context, abs string, data []byte, hash string, h model.HydratedWork, out ImportedFile) (ImportedFile, error) {
	if dup, ok, err := l.duplicate(ctx, abs, hash, out); err != nil || ok {
		return dup, err
	}
	h.Source = model.SourceUpload
	s, err := l.graph.AddSeedWork(ctx, h)
	if err != nil {
		return out, err
	}
	w, err := l.workWithAuthors(ctx, s.Work.FilID)
	if err != nil {
		return out, err
	}
	return l.fileWork(ctx, abs, data, hash, w, out)
}

// fileWork puts the PDF where it belongs for w, records it, and reads it.
func (l *Library) fileWork(ctx context.Context, abs string, data []byte, hash string, w model.Work, out ImportedFile) (ImportedFile, error) {
	out.Work = w
	src, rel := l.sourceOf(abs)

	// As in keep: checking the hash and recording the file are one step.
	l.keepMu.Lock()
	saved, err := func() (string, error) {
		defer l.keepMu.Unlock()
		present, gone, err := l.sameBytes(ctx, abs, hash)
		if err != nil {
			return "", err
		}
		if present != nil {
			out.Outcome = store.ImportDuplicate
			out.Reason = "the library already has this file (" + l.files.Abs(present.Path) + ")"
			return "", nil
		}
		to := rel
		if src != fromPapers {
			folder, err := l.ensureFolder(ctx, w)
			if err != nil {
				return "", err
			}
			dir, err := l.files.Rel(folder.Path)
			if err != nil {
				return "", err
			}
			if src == fromInbox {
				if to, err = l.files.MoveInto(rel, dir); err != nil {
					return "", err
				}
			} else {
				st, err := l.files.Stage(bytes.NewReader(data), blobs.WriteOptions{PDF: true, SHA256: hash})
				if err != nil {
					return "", err
				}
				base := filepath.Base(abs)
				ext := filepath.Ext(base)
				var f blobs.File
				for n := 1; ; n++ {
					name := base
					if n > 1 {
						name = fmt.Sprintf("%s-%d%s", strings.TrimSuffix(base, ext), n, ext)
					}
					f, err = st.Commit(dir + "/" + name)
					if !errors.Is(err, os.ErrExist) || n == 20 {
						break
					}
				}
				if err != nil {
					st.Discard()
					return "", err
				}
				to = f.Path
			}
		}
		var mtime int64
		if info, err := os.Stat(l.files.Abs(to)); err == nil {
			mtime = info.ModTime().Unix()
		}
		file := store.File{Path: to, FilID: w.FilID, SHA256: hash, Size: int64(len(data)),
			ModTime: mtime, Origin: store.OriginImport}
		err = l.db.Tx(ctx, func(tx *store.Tx) error {
			if gone != nil {
				// The deleted copy's record, restored at the new path.
				file.Version, file.Origin = gone.Version, gone.Origin
				return tx.MoveFile(ctx, gone.Path, file)
			}
			return tx.PutFile(ctx, file)
		})
		return to, err
	}()
	if err != nil {
		return out, err
	}
	attempt := store.ImportAttempt{SHA256: hash, Path: l.displayPath(abs)}
	if out.Outcome == store.ImportDuplicate {
		return out, l.recordImport(ctx, attempt, out)
	}
	out.Outcome, out.Saved = store.ImportImported, l.files.Abs(saved)
	if err := l.recordImport(ctx, attempt, out); err != nil {
		return out, err
	}
	if src == fromInbox {
		// The file has left Inbox/; its line in the report goes with it.
		if err := l.writeInboxReport(ctx); err != nil {
			return out, err
		}
	}
	t, err := l.extractWork(ctx, w.FilID, false)
	if err != nil && !errors.Is(err, ErrNoPDF) {
		return out, err
	}
	if err == nil {
		out.Text = &t
	}
	return out, nil
}

func (l *Library) recordImport(ctx context.Context, a store.ImportAttempt, f ImportedFile) error {
	a.Outcome, a.Reason = f.Outcome, f.Reason
	if f.Outcome == store.ImportImported {
		a.FilID = f.Work.FilID
	}
	return l.db.Tx(context.WithoutCancel(ctx), func(tx *store.Tx) error { return tx.RecordImport(ctx, a) })
}

// displayPath is a file's path relative to the library folder when it is
// inside it, absolute otherwise.
func (l *Library) displayPath(abs string) string {
	if rel, err := l.files.Rel(abs); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return abs
}

// refused reports OpenAlex refusing one query as malformed — 400, 414 or 422
// — which is about the title read off this PDF. Anything else, a 401 or 403
// refusing access, a rate limit, the network or the service failing, is
// about every request and stops the run.
func refused(err error) bool {
	var se *httpx.StatusError
	return errors.As(err, &se) && (se.StatusCode == 400 || se.StatusCode == 414 || se.StatusCode == 422)
}

// verifies reports whether the PDF's first pages carry h's title and its
// first author's surname — what makes a clue trustworthy.
func verifies(f text.Front, h model.HydratedWork) bool {
	return h.Title != nil && text.Mentions(f.Pages, *h.Title, firstSurname(h))
}

// resembles is verifies' looser form, for a work an identifier in the PDF
// named: most of its title near the top of page 1, its first author's
// surname in the text.
func resembles(f text.Front, h model.HydratedWork) bool {
	return h.Title != nil && text.Resembles(f.Pages, *h.Title, firstSurname(h))
}

// firstSurname is the last word of the first author's name, or "".
func firstSurname(h model.HydratedWork) string {
	if len(h.Authors) == 0 {
		return ""
	}
	fields := strings.Fields(h.Authors[0].Name)
	if len(fields) == 0 {
		return ""
	}
	return fields[len(fields)-1]
}

// hasText reports whether a PDF's front has text enough to check a work
// against.
func hasText(f text.Front) bool {
	n := 0
	for _, p := range f.Pages {
		n += len(strings.Fields(p))
	}
	return n >= 20
}

// titleQuery is what to search for when no identifier checks out: the
// largest text on page 1, or the metadata's title when that reads like one.
func titleQuery(f text.Front) string {
	if len(strings.Fields(f.Title)) >= 3 {
		return f.Title
	}
	if t := f.Meta["Title"]; len(strings.Fields(t)) >= 4 && !strings.ContainsAny(t, "_\\/") {
		return t
	}
	return ""
}

// InboxWaiting counts the PDFs in Inbox/: dropped there and not filed yet.
func (l *Library) InboxWaiting() (int, error) {
	if l.files == nil {
		return 0, nil
	}
	entries, err := l.files.Inbox()
	return len(entries), err
}

// importInbox files what in Inbox/ identifies itself for free — an
// identifier that checks out — before PDFs are read: fetch and extract call
// it, so dropping a PDF in Inbox/ is enough. No title search is spent and
// nothing is asked; the rest wait for fil import, with the reason in
// Inbox/fil-report.txt. Only cancelling stops the reading that follows.
func (l *Library) importInbox(ctx context.Context) (Imported, error) {
	entries, err := l.files.Inbox()
	if err != nil || len(entries) == 0 {
		return Imported{Counts: map[string]int{}}, nil
	}
	files := make([]string, len(entries))
	for i, e := range entries {
		files[i] = l.files.Abs(e.Path)
	}
	res, err := l.Import(ctx, ImportOptions{Files: files, TitleSearches: -1})
	if ctx.Err() != nil {
		return res, ctx.Err()
	}
	_ = err // a lookup that failed leaves the file in Inbox/ for next time
	return res, nil
}

// writeInboxReport rewrites Inbox/fil-report.txt to say why each PDF still
// in Inbox/ is there, or removes it when Inbox/ holds none.
func (l *Library) writeInboxReport(ctx context.Context) error {
	entries, err := l.files.Inbox()
	if err != nil {
		return err
	}
	var b strings.Builder
	for _, e := range entries {
		data, err := os.ReadFile(l.files.Abs(e.Path))
		if err != nil {
			continue
		}
		sum := sha256.Sum256(data)
		a, ok, err := l.db.ImportOf(ctx, hex.EncodeToString(sum[:]))
		if err != nil {
			return err
		}
		why := "not looked at yet — run fil import"
		if ok {
			why = a.Reason
		}
		fmt.Fprintf(&b, "%s\n  %s\n\n", strings.TrimPrefix(e.Path, blobs.InboxDir+"/"), why)
	}
	if b.Len() == 0 {
		return l.files.WriteReport("")
	}
	return l.files.WriteReport("fil could not file these PDFs yet. Run fil import to choose between\n" +
		"candidates, or name one with fil import <file> --as <DOI>.\n\n" + b.String())
}
