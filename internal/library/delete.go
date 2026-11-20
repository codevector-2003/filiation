package library

import (
	"context"
	"strings"

	"github.com/codevector-2003/filiation/internal/blobs"
	"github.com/codevector-2003/filiation/internal/store"
)

// DeletePlan is what deleting some papers takes with it, said before
// anything goes (D36).
type DeletePlan struct {
	Papers      int
	PDFs        int // PDF files of theirs on disk
	Notes       int // notes and highlights on them
	Collections int // collections they are filed in
	Labels      int // labels they carry
	Orphans     int // references only they cite, never fetched, removed too
}

// Deleted is what deleting papers did. Folders are their folders, absolute,
// still on disk: the library forgets them, and the caller moves them to the
// system's bin, after the library has changed so that a failure there can
// never leave the library half-deleted.
type Deleted struct {
	Papers  int
	Orphans int
	Folders []string
}

// DeletePlan counts what deleting these papers, by any identifier, would take.
func (l *Library) DeletePlan(ctx context.Context, papers []string) (DeletePlan, error) {
	ids, err := l.filIDs(ctx, papers)
	if err != nil {
		return DeletePlan{}, err
	}
	p, err := l.db.DeletePlan(ctx, ids)
	if err != nil {
		return DeletePlan{}, err
	}
	return DeletePlan{Papers: p.Papers, PDFs: p.PDFs, Notes: p.Notes, Collections: p.Collections, Labels: p.Labels, Orphans: len(p.Orphans)}, nil
}

// DeletePapers deletes these papers from the library, with the references
// only they cite, in one transaction, and remembers them so that following
// references does not bring them back; adding one again clears that.
func (l *Library) DeletePapers(ctx context.Context, papers []string) (Deleted, error) {
	ids, err := l.filIDs(ctx, papers)
	if err != nil {
		return Deleted{}, err
	}
	var p store.DeletePlan
	if err := l.db.Tx(ctx, func(tx *store.Tx) error {
		var err error
		p, err = tx.DeleteWorks(ctx, ids)
		return err
	}); err != nil {
		return Deleted{}, err
	}
	out := Deleted{Papers: p.Papers, Orphans: len(p.Orphans)}
	if l.files != nil {
		for _, f := range p.Folders {
			// Only a paper's own folder, strictly inside papers/, is ever
			// handed out to be moved to the bin.
			abs := l.files.Abs(f)
			rel, err := l.files.Rel(abs)
			if err != nil || !strings.HasPrefix(rel, blobs.PapersDir+"/") {
				continue
			}
			out.Folders = append(out.Folders, abs)
		}
	}
	return out, nil
}

// filIDs resolves papers named by any identifier to their fil IDs, once each.
func (l *Library) filIDs(ctx context.Context, papers []string) ([]string, error) {
	seen := map[string]bool{}
	var ids []string
	for _, p := range papers {
		w, err := l.Find(ctx, p)
		if err != nil {
			return nil, err
		}
		if !seen[w.FilID] {
			seen[w.FilID] = true
			ids = append(ids, w.FilID)
		}
	}
	return ids, nil
}
