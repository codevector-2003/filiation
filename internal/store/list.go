package store

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/codevector-2003/filiation/internal/errs"
)

// ListQuery filters and orders a listing of the library.
type ListQuery struct {
	Seeds        bool     // only the papers the user added or imported
	WithPDF      bool     // only papers with a PDF on disk
	WithoutText  bool     // only papers whose PDF gave no searchable text
	Collection   string   // only this collection's papers, by its stored name
	CollectionID int64    // only the papers in this collection, or one inside it
	Labels       []int64  // only papers carrying every one of these labels
	Favourites   bool     // only starred papers
	Title        string   // only titles containing all these words
	YearFrom     int      // only this year or later; zero means no bound
	YearTo       int      // only this year or earlier; zero means no bound
	Authors      []string // only papers by any of these authors, by name
	Venues       []string // only papers in any of these venues
	Among        []string // only these papers, by fil ID, when AmongSet is set
	AmongSet     bool     // so an empty Among finds nothing rather than everything
	Sort         string   // a key of listOrders; "" is seeds first, then most cited
	Offset       int      // skip this many, for the next page
	Limit        int      // zero means no limit

	within []int64 // CollectionID and the collections inside it, as inCollection found them
}

// listOrders are the orders a listing can take. A work with no year or title
// goes last either way, since a stub has neither. Titles compare without case
// for ASCII only, so "Étude" sorts after "Z".
var listOrders = map[string]string{
	"":           "w.is_seed DESC, cited_by DESC, w.rowid",
	"newest":     "ifnull(w.year, 0) = 0, w.year DESC, w.rowid",
	"oldest":     "ifnull(w.year, 0) = 0, w.year, w.rowid",
	"title":      "w.title IS NULL, w.title COLLATE NOCASE, w.rowid",
	"cited":      "cited_by DESC, w.rowid",
	"citedworld": "ifnull(w.cited_by_count, 0) DESC, w.rowid",
	"added":      "w.added_at DESC, w.rowid DESC",
	"arrived":    "w.rowid",
}

// conditions turns the query into SQL, leaving out the filter named by skip:
// a filter's own options are counted as if it were not set, or choosing one
// author would hide all the others.
func (q ListQuery) conditions(skip string) ([]string, []any) {
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
	if len(q.within) > 0 {
		where = append(where, "EXISTS (SELECT 1 FROM collection_work cw WHERE cw.work_id = w.fil_id AND cw.collection_id IN (?"+strings.Repeat(",?", len(q.within)-1)+"))")
		for _, id := range q.within {
			args = append(args, id)
		}
	}
	if q.Favourites {
		where = append(where, "EXISTS (SELECT 1 FROM favourite v WHERE v.work_id = w.fil_id)")
	}
	for _, id := range q.Labels {
		where = append(where, "EXISTS (SELECT 1 FROM work_label wl WHERE wl.work_id = w.fil_id AND wl.label_id = ?)")
		args = append(args, id)
	}
	if skip != "year" && q.YearFrom > 0 {
		where = append(where, "w.year >= ?")
		args = append(args, q.YearFrom)
	}
	if skip != "year" && q.YearTo > 0 {
		where = append(where, "w.year <= ?")
		args = append(args, q.YearTo)
	}
	if skip != "author" && len(q.Authors) > 0 {
		where = append(where, "EXISTS (SELECT 1 FROM authorship s JOIN author a ON a.openalex_id = s.author_id WHERE s.work_id = w.fil_id AND a.name IN (?"+strings.Repeat(",?", len(q.Authors)-1)+"))")
		for _, a := range q.Authors {
			args = append(args, a)
		}
	}
	if skip != "venue" && len(q.Venues) > 0 {
		where = append(where, "w.venue IN (?"+strings.Repeat(",?", len(q.Venues)-1)+")")
		for _, v := range q.Venues {
			args = append(args, v)
		}
	}
	if q.AmongSet {
		ids, _ := json.Marshal(append([]string{}, q.Among...))
		where = append(where, "w.fil_id IN (SELECT value FROM json_each(?))")
		args = append(args, string(ids))
	}
	return where, args
}

// ListRow is one paper in a listing, with its state in one row: what the
// list shows beside each title.
type ListRow struct {
	FilID       string
	OpenAlexID  string
	Title       string
	Year        int
	Author      string // first author's name, "" when not known
	Seed        bool
	Hydrated    bool
	RefsKnown   bool   // its reference list was read, even if it turned out empty
	CitedBy     int    // in this library
	CitedWorld  int    // by OpenAlex's count, across everything it indexes
	HasPDF      bool   // a PDF on disk
	TextStatus  string // "", ok, no-text, gibberish, unreadable, timed-out
	Contexts    int    // references cited in a sentence fil found
	Mismatch    bool   // its downloaded PDF names another paper or edition
	Favourite   bool
	Labels      []int64
	Collections []int64 // the ones it is in itself
	Zotero      string  // "" not from Zotero; "matched", or "unmatched": kept as a local document
}

