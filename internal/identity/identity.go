package identity

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"

	"github.com/codevector-2003/filiation/internal/errs"
)

// Kind is what an input was recognised as.
type Kind int

const (
	KindUnknown Kind = iota
	KindOpenAlex
	KindDOI
	KindArXiv
	KindPMID
	KindTitle
)

// String names the kind the way a user would, for messages like
// "resolving DOI 10.1145/...".
func (k Kind) String() string {
	switch k {
	case KindOpenAlex:
		return "OpenAlex ID"
	case KindDOI:
		return "DOI"
	case KindArXiv:
		return "arXiv ID"
	case KindPMID:
		return "PMID"
	case KindTitle:
		return "title"
	default:
		return "unknown identifier"
	}
}

// ID is one parsed input.
type ID struct {
	Kind Kind

	// Value is the normalised form, and the only form anything downstream
	// should use. Two inputs naming the same paper by the same scheme always
	// normalise to the same Value; that is the whole job of this package.
	Value string

	// Raw is exactly what the user typed, kept for error messages. A user who
	// pasted five arguments needs to see which one failed in the form they
	// pasted it, not in ours.
	Raw string
}

// Deterministic reports whether this ID names exactly one work.
//
// Every kind does except a title. A title search returns candidates, and per
// ADR-005 the CLI must show them and wait, or be given --accept-first
// explicitly. This is the flag it checks.
func (id ID) Deterministic() bool { return id.Kind != KindTitle && id.Kind != KindUnknown }

// arXivDOIPrefix is the DataCite prefix arXiv mints DOIs under, for every paper
// including those submitted before it started. Lowercase, because DOIs are
// case-insensitive and this package stores them folded.
const arXivDOIPrefix = "10.48550/arxiv."

// ArXivDOI is the DOI arXiv has minted for this paper, or the empty string if
// the ID is not an arXiv ID.
//
// It exists because a lookup needs something OpenAlex indexes, and DOIs are.
// Whether OpenAlex resolves every arXiv DOI is a question for
// internal/sources/openalex to answer against a recorded response, not for
// this package to assume; this package only knows the mapping, which is a fact
// about arXiv.
func (id ID) ArXivDOI() string {
	if id.Kind != KindArXiv {
		return ""
	}
	return arXivDOIPrefix + strings.ToLower(id.Value)
}

var (
	// prefixed matches "doi:10...", "arXiv 1706...", "PMID: 123", and so on.
	// It needs a colon or whitespace after the scheme, so a title that begins
	// "Doing..." or "Arxival..." is never mistaken for one.
	prefixed = regexp.MustCompile(`(?i)^(doi|arxiv|pmid|openalex)(?:\s*:\s*|\s+)(\S.*)$`)

	// openAlexWork is a work ID; the leading zero is refused so that one work
	// cannot be spelled two ways. Case is folded afterwards.
	openAlexWork = regexp.MustCompile(`(?i)^w([1-9][0-9]*)$`)

	// openAlexLike catches anything shaped like an OpenAlex ID that fails the
	// strict pattern — "W0123" — so it is refused instead of being searched for
	// as a title.
	openAlexLike = regexp.MustCompile(`(?i)^w[0-9]+$`)

	// openAlexEntity is any OpenAlex entity ID, used only to say "that is an
	// author, not a work" instead of a generic refusal.
	openAlexEntity = regexp.MustCompile(`^([A-Z])[1-9][0-9]*$`)

	// doiPattern is a normalised DOI: "10.", a registrant code of four to nine
	// digits with optional subdivisions, a slash, and a suffix with no
	// whitespace. The suffix is otherwise unrestricted — DOIs really do contain
	// parentheses, colons, semicolons and angle brackets.
	doiPattern = regexp.MustCompile(`^10\.[0-9]{4,9}(?:\.[0-9]+)*/\S+$`)

	// doiInPath finds a DOI inside a publisher URL path.
	doiInPath = regexp.MustCompile(`10\.[0-9]{4,9}(?:\.[0-9]+)*/.+`)

	// urlDOITail is what a publisher URL appends after the DOI in its path: a
	// file extension (".pdf", stacked as ".full.pdf") or a view segment
	// ("/full", "/epdf"). Neither is part of the DOI, and capturing it produces
	// a DOI that resolves to nothing. Applied only to DOIs cut out of URLs — a
	// DOI the user typed is taken as typed.
	urlDOITail = regexp.MustCompile(`(?i)(?:\.(?:pdf|html?|xml|txt|full|abstract)|/(?:full|abstract|pdf|epdf|html))$`)

	// preprintDOIVersion is the version suffix bioRxiv and medRxiv (both
	// registered under 10.1101) put after the DOI in their URLs:
	// ".../10.1101/2020.03.22.002386v1". The DOI itself has no version.
	preprintDOIVersion = regexp.MustCompile(`^(10\.1101/[0-9.]+)v[0-9]+$`)

	// arXivNew is the scheme in use since April 2007: YYMM.NNNN, five digits
	// from 2015. The month is checked separately.
	arXivNew = regexp.MustCompile(`^([0-9]{2})([0-9]{2})\.[0-9]{4,5}$`)

	// arXivOld is the pre-2007 scheme: archive, optional upper-case subject
	// class, slash, seven digits. "hep-th/9901001", "math.GT/0309136".
	arXivOld = regexp.MustCompile(`^[a-z]+(?:-[a-z]+)*(?:\.[A-Z]{2})?/[0-9]{7}$`)

	// arXivVersion is the trailing version, which does not change which paper
	// is meant and so is not part of the identity.
	arXivVersion = regexp.MustCompile(`v[0-9]+$`)

	// pmidPattern is a PubMed ID with no leading zero.
	pmidPattern = regexp.MustCompile(`^[1-9][0-9]*$`)

	// hostLike recognises a URL pasted without its scheme — "doi.org/10...",
	// "arxiv.org/abs/..." — by requiring the last host label to be letters.
	// That keeps "10.1145/..." and "1706.03762" out of it.
	hostLike = regexp.MustCompile(`(?i)^(?:[a-z0-9-]+\.)+[a-z]{2,}/`)
)

