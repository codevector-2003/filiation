package store

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/codevector-2003/filiation/internal/errs"
	"github.com/codevector-2003/filiation/internal/model"
)

// migrated opens a library and applies the schema, which is the state every
// query in this package expects to meet.
func migrated(t *testing.T) *DB {
	t.Helper()
	db := openTest(t)
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return db
}

// seedWork is a fully populated work, so a round-trip test has something to say
// about every column rather than only the ones a stub carries.
func seedWork(id string) *model.Work {
	return &model.Work{
		OpenAlexID:   id,
		DOI:          model.Ptr("10.1145/3292500"),
		ArXivID:      model.Ptr("1706.03762"),
		PMID:         model.Ptr("29051481"),
		Title:        model.Ptr("Attention Is All You Need"),
		Abstract:     "The dominant sequence transduction models...",
		Year:         model.Ptr(2017),
		Venue:        "NeurIPS",
		Type:         model.TypeArticle,
		CitedByCount: 94000,
		OAStatus:     model.OAGreen,
		OAURL:        model.Ptr("https://arxiv.org/pdf/1706.03762"),
		Depth:        model.Ptr(0),
		IsSeed:       true,
		Source:       model.SourceSeed,
	}
}

func TestTxCommits(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()

	err := db.Tx(ctx, func(tx *Tx) error {
		return tx.UpsertWork(ctx, seedWork("W1"))
	})
	if err != nil {
		t.Fatalf("Tx: %v", err)
	}

	if _, err := db.GetWork(ctx, "W1"); err != nil {
		t.Fatalf("GetWork after commit: %v", err)
	}
}

func TestTxRollsBackOnError(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()

	sentinel := errors.New("the caller changed its mind")
	err := db.Tx(ctx, func(tx *Tx) error {
		if err := tx.UpsertWork(ctx, seedWork("W1")); err != nil {
			return err
		}
		return sentinel
	})
	// Returned unwrapped: callers branch on the sentinels in internal/errs, and
	// a prefix from this package would only bury the message.
	if !errors.Is(err, sentinel) {
		t.Fatalf("Tx error = %v, want the caller's own error", err)
	}

	if _, err := db.GetWork(ctx, "W1"); !errors.Is(err, errs.ErrNotFound) {
		t.Errorf("GetWork after rollback = %v, want ErrNotFound — the write survived a rollback", err)
	}
}

func TestTxRollsBackOnPanic(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()

	func() {
		defer func() {
			if recover() == nil {
				t.Errorf("panic did not propagate out of Tx")
			}
		}()
		_ = db.Tx(ctx, func(tx *Tx) error {
			if err := tx.UpsertWork(ctx, seedWork("W1")); err != nil {
				return err
			}
			panic("something went badly wrong")
		})
	}()

	if _, err := db.GetWork(ctx, "W1"); !errors.Is(err, errs.ErrNotFound) {
		t.Errorf("GetWork after panic = %v, want ErrNotFound", err)
	}
	// A leaked transaction would hold the write lock and make this hang or fail.
	if err := db.UpsertWork(ctx, seedWork("W2")); err != nil {
		t.Errorf("write after a panicking transaction: %v — the transaction leaked", err)
	}
}

func TestTxSeesItsOwnWrites(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()

	err := db.Tx(ctx, func(tx *Tx) error {
		if err := tx.UpsertWork(ctx, seedWork("W1")); err != nil {
			return err
		}
		// The hydration path reads back what it just wrote. Having to commit
		// first would make that a bug waiting to happen.
		w, err := tx.GetWork(ctx, "W1")
		if err != nil {
			return fmt.Errorf("read back inside the transaction: %w", err)
		}
		if w.IsStub() {
			return errors.New("work read back inside the transaction is still a stub")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// And a reader outside must not have seen it until the commit above.
	if _, err := db.GetWork(ctx, "W1"); err != nil {
		t.Fatalf("GetWork after commit: %v", err)
	}
}

func TestConcurrentWritersQueueRatherThanFail(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()

	// The single-writer rule is hard rule 5, and this is the shape of code that
	// violates it in every other Go project: several goroutines, each convinced
	// it may write. Here they queue on a pool of one.
	const writers = 8
	var wg sync.WaitGroup
	failures := make(chan error, writers)
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := seedWork(fmt.Sprintf("W%d", 100+i))
			w.DOI = model.Ptr(fmt.Sprintf("10.0/%d", i))
			if err := db.UpsertWork(ctx, w); err != nil {
				failures <- err
			}
		}()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Errorf("concurrent write failed: %v", err)
	}

	s, err := db.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if s.Works != writers {
		t.Errorf("Stats().Works = %d, want %d", s.Works, writers)
	}
}
