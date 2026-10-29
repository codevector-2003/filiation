package text

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Ref is a work the paper cites, as OpenAlex gave it: what citation context
// looks for in the paper's text.
type Ref struct {
	ID      string // the cited work's fil ID
	Title   string
	Year    int
	DOI     string
	Surname string // first author's
}

// Mention is one place the paper cites one of its references: the sentence,
// where it is, and how it was found (ADR-015).
type Mention struct {
	Ref        string // Ref.ID
	Page       int    // from 1
	Start, End int    // byte offsets of Sentence in its page's cleaned text
	Sentence   string
	Section    string // a heading's name above it, or ""
	Marker     string // as printed
	Method     string // MethodNumeric or MethodAuthorYear
	Confidence float64
}

// How a mention was found.
const (
	MethodNumeric    = "numeric"     // [12], [3–5]: through the numbered reference list
	MethodAuthorYear = "author-year" // (Piwowar et al., 2018): straight to the reference
)

// Confidences. A numeric marker is as good as the match of its reference
// entry, which spike 11 measured at 97%; a DOI in the entry is better still.
// A bare "(12)" can be an equation number, so it is trusted less.
const (
	confNumericDOI   = 0.95
	confNumeric      = 0.9
	confNumericAY    = 0.8 // an entry with no title, matched by first author and year
	confAuthorYear   = 0.85
	confParenNumeric = 0.6
)

// maxSentence caps a sentence: past it, a window round the marker is kept.
const maxSentence = 600

// Contexts finds where a paper cites its references (ADR-015). pages are
// its cleaned pages; refs are the works OpenAlex says it cites. Nothing
// here adds a reference: a marker that cannot be tied to one of refs gives
// no mention. No match, no context — never a guess.
//
// Two routes. Author–year markers name the work — surname and year — and
// are matched to refs directly, with no need for the reference list. Numeric
// markers name an entry of the numbered reference list, so the list is
// found and split, and each entry matched to a ref by its DOI or by most of
// its title's words, confirmed by year or surname (spike 11).
func Contexts(pages []string, refs []Ref) []Mention {
	if len(pages) == 0 || len(refs) == 0 {
		return nil
	}
	listPage, listAt := referenceList(pages)
	body := func(page, at int) bool { // before the reference list
		return listPage == 0 || page < listPage || (page == listPage && at < listAt)
	}
	sections := headingsOf(pages)

	var out []Mention
	seen := map[[3]int]bool{} // page, sentence start, ref index
	add := func(page int, at, end int, marker, method string, conf float64, ref int) {
		s, e := sentenceAround(pages[page-1], at, end)
		key := [3]int{page, s, ref}
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, Mention{Ref: refs[ref].ID, Page: page, Start: s, End: e,
			Sentence: oneLine(pages[page-1][s:e]), Section: sections.at(page, at),
			Marker: oneLine(marker), Method: method, Confidence: conf})
	}

	// Numeric markers, through the numbered list.
	if listPage > 0 {
		byNumber, entryConf := matchEntries(numberedEntries(pages, listPage, listAt), refs)
		brackets := 0
		for p := 1; p <= len(pages); p++ {
			for _, loc := range bracketMarker.FindAllStringSubmatchIndex(pages[p-1], -1) {
				if !body(p, loc[0]) {
					continue
				}
				nums := expand(pages[p-1][loc[2]:loc[3]])
				if len(nums) > 0 {
					brackets++
				}
				for _, n := range nums {
					if ref, ok := byNumber[n]; ok {
						add(p, loc[0], loc[1], pages[p-1][loc[0]:loc[1]], MethodNumeric, entryConf[n], ref)
					}
				}
			}
		}
		// "(12)" only in a paper that never uses brackets: otherwise it is an
		// equation number, or anything else in parentheses.
		if brackets == 0 && len(byNumber) > 0 {
			for p := 1; p <= len(pages); p++ {
				for _, loc := range parenMarker.FindAllStringSubmatchIndex(pages[p-1], -1) {
					if !body(p, loc[0]) {
						continue
					}
					for _, n := range expand(pages[p-1][loc[2]:loc[3]]) {
						if ref, ok := byNumber[n]; ok {
							add(p, loc[0], loc[1], pages[p-1][loc[0]:loc[1]], MethodNumeric, confParenNumeric, ref)
						}
					}
				}
			}
		}
	}

	// Author–year markers, straight to the refs. Two refs sharing a first
	// author's surname and a year cannot be told apart from the marker, and
	// neither gets it.
	type key struct {
		surname string
		year    int
	}
	count := map[key]int{}
	for _, r := range refs {
		if r.Surname != "" && r.Year > 0 {
			count[key{fold(r.Surname), r.Year}]++
		}
	}
	for i, r := range refs {
		if !usableSurname(r.Surname) || r.Year == 0 || count[key{fold(r.Surname), r.Year}] > 1 {
			continue
		}
		re := authorYear(r.Surname, r.Year)
		for p := 1; p <= len(pages); p++ {
			for _, loc := range re.FindAllStringIndex(pages[p-1], -1) {
				if body(p, loc[0]) && citingContext(pages[p-1], loc[0], loc[1]) && !secondAuthor(pages[p-1], loc[0]) {
					add(p, loc[0], loc[1], pages[p-1][loc[0]:loc[1]], MethodAuthorYear, confAuthorYear, i)
				}
			}
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Page != out[j].Page {
			return out[i].Page < out[j].Page
		}
		return out[i].Start < out[j].Start
	})
	return out
}

