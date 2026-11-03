package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/codevector-2003/filiation/internal/errs"
	"github.com/codevector-2003/filiation/internal/identity"
	"github.com/codevector-2003/filiation/internal/model"
	"github.com/codevector-2003/filiation/internal/sources/zotero"
	"github.com/codevector-2003/filiation/internal/store"
)

// Why Zotero could not be read: compare with errors.Is.
var (
	ErrZoteroNotRunning = zotero.ErrNotRunning
	ErrZoteroNotAllowed = zotero.ErrNotAllowed
)

// ZoteroCollection is one Zotero collection and how many items are in it.
type ZoteroCollection struct {
	Key    string
	Name   string
	Parent string // "" at the top
	Items  int    // directly in it
}

// ZoteroPreview is what bringing in the Zotero library would do, before
// anything is written.
type ZoteroPreview struct {
	Items       int
	WithDOI     int
	WithOtherID int // an arXiv or PubMed ID, and no DOI
	NoID        int // kept as local documents
	PDFs        int // items with a PDF attached
	Tags        int // tags the user added, as distinct names
	Already     int // brought in before
	Collections []ZoteroCollection
}

// ZoteroPreview reads the Zotero library on this computer and counts what
// bringing it in would do. It writes nothing.
func (l *Library) ZoteroPreview(ctx context.Context) (ZoteroPreview, error) {
	cols, items, err := l.readZotero(ctx)
	if err != nil {
		return ZoteroPreview{}, err
	}
	known, err := l.db.ZoteroItems(ctx)
	if err != nil {
		return ZoteroPreview{}, err
	}
	out := ZoteroPreview{Items: len(items)}
	in := map[string]int{}
	tags := map[string]bool{}
	for _, it := range items {
		switch {
		case it.DOI != "":
			out.WithDOI++
		case it.ArXiv != "" || it.PMID != "":
			out.WithOtherID++
		default:
			out.NoID++
		}
		if len(it.PDFs) > 0 {
			out.PDFs++
		}
		if _, ok := known[it.Key]; ok {
			out.Already++
		}
		for _, c := range it.Collections {
			in[c]++
		}
		for _, t := range it.Tags {
			tags[t] = true
		}
	}
	out.Tags = len(tags)
	for _, c := range cols {
		out.Collections = append(out.Collections, ZoteroCollection{Key: c.Key, Name: c.Name, Parent: c.Parent, Items: in[c.Key]})
	}
	return out, nil
}

func (l *Library) readZotero(ctx context.Context) ([]zotero.Collection, []zotero.Item, error) {
	cols, err := l.zotero.Collections(ctx)
	if err != nil {
		return nil, nil, err
	}
	items, err := l.zotero.Items(ctx)
	if err != nil {
		return nil, nil, err
	}
	return treeOrder(cols), items, nil
}

// treeOrder puts each collection after the one it sits in.
func treeOrder(cols []zotero.Collection) []zotero.Collection {
	kids := map[string][]zotero.Collection{}
	keys := map[string]bool{}
	for _, c := range cols {
		keys[c.Key] = true
	}
	for _, c := range cols {
		if !keys[c.Parent] {
			c.Parent = "" // its parent is not there to read: it goes at the top
		}
		kids[c.Parent] = append(kids[c.Parent], c)
	}
	var out []zotero.Collection
	var walk func(string)
	walk = func(parent string) {
		for _, c := range kids[parent] {
			out = append(out, c)
			walk(c.Key)
		}
	}
	walk("")
	return out
}

// ZoteroOptions choose what bringing in Zotero does.
type ZoteroOptions struct {
	Skip []string // Zotero collections to leave out, by key; the ones inside them go too
	Refs bool     // record what each matched paper cites, as fil add does
	PDFs bool     // copy the PDFs into papers/ and read them
	Tags bool     // make labels from the tags the user added

	// Progress, when set, hears how far it has got.
	Progress func(done, total int, note string)
}

