package store

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// SearchQuery is one keyword search.
type SearchQuery struct {
	Words      string // what the person typed; see MatchQuery
	Limit      int    // passages to return
	PerPaper   int    // most passages from one paper; zero means no limit
	FilID      string // only this paper, when set
	References bool   // include reference lists, left out by default
	// Within keeps the search to these papers, by fil ID, when InSet is set:
	// a filter of the Papers list, applied before the best passages are
	// chosen so that none of its papers' matches are lost to others.
	Within []string
	InSet  bool
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
	Rank    float64 // the passage's own bm25: lower is better
	// Every says the passage has every word searched for. When none has,
	// the search returns passages with some of them, and Every is false.
	Every bool
}

// MarkStart and MarkEnd surround the matched words in a Hit's Snippet. They
// are control characters, which cleaned text never contains, so a front
// door can render them as it likes without mistaking a bracket for a match.
const (
	MarkStart = "\x02"
	MarkEnd   = "\x03"
)

// Search finds passages by keyword, grouped by paper, the best paper first
// (D35), with words matched by stem: "citing" finds "cited".
//
// The words are what the person typed, less the filler words a question is
// made of ("how", "the", "does"), unless that would leave none. When some
// passage has every word left, only such passages count; otherwise those with
// any of them do, and their hits say so.
//
// A paper's place fuses two rankings: its best passages, the best counting
// fully, the second half as much, the third a third; and its title and
// abstract. One stray passage that shares a few common words then counts for
// less than a paper about the subject. Measured on eval/retrieval, this finds
// the paper that answers a question more often than ranking each passage on
// its own did.
func (db *DB) Search(ctx context.Context, q SearchQuery) ([]Hit, error) {
	terms, err := matchTerms(q.Words)
	if err != nil {
		return nil, err
	}
	terms = subjectTerms(terms)
	every, anyOf := strings.Join(terms, " "), strings.Join(terms, " OR ")

	fil := ""
	if q.FilID != "" {
		if fil, err = resolveRef(ctx, db.read, q.FilID); err != nil {
			return nil, err
		}
	}
	within := ""
	if q.InSet {
		b, err := json.Marshal(append([]string{}, q.Within...))
		if err != nil {
			return nil, err
		}
		within = string(b)
	}
	perPaper := q.PerPaper
	if perPaper <= 0 {
		perPaper = q.Limit
	}
	// Passages are those of read works, as fil search has always shown.
	const where = `chunk_fts MATCH ?1
    AND (?2 OR c.section IS NULL)
    AND (?3 = '' OR c.work_id = ?3)
    AND (?4 = '' OR c.work_id IN (SELECT value FROM json_each(?4)))`
	const from = `FROM chunk_fts JOIN chunk c ON c.id = chunk_fts.rowid JOIN work_text t ON t.fil_id = c.work_id`

	match, all := every, true
	var found bool
	if err := db.read.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 `+from+` WHERE `+where+`);`,
		every, q.References, fil, within).Scan(&found); err != nil {
		return nil, fmt.Errorf("store: search for %q: %w", q.Words, err)
	}
	if !found {
		if len(terms) == 1 {
			return nil, nil
		}
		match, all = anyOf, false
	}

	// Every matching passage's score, best first. A cap keeps a search for
	// a word in every passage of a very large library bounded; the best
	// passages are the ones kept.
	rows, err := db.read.QueryContext(ctx, `SELECT c.id, c.work_id, bm25(chunk_fts) `+from+` WHERE `+where+`
ORDER BY bm25(chunk_fts) LIMIT 20000;`, match, q.References, fil, within)
	if err != nil {
		return nil, fmt.Errorf("store: search for %q: %w", q.Words, err)
	}
	type passage struct {
		id   int64
		rank float64
	}
	byPaper := map[string][]passage{}
	var papers []string
	for rows.Next() {
		var p passage
		var w string
		if err := rows.Scan(&p.id, &w, &p.rank); err != nil {
			rows.Close()
			return nil, fmt.Errorf("store: search for %q: %w", q.Words, err)
		}
		if _, ok := byPaper[w]; !ok {
			papers = append(papers, w)
		}
		byPaper[w] = append(byPaper[w], p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: search for %q: %w", q.Words, err)
	}
	if len(papers) == 0 {
		return nil, nil
	}

	// By passages: bm25 is negative, more negative better.
	score := map[string]float64{}
	for w, ps := range byPaper {
		for i, p := range ps[:min(3, len(ps))] {
			score[w] += -p.rank / float64(i+1)
		}
	}
	sort.SliceStable(papers, func(i, j int) bool {
		if score[papers[i]] != score[papers[j]] {
			return score[papers[i]] > score[papers[j]]
		}
		return papers[i] < papers[j]
	})
	if len(papers) > 1 {
		if papers, err = db.byTitleToo(ctx, papers, anyOf); err != nil {
			return nil, fmt.Errorf("store: search for %q: %w", q.Words, err)
		}
	}

	// The best passages of the best papers, in that order.
	var chosen []int64
	for _, w := range papers {
		for _, p := range byPaper[w][:min(perPaper, len(byPaper[w]))] {
			chosen = append(chosen, p.id)
			if len(chosen) == q.Limit {
				break
			}
		}
		if len(chosen) == q.Limit {
			break
		}
	}
	return db.hits(ctx, q.Words, match, chosen, all)
}

// byTitleToo reorders papers, already ranked by their passages, by fusing
// that ranking with how well each one's title and abstract match: reciprocal
// rank fusion, 1/(60 + rank) from each, which needs no weights fitted to any
// question set. Only these papers are ranked by title, so a paper with no
// matching passage never enters.
func (db *DB) byTitleToo(ctx context.Context, papers []string, match string) ([]string, error) {
	ids, err := json.Marshal(papers)
	if err != nil {
		return nil, err
	}
	rows, err := db.read.QueryContext(ctx, `