var (
	// bracketMarker is [12], [3–5], [1, 4, 7], [2-4,9].
	bracketMarker = regexp.MustCompile(`\[(\d{1,3}(?:\s*[–—-]\s*\d{1,3})?(?:\s*[,;]\s*\d{1,3}(?:\s*[–—-]\s*\d{1,3})?)*)\]`)
	// parenMarker is the same in parentheses, after a word, not after "=":
	// "shown before (12)", never "x = (2)".
	parenMarker = regexp.MustCompile(`[\p{L}.,]\s?\((\d{1,3}(?:\s*[–—-]\s*\d{1,3})?(?:\s*[,;]\s*\d{1,3}(?:\s*[–—-]\s*\d{1,3})?)*)\)`)
	numberRange = regexp.MustCompile(`^(\d{1,3})\s*[–—-]\s*(\d{1,3})$`)
)

// expand turns "3–5, 9" into 3 4 5 9. A range of more than 30 is not a
// citation.
func expand(s string) []int {
	var out []int
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' }) {
		part = strings.TrimSpace(part)
		if m := numberRange.FindStringSubmatch(part); m != nil {
			a, _ := strconv.Atoi(m[1])
			b, _ := strconv.Atoi(m[2])
			if a < 1 || b < a || b-a > 30 {
				return nil
			}
			for n := a; n <= b; n++ {
				out = append(out, n)
			}
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 1 {
			return nil
		}
		out = append(out, n)
	}
	return out
}

// authorYear matches a citation of the work by surname and year:
// "Piwowar et al. (2018)", "(Piwowar et al., 2018)", "Piwowar and Priem
// 2013", "Piwowar & Priem, 2013a".
func authorYear(surname string, year int) *regexp.Regexp {
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(surname) +
		`(?:\s+et\s+al\.?|\s+(?:and|&)\s+\p{Lu}[\p{L}'’-]+)?\s*,?\s*\(?\s*` + strconv.Itoa(year) + `[a-z]?\b`)
}

// citingContext reports a surname-and-year match that sits in a citation:
// inside parentheses, with the year in them, after "et al.", or in a list —
// the year followed by ";", "," or ")". Some PDFs lose their parentheses
// from the text layer altogether ("Loiseau et al., 2006 ."), so the
// parentheses cannot be required. "In 2011 Sutton wrote" and "Sutton 2011
// data" in running text are not citations.
func citingContext(page string, at, end int) bool {
	m := page[at:end]
	if strings.Contains(m, "(") || strings.Contains(m, "et al") {
		return true // Smith et al. (2001)
	}
	open := strings.LastIndexByte(page[max(0, at-200):at], '(')
	shut := strings.LastIndexByte(page[max(0, at-200):at], ')')
	if open >= 0 && open > shut {
		return true
	}
	next, _ := utf8.DecodeRuneInString(strings.TrimLeft(page[end:], " \n"))
	return next == ';' || next == ',' || next == ')'
}

