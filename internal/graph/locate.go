package graph

import (
	"context"
	"time"

	"github.com/codevector-2003/filiation/internal/model"
	"github.com/codevector-2003/filiation/internal/sources/openalex"
	"github.com/codevector-2003/filiation/internal/store"
)

// LocateOptions bound one lookup of PDF links.
type LocateOptions struct {
	// Max is the most works to look up. One request covers 100 works at one
	// credit, so 1,000 works cost 10 credits.
	Max int

	// MaxAge is how old an answer may be before it is asked again: open
	// copies appear as repositories catch up, so a "no open copy" is not
	// forever.
	MaxAge time.Duration

	// Progress, if set, is called after each committed batch with the running
	// totals.
	Progress func(Located)
}

// Located is what a lookup found.
type Located struct {
	Checked  int                           // works looked up
	By       map[model.PDFAvailability]int // how many of them have each answer
	Requests int                           // batch requests, one credit each
}

// Locate looks up the open-access PDF links of works whose links are not
// known or are older than MaxAge — seeds first, then the most cited — and
// records an answer for every one (ADR-013). Works fetched from now on get
// their links while being hydrated; this is for the rest, and for stubs.
//
// Each batch of 100 is committed on its own, so cancelling keeps what was
// done. A work OpenAlex has no record of is recorded as such and marked
// unresolved, which also stops expansion asking about it again.
func (g *Graph) Locate(ctx context.Context, o LocateOptions) (Located, error) {
	res := Located{By: map[model.PDFAvailability]int{}}
	if o.Max <= 0 {
		return res, nil
	}
	ids, err := g.db.ToLocate(ctx, o.Max, o.MaxAge)
	if err != nil {
		return res, err
	}
	return g.LocateIDs(ctx, ids, o.Progress)
}

// LocateIDs looks up and records the PDF links of the works with these
// OpenAlex IDs, 100 to a request, committing each batch on its own.
func (g *Graph) LocateIDs(ctx context.Context, ids []string, progress func(Located)) (Located, error) {
	res := Located{By: map[model.PDFAvailability]int{}}
	var chunks [][]string
	for start := 0; start < len(ids); start += openalex.MaxBatch {
		chunks = append(chunks, ids[start:min(start+openalex.MaxBatch, len(ids))])
	}

	// Up to locateInFlight requests run at once, because OpenAlex is slow to
	// answer for locations (a second or more a batch), not because the rate
	// limit allows it: every request still waits on the client's shared
	// bucket. Results are committed in order, by this goroutine alone, so the
	// single-writer rule holds.
	fetchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	type answer struct {
		found openalex.Located
		err   error
	}
	answers := make([]chan answer, len(chunks))
	for i := range answers {
		answers[i] = make(chan answer, 1) // buffered: a fetch never waits on the committer
	}
	slots := make(chan struct{}, locateInFlight)
	go func() {
		for i, chunk := range chunks {
			select {
			case slots <- struct{}{}:
			case <-fetchCtx.Done():
				return
			}
			go func() {
				found, err := g.src.GetLocations(fetchCtx, chunk)
				answers[i] <- answer{found, err}
			}()
		}
	}()

	for i, chunk := range chunks {
		var a answer
		select {
		case a = <-answers[i]:
			<-slots
		case <-ctx.Done():
			return res, ctx.Err()
		}
		res.Requests++
		err := g.db.Tx(ctx, func(tx *store.Tx) error {
			for _, id := range chunk {
				if links, ok := a.found.Links[id]; ok {
					if err := tx.PutPDFLinks(ctx, id, links); err != nil {
						return err
					}
					res.Checked++
					res.By[links.Availability]++
				}
			}
			for _, id := range a.found.Missing {
				if err := tx.PutPDFLinks(ctx, id, model.PDFLinks{Availability: model.PDFNotInOpenAlex}); err != nil {
					return err
				}
				if err := tx.MarkUnresolved(ctx, id); err != nil {
					return err
				}
				res.Checked++
				res.By[model.PDFNotInOpenAlex]++
			}
			return nil
		})
		if err != nil {
			return res, err
		}
		if a.err != nil {
			return res, a.err
		}
		if progress != nil {
			progress(res.copy())
		}
	}
	return res, nil
}

// locateInFlight is how many lookups of links may wait on OpenAlex at once.
const locateInFlight = 3

func (l Located) copy() Located {
	by := make(map[model.PDFAvailability]int, len(l.By))
	for k, v := range l.By {
		by[k] = v
	}
	l.By = by
	return l
}
