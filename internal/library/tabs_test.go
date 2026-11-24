package library

import (
	"errors"
	"testing"

	"github.com/codevector-2003/filiation/internal/errs"
	"github.com/codevector-2003/filiation/internal/store"
)

func TestReaderTabs(t *testing.T) {
	t.Parallel()
	l, ids, _ := viewsTest(t)
	ctx := t.Context()

	none, err := l.ReaderTabs(ctx)
	if err != nil || len(none.Tabs) != 0 || none.Active != "" || none.Tabs == nil {
		t.Fatalf("before any were kept = %+v, %v; want no tabs, as an empty list", none, err)
	}

	// The seed has no PDF, so a tab on it has nothing to show.
	seed, err := l.Add(ctx, "10.7717/peerj.4375", AddOptions{})
	if err != nil {
		t.Fatal(err)
	}
	err = l.SaveReaderTabs(ctx, ReaderTabs{
		Tabs: []ReaderTab{
			{ID: ids[0], Page: 4, Zoom: 1.3, Side: "cited", Find: "  hedgerow  "},
			{ID: ids[1], Page: 0, Zoom: 99, Side: "elsewhere"},
			{ID: seed.Work.FilID, Page: 2},
			{ID: ids[0], Page: 9}, // the same paper twice keeps the first
		},
		Active: ids[1],
		Screen: "search",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := l.ReaderTabs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []ReaderTab{
		{ID: ids[0], Page: 4, Zoom: 1.3, Side: "cited", Find: "hedgerow"},
		{ID: ids[1], Page: 1},
	}
	if len(got.Tabs) != len(want) || got.Tabs[0] != want[0] || got.Tabs[1] != want[1] {
		t.Fatalf("tabs = %+v, want %+v", got.Tabs, want)
	}
	if got.Active != ids[1] || got.Screen != "search" {
		t.Errorf("active %q on screen %q, want %q on search", got.Active, got.Screen, ids[1])
	}

	// A paper deleted since is left out, and the Library tab comes to the front.
	if _, err := l.DeletePapers(ctx, []string{ids[1]}); err != nil {
		t.Fatal(err)
	}
	got, err = l.ReaderTabs(ctx)
	if err != nil || len(got.Tabs) != 1 || got.Tabs[0].ID != ids[0] || got.Active != "" {
		t.Fatalf("after deleting the paper in front = %+v, %v", got, err)
	}
}

func TestSaveReaderTabsRefuses(t *testing.T) {
	t.Parallel()
	l, ids, _ := viewsTest(t)
	ctx := t.Context()

	if err := l.SaveReaderTabs(ctx, ReaderTabs{Tabs: []ReaderTab{{ID: "../papers", Page: 1}}}); !errors.Is(err, errs.ErrInvalidInput) {
		t.Errorf("a tab that names no paper: %v, want ErrInvalidInput", err)
	}
	many := make([]ReaderTab, MaxReaderTabs+1)
	for i := range many {
		many[i] = ReaderTab{ID: ids[0], Page: 1}
	}
	if err := l.SaveReaderTabs(ctx, ReaderTabs{Tabs: many}); !errors.Is(err, errs.ErrInvalidInput) {
		t.Errorf("%d tabs: %v, want ErrInvalidInput", len(many), err)
	}
}

func TestReaderTabsForgetWhatCannotBeRead(t *testing.T) {
	t.Parallel()
	l, _, _ := viewsTest(t)
	ctx := t.Context()
	if err := l.db.Tx(ctx, func(tx *store.Tx) error { return tx.SetMeta(ctx, metaReaderTabs, "{not json") }); err != nil {
		t.Fatal(err)
	}
	got, err := l.ReaderTabs(ctx)
	if err != nil || len(got.Tabs) != 0 {
		t.Fatalf("tabs kept as broken JSON = %+v, %v; want none and no error", got, err)
	}
}
