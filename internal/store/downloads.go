package store

import (
	"context"
	"fmt"
	"time"

	"github.com/codevector-2003/filiation/internal/model"
)

// Attempt is one row of download_attempt.
type Attempt struct {
	URL     string
	Host    string
	Outcome string // pdfs.Outcome, or "duplicate"
	Status  int
	Bytes   int64
	Millis  int64
	At      time.Time
}

// OutcomeOK and OutcomeDuplicate are the attempt outcomes store itself reads.
// A duplicate is a download whose bytes the library already holds for
// another work — a publisher's "access denied" PDF served for many papers is
// the usual cause — and counts as a failure of that link.
const (
	OutcomeOK        = "ok"
	OutcomeDuplicate = "duplicate"
)

// RecordAttempt logs one download of the work ref names.
func (tx *Tx) RecordAttempt(ctx context.Context, ref string, a Attempt) error {
	fil, err := resolveRef(ctx, tx.tx, ref)
	if err != nil {
		return err
	}
	_, err = tx.tx.ExecContext(ctx, `
INSERT INTO download_attempt (fil_id, url, host, outcome, status, bytes, millis)
VALUES (?, ?, ?, ?, ?, ?, ?);`,
		fil, a.URL, a.Host, a.Outcome, nullInt(a.Status), nullInt64(a.Bytes), a.Millis)
	if err != nil {
		return fmt.Errorf("store: record download of %s: %w", ref, err)
	}
	return nil
}

// AttemptsOf returns a work's download attempts, newest first.
func (db *DB) AttemptsOf(ctx context.Context, ref string) ([]Attempt, error) {
	fil, err := resolveRef(ctx, db.read, ref)
	if err != nil {
		return nil, err
	}
	rows, err := db.read.QueryContext(ctx, `
SELECT url, host, outcome, ifnull(status, 0), ifnull(bytes, 0), ifnull(millis, 0), at
FROM download_attempt WHERE fil_id = ? ORDER BY id DESC;`, fil)
	if err != nil {
		return nil, fmt.Errorf("store: read downloads of %s: %w", ref, err)
	}
	defer rows.Close()
	var out []Attempt
	for rows.Next() {
		var a Attempt
		var at string
		if err := rows.Scan(&a.URL, &a.Host, &a.Outcome, &a.Status, &a.Bytes, &a.Millis, &at); err != nil {
			return nil, fmt.Errorf("store: read downloads of %s: %w", ref, err)
		}
		a.At, _ = time.Parse(time.DateTime, at)
		out = append(out, a)
	}
	return out, rows.Err()
}

// ToFetch is a work fetch should download, with the links to try.
type ToFetch struct {
	FilID string
	Links []model.Location
}

// hasPDF is true for a work with any PDF recorded — on disk, or deleted by
// the researcher. A deleted one is never downloaded again unasked (ADR-012).
const hasPDF = `EXISTS (SELECT 1 FROM work_file f WHERE f.fil_id = w.fil_id AND lower(f.path) LIKE '%.pdf')`

// triable selects the links of work w.fil_id that have not failed since ?1.
const triable = `SELECT l.pdf_url FROM work_location l WHERE l.fil_id = w.fil_id
  AND NOT EXISTS (SELECT 1 FROM download_attempt a
                  WHERE a.fil_id = l.fil_id AND a.url = l.pdf_url
                    AND a.outcome <> 'ok' AND a.at >= ?1)`

// WorksToFetch returns up to limit works to download, in the order fetching
// wants them: the user's own papers first, then the works the library cites
// most (ADR-013). A work qualifies when it has a PDF link, no PDF recorded,
// and at least one link that has not failed since retryAfter ago. Each comes
// with those links, best first.
func (db *DB) WorksToFetch(ctx context.Context, limit int, retryAfter time.Duration) ([]ToFetch, error) {
	cutoff := time.Now().UTC().Add(-retryAfter).Format(time.DateTime)
	rows, err := db.read.QueryContext(ctx, `
SELECT w.fil_id
FROM work w JOIN work_pdf p ON p.fil_id = w.fil_id
WHERE p.availability = 'pdf-link' AND NOT `+hasPDF+`
  AND EXISTS (`+triable+`)
ORDER BY w.is_seed DESC,
         (SELECT count(*) FROM cites c WHERE c.to_work = w.fil_id) DESC,
         w.rowid
LIMIT ?2;`, cutoff, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list works to fetch: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, fmt.Errorf("store: list works to fetch: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list works to fetch: %w", err)
	}

	out := make([]ToFetch, 0, len(ids))
	for _, id := range ids {
		links, err := db.LinksToTry(ctx, id, retryAfter)
		if err != nil {
			return nil, err
		}
		out = append(out, ToFetch{FilID: id, Links: links})
	}
	return out, nil
}

// LinksToTry returns a work's PDF links that have not failed since
// retryAfter ago, best first. A retryAfter of zero returns every link — what
// a person asking for one paper by name gets.
func (db *DB) LinksToTry(ctx context.Context, ref string, retryAfter time.Duration) ([]model.Location, error) {
	fil, err := resolveRef(ctx, db.read, ref)
	if err != nil {
		return nil, err
	}
	cutoff := time.Now().UTC().Add(-retryAfter).Format(time.DateTime)
	if retryAfter == 0 {
		cutoff = "9999" // no attempt is that recent
	}
	rows, err := db.read.QueryContext(ctx, `
SELECT l.pdf_url, ifnull(l.landing_url, ''), ifnull(l.version, ''), ifnull(l.license, ''), l.host
FROM work_location l
WHERE l.fil_id = ?2
  AND NOT EXISTS (SELECT 1 FROM download_attempt a
                  WHERE a.fil_id = l.fil_id AND a.url = l.pdf_url
                    AND a.outcome <> 'ok' AND a.at >= ?1)
ORDER BY l.rank;`, cutoff, fil)
	if err != nil {
		return nil, fmt.Errorf("store: read links of %s: %w", ref, err)
	}
	defer rows.Close()
	var out []model.Location
	for rows.Next() {
		var l model.Location
		if err := rows.Scan(&l.PDFURL, &l.LandingURL, &l.Version, &l.License, &l.Host); err != nil {
			return nil, fmt.Errorf("store: read links of %s: %w", ref, err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// PDFsOnDisk counts the works with at least one PDF present on disk.
func (db *DB) PDFsOnDisk(ctx context.Context) (int, error) {
	var n int
	err := db.read.QueryRowContext(ctx, `
SELECT count(DISTINCT fil_id) FROM work_file WHERE missing = 0 AND lower(path) LIKE '%.pdf';`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("store: count PDFs on disk: %w", err)
	}
	return n, nil
}

func nullInt(n int) any {
	if n == 0 {
		return nil
	}
	return n
}

func nullInt64(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}