// HavePDFs reports which of these works have a PDF on disk.
func (db *DB) HavePDFs(ctx context.Context, filIDs []string) (map[string]bool, error) {
	out := make(map[string]bool, len(filIDs))
	if len(filIDs) == 0 {
		return out, nil
	}
	args := make([]any, len(filIDs))
	for i, id := range filIDs {
		args[i] = id
	}
	rows, err := db.read.QueryContext(ctx, `
SELECT DISTINCT fil_id FROM work_file
WHERE missing = 0 AND lower(path) LIKE '%.pdf' AND fil_id IN (?`+strings.Repeat(",?", len(filIDs)-1)+`);`, args...)
	if err != nil {
		return nil, fmt.Errorf("find PDFs: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("find PDFs: %w", err)
		}
		out[id] = true
	}
	return out, rows.Err()
}

// List lists the library's papers: unless asked otherwise, seeds first, then
// the most cited within the library, then the order they arrived.
func (db *DB) List(ctx context.Context, q ListQuery) ([]ListRow, error) {
	order, ok := listOrders[q.Sort]
	if !ok {
		return nil, fmt.Errorf("no order %q: %w", q.Sort, errs.ErrInvalidInput)
	}
	q, err := db.inCollection(ctx, q)
	if err != nil {
		return nil, err
	}
	where, args := q.conditions("")
	// Title words are matched here in Go, not with SQL's lower() and LIKE,
	// which fold only ASCII: "étude" must find "Étude", and "etude" too
	// (found in review, PR #12). The page then applies after the match.
	words := strings.Fields(foldTitle(q.Title))
	if len(words) > 0 {
		where = append(where, "w.title IS NOT NULL")
	}
	limit, skip := "", q.Offset
	if q.Limit > 0 && len(words) == 0 {
		limit = fmt.Sprintf(" LIMIT %d OFFSET %d", q.Limit, q.Offset)
		skip = 0
	}
	rows, err := db.read.QueryContext(ctx, `
SELECT w.fil_id, ifnull(w.openalex_id, ''), ifnull(w.title, ''), ifnull(w.year, 0),
       ifnull((SELECT a.name FROM authorship s JOIN author a ON a.openalex_id = s.author_id
               WHERE s.work_id = w.fil_id ORDER BY s.position LIMIT 1), ''),
       w.is_seed, w.hydrated, w.fetched_refs,
       (SELECT count(*) FROM cites c WHERE c.to_work = w.fil_id) AS cited_by,
       EXISTS (SELECT 1 FROM work_file f WHERE f.fil_id = w.fil_id AND f.missing = 0 AND lower(f.path) LIKE '%.pdf'),
       ifnull((SELECT t.status FROM work_text t WHERE t.fil_id = w.fil_id), ''),
       (SELECT count(*) FROM cites c WHERE c.from_work = w.fil_id AND c.context IS NOT NULL),
       EXISTS (SELECT 1 FROM work_file f WHERE f.fil_id = w.fil_id AND f.missing = 0 AND f.identity = 'mismatch'),
       EXISTS (SELECT 1 FROM favourite v WHERE v.work_id = w.fil_id),
       ifnull((SELECT group_concat(wl.label_id) FROM work_label wl WHERE wl.work_id = w.fil_id), ''),
       ifnull((SELECT group_concat(cw.collection_id) FROM collection_work cw WHERE cw.work_id = w.fil_id), ''),
       ifnull((SELECT CASE max(z.matched) WHEN 1 THEN 'matched' WHEN 0 THEN 'unmatched' END FROM zotero_item z WHERE z.fil_id = w.fil_id), ''),
       ifnull(w.cited_by_count, 0)
FROM work w
WHERE `+strings.Join(where, " AND ")+`
ORDER BY `+order+limit+`;`, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list the library: %w", err)
	}
	defer rows.Close()
	var out []ListRow
	for rows.Next() {
		var r ListRow
		var labels, collections string
		if err := rows.Scan(&r.FilID, &r.OpenAlexID, &r.Title, &r.Year, &r.Author, &r.Seed, &r.Hydrated,
			&r.RefsKnown, &r.CitedBy, &r.HasPDF, &r.TextStatus, &r.Contexts, &r.Mismatch, &r.Favourite, &labels, &collections, &r.Zotero, &r.CitedWorld); err != nil {
			return nil, fmt.Errorf("store: list the library: %w", err)
		}
		if !hasAll(foldTitle(r.Title), words) {
			continue
		}
		if r.Labels, err = idList(labels); err != nil {
			return nil, err
		}
		if r.Collections, err = idList(collections); err != nil {
			return nil, err
		}
		if skip > 0 {
			skip--
			continue
		}
		out = append(out, r)
		if q.Limit > 0 && len(out) == q.Limit {
			break
		}
	}
	return out, rows.Err()
}

