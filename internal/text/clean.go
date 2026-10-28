package text

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Clean turns one page of PDFium's output into the text fil stores: one
// newline per line break, single spaces, words broken across lines joined
// again, Unicode NFC. Line breaks are kept, because chunking and the
// references heading are found by line.
//
// What PDFium hands back, measured on real papers:
//
//   - lines end in "\r\n";
//   - a word hyphenated at the end of a line arrives already joined, with
//     U+0002 where the hyphen was ("de\x02scribe"). Mostly syllable breaks,
//     so the mark is dropped; a compound broken at its hyphen ("self-archiving")
//     loses the hyphen too, as it does in pdftotext;
//   - U+00AD, the soft hyphen, stands in for a printed hyphen inside a line
//     ("Ninety­seven");
//   - a hyphen that does end a line is a range ("1992-\n2003"), so the line
//     break goes and the hyphen stays;
//   - other control characters are glyphs with no Unicode meaning.
func Clean(page string) string {
	page = strings.ReplaceAll(page, "\r\n", "\n")
	page = strings.ReplaceAll(page, "\r", "\n")
	page = norm.NFC.String(page)

	var b strings.Builder
	b.Grow(len(page))
	var last rune = '\n' // the last rune written; a page starts on a new line
	space := false       // a space is due before the next word
	write := func(s string) {
		if space && last != '\n' {
			b.WriteByte(' ')
		}
		space = false
		b.WriteString(s)
		last, _ = utf8.DecodeLastRuneInString(s)
	}

	for i := 0; i < len(page); {
		r, size := utf8.DecodeRuneInString(page[i:])
		i += size
		next, _ := utf8.DecodeRuneInString(page[i:])

		switch {
		case r == '\n':
			space = false
			if last == '-' && unicode.IsDigit(next) {
				continue // a range broken across lines
			}
			b.WriteByte('\n')
			last = '\n'
		case r == '\x02':
			if !unicode.IsLetter(last) || !unicode.IsLetter(next) {
				space = true
			} // else a line-end hyphen; the halves are already together
		case r == '­':
			if next != '\n' {
				write("-")
			}
		case unicode.IsSpace(r), unicode.IsControl(r), unicode.Is(unicode.Co, r):
			// Control and private-use characters are glyphs with no meaning in
			// Unicode — pieces of large formula brackets, a font's own codes.
			// Neither is a word, so each separates words.
			space = true
		default:
			if l, ok := ligatures[r]; ok {
				write(l)
			} else {
				write(string(r))
			}
		}
	}
	return collapseBlankLines(strings.Trim(b.String(), "\n"))
}

// ligatures PDFium leaves alone are spelt out, so "ﬁnd" is found by "find".
// Spike 10 saw none left by PDFium; a PDF that maps its glyphs oddly can
// still produce them.
var ligatures = map[rune]string{
	'ﬀ': "ff", 'ﬁ': "fi", 'ﬂ': "fl", 'ﬃ': "ffi", 'ﬄ': "ffl", 'ﬅ': "st", 'ﬆ': "st",
}

// collapseBlankLines keeps at most one empty line in a row: a paragraph
// break survives, a page of blank lines does not.
func collapseBlankLines(s string) string {
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return s
}