// secondAuthor reports a surname that follows "and" or "&": "Cornish and
// Littenberg 2015" cites Cornish's paper, not one of Littenberg's.
func secondAuthor(page string, at int) bool {
	before := strings.TrimRight(page[max(0, at-6):at], " \n")
	return strings.HasSuffix(before, " and") || strings.HasSuffix(before, "&") || before == "and"
}

// referenceList finds where the numbered or author–year list of references
// starts: after the last references heading, or — physics layouts have none
// — at a run of entries numbered from 1 near the end. Zero means not found.
func referenceList(pages []string) (page, at int) {
	if p, a, _, _ := referencesSpan(pages); p > 0 {
		return p, a
	}
	// The list can sit well before the end: a collaboration paper is
	// followed by pages of author names.
	for i := len(pages) - 1; i >= 0; i-- {
		for _, re := range []*regexp.Regexp{firstBracketEntry, firstDotEntry} {
			if loc := re.FindStringIndex(pages[i]); loc != nil {
				if len(numberedEntries(pages, i+1, loc[0])) >= 5 {
					return i + 1, loc[0]
				}
			}
		}
	}
	return 0, 0
}

var (
	interleaved       = regexp.MustCompile(`^\s*(?:\[\d{1,3}\]|\d{1,3}[.)])\s*(?:\[\d{1,3}\]|\d{1,3}[.)])\s`)
	firstBracketEntry = regexp.MustCompile(`(?m)^\s*\[1\]\s+\S`)
	firstDotEntry     = regexp.MustCompile(`(?m)^\s*1\.\s+\p{Lu}`)
	bracketEntry      = regexp.MustCompile(`^\s*\[(\d{1,3})\]`)
	dotEntry          = regexp.MustCompile(`^\s*(\d{1,3})[.)]\s+\S`)
)

