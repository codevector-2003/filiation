package store

import (
	"errors"
	"testing"

	"github.com/codevector-2003/filiation/internal/errs"
	"github.com/codevector-2003/filiation/internal/model"
)

func TestUpsertWorkRoundTrip(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()

	want := seedWork("W2741809807")
	if err := db.UpsertWork(ctx, want); err != nil {
		t.Fatalf("UpsertWork: %v", err)
	}

	got, err := db.GetWork(ctx, "W2741809807")
	if err != nil {
		t.Fatalf("GetWork: %v", err)
	}

	tests := []struct {
		field string
		got   any
		want  any
	}{
		{"OpenAlexID", got.OpenAlexID, want.OpenAlexID},
		{"DOI", *got.DOI, *want.DOI},
		{"ArXivID", *got.ArXivID, *want.ArXivID},
		{"PMID", *got.PMID, *want.PMID},
		{"Title", *got.Title, *want.Title},
		{"Abstract", got.Abstract, want.Abstract},
		{"Year", *got.Year, *want.Year},
		{"Venue", got.Venue, want.Venue},
		{"Type", got.Type, want.Type},
		{"CitedByCount", got.CitedByCount, want.CitedByCount},
		{"OAStatus", got.OAStatus, want.OAStatus},
		{"OAURL", *got.OAURL, *want.OAURL},
		{"Depth", *got.Depth, *want.Depth},
		{"IsSeed", got.IsSeed, want.IsSeed},
		{"Source", got.Source, want.Source},
		{"Hydrated", got.Hydrated, true},
		{"FetchedRefs", got.FetchedRefs, false},
		{"Unresolved", got.Unresolved, false},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %v, want %v", tt.field, tt.got, tt.want)
		}
	}

	if got.PDFSHA256 != nil || got.PDFLicense != nil {
		t.Errorf("PDF columns = %v, %v; want nil — the metadata path knows nothing about them",
			got.PDFSHA256, got.PDFLicense)
	}
	if got.AddedAt.IsZero() {
		t.Errorf("AddedAt is zero, want the default datetime('now') parsed back")
	}
	if got.Authors != nil {
		t.Errorf("Authors = %v, want nil — this query did not ask for authors", got.Authors)
	}
}

func TestUpsertWorkStoresEmptyStringsAsNull(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()

	// Two spellings of absent in one column would make every WHERE clause choose
	// between them.
	w := &model.Work{OpenAlexID: "W1"}
	if err := db.UpsertWork(ctx, w); err != nil {
		t.Fatalf("UpsertWork: %v", err)
	}

	for _, col := range []string{"abstract", "venue", "type", "oa_status", "source"} {
		var nulls int
		q := `SELECT count(*) FROM work WHERE openalex_id = 'W1' AND ` + col + ` IS NULL;`
		if err := db.read.QueryRow(q).Scan(&nulls); err != nil {
			t.Fatalf("check %s: %v", col, err)
		}
		if nulls != 1 {
			t.Errorf("%s was stored as the empty string, want NULL", col)
		}
	}

	// And it reads back as the empty string, not as a failure to scan NULL.
	got, err := db.GetWork(ctx, "W1")
	if err != nil {
		t.Fatalf("GetWork: %v", err)
	}
	if got.Abstract != "" || got.Venue != "" || got.Type != model.TypeUnknown {
		t.Errorf("read back %+v, want the empty string in each", got)
	}
}

func TestUpsertWorkHydratesStubWithoutLosingProvenance(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()

	// The usual case: the work was already in the library as a stub, created the
	// moment some other paper was seen to cite it.
	err := db.Tx(ctx, func(tx *Tx) error {
		_, err := tx.UpsertStub(ctx, "W1", model.Ptr(2), model.SourceExpansion)
		return err
	})
	if err != nil {
		t.Fatalf("UpsertStub: %v", err)
	}

	// Hydration arrives carrying none of that provenance.
	h := seedWork("W1")
	h.Depth = nil
	h.IsSeed = false
	h.Source = model.SourceUnknown
	if err := db.UpsertWork(ctx, h); err != nil {
		t.Fatalf("UpsertWork: %v", err)
	}

	got, err := db.GetWork(ctx, "W1")
	if err != nil {
		t.Fatalf("GetWork: %v", err)
	}
	if got.IsStub() {
		t.Errorf("work is still a stub after hydration")
	}
	if got.Title == nil || *got.Title != "Attention Is All You Need" {
		t.Errorf("Title = %v, want the hydrated title", got.Title)
	}
	if model.Deref(got.Depth, -1) != 2 {
		t.Errorf("Depth = %v, want 2 kept from the stub", got.Depth)
	}
	if got.Source != model.SourceExpansion {
		t.Errorf("Source = %q, want %q — source records how the work first entered",
			got.Source, model.SourceExpansion)
	}
}

