package library

import (
	"errors"
	"maps"
	"os"
	"testing"

	"github.com/codevector-2003/filiation/internal/errs"
	"github.com/codevector-2003/filiation/internal/model"
	"github.com/codevector-2003/filiation/internal/text/pdftest"
)

func TestDeletePapers(t *testing.T) {
	t.Parallel()
	l, _ := openImportTest(t, maps.Clone(peerj))
	ctx := t.Context()
	file := putInInbox(t, l, "notes.pdf", pdftest.Make("Field notes on solitary bees, kept over three summers."))
	f, err := l.ImportLocal(ctx, file, "Field notes on solitary bees", model.Ptr(2025))
	if err != nil {
		t.Fatal(err)
	}
	id := f.Work.FilID

	plan, err := l.DeletePlan(ctx, []string{id, id})
	if err != nil || plan.Papers != 1 || plan.PDFs != 1 {
		t.Fatalf("plan = %+v, %v; want one paper with one PDF, counted once", plan, err)
	}
	got, err := l.DeletePapers(ctx, []string{id})
	if err != nil || got.Papers != 1 || len(got.Folders) != 1 {
		t.Fatalf("DeletePapers = %+v, %v", got, err)
	}
	// The library forgets the paper; its folder is left for the front door to
	// move to the bin, so it is still on disk here.
	if _, err := l.Find(ctx, id); !errors.Is(err, errs.ErrNotFound) {
		t.Errorf("Find after delete = %v; want not found", err)
	}
	if _, err := os.Stat(got.Folders[0]); err != nil {
		t.Errorf("the folder is gone before it was moved to the bin: %v", err)
	}
	if found, _ := l.Search(ctx, "solitary bees", SearchOptions{}); len(found.Hits) != 0 {
		t.Errorf("a deleted paper is still found by search: %+v", found.Hits)
	}
	if _, err := l.DeletePapers(ctx, []string{id}); !errors.Is(err, errs.ErrNotFound) {
		t.Errorf("deleting it again = %v; want not found", err)
	}
}