// minBarePMID is the shortest run of digits accepted as a PMID without a
// "PMID:" prefix. Shorter numbers are more often something else — a year, a
// page, a volume — and silently looking one up would seed a graph from an
// unrelated paper. PMIDs this short date from the 1970s and can still be given
// with the prefix.
const minBarePMID = 5

// Parse classifies input and normalises it.
//
// The order of the checks is the design. The most specific forms go first, so
// that nothing reaches the title fallback that could have been read as an
// identifier: a malformed identifier is refused with ErrInvalidInput rather
// than searched for as a title. Falling through would show the user a list of
// candidates for something that was never a title, which is the wrong question
// to be asking them.
func Parse(input string) (ID, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return ID{}, invalid(input, "it is empty")
	}

	// 1. An explicit scheme prefix settles the kind; the value must then be
	// valid for that kind, with no fallback.
	if m := prefixed.FindStringSubmatch(s); m != nil {
		return parsePrefixed(input, strings.ToLower(m[1]), strings.TrimSpace(m[2]))
	}

	// 2. Bare identifiers.
	switch {
	case openAlexLike.MatchString(s):
		v, err := NormaliseOpenAlexID(s)
		if err != nil {
			return ID{}, invalid(input, "it looks like an OpenAlex ID but is not one")
		}
		return ID{Kind: KindOpenAlex, Value: v, Raw: input}, nil

	case strings.HasPrefix(s, "10."):
		v, err := NormaliseDOI(s)
		if err != nil {
			return ID{}, invalid(input, "it starts like a DOI but is not one (a DOI looks like 10.1145/3292500)")
		}
		return ID{Kind: KindDOI, Value: v, Raw: input}, nil

	case isDigits(s):
		return parseBareNumber(input, s)
	}
	if v, ok := normaliseArXiv(s); ok {
		return ID{Kind: KindArXiv, Value: v, Raw: input}, nil
	}

	// 3. URLs, with or without a scheme.
	if strings.Contains(s, "://") || hostLike.MatchString(s) {
		return parseURL(input, s)
	}

	// 4. Whatever is left is a title, provided it has words in it.
	return parseTitle(input, s)
}

