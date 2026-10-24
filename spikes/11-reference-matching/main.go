// Command reference-matching is spike 11 (ARCHITECTURE_PHASE3.md §6): how well
// can a reference list be split out of a PDF's text and matched to real works?
//
// Measured on PDFs OpenAlex already knows, so OpenAlex's referenced_works is
// the answer key and the measurement is free. It decides whether parsed
// references are built, after M3, for local documents OpenAlex does not know.
// Hard rule 2 stands either way: for a work OpenAlex knows, its list is used,
// never a parse.
//
// For each text spike 10 extracted:
//
//  1. Find the reference section: the last line that is only "References",
//     "Bibliography", "Literature cited" or the like.
//  2. Split it into entries — numbered ([1], 1.) when the numbers run in
//     sequence, otherwise at lines that start like an author list.
//  3. Match each of OpenAlex's references to the entry holding most of its
//     title's words (at least 70% of at least three), each entry used once.
//     A match is confirmed when the entry also holds the year or the first
//     author's surname.
//
// Recall is matched references over OpenAlex references with a title. An
// entry left unmatched is not necessarily an error: OpenAlex's list misses
// references too (spike 4).
//
// Usage:
//
//	go run ./spikes/11-reference-matching -text <spike 10 out>/text -cache <spike 9 dir>/cache > spike11.md
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/codevector-2003/filiation/internal/httpx"
	"github.com/codevector-2003/filiation/internal/sources/openalex"
)

type ref struct {
	id, title, surname string
	year               int
}

type paper struct {
	field, id, extractor string
	section              bool
	style                string
	entries              []string
	refs                 []ref
	matched, confirmed   int
	titled               int
}

func main() {
	textDir := flag.String("text", "", "spike 10's text directory")
	cache := flag.String("cache", "", "HTTP cache directory (spike 9's)")
	flag.Parse()
	if *textDir == "" || *cache == "" {
		fatal(errors.New("-text and -cache are required"))
	}
	ctx := context.Background()
	dc, err := httpx.NewDirCache(*cache)
	if err != nil {
		fatal(err)
	}
	h, err := httpx.New(openalex.HTTPOptions("", dc))
	if err != nil {
		fatal(err)
	}

	extractors, _ := os.ReadDir(*textDir)
	var papers []*paper
	refsOf := map[string][]ref{}
	for _, ex := range extractors {
		files, _ := filepath.Glob(filepath.Join(*textDir, ex.Name(), "*", "*.txt"))
		sort.Strings(files)
		for _, f := range files {
			id := strings.TrimSuffix(filepath.Base(f), ".txt")
			if _, ok := refsOf[id]; !ok {
				refs, err := references(ctx, h, id)
				if err != nil {
					fatal(fmt.Errorf("%s: %w", id, err))
				}
				refsOf[id] = refs
			}
			raw, _ := os.ReadFile(f)
			p := &paper{field: filepath.Base(filepath.Dir(f)), id: id, extractor: ex.Name(), refs: refsOf[id]}
			parse(p, string(raw))
			match(p)
			papers = append(papers, p)
		}
	}
	report(papers)
}

