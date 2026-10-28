package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/codevector-2003/filiation/internal/errs"
)

// Text is one row of work_text: what reading a paper's PDF gave.
type Text struct {
	FilID     string
	Path      string // the PDF read, relative to the library folder
	SHA256    string
	Status    string // text.Status: ok | no-text | gibberish | unreadable | timed-out
	Reason    string
	Pages     int
	Chars     int
	Extractor string
	Millis    int64
	ReadAt    time.Time
}

// Chunk is one passage of a paper's text.
type Chunk struct {
	Page       int
	Ordinal    int
	Section    string // "references", or "" for the body
	Start, End int    // byte offsets in the page's cleaned text
	Text       string
}

// PutText records what reading a paper's PDF gave, replacing its passages:
// a paper's text is always one reading of one file, never two mixed.
func (tx *Tx) PutText(ctx context.Context, t Text, chunks []Chunk) error {
	fil, err := resolveRef(ctx, tx.tx, t.FilID)
	if err != nil {
		return err
	}
	// The delete trigger keeps chunk_fts in step.
	if _, err := tx.tx.ExecContext(ctx, `DELETE FROM chunk WHERE work_id = ?;`, fil); err != nil {
		return fmt.Errorf("store: replace text of %s: %w", t.FilID, err)
	}
	for _, c := range chunks {
		if _, err := tx.tx.ExecContext(ctx, `
INSERT INTO chunk (work_id, section, page, ordinal, text, char_start, char_end)
VALUES (?, ?, ?, ?, ?, ?, ?);`,
			fil, nullString(c.Section), c.Page, c.Ordinal, c.Text, c.Start, c.End); err != nil {
			return fmt.Errorf("store: write text of %s: %w", t.FilID, err)
		}
	}
	_, err = tx.tx.ExecContext(ctx, `
INSERT INTO work_text (fil_id, path, sha256, status, reason, pages, chars, extractor, millis, read_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
ON CONFLICT (fil_id) DO UPDATE SET
  path = excluded.path, sha256 = excluded.sha256, status = excluded.status,
  reason = excluded.reason, pages = excluded.pages, chars = excluded.chars,
  extractor = excluded.extractor, millis = excluded.millis, read_at = excluded.read_at;`,
		fil, t.Path, t.SHA256, t.Status, nullString(t.Reason), t.Pages, t.Chars, t.Extractor, t.Millis)
	if err != nil {
		return fmt.Errorf("store: record text of %s: %w", t.FilID, err)
	}
	return nil
}

// TextOf returns what reading a paper's PDF gave; false if it was never read.
func (db *DB) TextOf(ctx context.Context, ref string) (Text, bool, error) {
	fil, err := resolveRef(ctx, db.read, ref)
	if err != nil {
		return Text{}, false, err
	}
	var t Text
	var at string
	err = db.read.QueryRowContext(ctx, `
SELECT fil_id, path, sha256, status, ifnull(reason, ''), ifnull(pages, 0), ifnull(chars, 0),
       extractor, ifnull(millis, 0), read_at
FROM work_text WHERE fil_id = ?;`, fil).Scan(
		&t.FilID, &t.Path, &t.SHA256, &t.Status, &t.Reason, &t.Pages, &t.Chars, &t.Extractor, &t.Millis, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return Text{}, false, nil
	}
	if err != nil {
		return Text{}, false, fmt.Errorf("store: read text of %s: %w", ref, err)
	}
	t.ReadAt, _ = time.Parse(time.DateTime, at)
	return t, true, nil
}

// ToRead is a PDF to extract text from.
type ToRead struct {
	FilID  string
	Path   string
	SHA256 string
}

// chosenPDF picks each paper's one PDF to read: published, then accepted,
// then preprint, then any other, the earliest recorded first. Only files
// present on disk count.
const chosenPDF = `
WITH pdf AS (
  SELECT f.fil_id, f.path, f.sha256,
         row_number() OVER (PARTITION BY f.fil_id ORDER BY
           CASE f.version WHEN 'published' THEN 0 WHEN 'accepted' THEN 1
                          WHEN 'preprint' THEN 2 ELSE 3 END,
           f.added_at, f.path) AS n
  FROM work_file f
  WHERE f.missing = 0 AND lower(f.path) LIKE '%.pdf'
)`

