package text

import (
	"fmt"
	"strings"
	"unicode"
)

// Status is what reading a PDF gave: searchable text, or why there is none.
type Status string

const (
	StatusOK         Status = "ok"
	StatusNoText     Status = "no-text"    // no text layer: a scan, and fil does no OCR
	StatusGibberish  Status = "gibberish"  // a text layer that is not text
	StatusUnreadable Status = "unreadable" // damaged, encrypted, or not a PDF
	StatusTimedOut   Status = "timed-out"  // still being read at the time limit
)

// Verdict is Judge's answer.
type Verdict struct {
	Status Status
	Reason string // why, in words, when Status is not ok
}

// Thresholds. A text layer is not the same as text (spike 10): a scanned
// paper can carry a few stamped lines per page and nothing else, and one
// whose fonts map glyphs to the wrong characters produces runs of symbols
// and consonants. The checks are deliberately crude and language-blind —
// they must pass German, Chinese and formulas — and lean towards ok: a
// paper wrongly called gibberish disappears from search, which is worse
// than some noise in it.
const (
	minCharsPerPage = 100  // fewer, on average, and the PDF is a scan
	minWordLike     = 0.45 // fewer tokens that look like words, and it is gibberish
	minLetters      = 0.35 // fewer letters among the visible characters; formulas and tables lower it
	// More U+FFFD — glyphs with no character at all — and the fonts do not
	// say what they draw. Not lower: a formula-heavy physics review had 7%,
	// every one a mathematical symbol, its prose intact (live, M3 step 7).
	// A font with no character map loses its letters, which is far more.
	maxReplacement = 0.25
	minVowels      = 0.25 // fewer vowels among Latin letters; prose has 0.35–0.40
)

// Judge decides whether cleaned pages are text worth searching.
func Judge(pages []string) Verdict {
	m := measure(pages)
	switch {
	case len(pages) == 0:
		return Verdict{StatusNoText, "the PDF has no pages"}
	case m.chars < minCharsPerPage*len(pages):
		return Verdict{StatusNoText, fmt.Sprintf(
			"%d characters on %d pages: a scan with no text layer, and fil does no OCR", m.chars, len(pages))}
	case m.replacementShare() > maxReplacement:
		return Verdict{StatusGibberish, fmt.Sprintf(
			"%.0f%% of characters have no meaning: the PDF's fonts do not say which letters they draw", 100*m.replacementShare())}
	case m.letterShare() < minLetters:
		return Verdict{StatusGibberish, fmt.Sprintf(
			"only %.0f%% of characters are letters", 100*m.letterShare())}
	case m.latin*2 > m.letters && m.vowelShare() < minVowels:
		return Verdict{StatusGibberish, fmt.Sprintf(
			"only %.0f%% of letters are vowels: letters swapped for others", 100*m.vowelShare())}
	case m.wordLikeShare() < minWordLike:
		return Verdict{StatusGibberish, fmt.Sprintf(
			"only %.0f%% of the text looks like words", 100*m.wordLikeShare())}
	}
	return Verdict{Status: StatusOK}
}

type measures struct {
	chars, letters, replacement int // visible characters
	latin, vowels               int // Latin-script letters, and the vowels among them
	tokens, wordLike            int
}

func (m measures) letterShare() float64      { return share(m.letters, m.chars) }
func (m measures) replacementShare() float64 { return share(m.replacement, m.chars) }
func (m measures) wordLikeShare() float64    { return share(m.wordLike, m.tokens) }
func (m measures) vowelShare() float64       { return share(m.vowels, m.latin) }

func share(n, of int) float64 {
	if of == 0 {
		return 0
	}
	return float64(n) / float64(of)
}

func measure(pages []string) measures {
	var m measures
	for _, p := range pages {
		for _, r := range p {
			if unicode.IsSpace(r) {
				continue
			}
			m.chars++
			switch {
			case unicode.IsLetter(r):
				m.letters++
				if unicode.Is(unicode.Latin, r) {
					m.latin++
					if isVowel(r) {
						m.vowels++
					}
				}
			case r == unicode.ReplacementChar:
				m.replacement++
			}
		}
		for _, tok := range strings.Fields(p) {
			if isNumberOrSymbol(tok) {
				continue // formulas, tables and page numbers are not evidence either way
			}
			m.tokens++
			if wordLike(tok) {
				m.wordLike++
			}
		}
	}
	return m
}

// wordLike reports a token that could be a word in some language: mostly
// letters, and — in a Latin-script token of three letters or more — a vowel.
// Garbled Latin text fails the vowel test ("Tbc rcsnlts"); scripts without
// vowel letters skip it.
func wordLike(tok string) bool {
	tok = strings.TrimFunc(tok, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	letters, other, latin, vowels := 0, 0, 0, 0
	for _, r := range tok {
		switch {
		case unicode.IsLetter(r):
			letters++
			if unicode.Is(unicode.Latin, r) {
				latin++
				if isVowel(r) {
					vowels++
				}
			}
		case r == '-' || r == '\'' || r == '’':
		default:
			other++
		}
	}
	if letters == 0 || other*4 > letters {
		return false
	}
	if latin == letters && latin >= 3 && vowels == 0 {
		return false
	}
	return true
}

func isNumberOrSymbol(tok string) bool {
	for _, r := range tok {
		if unicode.IsLetter(r) {
			return false
		}
	}
	return true
}

func isVowel(r rune) bool {
	switch unicode.ToLower(baseLetter(r)) {
	case 'a', 'e', 'i', 'o', 'u', 'y':
		return true
	}
	return false
}

// baseLetter drops an accent from a precomposed Latin letter: é is e, ø is o.
func baseLetter(r rune) rune {
	if r < 0x80 {
		return r
	}
	switch {
	case strings.ContainsRune("àáâãäåāăąǎ", unicode.ToLower(r)):
		return 'a'
	case strings.ContainsRune("èéêëēĕėęě", unicode.ToLower(r)):
		return 'e'
	case strings.ContainsRune("ìíîïĩīĭįı", unicode.ToLower(r)):
		return 'i'
	case strings.ContainsRune("òóôõöøōŏőœ", unicode.ToLower(r)):
		return 'o'
	case strings.ContainsRune("ùúûüũūŭůűų", unicode.ToLower(r)):
		return 'u'
	case strings.ContainsRune("ýÿ", unicode.ToLower(r)):
		return 'y'
	case r == 'æ' || r == 'Æ':
		return 'a'
	}
	return r
}
