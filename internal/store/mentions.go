package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/codevector-2003/filiation/internal/model"
)

// CitedRef is one work a paper cites, with what citation context needs to
// recognise it in the paper's text.
type CitedRef struct {
	FilID   string
	Title   string // "" when not known: a stub never detailed
	Year    int    // 0 when not known
	DOI     string
	Surname string // the first author's, "" when not known
}

// RefsOf returns the works ref cites, as OpenAlex gave them.
func (db *DB) RefsOf(ctx context.Context, ref string) ([]CitedRef, error) {
	fil, err := resolveRef(ctx, db.read, ref)
	if err != nil {
		return nil, err
	}
	rows, err := db.read.QueryContext(ctx, `
SELECT w.fil_id, ifnull(w.title, ''), ifnull(w.year, 0), ifnull(w.doi, ''),
       ifnull((SELECT a.name FROM authorship s JOIN author a ON a.openalex_id = s.author_id
               WHERE s.work_id = w.fil_id ORDER BY s.position LIMIT 1), '')
FROM cites c JOIN work w ON w.fil_id = c.to_work
WHERE c.from_work = ?
ORDER BY c.rowid;`, fil)
	if err != nil {
		return nil, fmt.Errorf("store: read references of %s: %w", ref, err)
	}
	defer rows.Close()
	var out []CitedRef
	for rows.Next() {
		var r CitedRef
		var name string
		if err := rows.Scan(&r.FilID, &r.Title, &r.Year, &r.DOI, &name); err != nil {
			return nil, fmt.Errorf("store: read references of %s: %w", ref, err)
		}
		if f := strings.Fields(name); len(f) > 0 {
			r.Surname = f[len(f)-1]
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// StubsCitedBy returns the OpenAlex IDs of the works the given papers cite
// that are stubs with no title yet: what citation context needs details of.
func (db *DB) StubsCitedBy(ctx context.Context, filIDs []string) ([]string, error) {
	if len(filIDs) == 0 {
		return nil, nil
	}
	args := make([]any, len(filIDs))
	for i, id := range filIDs {
		args[i] = id
	}
	rows, err := db.read.QueryContext(ctx, `
SELECT DISTINCT w.openalex_id
FROM cites c JOIN work w ON w.fil_id = c.to_work
WHERE c.from_work IN (?`+strings.Repeat(",?", len(filIDs)-1)+`)
  AND w.hydrated = 0 AND w.unresolved = 0 AND w.title IS NULL AND w.openalex_id IS NOT NULL
ORDER BY w.openalex_id;`, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list cited stubs: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("store: list cited stubs: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// PutDetails gives a stub its title, year and authors without hydrating
// it: its own references are not recorded, so the graph does not grow, and
// it stays on the expansion frontier as a work still to fetch. A work
// already hydrated is left alone.
func (tx *Tx) PutDetails(ctx context.Context, h *model.HydratedWork) error {
	fil, found, err := filIDOf(ctx, tx.tx, h.OpenAlexID)
	if err != nil || !found {
		return err
	}
	res, err := tx.tx.ExecContext(ctx, `
UPDATE work SET title = ?1, year = ?2 WHERE fil_id = ?3 AND hydrated = 0;`, h.Title, h.Year, fil)
	if err != nil {
		return fmt.Errorf("store: details of %s: %w", h.OpenAlexID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 || !h.AuthorsLoaded() {
		return nil
	}
	return writeAuthors(ctx, tx.tx, fil, h.Authors)
}

// Mention is one place a paper cites another.
type Mention struct {
	ToWork     string // fil ID of the cited work
	Page       int
	Start, End int // byte offsets of Sentence in its page's cleaned text
	Sentence   string
	Section    string
	Marker     string
	Method     string // numeric | author-year
	Confidence float64
}

// PutMentions replaces what a paper's PDF says about whom it cites, and sets
// each edge's display context to its first mention. A mention for a work
// the paper does not cite in OpenAlex's list is refused: context attaches to
// edges, never makes them (ADR-015).
func (tx *Tx) PutMentions(ctx context.Context, from string, ms []Mention) error {
	fil, err := resolveRef(ctx, tx.tx, from)
	if err != nil {
		return err
	}
	for _, q := range []string{
		`DELETE FROM mention WHERE from_work = ?;`,
		`UPDATE cites SET context = NULL, section = NULL, confidence = NULL WHERE from_work = ?;`,
	} {
		if _, err := tx.tx.ExecContext(ctx, q, fil); err != nil {
			return fmt.Errorf("store: replace mentions of %s: %w", from, err)
		}
	}
	for _, m := range ms {
		res, err := tx.tx.ExecContext(ctx, `
INSERT INTO mention (from_work, to_work, page, char_start, char_end, sentence, section, marker, method, confidence)
SELECT ?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10
WHERE EXISTS (SELECT 1 FROM cites WHERE from_work = ?1 AND to_work = ?2);`,
			fil, m.ToWork, m.Page, m.Start, m.End, m.Sentence, nullString(m.Section), m.Marker, m.Method, m.Confidence)
		if err != nil {
			return fmt.Errorf("store: write mention in %s: %w", from, err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("store: %s does not cite %s: a mention attaches to an edge, never makes one", from, m.ToWork)
		}
	}
	_, err = tx.tx.ExecContext(ctx, `
UPDATE cites SET
  context    = (SELECT m.sentence   FROM mention m WHERE m.from_work = cites.from_work AND m.to_work = cites.to_work ORDER BY m.page, m.char_start LIMIT 1),
  section    = (SELECT m.section    FROM mention m WHERE m.from_work = cites.from_work AND m.to_work = cites.to_work ORDER BY m.page, m.char_start LIMIT 1),
  confidence = (SELECT max(m.confidence) FROM mention m WHERE m.from_work = cites.from_work AND m.to_work = cites.to_work)
WHERE from_work = ?;`, fil)
	if err != nil {
		return fmt.Errorf("store: set contexts of %s: %w", from, err)
	}
	return nil
}

// MentionsOf returns the mentions in a paper, in reading order.
func (db *DB) MentionsOf(ctx context.Context, ref string) ([]Mention, error) {
	fil, err := resolveRef(ctx, db.read, ref)
	if err != nil {
		return nil, err
	}
	rows, err := db.read.QueryContext(ctx, `
SELECT to_work, page, char_start, char_end, sentence, ifnull(section, ''), marker, method, confidence
FROM mention WHERE from_work = ? ORDER BY page, char_start, id;`, fil)
	if err != nil {
		return nil, fmt.Errorf("store: read mentions of %s: %w", ref, err)
	}
	defer rows.Close()
	var out []Mention
	for rows.Next() {
		var m Mention
		if err := rows.Scan(&m.ToWork, &m.Page, &m.Start, &m.End, &m.Sentence, &m.Section, &m.Marker, &m.Method, &m.Confidence); err != nil {
			return nil, fmt.Errorf("store: read mentions of %s: %w", ref, err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ContextCounts is how far citation context reaches: of the citations made
// by papers whose PDF has text, how many carry a sentence.
type ContextCounts struct {
	Citations   int // edges from papers with text
	WithContext int
	Mentions    int
}

// ContextCounts counts citation context across the library.
func (db *DB) ContextCounts(ctx context.Context) (ContextCounts, error) {
	var c ContextCounts
	err := db.read.QueryRowContext(ctx, `
SELECT count(*), count(c.context),
       (SELECT count(*) FROM mention)
FROM cites c JOIN work_text t ON t.fil_id = c.from_work AND t.status = 'ok';`).Scan(&c.Citations, &c.WithContext, &c.Mentions)
	if err != nil {
		return c, fmt.Errorf("store: count citation context: %w", err)
	}
	return c, nil
}
