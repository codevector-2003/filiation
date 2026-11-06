package library

import (
	"errors"
	"maps"
	"testing"

	"github.com/codevector-2003/filiation/internal/errs"
	"github.com/codevector-2003/filiation/internal/model"
	"github.com/codevector-2003/filiation/internal/text/pdftest"
)

// Three documents of the user's own, each mentioning hedgerows, two of them
// filed in a collection.
func viewsTest(t *testing.T) (*Library, []string, int64) {
	t.Helper()
	l, _ := openImportTest(t, maps.Clone(peerj))
	ctx := t.Context()
	var ids []string
	for i, title := range []string{"Bees in the meadow", "Hedgerows and their birds", "Moths at night"} {
		file := putInInbox(t, l, title+".pdf", pdftest.Make(title+"\n"+
			"Notes kept over three summers. The hedgerow was replanted along the eastern edge, and counts rose.\n"+
			"Each survey walked the same line at the same hour, in dry weather only."))
		f, err := l.ImportLocal(ctx, file, title, model.Ptr(2020+i))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, f.Work.FilID)
	}
	col, err := l.CreateCollection(ctx, "Fieldwork", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.FileInCollection(ctx, col, ids[:2], true); err != nil {
		t.Fatal(err)
	}
	return l, ids, col
}

func TestSearchKeepsToTheFilters(t *testing.T) {
	t.Parallel()
	l, ids, col := viewsTest(t)
	ctx := t.Context()

	all, err := l.Search(ctx, "hedgerow", SearchOptions{})
	if err != nil || len(all.Hits) != 3 {
		t.Fatalf("the whole library: %d hits, %v", len(all.Hits), err)
	}
	// The limit is applied after the filter: one passage, from the collection.
	in, err := l.Search(ctx, "hedgerow", SearchOptions{Limit: 1, Within: &ListOptions{CollectionID: col, YearFrom: 2021}})
	if err != nil {
		t.Fatal(err)
	}
	if len(in.Hits) != 1 || in.Hits[0].Work.FilID != ids[1] || in.Papers != 1 || in.Readable != 1 {
		t.Fatalf("within Fieldwork from 2021 = %+v", in)
	}
	none, err := l.Search(ctx, "hedgerow", SearchOptions{Within: &ListOptions{YearFrom: 3000}})
	if err != nil || len(none.Hits) != 0 || none.Papers != 0 {
		t.Fatalf("within a filter finding nothing = %+v, %v", none, err)
	}
}

func TestSavedViewsFindWhatMatchesNow(t *testing.T) {
	t.Parallel()
	l, ids, col := viewsTest(t)
	ctx := t.Context()

	if _, err := l.SaveView(ctx, "Everything", ViewFilters{}); !errors.Is(err, errs.ErrInvalidInput) {
		t.Errorf("a view of every paper = %v; want it refused", err)
	}
	v, err := l.SaveView(ctx, "Fieldwork, recent", ViewFilters{CollectionID: col, YearFrom: 2020, Sort: "newest"})
	if err != nil || v.Papers != 2 {
		t.Fatalf("SaveView = %+v, %v", v, err)
	}
	if _, err := l.SaveView(ctx, "fieldwork, RECENT", ViewFilters{Favourites: true}); !errors.Is(err, errs.ErrInvalidInput) {
		t.Errorf("a second view of the same name = %v", err)
	}

	// Filed later, a paper shows up in the view without saving it again.
	if err := l.FileInCollection(ctx, col, ids[2:], true); err != nil {
		t.Fatal(err)
	}
	now, err := l.ViewNamed(ctx, "Fieldwork, recent")
	if err != nil || now.Papers != 3 || now.Filters.Sort != "newest" {
		t.Fatalf("the view now = %+v, %v", now, err)
	}

	from := ViewFilters{CollectionID: col, YearFrom: 2022}
	if err := l.ChangeView(ctx, v.ID, "Fieldwork, 2022", &from); err != nil {
		t.Fatal(err)
	}
	views, err := l.Views(ctx)
	if err != nil || len(views) != 1 || views[0].Name != "Fieldwork, 2022" || views[0].Papers != 1 {
		t.Fatalf("after the change = %+v, %v", views, err)
	}
	if err := l.DeleteView(ctx, v.ID); err != nil {
		t.Fatal(err)
	}
	if views, _ := l.Views(ctx); len(views) != 0 {
		t.Fatalf("after deleting = %+v", views)
	}
	if rows, _ := l.List(ctx, ListOptions{CollectionID: col}); len(rows) != 3 {
		t.Errorf("deleting the view touched its papers: %d left in the collection", len(rows))
	}
}