// PDFsToRead returns the papers whose PDF has not been read: never read,
// changed since (another hash), or read by an older extractor. With again,
// every paper with a PDF on disk. Seeds come first.
func (db *DB) PDFsToRead(ctx context.Context, extractor string, again bool) ([]ToRead, error) {
	rows, err := db.read.QueryContext(ctx, chosenPDF+`
SELECT p.fil_id, p.path, p.sha256
FROM pdf p JOIN work w ON w.fil_id = p.fil_id
LEFT JOIN work_text t ON t.fil_id = p.fil_id
WHERE p.n = 1
  AND (?2 OR t.fil_id IS NULL OR t.sha256 <> p.sha256 OR t.extractor <> ?1)
ORDER BY w.is_seed DESC, w.rowid;`, extractor, again)
	if err != nil {
		return nil, fmt.Errorf("store: list PDFs to read: %w", err)
	}
	defer rows.Close()
	var out []ToRead
	for rows.Next() {
		var r ToRead
		if err := rows.Scan(&r.FilID, &r.Path, &r.SHA256); err != nil {
			return nil, fmt.Errorf("store: list PDFs to read: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// PDFToRead returns the PDF a paper's text comes from; false if it has none
// on disk.
func (db *DB) PDFToRead(ctx context.Context, ref string) (ToRead, bool, error) {
	fil, err := resolveRef(ctx, db.read, ref)
	if err != nil {
		return ToRead{}, false, err
	}
	var r ToRead
	err = db.read.QueryRowContext(ctx, chosenPDF+`
SELECT fil_id, path, sha256 FROM pdf WHERE n = 1 AND fil_id = ?;`, fil).Scan(&r.FilID, &r.Path, &r.SHA256)
	if errors.Is(err, sql.ErrNoRows) {
		return ToRead{}, false, nil
	}
	if err != nil {
		return ToRead{}, false, fmt.Errorf("store: find the PDF of %s: %w", ref, err)
	}
	return r, true, nil
}

// TextCounts counts the papers by what reading their PDF gave, keyed by
// status.
func (db *DB) TextCounts(ctx context.Context) (map[string]int, error) {
	rows, err := db.read.QueryContext(ctx, `SELECT status, count(*) FROM work_text GROUP BY status;`)
	if err != nil {
		return nil, fmt.Errorf("store: count texts: %w", err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var s string
		var n int
		if err := rows.Scan(&s, &n); err != nil {
			return nil, fmt.Errorf("store: count texts: %w", err)
		}
		out[s] = n
	}
	return out, rows.Err()
}

// SearchQuery is one keyword search.
type SearchQuery struct {
	Words      string // what the person typed; see MatchQuery
	Limit      int    // passages to return
	PerPaper   int    // most passages from one paper; zero means no limit
	FilID      string // only this paper, when set
	References bool   // include reference lists, left out by default
}

// Hit is one passage that matched.
type Hit struct {
	FilID   string
	Page    int
	Section string
	// Snippet is the matching stretch of the passage, with each matched word
	// between MarkStart and MarkEnd.
	Snippet string
	Path    string  // the PDF it came from, relative to the library folder
	Rank    float64 // bm25: lower is better
}

// MarkStart and MarkEnd surround the matched words in a Hit's Snippet. They
// are control characters, which cleaned text never contains, so a front
// door can render them as it likes without mistaking a bracket for a match.
const (
	MarkStart = "\x02"
	MarkEnd   = "\x03"
)

// Search finds passages by keyword, best first (FTS5's bm25), with words
// matched by stem — "citing" finds "cited".
func (db *DB) Search(ctx context.Context, q SearchQuery) ([]Hit, error) {
	match, err := MatchQuery(q.Words)
	if err != nil {
		return nil, err
	}
	fil := ""
	if q.FilID != "" {
		if fil, err = resolveRef(ctx, db.read, q.FilID); err != nil {
			return nil, err
		}
	}
	perPaper := q.PerPaper
	if perPaper <= 0 {
		perPaper = q.Limit
	}
	rows, err := db.read.QueryContext(ctx, `
WITH hits AS (
  SELECT c.work_id, c.page, ifnull(c.section, '') AS section, bm25(chunk_fts) AS rank,
         snippet(chunk_fts, 0, ?5, ?6, '…', 32) AS snip
  FROM chunk_fts JOIN chunk c ON c.id = chunk_fts.rowid
  WHERE chunk_fts MATCH ?1
    AND (?2 OR c.section IS NULL)
    AND (?3 = '' OR c.work_id = ?3)
), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY work_id ORDER BY rank) AS n FROM hits
)
SELECT r.work_id, r.page, r.section, r.snip, r.rank,
       ifnull((SELECT f.path FROM work_file f
               WHERE f.fil_id = t.fil_id AND f.sha256 = t.sha256 AND f.missing = 0
               ORDER BY f.path LIMIT 1), t.path)
FROM ranked r JOIN work_text t ON t.fil_id = r.work_id
WHERE r.n <= ?4
ORDER BY r.rank
LIMIT ?7;`, match, q.References, fil, perPaper, MarkStart, MarkEnd, q.Limit)
	if err != nil {
		return nil, fmt.Errorf("store: search for %q: %w", q.Words, err)
	}
	defer rows.Close()
	var out []Hit
	for rows.Next() {
		var h Hit
		if err := rows.Scan(&h.FilID, &h.Page, &h.Section, &h.Snippet, &h.Rank, &h.Path); err != nil {
			return nil, fmt.Errorf("store: search for %q: %w", q.Words, err)
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// MatchQuery turns what a person typed into an FTS5 query that cannot be a
// syntax error: every word, and every "quoted phrase", becomes a quoted FTS5
// string, and all must match. A word ending in * matches as a prefix. FTS5's
// own operators — AND, OR, NOT, NEAR, column filters, parentheses — are not
// offered: typed by someone who does not know them, they fail or surprise.
func MatchQuery(typed string) (string, error) {
	// A control character separates words; inside an FTS5 string a NUL
	// would end the statement.
	words := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, typed)
	var terms []string
	add := func(s string, prefix bool) {
		if !strings.ContainsFunc(s, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) {
			return // nothing the tokenizer would keep: "-", "+", "&"
		}
		t := `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
		if prefix {
			t += "*"
		}
		terms = append(terms, t)
	}
	rest := words
	for {
		rest = strings.TrimLeftFunc(rest, unicode.IsSpace)
		if rest == "" {
			break
		}
		if rest[0] == '"' {
			phrase, after, closed := strings.Cut(rest[1:], `"`)
			add(phrase, false)
			if !closed {
				break
			}
			rest = after
			continue
		}
		end := strings.IndexFunc(rest, func(r rune) bool { return unicode.IsSpace(r) || r == '"' })
		if end < 0 {
			end = len(rest)
		}
		word := rest[:end]
		rest = rest[end:]
		prefix := strings.HasSuffix(word, "*")
		add(strings.TrimRight(word, "*"), prefix)
	}
	if len(terms) == 0 {
		return "", fmt.Errorf("store: search for %q: %w", typed, errs.ErrEmptyQuery)
	}
	return strings.Join(terms, " "), nil
}