func parsePrefixed(raw, scheme, value string) (ID, error) {
	switch scheme {
	case "doi":
		v, err := NormaliseDOI(value)
		if err != nil {
			return ID{}, invalid(raw, "it is marked as a DOI but is not one (a DOI looks like 10.1145/3292500)")
		}
		return ID{Kind: KindDOI, Value: v, Raw: raw}, nil

	case "arxiv":
		v, ok := normaliseArXiv(value)
		if !ok {
			return ID{}, invalid(raw, "it is marked as an arXiv ID but is not one (one looks like 1706.03762 or hep-th/9901001)")
		}
		return ID{Kind: KindArXiv, Value: v, Raw: raw}, nil

	case "pmid":
		// Prefixed, a PMID of any length is accepted: the user has said what
		// it is, which is the whole point of asking them to.
		if !pmidPattern.MatchString(value) {
			return ID{}, invalid(raw, "it is marked as a PMID but is not a positive whole number")
		}
		return ID{Kind: KindPMID, Value: value, Raw: raw}, nil

	default: // "openalex"
		v, err := NormaliseOpenAlexID(value)
		if err != nil {
			return ID{}, invalid(raw, "it is marked as an OpenAlex ID but is not a work ID (one looks like W2741809807)")
		}
		return ID{Kind: KindOpenAlex, Value: v, Raw: raw}, nil
	}
}

func parseBareNumber(raw, s string) (ID, error) {
	if len(s) >= minBarePMID && pmidPattern.MatchString(s) {
		return ID{Kind: KindPMID, Value: s, Raw: raw}, nil
	}
	return ID{}, invalid(raw, fmt.Sprintf(
		"a number this short could be a year or a page, so it is not read as a PMID — "+
			"write PMID:%s if that is what you meant", strings.TrimLeft(s, "0")))
}

func parseURL(raw, s string) (ID, error) {
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return ID{}, invalid(raw, "it looks like a link but could not be read as one")
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	path := strings.Trim(u.Path, "/")

	switch host {
	case "doi.org", "dx.doi.org":
		v, err := NormaliseDOI(path)
		if err != nil {
			return ID{}, invalid(raw, "it is a doi.org link with no DOI in it")
		}
		return ID{Kind: KindDOI, Value: v, Raw: raw}, nil

	case "openalex.org", "api.openalex.org":
		v, err := NormaliseOpenAlexID(path)
		if err != nil {
			if e := openAlexEntity.FindStringSubmatch(lastSegment(path)); e != nil && e[1] != "W" {
				return ID{}, invalid(raw, "it is an OpenAlex "+entityName(e[1])+", not a work")
			}
			return ID{}, invalid(raw, "it is an OpenAlex link with no work ID in it")
		}
		return ID{Kind: KindOpenAlex, Value: v, Raw: raw}, nil

	case "arxiv.org", "export.arxiv.org":
		for _, p := range []string{"abs/", "pdf/", "html/"} {
			if rest, ok := strings.CutPrefix(path, p); ok {
				if v, ok := normaliseArXiv(rest); ok {
					return ID{Kind: KindArXiv, Value: v, Raw: raw}, nil
				}
			}
		}
		return ID{}, invalid(raw, "it is an arXiv link but not to a paper (use the /abs/, /pdf/ or /html/ link)")

	case "pubmed.ncbi.nlm.nih.gov":
		if pmidPattern.MatchString(path) {
			return ID{Kind: KindPMID, Value: path, Raw: raw}, nil
		}
		return ID{}, invalid(raw, "it is a PubMed link but not to an article")

	case "ncbi.nlm.nih.gov":
		if rest, ok := strings.CutPrefix(path, "pubmed/"); ok && pmidPattern.MatchString(rest) {
			return ID{Kind: KindPMID, Value: rest, Raw: raw}, nil
		}
		return ID{}, invalid(raw, "it is an NCBI link but not to a PubMed article")
	}

	// Any other site: many publishers put the DOI in the path, under /doi/ or
	// directly. u.Path is already percent-decoded.
	if m := doiInPath.FindString(u.Path); m != "" {
		if v, err := NormaliseDOI(trimURLDOI(m)); err == nil {
			return ID{Kind: KindDOI, Value: v, Raw: raw}, nil
		}
	}
	return ID{}, invalid(raw, "it is a link with no DOI, arXiv ID, PMID or OpenAlex ID in it — "+
		"try the paper's DOI instead")
}

func parseTitle(raw, s string) (ID, error) {
	s = strings.Trim(s, `"'“”‘’`)
	s = strings.Join(strings.Fields(s), " ")
	if !strings.ContainsFunc(s, unicode.IsLetter) {
		return ID{}, invalid(raw, "it has no words in it to search for as a title")
	}
	return ID{Kind: KindTitle, Value: s, Raw: raw}, nil
}