// Facets says what a filter can offer next: how the papers a query finds
// spread over years, authors and venues.
type Facets struct {
	Total   int // papers the query finds
	Years   []YearCount
	Authors []FacetCount // the most common, and any already chosen
	Venues  []FacetCount
}

// YearCount is how many papers carry one year.
type YearCount struct {
	Year  int
	Works int
}

// FacetCount is how many papers carry one author or venue.
type FacetCount struct {
	Name  string
	Works int
}

// FacetFind narrows the authors and venues offered, by words in the name.
type FacetFind struct {
	Author string
	Venue  string
}

// facetTop is how many authors, and how many venues, are offered at once.
const facetTop = 8

// Facets counts the papers q finds. The title words are matched here in Go,
// as List does. Each filter's options ignore that filter, so they show what
// choosing another would give.
func (db *DB) Facets(ctx context.Context, q ListQuery, find FacetFind) (Facets, error) {
	q, err := db.inCollection(ctx, q)
	if err != nil {
		return Facets{}, err
	}
	years, err := db.facetCounts(ctx, q, "year", "CAST(ifnull(w.year, 0) AS TEXT)", "")
	if err != nil {
		return Facets{}, err
	}
	authors, err := db.facetCounts(ctx, q, "author", "a.name",
		"JOIN authorship s ON s.work_id = w.fil_id JOIN author a ON a.openalex_id = s.author_id")
	if err != nil {
		return Facets{}, err
	}
	venues, err := db.facetCounts(ctx, q, "venue", "ifnull(w.venue, '')", "")
	if err != nil {
		return Facets{}, err
	}
	out := Facets{Authors: topFacet(authors, q.Authors, find.Author), Venues: topFacet(venues, q.Venues, find.Venue)}
	for y, n := range years {
		year, err := strconv.Atoi(y)
		if err != nil {
			return Facets{}, fmt.Errorf("store: count years: %w", err)
		}
		if (q.YearFrom == 0 || year >= q.YearFrom) && (q.YearTo == 0 || (year != 0 && year <= q.YearTo)) {
			out.Total += n
		}
		if year != 0 {
			out.Years = append(out.Years, YearCount{Year: year, Works: n})
		}
	}
	slices.SortFunc(out.Years, func(a, b YearCount) int { return cmp.Compare(a.Year, b.Year) })
	return out, nil
}

// facetCounts counts the papers carrying each value of expr, ignoring the
// filter named by skip.
func (db *DB) facetCounts(ctx context.Context, q ListQuery, skip, expr, join string) (map[string]int, error) {
	where, args := q.conditions(skip)
	rows, err := db.read.QueryContext(ctx, `
SELECT DISTINCT `+expr+`, w.fil_id, ifnull(w.title, '')
FROM work w `+join+`
WHERE `+strings.Join(where, " AND ")+`;`, args...)
	if err != nil {
		return nil, fmt.Errorf("store: count the library's %ss: %w", skip, err)
	}
	defer rows.Close()
	words := strings.Fields(foldTitle(q.Title))
	out := map[string]int{}
	for rows.Next() {
		var value, id, title string
		if err := rows.Scan(&value, &id, &title); err != nil {
			return nil, fmt.Errorf("store: count the library's %ss: %w", skip, err)
		}
		if hasAll(foldTitle(title), words) {
			out[value]++
		}
	}
	return out, rows.Err()
}

// topFacet picks what to offer: the most common values matching find, then
// whatever is already chosen, which stays on show even at a count of zero.
func topFacet(counts map[string]int, chosen []string, find string) []FacetCount {
	words := strings.Fields(foldTitle(find))
	var all []FacetCount
	for name, n := range counts {
		if name != "" && hasAll(foldTitle(name), words) {
			all = append(all, FacetCount{Name: name, Works: n})
		}
	}
	slices.SortFunc(all, func(a, b FacetCount) int {
		return cmp.Or(cmp.Compare(b.Works, a.Works), cmp.Compare(a.Name, b.Name))
	})
	if len(all) > facetTop {
		all = all[:facetTop]
	}
	for _, name := range chosen {
		if !slices.ContainsFunc(all, func(f FacetCount) bool { return f.Name == name }) {
			all = append(all, FacetCount{Name: name, Works: counts[name]})
		}
	}
	return all
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