func TestUpsertWorkKeepsTheShorterDepth(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()

	cases := []struct {
		name             string
		stored, incoming *int
		want             *int
	}{
		{"shorter route wins", model.Ptr(4), model.Ptr(1), model.Ptr(1)},
		{"longer route does not move the work further away", model.Ptr(1), model.Ptr(4), model.Ptr(1)},
		{"no stored depth takes the incoming one", nil, model.Ptr(3), model.Ptr(3)},
		{"no incoming depth keeps the stored one", model.Ptr(3), nil, model.Ptr(3)},
		{"neither knows", nil, nil, nil},
	}
	for i, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			id := "W" + string(rune('1'+i))
			err := db.Tx(ctx, func(tx *Tx) error {
				_, err := tx.UpsertStub(ctx, id, tt.stored, model.SourceExpansion)
				return err
			})
			if err != nil {
				t.Fatalf("UpsertStub: %v", err)
			}

			w := seedWork(id)
			w.DOI = nil // the UNIQUE column; these works are not the same paper
			w.Depth = tt.incoming
			if err := db.UpsertWork(ctx, w); err != nil {
				t.Fatalf("UpsertWork: %v", err)
			}

			got, err := db.GetWork(ctx, id)
			if err != nil {
				t.Fatalf("GetWork: %v", err)
			}
			if model.Deref(got.Depth, -1) != model.Deref(tt.want, -1) {
				t.Errorf("Depth = %v, want %v", got.Depth, tt.want)
			}
		})
	}
}

func TestUpsertWorkIsIdempotent(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()

	// "Running it twice adds nothing the second time" is half of M0's definition
	// of done.
	for range 3 {
		if err := db.UpsertWork(ctx, seedWork("W1")); err != nil {
			t.Fatalf("UpsertWork: %v", err)
		}
	}
	s, err := db.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if s.Works != 1 {
		t.Errorf("Stats().Works = %d after three identical upserts, want 1", s.Works)
	}
}

func TestUpsertWorkClearsUnresolved(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()

	err := db.Tx(ctx, func(tx *Tx) error {
		if _, err := tx.UpsertStub(ctx, "W1", nil, model.SourceExpansion); err != nil {
			return err
		}
		return tx.MarkUnresolved(ctx, "W1")
	})
	if err != nil {
		t.Fatalf("mark unresolved: %v", err)
	}

	// Finding the work is the answer to "OpenAlex has no record of it".
	if err := db.UpsertWork(ctx, seedWork("W1")); err != nil {
		t.Fatalf("UpsertWork: %v", err)
	}
	got, err := db.GetWork(ctx, "W1")
	if err != nil {
		t.Fatalf("GetWork: %v", err)
	}
	if got.Unresolved {
		t.Errorf("Unresolved is still set after the work was hydrated")
	}
}

