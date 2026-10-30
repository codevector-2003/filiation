package text

import (
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/codevector-2003/filiation/internal/identity"
)

// Clue is an identifier a PDF carries, and where it was found. A clue is
// never trusted: import resolves it and checks the work's title is in the
// PDF before filing anything under it (ADR-014).
type Clue struct {
	ID    identity.ID
	Where string // in words: "the arXiv stamp", "a DOI label on page 1", …
}

// maxClues is how many identifiers are tried for one PDF. Each costs a
// single OpenAlex lookup, which is free, but a page of references would
// otherwise offer dozens.
const maxClues = 6

var (
	// arXivStampLine is the stamp as it reads in the page text: the ID, a
	// subject class for new-style IDs, and the date.
	arXivStampLine = regexp.MustCompile(`(?i)\barXiv:\s*((?:\d{4}\.\d{4,5})|(?:[a-z-]+(?:\.[a-z]{2})?/\d{7}))(?:v\d+)?\s+(?:\[[^\]\n]{1,30}\]\s+)?\d{1,2}\s+[A-Z][a-z]{2}\s+\d{4}`)

	// doiPattern finds a DOI in running text. Trailing punctuation is
	// trimmed by identity.NormaliseDOI.
	// A DOI is printable ASCII; a bracket or quote set around one is not
	// part of it.
	doiPattern = regexp.MustCompile(`\b10\.\d{4,9}/[!#-;=?-Z^-z|~]+`)

	// doiPart is what may sit in parentheses inside a DOI, as in The
	// Lancet's 10.1016/S0140-6736(20)30183-5.
	doiPart = regexp.MustCompile(`^[0-9A-Za-z-]+$`)

	// doiLabel is what marks a DOI as the paper's own rather than one it
	// cites: "doi:", "DOI ", "https://doi.org/".
	doiLabel = regexp.MustCompile(`(?i)(?:\bdoi\s*:?\s*|doi\.org/)$`)

	pmidLabel = regexp.MustCompile(`\bPMID:?\s*([1-9][0-9]{4,8})\b`)

	// arXivFileName is how arXiv names a download: 1706.03762v5.pdf.
	arXivFileName = regexp.MustCompile(`^(\d{4}\.\d{4,5})(?:v\d+)?$`)

	// doiFileName is a DOI written into a file name, its slash replaced:
	// 10.1371_journal.pmed.0020124.pdf.
	doiFileName = regexp.MustCompile(`^(10\.\d{4,9})[_/](.+)$`)
)

// Clues lists the identifiers a PDF carries, the most telling first: the
// arXiv stamp; an arXiv download's file name; a DOI labelled as one in page
// 1's header or footer; the PDF's metadata; a labelled DOI anywhere on page
// 1; a labelled PMID; then any DOI on page 1, and on page 2. Page 1 carries
// other papers' DOIs — "cite this article", related articles, a short
// paper's references — which is why where a DOI sits matters.
func Clues(fileName string, f Front) []Clue {
	var out []Clue
	seen := map[string]bool{}
	add := func(raw, where string) {
		id, err := identity.Parse(raw)
		if err != nil || !id.Deterministic() || seen[id.Kind.String()+id.Value] {
			return
		}
		seen[id.Kind.String()+id.Value] = true
		out = append(out, Clue{ID: id, Where: where})
	}

	page1, page2 := "", ""
	if len(f.Pages) > 0 {
		page1 = f.Pages[0]
	}
	if len(f.Pages) > 1 {
		page2 = f.Pages[1]
	}

	for _, p := range []string{page1, page2} {
		if m := arXivStampLine.FindStringSubmatch(p); m != nil {
			add("arXiv:"+m[1], "the arXiv stamp")
		}
	}

	base := strings.TrimSuffix(filepath.Base(fileName), filepath.Ext(fileName))
	if m := arXivFileName.FindStringSubmatch(base); m != nil {
		add("arXiv:"+m[1], "the file name")
	}

	head, foot := edges(page1)
	for _, d := range dois(head + "\n" + foot) {
		if d.labelled {
			add(d.doi, "a DOI label in page 1's header or footer")
		}
	}
	for _, k := range metaKeys {
		for _, d := range dois(f.Meta[k]) {
			add(d.doi, "the PDF's metadata")
		}
	}
	for _, d := range dois(page1) {
		if d.labelled {
			add(d.doi, "a DOI label on page 1")
		}
	}
	for _, p := range []string{page1, page2} {
		if m := pmidLabel.FindStringSubmatch(p); m != nil {
			add("pmid:"+m[1], "a PMID label")
		}
	}
	if m := doiFileName.FindStringSubmatch(base); m != nil {
		add(m[1]+"/"+m[2], "the file name")
	}
	for i, p := range []string{page1, page2} {
		for _, d := range dois(p) {
			add(d.doi, []string{"page 1", "page 2"}[i])
		}
	}
	if len(out) > maxClues {
		out = out[:maxClues]
	}
	return out
}

