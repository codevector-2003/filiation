package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/codevector-2003/filiation/internal/errs"
	"github.com/codevector-2003/filiation/internal/identity"
	"github.com/codevector-2003/filiation/internal/model"
)

// openAlexIDPattern is the bare OpenAlex work ID, "W2741809807".
//
// The store rejects anything else — including the https://openalex.org/W... URL
// the API actually returns — rather than storing it. openalex_id is the
// deduplication key (hard rule 6), so a work admitted under a second spelling is
// not a formatting problem: it is a duplicate node the UNIQUE index can no longer
// catch, and the graph quietly rots around it. Normalising the URL form is
// internal/identity's job, one layer up.
var openAlexIDPattern = regexp.MustCompile(`^W[1-9][0-9]*$`)

// timeLayout matches SQLite's datetime('now'), which is what the added_at
// default produces. It carries no zone because SQLite writes UTC.
const timeLayout = "2006-01-02 15:04:05"

// workColumns is every column of work, in the order scanWork reads them.
const workColumns = `fil_id, openalex_id, doi, arxiv_id, pmid, title, abstract, year,
	venue, type, cited_by_count, oa_status, oa_url,
	hydrated, fetched_refs, unresolved, depth, is_seed, source, added_at`

// UpsertWork writes a hydrated work, creating the row if this library has never
// seen it and filling in the metadata if it was already there as a stub.
//
// Hydration is an update, not a replacement, because a work is almost always
// already present when it is hydrated — a stub was created the moment some other
// paper was seen to cite it (§4). Three columns are therefore preserved rather
// than overwritten, and losing any of them would be a real loss:
//
//   - depth keeps the smaller of the two. It is hops from the nearest seed, and
//     a work reached again by a longer route has not moved further away.
//   - is_seed is sticky. A work the user named stays named even when expansion
//     reaches it again from somewhere else.
//   - source records how the work first entered the library and is never
//     rewritten, so "this arrived through expansion" survives a later re-fetch.
//
// Everything else is taken from w, which is treated as the authoritative record.
//
// The work is found by its OpenAlex ID, which is what OpenAlex sent. A work new
// to the library is given a fresh fil ID (D17); one already present keeps the
// fil ID it has, forever. Either way w.FilID is set on return.
func (tx *Tx) UpsertWork(ctx context.Context, w *model.Work) error {
	return upsertWork(ctx, tx.tx, tx.newID, w)
}

// UpsertWork runs UpsertWork in a transaction of its own. It is the one-work
// convenience; a batch should open one transaction and reuse it.
func (db *DB) UpsertWork(ctx context.Context, w *model.Work) error {
	return db.Tx(ctx, func(tx *Tx) error { return tx.UpsertWork(ctx, w) })
}

func upsertWork(ctx context.Context, e execer, newID func() string, w *model.Work) error {
	if w == nil {
		return errors.New("store: upsert work: nil work")
	}
	if err := checkOpenAlexID(w.OpenAlexID); err != nil {
		return err
	}

	fil, found, err := filIDOf(ctx, e, w.OpenAlexID)
	if err != nil {
		return fmt.Errorf("store: upsert work %s: %w", w.OpenAlexID, err)
	}
	if !found {
		if fil, err = freshFilID(ctx, e, newID); err != nil {
			return fmt.Errorf("store: upsert work %s: %w", w.OpenAlexID, err)
		}
		// The row is created bare and then filled in by the same update an
		// existing row gets, so the two paths cannot drift apart.
		if _, err := e.ExecContext(ctx,
			`INSERT INTO work (fil_id, openalex_id) VALUES (?, ?);`, fil, w.OpenAlexID); err != nil {
			return fmt.Errorf("store: upsert work %s: %w", w.OpenAlexID, err)
		}
	}

	_, err = e.ExecContext(ctx, `
UPDATE work SET
	doi            = ?1,
	arxiv_id       = ?2,
	pmid           = ?3,
	title          = ?4,
	abstract       = ?5,
	year           = ?6,
	venue          = ?7,
	type           = ?8,
	cited_by_count = ?9,
	oa_status      = ?10,
	oa_url         = ?11,
	hydrated       = 1,
	unresolved     = 0,
	depth          = min(ifnull(depth, ?12), ifnull(?12, depth)),
	is_seed        = max(is_seed, ?13),
	source         = ifnull(source, ?14)
WHERE fil_id = ?15;`,
		w.DOI, w.ArXivID, w.PMID, w.Title,
		nullString(w.Abstract), w.Year, nullString(w.Venue),
		nullString(string(w.Type)), w.CitedByCount,
		nullString(string(w.OAStatus)), w.OAURL,
		w.Depth, boolToInt(w.IsSeed), nullString(string(w.Source)), fil,
	)
	if err != nil {
		return fmt.Errorf("store: upsert work %s: %w", w.OpenAlexID, err)
	}
	w.FilID = fil
	// Authors not loaded means this caller has no byline to give, which is not
	// the same as a work with none; leave whatever is stored alone.
	if w.AuthorsLoaded() {
		return writeAuthors(ctx, e, fil, w.Authors)
	}
	return nil
}