SELECT s.fil_id FROM work_fts JOIN work_search s ON s.id = work_fts.rowid
WHERE work_fts MATCH ?1 AND s.fil_id IN (SELECT value FROM json_each(?2))
ORDER BY bm25(work_fts), s.fil_id;`, match, string(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	const k = 60
	fused := map[string]float64{}
	for i, w := range papers {
		fused[w] = 1 / float64(k+i+1)
	}
	for i := 0; rows.Next(); i++ {
		var w string
		if err := rows.Scan(&w); err != nil {
			return nil, err
		}
		fused[w] += 1 / float64(k+i+1)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := append([]string(nil), papers...)
	sort.SliceStable(out, func(i, j int) bool { return fused[out[i]] > fused[out[j]] })
	return out, nil
}

// hits reads the chosen passages, keeping their order.
func (db *DB) hits(ctx context.Context, words, match string, chosen []int64, all bool) ([]Hit, error) {
	ids, err := json.Marshal(chosen)
	if err != nil {
		return nil, err
	}
	rows, err := db.read.QueryContext(ctx, `
SELECT c.id, c.work_id, c.page, ifnull(c.section, ''), snippet(chunk_fts, 0, ?2, ?3, '…', 32), bm25(chunk_fts),
       ifnull((SELECT f.path FROM work_file f
               WHERE f.fil_id = t.fil_id AND f.sha256 = t.sha256 AND f.missing = 0
               ORDER BY f.path LIMIT 1), t.path)
FROM chunk_fts JOIN chunk c ON c.id = chunk_fts.rowid JOIN work_text t ON t.fil_id = c.work_id
WHERE chunk_fts MATCH ?1 AND chunk_fts.rowid IN (SELECT value FROM json_each(?4));`,
		match, MarkStart, MarkEnd, string(ids))
	if err != nil {
		return nil, fmt.Errorf("store: search for %q: %w", words, err)
	}
	defer rows.Close()
	byID := map[int64]Hit{}
	for rows.Next() {
		var id int64
		h := Hit{Every: all}
		if err := rows.Scan(&id, &h.FilID, &h.Page, &h.Section, &h.Snippet, &h.Rank, &h.Path); err != nil {
			return nil, fmt.Errorf("store: search for %q: %w", words, err)
		}
		byID[id] = h
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: search for %q: %w", words, err)
	}
	out := make([]Hit, 0, len(chosen))
	for _, id := range chosen {
		if h, ok := byID[id]; ok {
			out = append(out, h)
		}
	}
	return out, nil
}

// subjectTerms drops the filler words from a search's terms, unless that
// would leave none: a phrase, a prefix and any other word stay.
func subjectTerms(terms []string) []string {
	var out []string
	for _, t := range terms {
		w := strings.ToLower(strings.TrimFunc(t, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }))
		if !strings.HasSuffix(t, "*") && fillerWords[w] {
			continue
		}
		out = append(out, t)
	}
	if len(out) == 0 {
		return terms
	}
	return out
}

// fillerWords carry no subject: function words, and the words questions are
// made of. A question searched for word by word matches them everywhere.
var fillerWords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`a about above after again against all also am an and any are as at be because
	been before being below between both but by can could did do does doing down during each few for from further
	get got had has have having he her here hers herself him himself his how i if in instead into is it its itself
	just made make many may me might more most much must my myself no nor not now of off on once one ones only or
	other our ours ourselves out over own quite rather really roughly same shall she should so some such than that
	the their theirs them themselves then there these they this those through to too truly under until up very was
	we were what when where whether which while who whom why will with would you your yours yourself yourselves`) {
		fillerWords[w] = true
	}
}