// edges returns a page's first and last few lines, where a journal prints
// the paper's own DOI.
func edges(page string) (head, foot string) {
	const n = 5
	lines := strings.Split(page, "\n")
	head = strings.Join(lines[:min(n, len(lines))], "\n")
	foot = strings.Join(lines[max(0, len(lines)-n):], "\n")
	return head, foot
}

type foundDOI struct {
	doi      string
	labelled bool
}

// dois finds the DOIs in s, in order. A DOI broken across a line — the line
// ends on ".", "-", "/" or "_" and the next begins with more of it — is
// joined again: narrow columns break them.
func dois(s string) []foundDOI {
	var out []foundDOI
	for _, loc := range doiPattern.FindAllStringIndex(s, -1) {
		d := s[loc[0]:loc[1]]
		end := loc[1]
		if end < len(s) && s[end] == '\n' && strings.ContainsAny(d[len(d)-1:], ".-/_") {
			rest := s[end+1:]
			if cut := strings.IndexAny(rest, " \n\t"); cut > 0 {
				rest = rest[:cut]
			}
			d += rest
		}
		before := s[max(0, loc[0]-16):loc[0]]
		out = append(out, foundDOI{doi: trimDOI(d), labelled: doiLabel.MatchString(before)})
	}
	return out
}

// trimDOI cuts a DOI at a parenthesis that does not belong to it: Springer
// sets "(0123456789().,-volV)" in invisible text straight after its DOIs.
func trimDOI(d string) string {
	for i := 0; i < len(d); i++ {
		if d[i] != '(' {
			continue
		}
		end := strings.IndexByte(d[i:], ')')
		if end < 0 || !doiPart.MatchString(d[i+1:i+end]) {
			return d[:i]
		}
		i += end
	}
	return d
}

// Mentions reports whether a work's title, and its first author's surname
// when known, appear in the text of a PDF's first pages: the check that
// lets a clue be trusted (ADR-014). Case, spacing, punctuation and line
// breaks are ignored. A title whose full form is not found passes on its
// main title — the part before a colon — if that is long enough to be
// telling, since subtitles are set apart or dropped.
func Mentions(pages []string, title, surname string) bool {
	text := identity.TitleKey(fold(strings.Join(pages, " ")))
	key := identity.TitleKey(fold(title))
	if len(key) < 10 || text == "" {
		return false
	}
	s := identity.TitleKey(fold(surname))
	if s != "" && !hasWord(pages, surname) {
		return false
	}
	if strings.Contains(text, key) {
		return true
	}
	// The main title alone is weaker evidence — "Attention is all you need"
	// is also the start of other papers' titles — so it counts only with
	// the first author's surname found too.
	if main, _, ok := strings.Cut(title, ":"); ok && s != "" {
		if k := identity.TitleKey(fold(main)); len(k) >= 20 {
			return strings.Contains(text, k)
		}
	}
	return false
}

// hasWord reports whether surname is among the words of pages. A surname is
// looked for as a word: "Ma" is in "parmar" and "models", and matching
// inside words would let any paper by Ma pass.
//
// Words are runs of letters: an affiliation mark set against a name
// ("Alicea1", "Foster∗") does not hide it. A surname of several words —
// "Aspuru-Guzik", "Martínez-Cantín" — must be there as those words in a
// row, whatever joins them: a hyphen of another kind, or a line break.
func hasWord(pages []string, surname string) bool {
	var want []string
	for _, w := range letterWords(surname) {
		if k := identity.TitleKey(fold(w)); k != "" {
			want = append(want, k)
		}
	}
	if len(want) == 0 {
		return false
	}
	for _, p := range pages {
		words := letterWords(p)
		for i := 0; i+len(want) <= len(words); i++ {
			if inRow(words[i:i+len(want)], want) {
				return true
			}
		}
	}
	return false
}

func inRow(words, want []string) bool {
	for j, k := range want {
		if identity.TitleKey(fold(words[j])) != k {
			return false
		}
	}
	return true
}

func letterWords(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) })
}

// headLines is how far down page 1 a title is looked for by Resembles: past
// a journal's running head and the arXiv stamp, short of the references.
const headLines = 12

// Resembles is the looser check, for a work an identifier in the PDF itself
// named: a preprint's title is often not the published one OpenAlex keeps —
// "2D" for "two-dimensional", "the" dropped. It passes when the first
// author's surname is in the PDF as a word, and most of the title's words —
// three letters or more, at least 60% — are in the top lines of page 1,
// where a paper's own title sits; a cited paper's title sits in the
// references, further down. A title search's candidates get Mentions only.
func Resembles(pages []string, title, surname string) bool {
	s := identity.TitleKey(fold(surname))
	if len(pages) == 0 || s == "" || !hasWord(pages, surname) {
		return false
	}
	lines := strings.Split(pages[0], "\n")
	head := map[string]bool{}
	for _, w := range letterWords(strings.Join(lines[:min(headLines, len(lines))], " ")) {
		head[fold(w)] = true
	}
	want, found := 0, 0
	for _, w := range letterWords(title) {
		if len([]rune(w)) < 3 {
			continue
		}
		want++
		if head[fold(w)] {
			found++
		}
	}
	return want >= 3 && found*10 >= want*6
}