// UpsertStub creates a work row holding nothing but an identifier, and reports
// whether it had to create it.
//
// This is what makes deduplication free (§4): the target of a citation gets a
// row the moment the edge is written, so "connect to the existing node if the
// paper is already there" is a primary key conflict rather than matching logic
// that can be got wrong.
//
// It never touches a row that already exists, with one exception: depth is
// lowered if this route to the work is shorter than the one already recorded.
// Expansion is best-first, so a work can be reached the long way round first,
// and leaving the longer distance in place would misreport how far from the seed
// the user's graph actually reaches.
func (tx *Tx) UpsertStub(ctx context.Context, id string, depth *int, source model.Source) (bool, error) {
	_, created, err := upsertStub(ctx, tx.tx, tx.newID, id, depth, source)
	return created, err
}

// upsertStub is UpsertStub, also returning the stub's fil ID — which the edge
// writer needs, since cites refers to works by fil ID.
//
// It is the hottest write in the program: an expansion records tens of
// thousands of references, and in a dense graph most of them point at works
// already present — that is how in-graph in-degree accumulates. So it looks
// first, reading the stored depth in the same query, and writes only when there
// is something to write: a new row, or a shorter route. A new row is one
// statement, with the check that its fil ID is not an alias folded into the
// insert. Measured on 200 works citing 80 overlapping references each (16,000
// edges, 6,000 works): v1 took 0.95 s and this takes 1.35 s — the difference
// is mostly the second unique index v2 keeps on work. Checking the new ID in a
// query of its own took 1.43 s; trying the insert first took 2.1 s, because a
// refused insert costs more than a lookup.
func upsertStub(ctx context.Context, e execer, newID func() string, id string, depth *int, source model.Source) (string, bool, error) {
	if err := checkOpenAlexID(id); err != nil {
		return "", false, err
	}

	var (
		existing string
		stored   *int
	)
	err := e.QueryRowContext(ctx,
		`SELECT fil_id, depth FROM work WHERE openalex_id = ?;`, id).Scan(&existing, &stored)
	switch {
	case err == nil:
		// The row was already there. The only thing worth updating is a
		// depth this route improves on.
		if depth != nil && (stored == nil || *stored > *depth) {
			if _, err := e.ExecContext(ctx,
				`UPDATE work SET depth = ? WHERE fil_id = ?;`, *depth, existing); err != nil {
				return "", false, fmt.Errorf("store: lower depth of %s: %w", id, err)
			}
		}
		return existing, false, nil
	case !errors.Is(err, sql.ErrNoRows):
		return "", false, fmt.Errorf("store: upsert stub %s: %w", id, err)
	}

	// New to the library. The OpenAlex ID is known to be free, so an insert
	// that writes nothing means the candidate fil ID was taken — by a work or
	// an alias — and another is drawn.
	for range maxIDDraws {
		fil := newID()
		res, err := e.ExecContext(ctx, `
INSERT OR IGNORE INTO work (fil_id, openalex_id, depth, source)
SELECT ?1, ?2, ?3, ?4 WHERE NOT EXISTS (SELECT 1 FROM work_alias WHERE alias = ?1);`,
			fil, id, depth, nullString(string(source)))
		if err != nil {
			return "", false, fmt.Errorf("store: upsert stub %s: %w", id, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return "", false, fmt.Errorf("store: upsert stub %s: %w", id, err)
		}
		if n > 0 {
			return fil, true, nil
		}
	}
	return "", false, fmt.Errorf("store: upsert stub %s: no unused fil ID after %d draws", id, maxIDDraws)
}

// MarkUnresolved records that OpenAlex has no record of a work some other paper
// cited, so expansion stops spending budget on it.
//
// The citation edge stays. It remains true that the paper was cited, whatever
// the index knows, and deleting the edge to tidy up would silently falsify the
// graph (§7). The row stays a stub: unresolved is not hydration.
func (tx *Tx) MarkUnresolved(ctx context.Context, id string) error {
	if err := checkOpenAlexID(id); err != nil {
		return err
	}
	res, err := tx.tx.ExecContext(ctx,
		`UPDATE work SET unresolved = 1 WHERE openalex_id = ?;`, id)
	if err != nil {
		return fmt.Errorf("store: mark %s unresolved: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: mark %s unresolved: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("store: mark %s unresolved: %w", id, errs.ErrNotFound)
	}
	return nil
}

// GetWork reads one work, from the read pool. ref is any ID the work has or
// had: its fil ID, its OpenAlex ID, or an alias left by a fold (D17).
//
// Authors are not loaded: Work.Authors stays nil, which is how a caller tells
// "this query did not ask for authors" from "this work has none". It returns a
// stub as readily as a hydrated work — stubs are the common case, not the
// exception, and a read path that treated one as missing would hide most of the
// graph.
func (db *DB) GetWork(ctx context.Context, ref string) (*model.Work, error) {
	return getWork(ctx, db.read, ref)
}

// GetWork reads one work inside this transaction, so it sees writes the
// transaction has made but not yet committed.
func (tx *Tx) GetWork(ctx context.Context, ref string) (*model.Work, error) {
	return getWork(ctx, tx.tx, ref)
}

func getWork(ctx context.Context, q queryer, ref string) (*model.Work, error) {
	ref, err := checkRef(ref)
	if err != nil {
		return nil, err
	}
	row := q.QueryRowContext(ctx, `SELECT `+workColumns+` FROM work `+byRef+`;`, ref)

	w, err := scanWork(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("store: get work %s: %w", ref, errs.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("store: get work %s: %w", ref, err)
	}
	return w, nil
}

// byRef selects the one work ?1 names, whichever of its IDs ?1 is. A work's own
// fil ID or OpenAlex ID wins over an alias: an OpenAlex ID folded away can come
// back as a new stub when a later paper cites it, and then names that stub.
const byRef = `WHERE fil_id = ?1 OR openalex_id = ?1
	OR fil_id = (SELECT fil_id FROM work_alias WHERE alias = ?1)
	ORDER BY (fil_id = ?1 OR openalex_id = ?1) DESC LIMIT 1`

// resolveRef returns the fil ID of the work ref names, or errs.ErrNotFound.
func resolveRef(ctx context.Context, q queryer, ref string) (string, error) {
	ref, err := checkRef(ref)
	if err != nil {
		return "", err
	}
	var fil string
	err = q.QueryRowContext(ctx, `SELECT fil_id FROM work `+byRef+`;`, ref).Scan(&fil)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("store: no work %s in this library: %w", ref, errs.ErrNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("store: look up %s: %w", ref, err)
	}
	return fil, nil
}

// filIDOf returns the fil ID of the work holding an OpenAlex ID, and whether
// there is one. Aliases are not consulted: this is the write path's question,
// "is this OpenAlex work already a row?", and the UNIQUE index answers it.
func filIDOf(ctx context.Context, q queryer, openAlexID string) (string, bool, error) {
	var fil string
	err := q.QueryRowContext(ctx,
		`SELECT fil_id FROM work WHERE openalex_id = ?;`, openAlexID).Scan(&fil)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return fil, true, nil
}

// maxIDDraws bounds the search for an unused fil ID. With 40 random bits even
// a million-work library clashes about once in a million draws, so needing
// more than a handful means the randomness is broken, not the library full.
const maxIDDraws = 16

// freshFilID draws fil IDs until one is used by no work and no alias. It runs
// inside the single writer's transaction, so nothing can take the ID between
// the check and the insert that follows it.
func freshFilID(ctx context.Context, q queryer, newID func() string) (string, error) {
	for range maxIDDraws {
		id := newID()
		var taken int
		err := q.QueryRowContext(ctx, `
SELECT (SELECT count(*) FROM work WHERE fil_id = ?1)
     + (SELECT count(*) FROM work_alias WHERE alias = ?1);`, id).Scan(&taken)
		if err != nil {
			return "", fmt.Errorf("check fil ID %s: %w", id, err)
		}
		if taken == 0 {
			return id, nil
		}
	}
	return "", fmt.Errorf("no unused fil ID after %d draws", maxIDDraws)
}

// scanner is what both *sql.Row and *sql.Rows provide, so one scan function
// serves a single lookup and a list query alike.
type scanner interface {
	Scan(dest ...any) error
}

func scanWork(s scanner) (*model.Work, error) {
	var (
		w        model.Work
		openalex sql.NullString
		abstract sql.NullString
		venue    sql.NullString
		workType sql.NullString
		oaStatus sql.NullString
		citedBy  sql.NullInt64
		source   sql.NullString
		addedAt  sql.NullString
	)
	err := s.Scan(
		&w.FilID, &openalex, &w.DOI, &w.ArXivID, &w.PMID, &w.Title, &abstract, &w.Year,
		&venue, &workType, &citedBy, &oaStatus, &w.OAURL,
		&w.Hydrated, &w.FetchedRefs, &w.Unresolved, &w.Depth,
		&w.IsSeed, &source, &addedAt,
	)
	if err != nil {
		return nil, err
	}

	w.OpenAlexID = openalex.String
	w.Abstract = abstract.String
	w.Venue = venue.String
	w.Type = model.WorkType(workType.String)
	w.CitedByCount = int(citedBy.Int64)
	w.OAStatus = model.OAStatus(oaStatus.String)
	w.Source = model.Source(source.String)

	if addedAt.Valid {
		// A timestamp this build cannot parse is not worth failing a read over:
		// the zero time is honest about knowing nothing, and nothing decides
		// anything on added_at.
		if t, err := time.Parse(timeLayout, addedAt.String); err == nil {
			w.AddedAt = t.UTC()
		}
	}
	return &w, nil
}

// Stats counts what the library holds. The CLI prints it after add and expand,
// and the stub count is the honest half of that report: it is how much of the
// graph is still only an identifier.
type Stats struct {
	Works int // every row in work, stubs included
	Stubs int // of those, the ones never hydrated
	Edges int // rows in cites
}

// Stats reads the library's counts in one query.
func (db *DB) Stats(ctx context.Context) (Stats, error) {
	var s Stats
	err := db.read.QueryRowContext(ctx, `
SELECT (SELECT count(*) FROM work),
       (SELECT count(*) FROM work WHERE hydrated = 0),
       (SELECT count(*) FROM cites);`).Scan(&s.Works, &s.Stubs, &s.Edges)
	if err != nil {
		return Stats{}, fmt.Errorf("store: read library stats: %w", err)
	}
	return s, nil
}

// checkOpenAlexID rejects anything that is not a bare OpenAlex work ID. See
// openAlexIDPattern for why this is a refusal and not a normalisation. The
// write path takes OpenAlex IDs, because they are what OpenAlex sends.
func checkOpenAlexID(id string) error {
	if openAlexIDPattern.MatchString(id) {
		return nil
	}
	return fmt.Errorf("store: %q is not a bare OpenAlex work ID such as W2741809807 "+
		"(normalise it with identity.NormaliseOpenAlexID): %w", id, errs.ErrInvalidInput)
}

// checkRef accepts either ID a work can be read by — a fil ID, in either case,
// or a bare OpenAlex ID — and returns it in the form the store compares.
func checkRef(ref string) (string, error) {
	if fil, err := identity.NormaliseFilID(ref); err == nil {
		return fil, nil
	}
	if openAlexIDPattern.MatchString(ref) {
		return ref, nil
	}
	return "", fmt.Errorf("store: %q is neither a fil ID such as F7K2M9QXA nor a bare "+
		"OpenAlex work ID such as W2741809807: %w", ref, errs.ErrInvalidInput)
}

// nullString stores the empty string as NULL.
//
// Work models a handful of columns as plain strings rather than pointers, where
// the empty string is an honest way to say "not set" and nothing downstream can
// misread it. Writing "" into the column instead of NULL would put two spellings
// of absent into the same database and make every WHERE clause choose between
// them.
func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