// ZoteroReport is what bringing in Zotero did.
type ZoteroReport struct {
	Items       int // read from Zotero, in the collections chosen
	Matched     int // found in OpenAlex and added as papers
	Unmatched   int // kept as local documents
	Already     int // brought in before; only their collections were brought up to date
	PDFs        int // copied into papers/
	PDFsMissing int // listed in Zotero, but not on this computer
	Collections int
	NotMatched  []ZoteroUnmatched
}

// ZoteroUnmatched is an item kept as a local document, and why.
type ZoteroUnmatched struct {
	FilID  string
	Title  string
	By     string
	Type   string // Zotero's item type
	Reason string
}

// labelColoursUsed picks the next colour for a label made from a tag.
func nextColour(n int) string { return LabelColours[n%len(LabelColours)] }

// ZoteroImport brings the Zotero library on this computer in: its items as
// papers, its collections, and, as opts says, PDFs and tags. Zotero is only
// read. An item with a DOI, arXiv ID or PubMed ID is looked up in OpenAlex;
// one without, or that OpenAlex does not know, becomes a local document. A
// title is never searched: that costs credits, and is the user's to ask for.
//
// Running it again adds what is new. Stopping keeps what is in so far.
func (l *Library) ZoteroImport(ctx context.Context, opts ZoteroOptions) (ZoteroReport, error) {
	progress := opts.Progress
	if progress == nil {
		progress = func(int, int, string) {}
	}
	cols, items, err := l.readZotero(ctx)
	if err != nil {
		return ZoteroReport{}, err
	}
	known, err := l.db.ZoteroItems(ctx)
	if err != nil {
		return ZoteroReport{}, err
	}

	// The collections, parents first. A skipped one takes those inside it.
	skip := map[string]bool{}
	for _, k := range opts.Skip {
		skip[k] = true
	}
	ids := map[string]int64{}
	var out ZoteroReport
	err = l.db.Tx(ctx, func(tx *store.Tx) error {
		for _, c := range cols {
			if skip[c.Key] || skip[c.Parent] {
				skip[c.Key] = true
				continue
			}
			name := strings.TrimSpace(c.Name)
			if name == "" {
				name = "Untitled collection"
			}
			id, err := tx.ZoteroCollection(ctx, c.Key, name, ids[c.Parent])
			if err != nil {
				return err
			}
			ids[c.Key] = id
			out.Collections++
		}
		return nil
	})
	if err != nil {
		return out, err
	}

	var chosen []zotero.Item
	for _, it := range items {
		keep := len(it.Collections) == 0
		for _, c := range it.Collections {
			if !skip[c] {
				keep = true
			}
		}
		if keep {
			chosen = append(chosen, it)
		}
	}
	out.Items = len(chosen)
	labels, err := l.Labels(ctx)
	if err != nil {
		return out, err
	}
	made := len(labels)

	for i, it := range chosen {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		progress(i, len(chosen), "Bringing in "+shorten(it.Title))
		fil, matched, err := l.zoteroWork(ctx, it, known, opts.Refs, &out)
		if err != nil {
			return out, err
		}
		err = l.db.Tx(ctx, func(tx *store.Tx) error {
			if err := tx.PutZoteroItem(ctx, store.ZoteroItem{Key: it.Key, FilID: fil, Version: it.Version, Matched: matched}); err != nil {
				return err
			}
			for _, c := range it.Collections {
				if id, ok := ids[c]; ok {
					if err := tx.FileInCollection(ctx, id, []string{fil}, true); err != nil {
						return err
					}
				}
			}
			if !opts.Tags {
				return nil
			}
			for _, t := range it.Tags {
				if len([]rune(t)) > nameMax {
					continue
				}
				id, err := tx.LabelNamed(ctx, t, nextColour(made))
				if err != nil {
					return err
				}
				made++
				if err := tx.SetLabel(ctx, id, []string{fil}, true); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return out, err
		}
		if opts.PDFs && l.files != nil {
			if err := l.zoteroPDFs(ctx, it, fil, &out); err != nil {
				return out, err
			}
		}
	}
	progress(len(chosen), len(chosen), "")
	return out, nil
}

func shorten(title string) string {
	if r := []rune(title); len(r) > 60 {
		return string(r[:57]) + "..."
	}
	return title
}

// zoteroWork finds or makes the work an item becomes.
func (l *Library) zoteroWork(ctx context.Context, it zotero.Item, known map[string]store.ZoteroItem, refs bool, out *ZoteroReport) (string, bool, error) {
	if k, ok := known[it.Key]; ok {
		if _, err := l.db.GetWork(ctx, k.FilID); err == nil {
			out.Already++
			return k.FilID, k.Matched, nil
		}
	}
	unmatched := func(reason string) (string, bool, error) {
		title := it.Title
		if title == "" {
			title = "Untitled Zotero item " + it.Key
		}
		var year *int
		if it.Year > 0 {
			year = model.Ptr(it.Year)
		}
		var fil string
		err := l.db.Tx(ctx, func(tx *store.Tx) error {
			var err error
			fil, err = tx.AddLocalWorkFrom(ctx, title, year, model.SourceZotero)
			return err
		})
		if err != nil {
			return "", false, err
		}
		out.Unmatched++
		by := ""
		if len(it.Creators) > 0 {
			by = it.Creators[0]
		}
		out.NotMatched = append(out.NotMatched, ZoteroUnmatched{FilID: fil, Title: title, By: by, Type: it.Type, Reason: reason})
		return fil, false, nil
	}

	input := ""
	switch {
	case it.DOI != "":
		input = it.DOI
	case it.ArXiv != "":
		input = "arXiv:" + it.ArXiv
	case it.PMID != "":
		input = "pmid:" + it.PMID
	default:
		return unmatched("no DOI, arXiv ID or PubMed ID in Zotero")
	}
	id, err := identity.Parse(input)
	if err != nil || !id.Deterministic() {
		return unmatched("Zotero's identifier " + input + " is not one fil can read")
	}
	h, err := l.graph.Resolve(ctx, id)
	if errors.Is(err, errs.ErrUnresolved) || errors.Is(err, errs.ErrNotFound) {
		return unmatched("OpenAlex has no record of " + input)
	}
	if err != nil {
		return "", false, err
	}
	h.Source = model.SourceZotero
	if !refs {
		h.FetchedRefs, h.ReferencedWorks = false, nil
	}
	s, err := l.graph.AddSeedWork(ctx, h)
	if err != nil {
		return "", false, err
	}
	out.Matched++
	return s.Work.FilID, true, nil
}

// zoteroPDFs copies an item's PDFs into papers/ and reads them. Zotero's own
// copies are never moved or changed.
func (l *Library) zoteroPDFs(ctx context.Context, it zotero.Item, fil string, out *ZoteroReport) error {
	for _, a := range it.PDFs {
		path, err := l.zotero.FilePath(ctx, a.Key)
		if err != nil {
			if errors.Is(err, zotero.ErrNotRunning) || errors.Is(err, zotero.ErrNotAllowed) || ctx.Err() != nil {
				return err
			}
			out.PDFsMissing++
			continue
		}
		if _, err := os.Stat(path); err != nil {
			out.PDFsMissing++
			continue
		}
		abs, data, hash, err := l.readImport(path)
		if err != nil {
			out.PDFsMissing++
			continue
		}
		w, err := l.workWithAuthors(ctx, fil)
		if err != nil {
			return err
		}
		f, err := l.fileWork(ctx, abs, data, hash, w, ImportedFile{File: abs, Clue: "from Zotero"})
		if err != nil {
			return fmt.Errorf("copy %s: %w", path, err)
		}
		if f.Outcome == store.ImportImported {
			out.PDFs++
		}
	}
	return nil
}
