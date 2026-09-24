package graph

import (
	"context"
	"fmt"
	"sync"

	"github.com/codevector-2003/filiation/internal/errs"
	"github.com/codevector-2003/filiation/internal/identity"
	"github.com/codevector-2003/filiation/internal/model"
	"github.com/codevector-2003/filiation/internal/sources/openalex"
)

// fakeSource is an index of works held in memory, so these tests are about
// what the graph does, not about OpenAlex — sources/openalex has its own tests
// against recorded responses. It answers a batch the way OpenAlex does:
// found works, merged records as aliases, and unknown IDs as missing.
type fakeSource struct {
	mu     sync.Mutex
	works  map[string]model.HydratedWork // by OpenAlex ID and by DOI
	merged map[string]string             // retired ID -> surviving ID

	resolved int
	batches  [][]string // the IDs of every GetWorksBatch call, in order

	// beforeBatch, if set, runs at the start of each batch call with its
	// 1-based number; a non-nil error fails the call. Tests use it to fail a
	// batch or cancel the run at a chosen point.
	beforeBatch func(call int) error
}

func newFakeSource() *fakeSource {
	return &fakeSource{works: map[string]model.HydratedWork{}, merged: map[string]string{}}
}

func (f *fakeSource) add(w model.HydratedWork) {
	f.works[w.OpenAlexID] = w
	if w.DOI != nil {
		f.works[*w.DOI] = w
	}
}

func (f *fakeSource) Resolve(_ context.Context, id identity.ID) (model.HydratedWork, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resolved++
	h, ok := f.works[id.Value]
	if !ok {
		return model.HydratedWork{}, fmt.Errorf("fake: %s: %w", id.Value, errs.ErrUnresolved)
	}
	return h, nil
}

func (f *fakeSource) SearchByTitle(context.Context, string, int) ([]openalex.Candidate, error) {
	return nil, nil
}

func (f *fakeSource) GetWorksBatch(ctx context.Context, ids []string) (openalex.Batch, error) {
	f.mu.Lock()
	f.batches = append(f.batches, append([]string(nil), ids...))
	call := len(f.batches)
	hook := f.beforeBatch
	f.mu.Unlock()

	if hook != nil {
		if err := hook(call); err != nil {
			return openalex.Batch{Aliases: map[string]string{}}, err
		}
	}
	if err := ctx.Err(); err != nil {
		return openalex.Batch{Aliases: map[string]string{}}, err
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	b := openalex.Batch{Aliases: map[string]string{}}
	seen := map[string]bool{}
	for _, id := range ids {
		target := id
		if to, ok := f.merged[id]; ok {
			b.Aliases[id] = to
			target = to
		}
		w, ok := f.works[target]
		if !ok {
			b.Missing = append(b.Missing, id)
			continue
		}
		if !seen[w.OpenAlexID] {
			seen[w.OpenAlexID] = true
			b.Works = append(b.Works, w)
		}
	}
	return b, nil
}

func (f *fakeSource) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.batches)
}

// universe builds n works, W1..Wn, where work i cites fanout others chosen by
// a fixed arithmetic rule. It is deterministic, dense enough to have shared
// references and cycles, and big enough that a budget binds.
func universe(n, fanout int) []model.HydratedWork {
	works := make([]model.HydratedWork, 0, n)
	for i := 1; i <= n; i++ {
		var refs []string
		seen := map[int]bool{i: true}
		for k := 0; len(refs) < fanout && k < 4*fanout; k++ {
			j := (i*7+k*13)%n + 1
			if !seen[j] {
				seen[j] = true
				refs = append(refs, fmt.Sprintf("W%d", j))
			}
		}
		works = append(works, hydrated(fmt.Sprintf("W%d", i), fmt.Sprintf("10.1000/%d", i), refs...))
	}
	return works
}