// NormaliseOpenAlexID reduces any spelling of an OpenAlex work ID to the bare
// form the store keys on: "W2741809807".
//
// It accepts the URL forms OpenAlex itself returns — https://openalex.org/W...
// and https://api.openalex.org/works/W... — because sources/openalex runs every
// ID in every response through here. That is where the URL form is the norm, and
// where a missed normalisation would create a second node for the same paper.
func NormaliseOpenAlexID(s string) (string, error) {
	s = strings.TrimSpace(s)
	s, _, _ = strings.Cut(s, "?")
	s, _, _ = strings.Cut(s, "#")
	m := openAlexWork.FindStringSubmatch(lastSegment(s))
	if m == nil {
		return "", fmt.Errorf("identity: %q is not an OpenAlex work ID such as W2741809807: %w",
			s, errs.ErrInvalidInput)
	}
	return "W" + m[1], nil
}

// NormaliseDOI reduces a DOI to the form the store keys on: no resolver prefix,
// percent-decoded, stray trailing punctuation removed, and lower-cased.
//
// Lower-casing is safe because DOIs are case-insensitive by specification, and
// necessary because the same DOI arrives in different cases from the user,
// from OpenAlex and from publisher pages; the UNIQUE index on work.doi can only
// catch duplicates it can see.
func NormaliseDOI(s string) (string, error) {
	s = strings.TrimSpace(s)
	lower := strings.ToLower(s)
	for _, p := range []string{
		"https://doi.org/", "http://doi.org/",
		"https://dx.doi.org/", "http://dx.doi.org/",
		"doi.org/", "dx.doi.org/", "doi:",
	} {
		if strings.HasPrefix(lower, p) {
			s = strings.TrimSpace(s[len(p):])
			break
		}
	}
	if strings.Contains(s, "%") {
		if dec, err := url.PathUnescape(s); err == nil {
			s = dec
		}
	}
	s = trimDOITail(s)
	s = strings.ToLower(s)

	if !doiPattern.MatchString(s) {
		return "", fmt.Errorf("identity: %q is not a DOI such as 10.1145/3292500: %w",
			s, errs.ErrInvalidInput)
	}
	return s, nil
}

// trimDOITail removes what a DOI picks up from the sentence it was copied out
// of: a full stop, a comma, a semicolon, or the closing bracket of a
// parenthetical. A closing bracket is removed only when unbalanced, because
// real DOIs contain balanced ones — 10.1016/S0140-6736(97)11096-0.
func trimDOITail(s string) string {
	for s != "" {
		switch last := s[len(s)-1]; {
		case last == '.' || last == ',' || last == ';':
			s = s[:len(s)-1]
		case last == ')' && strings.Count(s, ")") > strings.Count(s, "("):
			s = s[:len(s)-1]
		default:
			return s
		}
	}
	return s
}

// trimURLDOI strips what a publisher URL wraps around a DOI: stacked file
// extensions and view segments, then a preprint server's version suffix.
func trimURLDOI(s string) string {
	for {
		t := urlDOITail.ReplaceAllString(s, "")
		if t == s {
			break
		}
		s = t
	}
	return preprintDOIVersion.ReplaceAllString(s, "$1")
}

// normaliseArXiv reduces an arXiv ID to its unversioned form. The version is
// dropped because v1 and v5 are the same paper to a citation graph; keeping it
// would make them two nodes.
func normaliseArXiv(s string) (string, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, ".pdf")
	s = arXivVersion.ReplaceAllString(s, "")

	if m := arXivNew.FindStringSubmatch(s); m != nil {
		if m[2] < "01" || m[2] > "12" {
			return "", false
		}
		return s, true
	}
	if arXivOld.MatchString(s) {
		return s, true
	}
	return "", false
}

func invalid(raw, why string) error {
	return fmt.Errorf("%q is not something fil can look up: %s: %w",
		strings.TrimSpace(raw), why, errs.ErrInvalidInput)
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

func lastSegment(s string) string {
	s = strings.TrimRight(s, "/")
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		return s[i+1:]
	}
	return s
}

func entityName(letter string) string {
	switch letter {
	case "A":
		return "author"
	case "S":
		return "source"
	case "I":
		return "institution"
	case "T":
		return "topic"
	case "P":
		return "publisher"
	case "F":
		return "funder"
	default:
		return "entity"
	}
}
