package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/codevector-2003/filiation/internal/errs"
	"github.com/codevector-2003/filiation/internal/model"
)

// workIDPattern is the bare OpenAlex work ID, "W2741809807".
//
// The store rejects anything else — including the https://openalex.org/W... URL
// the API actually returns — rather than storing it. openalex_id is the
// deduplication key (hard rule 6), so a work admitted under a second spelling is
// not a formatting problem: it is a duplicate node the primary key can no longer
// catch, and the graph quietly rots around it. Normalising the URL form is
// internal/identity's job, one layer up.
var workIDPattern = regexp.MustCompile(`^W[1-9][0-9]*$`)

// timeLayout matches SQLite's datetime('now'), which is what the added_at
// default produces. It carries no zone because SQLite writes UTC.
const timeLayout = "2006-01-02 15:04:05"

// workColumns is every column of work, in the order scanWork reads them.
const workColumns = `openalex_id, doi, arxiv_id, pmid, title, abstract, year,
	venue, type, cited_by_count, oa_status, oa_url, pdf_sha256, pdf_license,
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
// The PDF columns are left alone entirely: they belong to M3, and nothing in the
// metadata path knows anything about them.
func (tx *Tx) UpsertWork(ctx context.Context, w *model.Work) error {
	return upsertWork(ctx, tx.tx, w)
}

// UpsertWork runs UpsertWork in a transaction of its own. It is the one-work
// convenience; a batch should open one transaction and reuse it.
func (db *DB) UpsertWork(ctx context.Context, w *model.Work) error {
	return db.Tx(ctx, func(tx *Tx) error { return tx.UpsertWork(ctx, w) })
}

func upsertWork(ctx context.Context, e execer, w *model.Work) error {
	if w == nil {
		return errors.New("store: upsert work: nil work")
	}
	if err := checkWorkID(w.OpenAlexID); err != nil {
		return err
	}

	const q = `
