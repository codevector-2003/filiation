package graph

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codevector-2003/filiation/internal/errs"
	"github.com/codevector-2003/filiation/internal/model"
)

func withPDF(h model.HydratedWork, url string) model.HydratedWork {
	h.PDF = &model.PDFLinks{Availability: model.PDFLink, Links: []model.Location{{PDFURL: url, Host: "arxiv.org"}}}
	return h
}

// A work hydrated with its locations knows its links at once, at no cost.
func TestHydrationRecordsPDFLinks(t *testing.T) {
	t.Parallel()
	g, db, _ := newGraph(t, withPDF(hydrated("W1", "10.1000/one", "W2"), "https://arxiv.org/pdf/1"))
	if _, err := g.AddSeed(t.Context(), id(t, "10.1000/one")); err != nil {
		t.Fatal(err)
	}
	got, _, err := db.PDFLinksOf(t.Context(), "W1")
	if err != nil || got.Availability != model.PDFLink || got.Links[0].PDFURL != "https://arxiv.org/pdf/1" {
		t.Errorf("seed links = %+v, %v", got, err)
	}
	// The stub it cites was not hydrated, so nothing is known about it yet.
	if got, _, _ := db.PDFLinksOf(t.Context(), "W2"); got.Availability != model.PDFUnchecked {
		t.Errorf("stub = %+v, want unchecked", got)
	}
}

// Every work looked up gets an answer — a link, or why not — seeds first, and
// a fresh answer is not asked for again.
func TestLocateAnswersEveryWork(t *testing.T) {
	t.Parallel()
	works := []model.HydratedWork{
		hydrated("W1", "10.1000/one", "W2", "W3", "W404"),
		withPDF(hydrated("W2", "10.1000/two"), "https://arxiv.org/pdf/2"),
		hydrated("W3", "10.1000/three"),
	}
	g, db, src := seeded(t, works, "10.1000/one")
	ctx := t.Context()

	var progress []int
	res, err := g.Locate(ctx, LocateOptions{Max: 10, MaxAge: 24 * time.Hour,
		Progress: func(l Located) { progress = append(progress, l.Checked) }})
	if err != nil {
		t.Fatal(err)
	}
	// The seed was hydrated without locations, so it is looked up too, with
	// its three stubs: W2 has a link, W1 and W3 none, W404 is unknown.
	if res.Checked != 4 || res.Requests != 1 || res.By[model.PDFLink] != 1 ||
		res.By[model.PDFNoOpenCopy] != 2 || res.By[model.PDFNotInOpenAlex] != 1 {
		t.Errorf("Locate = %+v", res)
	}
	if got := src.locates[0]; len(got) != 4 || got[0] != "W1" {
		t.Errorf("looked up %v; want the seed first", got)
	}
	if len(progress) != 1 || progress[0] != 4 {
		t.Errorf("progress = %v", progress)
	}
	if w, _ := db.GetWork(ctx, "W404"); !w.Unresolved {
		t.Error("a work OpenAlex does not know was not marked unresolved")
	}
	if l, _, _ := db.PDFLinksOf(ctx, "W2"); l.Availability != model.PDFLink {
		t.Errorf("W2 = %+v", l)
	}

	again, err := g.Locate(ctx, LocateOptions{Max: 10, MaxAge: 24 * time.Hour})
	if err != nil || again.Checked != 0 || again.Requests != 0 || len(src.locates) != 1 {
		t.Errorf("second Locate = %+v, %v; %d lookups in all, want 1", again, err, len(src.locates))
	}
	counts, _ := db.PDFCounts(ctx)
	if counts[model.PDFUnchecked] != 0 {
		t.Errorf("unchecked works left: %v", counts)
	}
}

// Max bounds the lookup, a batch is 100, and a failed batch keeps what the
// batches before it found.
func TestLocateBatchesAndStopsCleanly(t *testing.T) {
	t.Parallel()
	refs := make([]string, 250)
	works := []model.HydratedWork{}
	for i := range refs {
		refs[i] = "W" + strconv.Itoa(1000+i)
		works = append(works, hydrated(refs[i], "10.1000/"+strconv.Itoa(i)))
	}
	works = append(works, hydrated("W1", "10.1000/seed", refs...))
	g, _, src := seeded(t, works, "10.1000/seed")
	// The third batch fails, wherever it falls among lookups running at once.
	src.beforeLocate = func(ids []string) error {
		if len(ids) == 40 {
			return errs.ErrTransient
		}
		return nil
	}

	res, err := g.Locate(t.Context(), LocateOptions{Max: 240, MaxAge: time.Hour})
	if !errors.Is(err, errs.ErrTransient) {
		t.Fatalf("err = %v, want the third batch's failure", err)
	}
	if res.Checked != 200 || res.Requests != 3 {
		t.Errorf("Locate = %+v; want two batches of 100 kept", res)
	}
	sizes := map[int]int{}
	for _, l := range src.locates {
		sizes[len(l)]++
	}
	if sizes[100] != 2 || sizes[40] != 1 {
		t.Errorf("batch sizes = %v, want two of 100 and one of 40", sizes)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := g.Locate(ctx, LocateOptions{Max: 240, MaxAge: time.Hour}); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled Locate = %v", err)
	}
}

// Lookups overlap — the first is held until the third has started — and the
// answers are still committed in order.
func TestLocateOverlapsLookupsAndCommitsInOrder(t *testing.T) {
	t.Parallel()
	refs := make([]string, 299)
	works := []model.HydratedWork{}
	for i := range refs {
		refs[i] = "W" + strconv.Itoa(1000+i)
		works = append(works, hydrated(refs[i], "10.1000/"+strconv.Itoa(i)))
	}
	works = append(works, hydrated("W1", "10.1000/seed", refs...))
	g, _, src := seeded(t, works, "10.1000/seed")

	var mu sync.Mutex
	started := 0
	third := make(chan struct{})
	src.beforeLocate = func(ids []string) error {
		mu.Lock()
		started++
		if started == 3 {
			close(third)
		}
		mu.Unlock()
		if ids[0] == "W1" { // the first batch: the seed comes first
			select {
			case <-third:
			case <-time.After(5 * time.Second):
				return errors.New("the third lookup never started alongside the first")
			}
		}
		return nil
	}
	var order []int
	res, err := g.Locate(t.Context(), LocateOptions{Max: 1000, MaxAge: time.Hour,
		Progress: func(l Located) { order = append(order, l.Checked) }})
	if err != nil {
		t.Fatal(err)
	}
	if res.Checked != 300 || strings.Join(strings.Fields(fmt.Sprint(order)), ",") != "[100,200,300]" {
		t.Errorf("checked %d, progress %v; want 300 in order 100, 200, 300", res.Checked, order)
	}
}
