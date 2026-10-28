package text

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Chunk is one passage of a paper: what keyword search finds, and later what
// is embedded (M4). A chunk never crosses a page, so every hit has one page
// number.
type Chunk struct {
	Page    int    // from 1
	Ordinal int    // position in the paper, from 0
	Section string // SectionReferences, or "" for the body
	// Start and End are byte offsets of Text in its page's cleaned text, so
	// a passage can be found again in its page and its neighbours stitched
	// back on.
	Start, End int
	Text       string
}

// SectionReferences marks the passages after a paper's last references
// heading. Search leaves them out unless asked: otherwise a search for an
// author's name fills up with bibliographies.
const SectionReferences = "references"

// Sizes in words. A passage aims at about 250 — long enough to carry a
// claim and its context, short enough that a hit points somewhere — and ends
// at a sentence where it can.
const (
	chunkTarget = 250
	chunkMax    = 400 // a "sentence" this long (a table, a run of references) is cut at a word
	chunkMin    = 60  // a page's last passage shorter than this joins the one before
)

// Chunks splits cleaned pages into passages.
func Chunks(pages []string) []Chunk {
	refPage, refAt, endPage, endAt := referencesSpan(pages)
	var out []Chunk
	for i, page := range pages {
		n := i + 1
		// Up to three spans per page: before the references heading, the
		// references, and after the appendix heading that ends them.
		cuts := []int{0}
		if n == refPage {
			cuts = append(cuts, refAt)
		}
		if n == endPage {
			cuts = append(cuts, endAt)
		}
		cuts = append(cuts, len(page))
		for j := 0; j+1 < len(cuts); j++ {
			from, to := cuts[j], cuts[j+1]
			section := ""
			if inSpan(n, from, refPage, refAt, endPage, endAt) {
				section = SectionReferences
			}
			for _, s := range split(page, from, to) {
				out = append(out, Chunk{Page: n, Ordinal: len(out), Section: section,
					Start: s[0], End: s[1], Text: page[s[0]:s[1]]})
			}
		}
	}
	return out
}

// inSpan reports whether text at page n, offset at, lies within the
// references: at or after their heading, and before the heading that ends them.
func inSpan(n, at, refPage, refAt, endPage, endAt int) bool {
	if refPage == 0 || n < refPage || (n == refPage && at < refAt) {
		return false
	}
	return endPage == 0 || n < endPage || (n == endPage && at < endAt)
}

var (
	// referencesHeading is a line that is only a references heading,
	// optionally numbered: "References", "7. REFERENCES", "IV Bibliography".
	referencesHeading = regexp.MustCompile(`(?im)^[ \t]*(?:[0-9]+\.?|[IVXL]+\.?)?[ \t]*` +
		`(?:references(?: and notes| cited)?|reference list|bibliography|literature cited|works cited|literatur|références|bibliografía)[ \t]*:?[ \t]*$`)
	// afterReferences is a heading that ends them.
	afterReferences = regexp.MustCompile(`(?im)^[ \t]*(?:[A-Z0-9]+\.?)?[ \t]*` +
		`(?:appendix|appendices|supplementary (?:material|information)|supporting information)\b[^\n]{0,60}$`)
)

// referencesSpan finds the last references heading, and the first appendix
// heading after it. Spike 11 found that finding the list is the hard part:
// a paper whose heading is missed keeps its references in the body, which
// costs some noise in search and nothing else. Zero pages mean not found.
func referencesSpan(pages []string) (refPage, refAt, endPage, endAt int) {
	for i := len(pages) - 1; i >= 0 && refPage == 0; i-- {
		if locs := referencesHeading.FindAllStringIndex(pages[i], -1); locs != nil {
			refPage, refAt = i+1, locs[len(locs)-1][0]
		}
	}
	if refPage == 0 {
		return 0, 0, 0, 0
	}
	for i := refPage - 1; i < len(pages); i++ {
		from := 0
		if i == refPage-1 {
			from = refAt
		}
		if loc := afterReferences.FindStringIndex(pages[i][from:]); loc != nil {
			return refPage, refAt, i + 1, from + loc[0]
		}
	}
	return refPage, refAt, 0, 0
}

// split cuts page[from:to] into passages, returning their [start, end)
// offsets with surrounding space trimmed.
func split(page string, from, to int) [][2]int {
	var out [][2]int
	start, words := -1, 0
	lastBreak := -1 // offset just after the last sentence end in this passage
	flush := func(end int) {
		s, e := trim(page, start, end)
		if s < e {
			out = append(out, [2]int{s, e})
		}
		start, words, lastBreak = -1, 0, -1
	}
	for i := from; i < to; {
		// Skip to the next word.
		r, size := utf8.DecodeRuneInString(page[i:])
		if unicode.IsSpace(r) {
			i += size
			continue
		}
		if start < 0 {
			start = i
		}
		// Find its end.
		j := i
		for j < to {
			r, size := utf8.DecodeRuneInString(page[j:])
			if unicode.IsSpace(r) {
				break
			}
			j += size
		}
		words++
		if endsSentence(page, i, j, to) {
			lastBreak = j
		}
		switch {
		case words >= chunkTarget && lastBreak == j:
			flush(j)
		case words >= chunkMax:
			if lastBreak > start {
				// Cut at the last sentence end, and carry the words after it
				// over to the next passage.
				cut := lastBreak
				flush(cut)
				j = cut
			} else {
				flush(j)
			}
		}
		i = j
	}
	if start >= 0 {
		flush(to)
	}
	// A short last passage joins the one before it.
	if n := len(out); n >= 2 && wordCount(page[out[n-1][0]:out[n-1][1]]) < chunkMin {
		out[n-2][1] = out[n-1][1]
		out = out[:n-1]
	}
	return out
}

// endsSentence reports a word page[i:j] that ends a sentence: it ends in
// . ? or ! and the next word starts with a capital or a digit, or a blank
// line follows. "et al." and "Fig." followed by lower case do not end one.
func endsSentence(page string, i, j, to int) bool {
	rest := page[j:to]
	if strings.HasPrefix(rest, "\n\n") || j == to {
		return true
	}
	last, _ := utf8.DecodeLastRuneInString(page[i:j])
	if last != '.' && last != '?' && last != '!' {
		return false
	}
	next, _ := utf8.DecodeRuneInString(strings.TrimLeft(rest, " \n"))
	return unicode.IsUpper(next) || unicode.IsDigit(next) || next == '('
}

func trim(page string, s, e int) (int, int) {
	for s < e {
		r, size := utf8.DecodeRuneInString(page[s:])
		if !unicode.IsSpace(r) {
			break
		}
		s += size
	}
	for e > s {
		r, size := utf8.DecodeLastRuneInString(page[:e])
		if !unicode.IsSpace(r) {
			break
		}
		e -= size
	}
	return s, e
}

func wordCount(s string) int { return len(strings.Fields(s)) }
