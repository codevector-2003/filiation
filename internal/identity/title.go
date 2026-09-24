package identity

import (
	"strings"
	"unicode"
)

// titleMatchRatio is how similar two normalised titles must be for
// TitlesMatch to call them the same work. It tolerates a typo or two and a
// dropped article; it does not tolerate a missing subtitle, which is often
// what distinguishes a paper from its follow-up.
const titleMatchRatio = 0.85

// TitleKey reduces a title to what survives every way it gets written: case
// folded, and everything but letters and digits removed. Punctuation, spacing,
// hyphenation and dash style vary between OpenAlex, publisher pages and what a
// user types from memory, and none of it distinguishes one paper from another.
//
// It is a comparison key, never a display form and never an identity: two
// different papers can share a title, which is exactly why a title search
// needs confirmation (ADR-005).
func TitleKey(title string) string {
	var b strings.Builder
	b.Grow(len(title))
	for _, r := range title {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// TitlesMatch reports whether two titles plausibly name the same work.
//
// It is for ranking and flagging title-search candidates — putting the one
// that matches what the user typed first and marking it — and it never
// licenses accepting one. Per ADR-005 a title search always waits for the user
// or an explicit --accept-first; a wrong seed poisons every node expanded from
// it, and a string similarity score is exactly the kind of evidence that is
// right most of the time and silently wrong the rest.
//
// Deliberately stricter than a substring test. "Attention" is contained in
// "Attention Is All You Need" and is not the same paper.
func TitlesMatch(a, b string) bool {
	ka, kb := TitleKey(a), TitleKey(b)
	if ka == "" || kb == "" {
		return false
	}
	return TitleSimilarity(ka, kb) >= titleMatchRatio
}

// TitleSimilarity is 1 minus the edit distance between the two titles' keys,
// scaled by the longer key: 1 for identical, 0 for nothing in common.
func TitleSimilarity(a, b string) float64 {
	ra, rb := []rune(TitleKey(a)), []rune(TitleKey(b))
	longest := max(len(ra), len(rb))
	if longest == 0 {
		return 0
	}
	return 1 - float64(levenshtein(ra, rb))/float64(longest)
}

// levenshtein is the edit distance between two rune slices, in two rows of
// memory. Titles are short, so the quadratic time is not worth avoiding.
func levenshtein(a, b []rune) int {
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}