func TestUpsertStub(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()

	err := db.Tx(ctx, func(tx *Tx) error {
		created, err := tx.UpsertStub(ctx, "W1", model.Ptr(3), model.SourceExpansion)
		if err != nil {
			return err
		}
		if !created {
			t.Errorf("first UpsertStub reported the work was already there")
		}

		created, err = tx.UpsertStub(ctx, "W1", model.Ptr(3), model.SourceExpansion)
		if err != nil {
			return err
		}
		if created {
			t.Errorf("second UpsertStub reported a new work — deduplication failed")
		}

		// A shorter route to the same work.
		if _, err := tx.UpsertStub(ctx, "W1", model.Ptr(1), model.SourceExpansion); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatalf("UpsertStub: %v", err)
	}

	got, err := db.GetWork(ctx, "W1")
	if err != nil {
		t.Fatalf("GetWork: %v", err)
	}
	if !got.IsStub() {
		t.Errorf("IsStub() = false, want true")
	}
	if model.Deref(got.Depth, -1) != 1 {
		t.Errorf("Depth = %v, want 1 — a shorter route should lower it", got.Depth)
	}
	if got.DisplayTitle() == "" {
		t.Errorf("DisplayTitle() is empty for a stub")
	}
}

func TestUpsertStubNeverUnhydratesAWork(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()

	if err := db.UpsertWork(ctx, seedWork("W1")); err != nil {
		t.Fatalf("UpsertWork: %v", err)
	}
	// Expansion reaches an already-hydrated work again, which happens constantly
	// in a real graph.
	err := db.Tx(ctx, func(tx *Tx) error {
		_, err := tx.UpsertStub(ctx, "W1", model.Ptr(5), model.SourceExpansion)
		return err
	})
	if err != nil {
		t.Fatalf("UpsertStub: %v", err)
	}

	got, err := db.GetWork(ctx, "W1")
	if err != nil {
		t.Fatalf("GetWork: %v", err)
	}
	if got.IsStub() {
		t.Fatalf("a hydrated work was turned back into a stub")
	}
	if got.Title == nil {
		t.Errorf("Title was cleared by a stub upsert")
	}
	if model.Deref(got.Depth, -1) != 0 {
		t.Errorf("Depth = %v, want 0 — a longer route must not overwrite it", got.Depth)
	}
}

func TestGetWorkNotFound(t *testing.T) {
	t.Parallel()
	db := migrated(t)

	_, err := db.GetWork(t.Context(), "W404")
	if !errors.Is(err, errs.ErrNotFound) {
		t.Errorf("GetWork on an empty library = %v, want ErrNotFound", err)
	}
}

func TestWorkIDMustBeBare(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()

	// openalex_id is the deduplication key. A second spelling admitted here is a
	// duplicate node the primary key can no longer catch.
	bad := []string{
		"https://openalex.org/W2741809807",
		"openalex.org/W2741809807",
		"w2741809807",
		"W",
		"W0123",
		"10.1145/3292500",
		"",
	}
	for _, id := range bad {
		t.Run(id, func(t *testing.T) {
			if err := db.UpsertWork(ctx, &model.Work{OpenAlexID: id}); err == nil {
				t.Errorf("UpsertWork(%q) succeeded, want a refusal", id)
			}
			if _, err := db.GetWork(ctx, id); !errors.Is(err, errs.ErrInvalidInput) {
				t.Errorf("GetWork(%q) = %v, want ErrInvalidInput", id, err)
			}
		})
	}

	if err := db.UpsertWork(ctx, &model.Work{OpenAlexID: "W2741809807"}); err != nil {
		t.Errorf("UpsertWork on a bare ID: %v", err)
	}
}

func TestMarkUnresolved(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()

	err := db.Tx(ctx, func(tx *Tx) error {
		if _, err := tx.UpsertStub(ctx, "W1", nil, model.SourceExpansion); err != nil {
			return err
		}
		return tx.MarkUnresolved(ctx, "W1")
	})
	if err != nil {
		t.Fatalf("MarkUnresolved: %v", err)
	}

	got, err := db.GetWork(ctx, "W1")
	if err != nil {
		t.Fatalf("GetWork: %v", err)
	}
	if !got.Unresolved {
		t.Errorf("Unresolved = false, want true")
	}
	if !got.IsStub() {
		t.Errorf("IsStub() = false — unresolved is not hydration")
	}
	if got.DisplayTitle() != "[not in OpenAlex: W1]" {
		t.Errorf("DisplayTitle() = %q, want the unresolved form", got.DisplayTitle())
	}
}

func TestMarkUnresolvedOnAbsentWork(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()

	err := db.Tx(ctx, func(tx *Tx) error { return tx.MarkUnresolved(ctx, "W404") })
	if !errors.Is(err, errs.ErrNotFound) {
		t.Errorf("MarkUnresolved on an absent work = %v, want ErrNotFound", err)
	}
}

func TestStats(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()

	if s, err := db.Stats(ctx); err != nil || s != (Stats{}) {
		t.Fatalf("Stats() on an empty library = %+v, %v; want a zero Stats", s, err)
	}

	err := db.Tx(ctx, func(tx *Tx) error {
		if err := tx.UpsertWork(ctx, seedWork("W1")); err != nil {
			return err
		}
		if _, err := tx.UpsertStub(ctx, "W2", model.Ptr(1), model.SourceExpansion); err != nil {
			return err
		}
		return tx.UpsertEdge(ctx, model.Edge{FromWork: "W1", ToWork: "W2"})
	})
	if err != nil {
		t.Fatalf("seed the library: %v", err)
	}

	got, err := db.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	want := Stats{Works: 2, Stubs: 1, Edges: 1}
	if got != want {
		t.Errorf("Stats() = %+v, want %+v", got, want)
	}
}