// numberedEntries splits the list from page, at, into entries by their
// numbers — [1] [2] … or 1. 2. … — accepting only the next number in
// sequence, so a "3." inside an entry does not start one. Nil if the list
// is not numbered.
func numberedEntries(pages []string, page, at int) map[int]string {
	var lines []string
	for p := page; p <= len(pages); p++ {
		text := pages[p-1]
		if p == page {
			text = text[at:]
		}
		lines = append(lines, strings.Split(text, "\n")...)
	}
	for _, re := range []*regexp.Regexp{bracketEntry, dotEntry} {
		out := map[int]string{}
		var cur strings.Builder
		next := 1
		for _, l := range lines {
			if m := re.FindStringSubmatch(l); m != nil {
				if n, _ := strconv.Atoi(m[1]); n == next {
					if cur.Len() > 0 {
						out[next-1] = cur.String()
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
			out[next-1] = cur.String()
		}
		if len(out) >= 5 {
			// Two columns read across can interleave entries: "9. 16. Peres
			// …" is not entry 9. Such an entry is dropped, not guessed at.
			for n, e := range out {
				if interleaved.MatchString(e) {
					delete(out, n)
				}
			}
			return out
		}
	}
	return nil
}

// matchEntries ties numbered entries to refs: by DOI; by most of the
// title's words (70%) confirmed by the year or the first author's surname;
// or — physics styles print no titles — by the first author's surname at
// the start of the entry and the year, when exactly one ref has both. Each
// entry and each ref is used once, best matches first. It returns the ref
// index for each entry number, and how sure each match is.
func matchEntries(entries map[int]string, refs []Ref) (map[int]int, map[int]float64) {
	byNumber, conf := map[int]int{}, map[int]float64{}
	usedRef := map[int]bool{}
	for _, n := range sortedKeys(entries) {
		e := entries[n]
		le := strings.ToLower(e)
		for i, r := range refs {
			if r.DOI != "" && !usedRef[i] && strings.Contains(le, strings.ToLower(r.DOI)) {
				byNumber[n], conf[n], usedRef[i] = i, confNumericDOI, true
				break
			}
		}
	}
	type pair struct {
		n, ref int
		score  float64
	}
	var pairs []pair
	for _, n := range sortedKeys(entries) {
		e := entries[n]
		if _, done := byNumber[n]; done {
			continue
		}
		words := set(tokens(e))
		fe := fold(e)
		for i, r := range refs {
			if usedRef[i] {
				continue
			}
			tw := tokens(r.Title)
			if len(tw) < 3 {
				continue
			}
			hit := 0
			for _, w := range tw {
				if words[w] {
					hit++
				}
			}
			score := float64(hit) / float64(len(tw))
			confirmed := (r.Year > 0 && strings.Contains(e, strconv.Itoa(r.Year))) ||
				(r.Surname != "" && strings.Contains(fe, fold(r.Surname)))
			if score >= 0.7 && confirmed {
				pairs = append(pairs, pair{n, i, score})
			}
		}
	}
	sort.SliceStable(pairs, func(i, j int) bool { return pairs[i].score > pairs[j].score })
	// (pairs were built in entry order, so ties keep it.)
	for _, p := range pairs {
		if _, done := byNumber[p.n]; done || usedRef[p.ref] {
			continue
		}
		byNumber[p.n], conf[p.n], usedRef[p.ref] = p.ref, confNumeric, true
	}

	// Entries with no title to match: first author and year. Two refs
	// sharing both — a collaboration's many papers in one year — are never
	// told apart this way, matched or not.
	for _, n := range sortedKeys(entries) {
		e := entries[n]
		if _, done := byNumber[n]; done {
			continue
		}
		start := map[string]bool{}
		for _, w := range letterWords(e[:min(len(e), 60)]) {
			start[fold(w)] = true
		}
		found := -1
		for i, r := range refs {
			if !usableSurname(r.Surname) || r.Year == 0 || !start[fold(r.Surname)] ||
				!strings.Contains(e, strconv.Itoa(r.Year)) {
				continue
			}
			if found >= 0 {
				found = -2 // two fit: neither
				break
			}
			found = i
		}
		if found >= 0 && !usedRef[found] {
			byNumber[n], conf[n], usedRef[found] = found, confNumericAY, true
		}
	}
	return byNumber, conf
}

// usableSurname is a surname long enough to recognise: a record whose first
// author is an initial ("C") would match every "C. S. Ting".
func usableSurname(s string) bool { return len([]rune(s)) >= 2 }

func sortedKeys(m map[int]string) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}

// sentenceAround returns the sentence holding page[at:end]: back to the end
// of the one before, on to its own end. "et al.", "Fig." and their kind do
// not end a sentence. A very long one is cut to a window round the marker.
func sentenceAround(page string, at, end int) (int, int) {
	s := at
	for s > 0 {
		i := strings.LastIndexAny(page[:s], ".?!\n")
		if i < 0 {
			s = 0
			break
		}
		if page[i] == '\n' {
			if i > 0 && page[i-1] == '\n' || isHeadingLine(page, i) { // a paragraph break, or a heading above
				s = i + 1
				break
			}
			s = i
			continue
		}
		if sentenceEnd(page, i) {
			s = i + 1
			break
		}
		s = i
	}
	e := end
	for e < len(page) {
		i := strings.IndexAny(page[e:], ".?!\n")
		if i < 0 {
			e = len(page)
			break
		}
		i += e
		if page[i] == '\n' {
			if i+1 < len(page) && page[i+1] == '\n' {
				e = i
				break
			}
			e = i + 1
			continue
		}
		if sentenceEnd(page, i) {
			e = i + 1
			break
		}
		e = i + 1
	}
	s, e = trim(page, s, e)
	if e-s > maxSentence {
		s, e = trim(page, max(s, wordStart(page, at-maxSentence/3)), min(e, wordEnd(page, end+maxSentence/3)))
	}
	return s, e
}

// sentenceEnd reports whether the punctuation at page[i] ends a sentence:
// space and then a capital, a digit or an opening bracket follow, and the
// word it ends is not an abbreviation.
func sentenceEnd(page string, i int) bool {
	rest := page[i+1:]
	if rest == "" {
		return true
	}
	if !strings.HasPrefix(rest, " ") && !strings.HasPrefix(rest, "\n") {
		return false
	}
	next, _ := utf8.DecodeRuneInString(strings.TrimLeft(rest, " \n"))
	if !unicode.IsUpper(next) && !unicode.IsDigit(next) && next != '[' && next != '(' {
		return false
	}
	if page[i] != '.' {
		return true
	}
	w := page[:i]
	if j := strings.LastIndexAny(w, " \n(["); j >= 0 {
		w = w[j+1:]
	}
	return !abbreviation[strings.ToLower(w)] && !(len(w) == 1 && unicode.IsUpper(rune(w[0])))
}

// isHeadingLine reports whether the line ending at page[nl] is a heading
// rather than part of a sentence: a section's name, or a short line with no
// closing punctuation followed by a line starting a new sentence.
func isHeadingLine(page string, nl int) bool {
	start := strings.LastIndexByte(page[:nl], '\n') + 1
	line := strings.TrimSpace(page[start:nl])
	if line == "" {
		return true
	}
	if sectionHeading.MatchString(line) {
		return true
	}
	next, _ := utf8.DecodeRuneInString(page[nl+1:])
	last, _ := utf8.DecodeLastRuneInString(line)
	return len(strings.Fields(line)) <= 4 && !strings.ContainsRune(".,;:-", last) && unicode.IsUpper(next)
}

var abbreviation = map[string]bool{
	"al": true, "fig": true, "figs": true, "eq": true, "eqs": true, "ref": true, "refs": true,
	"e.g": true, "i.e": true, "cf": true, "vs": true, "sec": true, "no": true, "vol": true,
	"pp": true, "ed": true, "eds": true, "approx": true, "dr": true, "mr": true, "ms": true,
}

func wordStart(page string, i int) int {
	i = max(0, i)
	for i > 0 && page[i-1] != ' ' && page[i-1] != '\n' {
		i--
	}
	return i
}

func wordEnd(page string, i int) int {
	i = min(len(page), i)
	for i < len(page) && page[i] != ' ' && page[i] != '\n' {
		i++
	}
	return i
}

// oneLine joins a passage's lines.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// sectionHeading is a line that is only a section's name, optionally
// numbered: "2. Methods", "III. RESULTS", "Materials and Methods".
var sectionHeading = regexp.MustCompile(`(?im)^[ \t]*(?:[0-9]+(?:\.[0-9]+)*\.?|[IVX]+\.)?[ \t]*` +
	`(introduction|background|related work|literature review|materials and methods|methods?|methodology|` +
	`data|experiments?|results and discussion|results|discussion|conclusions?|summary)[ \t]*:?[ \t]*$`)

type heading struct {
	page, at int
	name     string
}

type headings []heading

func headingsOf(pages []string) headings {
	var hs headings
	for i, p := range pages {
		for _, m := range sectionHeading.FindAllStringSubmatchIndex(p, -1) {
			name := strings.ToLower(p[m[2]:m[3]])
			switch name {
			case "method", "methodology", "materials and methods":
				name = "methods"
			case "conclusion":
				name = "conclusions"
			case "experiment":
				name = "experiments"
			case "literature review":
				name = "related work"
			}
			hs = append(hs, heading{i + 1, m[0], name})
		}
	}
	return hs
}

// at is the section a position falls in: the last heading above it.
func (hs headings) at(page, at int) string {
	name := ""
	for _, h := range hs {
		if h.page < page || (h.page == page && h.at <= at) {
			name = h.name
		}
	}
	return name
}

// tokens are a text's words of three letters or more, folded, without the
// commonest English words, which match anything.
func tokens(s string) []string {
	var out []string
	for _, w := range strings.FieldsFunc(fold(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if len(w) >= 3 && !stopWords[w] {
			out = append(out, w)
		}
	}
	return out
}

var stopWords = map[string]bool{"the": true, "and": true, "for": true, "with": true, "from": true, "are": true,
	"its": true, "into": true, "that": true, "this": true, "between": true, "their": true, "our": true}

// fold lower-cases and drops accents: Larivière is lariviere.
func fold(s string) string {
	var b strings.Builder
	for _, r := range norm.NFKD.String(s) {
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
