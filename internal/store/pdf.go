package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/codevector-2003/filiation/internal/model"
)

// LinkSourceOpenAlex marks links that came from OpenAlex's locations.
const LinkSourceOpenAlex = "openalex"

// PutPDFLinks records what is known about a work's open-access PDF, replacing
// what was recorded before. ref is any ID the work has.
func (tx *Tx) PutPDFLinks(ctx context.Context, ref string, links model.PDFLinks) error {
	fil, err := resolveRef(ctx, tx.tx, ref)
	if err != nil {
		return err
	}
	if links.Availability == model.PDFUnchecked {
		return fmt.Errorf("store: record PDF links of %s: no availability", ref)
	}
	if _, err := tx.tx.ExecContext(ctx, `DELETE FROM work_location WHERE fil_id = ?;`, fil); err != nil {
		return fmt.Errorf("store: record PDF links of %s: %w", ref, err)
	}
	for i, l := range links.Links {
		_, err := tx.tx.ExecContext(ctx, `
INSERT INTO work_location (fil_id, rank, pdf_url, landing_url, version, license, host, source)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);`,
			fil, i, l.PDFURL, nullString(l.LandingURL), nullString(l.Version), nullString(l.License), l.Host, LinkSourceOpenAlex)
		if err != nil {
			return fmt.Errorf("store: record PDF link of %s: %w", ref, err)
		}
	}
	_, err = tx.tx.ExecContext(ctx, `
INSERT INTO work_pdf (fil_id, availability, checked_at) VALUES (?, ?, datetime('now'))
ON CONFLICT (fil_id) DO UPDATE SET availability = excluded.availability, checked_at = excluded.checked_at;`,
		fil, string(links.Availability))
	if err != nil {
		return fmt.Errorf("store: record PDF availability of %s: %w", ref, err)
	}
	return nil
}

// PDFLinksOf returns what is known about a work's open-access PDF, and when it
// was looked up. A work never looked up has PDFUnchecked and a zero time,
// unless expansion already found OpenAlex has no record of it.
func (db *DB) PDFLinksOf(ctx context.Context, ref string) (model.PDFLinks, time.Time, error) {
	fil, err := resolveRef(ctx, db.read, ref)
	if err != nil {
		return model.PDFLinks{}, time.Time{}, err
	}
	var out model.PDFLinks
	var avail, checked string
	err = db.read.QueryRowContext(ctx, `
SELECT ifnull(p.availability, CASE WHEN w.unresolved = 1 THEN ?2 ELSE '' END), ifnull(p.checked_at, '')
FROM work w LEFT JOIN work_pdf p ON p.fil_id = w.fil_id
WHERE w.fil_id = ?1;`, fil, string(model.PDFNotInOpenAlex)).Scan(&avail, &checked)
	if errors.Is(err, sql.ErrNoRows) {
		return out, time.Time{}, nil
	}
	if err != nil {
		return out, time.Time{}, fmt.Errorf("store: read PDF availability of %s: %w", ref, err)
	}
	out.Availability = model.PDFAvailability(avail)
	at, _ := time.Parse(time.DateTime, checked)

	rows, err := db.read.QueryContext(ctx, `
SELECT pdf_url, ifnull(landing_url, ''), ifnull(version, ''), ifnull(license, ''), host
FROM work_location WHERE fil_id = ? ORDER BY rank;`, fil)
	if err != nil {
		return out, at, fmt.Errorf("store: read PDF links of %s: %w", ref, err)
	}
	defer rows.Close()
	for rows.Next() {
		var l model.Location
		if err := rows.Scan(&l.PDFURL, &l.LandingURL, &l.Version, &l.License, &l.Host); err != nil {
			return out, at, fmt.Errorf("store: read PDF links of %s: %w", ref, err)
		}
		out.Links = append(out.Links, l)
	}
	return out, at, rows.Err()
}

// ToLocate returns the OpenAlex IDs of up to limit works whose PDF links are
// not known, or were looked up longer ago than maxAge, in the order fetching
// would want them: the user's own papers first, then the works the library
// cites most (ADR-013's ranking, the same signal expansion uses). Works
// OpenAlex has no record of are left out; there is nothing to ask about them.
func (db *DB) ToLocate(ctx context.Context, limit int, maxAge time.Duration) ([]string, error) {
	cutoff := time.Now().UTC().Add(-maxAge).Format(time.DateTime)
	rows, err := db.read.QueryContext(ctx, `
SELECT w.openalex_id
FROM work w
LEFT JOIN work_pdf p ON p.fil_id = w.fil_id
WHERE w.openalex_id IS NOT NULL AND w.unresolved = 0
  AND (p.fil_id IS NULL OR p.checked_at < ?)
ORDER BY w.is_seed DESC,
         (SELECT count(*) FROM cites c WHERE c.to_work = w.fil_id) DESC,
         w.rowid
LIMIT ?;`, cutoff, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list works to locate: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("store: list works to locate: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// PDFCounts counts works by what is known about their PDF. Works never looked
// up count under PDFUnchecked.
func (db *DB) PDFCounts(ctx context.Context) (map[model.PDFAvailability]int, error) {
	// A work expansion already found missing from OpenAlex needs no lookup to
	// know its answer.
	rows, err := db.read.QueryContext(ctx, `
SELECT ifnull(p.availability, CASE WHEN w.unresolved = 1 THEN ? ELSE '' END), count(*)
FROM work w LEFT JOIN work_pdf p ON p.fil_id = w.fil_id
GROUP BY 1;`, string(model.PDFNotInOpenAlex))
	if err != nil {
		return nil, fmt.Errorf("store: count PDF availability: %w", err)
	}
	defer rows.Close()
	out := map[model.PDFAvailability]int{}
	for rows.Next() {
		var a string
		var n int
		if err := rows.Scan(&a, &n); err != nil {
			return nil, fmt.Errorf("store: count PDF availability: %w", err)
		}
		out[model.PDFAvailability(a)] = n
	}
	return out, rows.Err()
}
