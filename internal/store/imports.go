package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/codevector-2003/filiation/internal/model"
)

// Import outcomes. Only OutcomeImported files a PDF; the rest say why not.
const (
	ImportImported      = "imported"
	ImportDuplicate     = "duplicate"       // the library already holds these bytes
	ImportUnconfirmed   = "unconfirmed"     // an identifier resolved, but its title is not in the PDF
	ImportCandidates    = "candidates"      // only a title search's candidates; the user chooses
	ImportUnidentified  = "unidentified"    // nothing to go on: a scan, or no identifier and no title
	ImportNotInOpenAlex = "not-in-openalex" // a title search found nothing; may become a local document
	ImportUnreadable    = "unreadable"      // PDFium could not open it
)

// ImportAttempt is one row of import_attempt.
type ImportAttempt struct {
	SHA256   string
	Path     string
	Outcome  string
	Reason   string
	FilID    string // the work it was filed under, when it was
	Searched bool   // a title search was spent on it
	At       time.Time
}

// RecordImport records what importing a file gave, replacing what an
// earlier try of the same bytes gave.
func (tx *Tx) RecordImport(ctx context.Context, a ImportAttempt) error {
	_, err := tx.tx.ExecContext(ctx, `
INSERT INTO import_attempt (sha256, path, outcome, reason, fil_id, searched, at)
VALUES (?, ?, ?, ?, ?, ?, datetime('now'))
ON CONFLICT (sha256) DO UPDATE SET
  path = excluded.path, outcome = excluded.outcome, reason = excluded.reason,
  fil_id = excluded.fil_id, searched = max(searched, excluded.searched), at = excluded.at;`,
		a.SHA256, a.Path, a.Outcome, nullString(a.Reason), nullString(a.FilID), boolToInt(a.Searched))
	if err != nil {
		return fmt.Errorf("store: record import of %s: %w", a.Path, err)
	}
	return nil
}

// ImportOf returns what importing the file with this hash last gave; false
// if it was never tried.
func (db *DB) ImportOf(ctx context.Context, sha256 string) (ImportAttempt, bool, error) {
	var a ImportAttempt
	var at string
	var searched int
	err := db.read.QueryRowContext(ctx, `
SELECT sha256, path, outcome, ifnull(reason, ''), ifnull(fil_id, ''), searched, at
FROM import_attempt WHERE sha256 = ?;`, sha256).Scan(&a.SHA256, &a.Path, &a.Outcome, &a.Reason, &a.FilID, &searched, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return ImportAttempt{}, false, nil
	}
	if err != nil {
		return ImportAttempt{}, false, fmt.Errorf("store: read import of %s: %w", sha256, err)
	}
	a.Searched = searched != 0
	a.At, _ = time.Parse(time.DateTime, at)
	return a, true, nil
}

// AddLocalWork records a document OpenAlex does not know: a fil ID, the
// title the user gave it, no OpenAlex ID and no references (ADR-014). It is
// a seed the user uploaded, and it is never on the expansion frontier.
func (tx *Tx) AddLocalWork(ctx context.Context, title string, year *int) (string, error) {
	fil := tx.newID()
	_, err := tx.tx.ExecContext(ctx, `
INSERT INTO work (fil_id, title, year, hydrated, fetched_refs, depth, is_seed, source)
VALUES (?, ?, ?, 1, 1, 0, 1, ?);`, fil, title, year, string(model.SourceUpload))
	if err != nil {
		return "", fmt.Errorf("store: add local document %q: %w", title, err)
	}
	return fil, nil
}
