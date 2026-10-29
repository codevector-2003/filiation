package store

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// ListQuery filters and orders a listing of the library.
type ListQuery struct {
	Seeds       bool   // only the papers the user added or imported
	WithPDF     bool   // only papers with a PDF on disk
	WithoutText bool   // only papers whose PDF gave no searchable text
	Collection  string // only this collection's papers
	Title       string // only titles containing all these words
	Limit       int    // zero means no limit
}

// ListRow is one paper in a listing, with its state in one row: what the
// list shows beside each title.
type ListRow struct {
	FilID      string
	OpenAlexID string
	Title      string
	Year       int
	Author     string // first author's name, "" when not known
	Seed       bool
	Hydrated   bool
	CitedBy    int    // in this library
	HasPDF     bool   // a PDF on disk
	TextStatus string // "", ok, no-text, gibberish, unreadable, timed-out
	Contexts   int    // references cited in a sentence fil found
	Mismatch   bool   // its downloaded PDF names another paper or edition
}

// List lists the library's papers: seeds first, then the most cited within
// the library, then the order they arrived.
func (db *DB) List(ctx context.Context, q ListQuery) ([]ListRow, error) {
	where := []string{"1 = 1"}
	var args []any
	if q.Seeds {
		where = append(where, "w.is_seed = 1")
	}
	if q.WithPDF {
		where = append(where, "EXISTS (SELECT 1 FROM work_file f WHERE f.fil_id = w.fil_id AND f.missing = 0 AND lower(f.path) LIKE '%.pdf')")
	}
	if q.WithoutText {
		where = append(where, "EXISTS (SELECT 1 FROM work_text t WHERE t.fil_id = w.fil_id AND t.status <> 'ok')")
	}
	if q.Collection != "" {
		where = append(where, "EXISTS (SELECT 1 FROM collection_work cw JOIN collection c ON c.id = cw.collection_id WHERE cw.work_id = w.fil_id AND c.name = ?)")
		args = append(args, q.Collection)
	}
	// Title words are matched here in Go, not with SQL's lower() and LIKE,
	// which fold only ASCII: "étude" must find "Étude", and "etude" too
	// (found in review, PR #12). The limit then applies after the match.
	words := strings.Fields(foldTitle(q.Title))
	if len(words) > 0 {
		where = append(where, "w.title IS NOT NULL")
	}
	limit := ""
	if q.Limit > 0 && len(words) == 0 {
		limit = fmt.Sprintf(" LIMIT %d", q.Limit)
	}
	rows, err := db.read.QueryContext(ctx, `
SELECT w.fil_id, ifnull(w.openalex_id, ''), ifnull(w.title, ''), ifnull(w.year, 0),
       ifnull((SELECT a.name FROM authorship s JOIN author a ON a.openalex_id = s.author_id
               WHERE s.work_id = w.fil_id ORDER BY s.position LIMIT 1), ''),
       w.is_seed, w.hydrated,
       (SELECT count(*) FROM cites c WHERE c.to_work = w.fil_id) AS cited_by,
       EXISTS (SELECT 1 FROM work_file f WHERE f.fil_id = w.fil_id AND f.missing = 0 AND lower(f.path) LIKE '%.pdf'),
       ifnull((SELECT t.status FROM work_text t WHERE t.fil_id = w.fil_id), ''),
       (SELECT count(*) FROM cites c WHERE c.from_work = w.fil_id AND c.context IS NOT NULL),
       EXISTS (SELECT 1 FROM work_file f WHERE f.fil_id = w.fil_id AND f.missing = 0 AND f.identity = 'mismatch')
FROM work w
WHERE `+strings.Join(where, " AND ")+`
ORDER BY w.is_seed DESC, cited_by DESC, w.rowid`+limit+`;`, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list the library: %w", err)
	}
	defer rows.Close()
	var out []ListRow
	for rows.Next() {
		var r ListRow
		if err := rows.Scan(&r.FilID, &r.OpenAlexID, &r.Title, &r.Year, &r.Author, &r.Seed, &r.Hydrated,
			&r.CitedBy, &r.HasPDF, &r.TextStatus, &r.Contexts, &r.Mismatch); err != nil {
			return nil, fmt.Errorf("store: list the library: %w", err)
		}
		if !hasAll(foldTitle(r.Title), words) {
			continue
		}
		out = append(out, r)
		if q.Limit > 0 && len(out) == q.Limit {
			break
		}
	}
	return out, rows.Err()
}

// foldTitle lower-cases a title and drops its accents, Unicode-aware.
func foldTitle(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(s) {
		if !unicode.Is(unicode.Mn, r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

func hasAll(s string, words []string) bool {
	for _, w := range words {
		if !strings.Contains(s, w) {
			return false
		}
	}
	return true
}

// WithoutText lists the papers whose PDF gave no searchable text, with the
// status and the reason, grouped by status.
func (db *DB) WithoutText(ctx context.Context) ([]Text, error) {
	rows, err := db.read.QueryContext(ctx, `
SELECT fil_id, path, sha256, status, ifnull(reason, ''), ifnull(pages, 0), ifnull(chars, 0), extractor, ifnull(millis, 0)
FROM work_text WHERE status <> 'ok' ORDER BY status, fil_id;`)
	if err != nil {
		return nil, fmt.Errorf("store: list papers without text: %w", err)
	}
	defer rows.Close()
	var out []Text
	for rows.Next() {
		var t Text
		if err := rows.Scan(&t.FilID, &t.Path, &t.SHA256, &t.Status, &t.Reason, &t.Pages, &t.Chars, &t.Extractor, &t.Millis); err != nil {
			return nil, fmt.Errorf("store: list papers without text: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// File identity, as checked against the paper it is filed under.
const (
	IdentityMatch    = "match"    // its first pages carry the paper's title and author
	IdentityMismatch = "mismatch" // they do not: another paper, or another edition
)

// SetIdentity records whether a file's first pages name its paper.
func (tx *Tx) SetIdentity(ctx context.Context, path, identity string) error {
	if _, err := tx.tx.ExecContext(ctx, `UPDATE work_file SET identity = ? WHERE path = ?;`, identity, path); err != nil {
		return fmt.Errorf("store: record identity of %s: %w", path, err)
	}
	return nil
}