// references reads a work's reference list and the title, year and first
// author of each reference. The work itself is a free single lookup; the
// references cost one credit per 100.
func references(ctx context.Context, h *httpx.Client, id string) ([]ref, error) {
	body, err := h.Get(ctx, "https://api.openalex.org/works/"+id+"?select=id,referenced_works")
	if err != nil {
		return nil, err
	}
	var w struct {
		Refs []string `json:"referenced_works"`
	}
	if err := json.Unmarshal(body, &w); err != nil {
		return nil, err
	}
	var out []ref
	for start := 0; start < len(w.Refs); start += 100 {
		var ids []string
		for _, r := range w.Refs[start:min(start+100, len(w.Refs))] {
			ids = append(ids, r[strings.LastIndex(r, "/")+1:])
		}
		q := url.Values{
			"filter":   {"openalex_id:" + strings.Join(ids, "|")},
			"per_page": {"100"},
			"select":   {"id,title,publication_year,authorships"},
		}
		body, err := h.Get(ctx, "https://api.openalex.org/works?"+q.Encode())
		if err != nil {
			return nil, err
		}
		var page struct {
			Results []struct {
				ID          string  `json:"id"`
				Title       *string `json:"title"`
				Year        int     `json:"publication_year"`
				Authorships []struct {
					Author struct {
						Name string `json:"display_name"`
					} `json:"author"`
				} `json:"authorships"`
			} `json:"results"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, err
		}
		for _, r := range page.Results {
			x := ref{id: r.ID, year: r.Year}
			if r.Title != nil {
				x.title = *r.Title
			}
			if len(r.Authorships) > 0 {
				f := strings.Fields(r.Authorships[0].Author.Name)
				if len(f) > 0 {
					x.surname = f[len(f)-1]
				}
			}
			out = append(out, x)
		}
	}
	return out, nil
}

var (
	heading  = regexp.MustCompile(`(?im)^\s*(?:\d+\.?\s*)?(references|reference list|bibliography|literature cited|works cited|references and notes|notes and references|cited literature)\s*:?\s*$`)
	bracketN = regexp.MustCompile(`^\s*\[(\d{1,3})\]`)
	dotN     = regexp.MustCompile(`^\s*(\d{1,3})[.)]\s+\S`)
	// Surname, I. — or Surname I, Surname IJ, — or SURNAME, I.
	authorStart = regexp.MustCompile(`^\s*\p{Lu}[\p{L}'’\-]+(?: [\p{Lu}][\p{L}'’\-]+)?(?:,\s*\p{Lu}[.\s]|\s\p{Lu}{1,3}[,.]|,\s+\p{Lu}[\p{Ll}]+)`)
)

// parse finds the reference section and splits it into entries.
func parse(p *paper, text string) {
	locs := heading.FindAllStringIndex(text, -1)
	if len(locs) == 0 {
		return
	}
	p.section = true
	body := text[locs[len(locs)-1][1]:]
	lines := strings.Split(strings.ReplaceAll(body, "\f", "\n"), "\n")

	if entries := numbered(lines, bracketN); len(entries) >= 5 {
		p.style, p.entries = "[n]", entries
		return
	}
	if entries := numbered(lines, dotN); len(entries) >= 5 {
		p.style, p.entries = "n.", entries
		return
	}
	p.style = "author-year"
	var cur strings.Builder
	prevEnds := true
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" {
			prevEnds = true
			continue
		}
		if authorStart.MatchString(t) && prevEnds && cur.Len() > 0 {
			p.entries = append(p.entries, cur.String())
			cur.Reset()
		}
		cur.WriteString(t)
		cur.WriteByte(' ')
		prevEnds = strings.HasSuffix(t, ".") || strings.HasSuffix(t, ")") || unicode.IsDigit(lastRune(t))
	}
	if cur.Len() > 0 {
		p.entries = append(p.entries, cur.String())
	}
}

// numbered splits at lines starting with the next number in sequence, which
// keeps a "3." inside an entry from starting a new one.
func numbered(lines []string, re *regexp.Regexp) []string {
	var out []string
	var cur strings.Builder
	next := 1
	for _, l := range lines {
		if m := re.FindStringSubmatch(l); m != nil {
			if n, _ := strconv.Atoi(m[1]); n == next {
				if cur.Len() > 0 {
					out = append(out, cur.String())
					cur.Reset()
				}
				next++
			}
		}
		if next > 1 {
			cur.WriteString(strings.TrimSpace(l))
			cur.WriteByte(' ')
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// match pairs references with entries, best title overlap first.
func match(p *paper) {
	entryWords := make([]map[string]bool, len(p.entries))
	for i, e := range p.entries {
		entryWords[i] = set(tokens(e))
	}
	type pair struct {
		r, e  int
		score float64
	}
	var pairs []pair
	for ri, r := range p.refs {
		tw := tokens(r.title)
		if len(tw) < 3 {
			continue
		}
		p.titled++
		for ei, ew := range entryWords {
			hit := 0
			for _, w := range tw {
				if ew[w] {
					hit++
				}
			}
			if s := float64(hit) / float64(len(tw)); s >= 0.7 {
				pairs = append(pairs, pair{ri, ei, s})
			}
		}
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].score > pairs[j].score })
	usedR, usedE := map[int]bool{}, map[int]bool{}
	for _, pr := range pairs {
		if usedR[pr.r] || usedE[pr.e] {
			continue
		}
		usedR[pr.r], usedE[pr.e] = true, true
		p.matched++
		r, e := p.refs[pr.r], p.entries[pr.e]
		if (r.year > 0 && strings.Contains(e, strconv.Itoa(r.year))) ||
			(r.surname != "" && strings.Contains(fold(e), fold(r.surname))) {
			p.confirmed++
		}
	}
}

// tokens are a text's words of three letters or more, folded, without the
// commonest English words, which match anything.
func tokens(s string) []string {
	var out []string
	for _, w := range strings.FieldsFunc(fold(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if len(w) >= 3 && !stop[w] {
			out = append(out, w)
		}
	}
	return out
}

var stop = map[string]bool{"the": true, "and": true, "for": true, "with": true, "from": true, "are": true,
	"its": true, "into": true, "that": true, "this": true, "between": true, "their": true, "our": true}

func fold(s string) string {
	s = norm.NFKD.String(s)
	var b strings.Builder
	for _, r := range s {
		if !unicode.Is(unicode.Mn, r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

func set(ws []string) map[string]bool {
	m := make(map[string]bool, len(ws))
	for _, w := range ws {
		m[w] = true
	}
	return m
}

func lastRune(s string) rune {
	r := []rune(s)
	return r[len(r)-1]
}

func report(papers []*paper) {
	fmt.Println("## Spike 11 — matching references from PDF text")
	fmt.Println()
	fmt.Println("`go run ./spikes/11-reference-matching`, on the texts spike 10 extracted. OpenAlex's " +
		"`referenced_works` is the answer key.")
	fmt.Println()
	byEx := map[string][]*paper{}
	var names []string
	for _, p := range papers {
		if byEx[p.extractor] == nil {
			names = append(names, p.extractor)
		}
		byEx[p.extractor] = append(byEx[p.extractor], p)
	}
	fmt.Println("| Text from | Papers | Section found | Entries | OpenAlex refs with a title | Matched (recall) | Confirmed by year or surname |")
	fmt.Println("| --- | --- | --- | --- | --- | --- | --- |")
	for _, n := range names {
		var sec, entries, titled, matched, confirmed int
		for _, p := range byEx[n] {
			if p.section {
				sec++
			}
			entries += len(p.entries)
			titled += p.titled
			matched += p.matched
			confirmed += p.confirmed
		}
		fmt.Printf("| %s | %d | %d | %d | %d | %s | %s |\n", n, len(byEx[n]), sec, entries, titled,
			pct(matched, titled), pct(confirmed, matched))
	}
	fmt.Println()
	fmt.Println("### Per paper (pdftotext text)")
	fmt.Println()
	fmt.Println("| Field | Work | Style | Entries | Refs with title | Matched | Confirmed |")
	fmt.Println("| --- | --- | --- | --- | --- | --- | --- |")
	for _, p := range byEx["pdftotext"] {
		style := p.style
		if !p.section {
			style = "no section found"
		}
		fmt.Printf("| %s | %s | %s | %d | %d | %s | %d |\n", p.field, p.id, style, len(p.entries), p.titled, pct(p.matched, p.titled), p.confirmed)
	}
	fmt.Println()
}

func pct(n, of int) string {
	if of == 0 {
		return "—"
	}
	return fmt.Sprintf("%d (%.0f%%)", n, 100*float64(n)/float64(of))
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "spike 11:", err)
	os.Exit(1)
}