INSERT INTO work (
	openalex_id, doi, arxiv_id, pmid, title, abstract, year, venue, type,
	cited_by_count, oa_status, oa_url, hydrated, unresolved, depth, is_seed, source
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, 0, ?, ?, ?)
ON CONFLICT(openalex_id) DO UPDATE SET
	doi            = excluded.doi,
	arxiv_id       = excluded.arxiv_id,
	pmid           = excluded.pmid,
	title          = excluded.title,
	abstract       = excluded.abstract,
	year           = excluded.year,
	venue          = excluded.venue,
	type           = excluded.type,
	cited_by_count = excluded.cited_by_count,
	oa_status      = excluded.oa_status,
	oa_url         = excluded.oa_url,
	hydrated       = 1,
	unresolved     = 0,
	depth          = min(ifnull(work.depth, excluded.depth),
	                     ifnull(excluded.depth, work.depth)),
	is_seed        = max(work.is_seed, excluded.is_seed),
	source         = ifnull(work.source, excluded.source);`

	_, err := e.ExecContext(ctx, q,
		w.OpenAlexID, w.DOI, w.ArXivID, w.PMID, w.Title,
		nullString(w.Abstract), w.Year, nullString(w.Venue),
		nullString(string(w.Type)), w.CitedByCount,
		nullString(string(w.OAStatus)), w.OAURL,
		w.Depth, boolToInt(w.IsSeed), nullString(string(w.Source)),
	)
	if err != nil {
		return fmt.Errorf("store: upsert work %s: %w", w.OpenAlexID, err)
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
	return upsertStub(ctx, tx.tx, id, depth, source)
}

func upsertStub(ctx context.Context, e execer, id string, depth *int, source model.Source) (bool, error) {
	if err := checkWorkID(id); err != nil {
		return false, err
	}

	// OR IGNORE rather than ON CONFLICT DO UPDATE, so RowsAffected answers "was
	// this work new to the library?" — a conditional update would report 1 for a
	// row it merely touched. A stub carries no DOI, so the primary key is the
	// only constraint this can suppress.
	res, err := e.ExecContext(ctx,
		`INSERT OR IGNORE INTO work (openalex_id, depth, source) VALUES (?, ?, ?);`,
		id, depth, nullString(string(source)))
	if err != nil {
		return false, fmt.Errorf("store: upsert stub %s: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store: upsert stub %s: %w", id, err)
	}
	if n > 0 {
		return true, nil
	}

	// The row was already there. The only thing worth updating is a depth this
	// route improves on.
	if depth != nil {
		_, err := e.ExecContext(ctx,
			`UPDATE work SET depth = ? WHERE openalex_id = ? AND (depth IS NULL OR depth > ?);`,
			*depth, id, *depth)
		if err != nil {
			return false, fmt.Errorf("store: lower depth of %s: %w", id, err)
		}
	}
	return false, nil
}

// MarkUnresolved records that OpenAlex has no record of a work some other paper
// cited, so expansion stops spending budget on it.
//
// The citation edge stays. It remains true that the paper was cited, whatever
// the index knows, and deleting the edge to tidy up would silently falsify the
// graph (§7). The row stays a stub: unresolved is not hydration.
func (tx *Tx) MarkUnresolved(ctx context.Context, id string) error {
	if err := checkWorkID(id); err != nil {
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

// GetWork reads one work by its OpenAlex ID, from the read pool.
//
// Authors are not loaded: Work.Authors stays nil, which is how a caller tells
// "this query did not ask for authors" from "this work has none". It returns a
// stub as readily as a hydrated work — stubs are the common case, not the
// exception, and a read path that treated one as missing would hide most of the
// graph.
func (db *DB) GetWork(ctx context.Context, id string) (*model.Work, error) {
	return getWork(ctx, db.read, id)
}

// GetWork reads one work inside this transaction, so it sees writes the
// transaction has made but not yet committed.
func (tx *Tx) GetWork(ctx context.Context, id string) (*model.Work, error) {
	return getWork(ctx, tx.tx, id)
}

func getWork(ctx context.Context, q queryer, id string) (*model.Work, error) {
	if err := checkWorkID(id); err != nil {
		return nil, err
	}
	row := q.QueryRowContext(ctx,
		`SELECT `+workColumns+` FROM work WHERE openalex_id = ?;`, id)

	w, err := scanWork(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("store: get work %s: %w", id, errs.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("store: get work %s: %w", id, err)
	}
	return w, nil
}

// scanner is what both *sql.Row and *sql.Rows provide, so one scan function
// serves a single lookup and a list query alike.
type scanner interface {
	Scan(dest ...any) error
}

func scanWork(s scanner) (*model.Work, error) {
	var (
		w        model.Work
		abstract sql.NullString
		venue    sql.NullString
		workType sql.NullString
		oaStatus sql.NullString
		citedBy  sql.NullInt64
		source   sql.NullString
		addedAt  sql.NullString
	)
	err := s.Scan(
		&w.OpenAlexID, &w.DOI, &w.ArXivID, &w.PMID, &w.Title, &abstract, &w.Year,
		&venue, &workType, &citedBy, &oaStatus, &w.OAURL, &w.PDFSHA256,
		&w.PDFLicense, &w.Hydrated, &w.FetchedRefs, &w.Unresolved, &w.Depth,
		&w.IsSeed, &source, &addedAt,
	)
	if err != nil {
		return nil, err
	}

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

// checkWorkID rejects anything that is not a bare OpenAlex work ID. See
// workIDPattern for why this is a refusal and not a normalisation.
func checkWorkID(id string) error {
	if workIDPattern.MatchString(id) {
		return nil
	}
	return fmt.Errorf("store: %q is not a bare OpenAlex work ID such as W2741809807 "+
		"(normalise it with identity.NormaliseOpenAlexID): %w", id, errs.ErrInvalidInput)
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
