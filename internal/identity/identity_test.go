package identity

import (
	"errors"
	"strings"
	"testing"

	"github.com/codevector-2003/filiation/internal/errs"
)

// TestParse is the contract. Every row is a form someone will actually type or
// paste, and the expected value is the one the rest of the tool relies on to
// deduplicate — so a row changing its answer is a change to what counts as the
// same paper.
func TestParse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		kind  Kind
		value string
	}{
		// ---- OpenAlex IDs
		{"bare", "W2741809807", KindOpenAlex, "W2741809807"},
		{"lowercase", "w2741809807", KindOpenAlex, "W2741809807"},
		{"prefixed", "openalex:W2741809807", KindOpenAlex, "W2741809807"},
		{"url", "https://openalex.org/W2741809807", KindOpenAlex, "W2741809807"},
		{"url without scheme", "openalex.org/W2741809807", KindOpenAlex, "W2741809807"},
		{"api url", "https://api.openalex.org/works/W2741809807", KindOpenAlex, "W2741809807"},
		{"api url with query", "https://api.openalex.org/works/W2741809807?mailto=a@b.c", KindOpenAlex, "W2741809807"},
		{"surrounding space", "  W2741809807\n", KindOpenAlex, "W2741809807"},

		// ---- DOIs: the five formats, then what people actually paste
		{"doi bare", "10.1145/3292500.3330701", KindDOI, "10.1145/3292500.3330701"},
		{"doi prefixed", "doi:10.1145/3292500.3330701", KindDOI, "10.1145/3292500.3330701"},
		{"doi prefixed upper with space", "DOI: 10.1145/3292500.3330701", KindDOI, "10.1145/3292500.3330701"},
		{"doi https", "https://doi.org/10.1145/3292500.3330701", KindDOI, "10.1145/3292500.3330701"},
		{"doi dx", "http://dx.doi.org/10.1145/3292500.3330701", KindDOI, "10.1145/3292500.3330701"},
		{"doi host no scheme", "doi.org/10.1145/3292500.3330701", KindDOI, "10.1145/3292500.3330701"},
		{"doi uppercase folded", "10.1016/S0140-6736(97)11096-0", KindDOI, "10.1016/s0140-6736(97)11096-0"},
		{"doi sentence full stop", "10.1145/3292500.3330701.", KindDOI, "10.1145/3292500.3330701"},
		{"doi trailing comma", "10.1145/3292500.3330701,", KindDOI, "10.1145/3292500.3330701"},
		{"doi unbalanced close paren", "(see 10.1038/nature14539)", KindTitle, "(see 10.1038/nature14539)"},
		{"doi pasted with close paren", "10.1038/nature14539)", KindDOI, "10.1038/nature14539"},
		{"doi balanced parens kept", "10.1002/(SICI)1097-4571(199806)49:8<693::AID-ASI3>3.0.CO;2-O", KindDOI, "10.1002/(sici)1097-4571(199806)49:8<693::aid-asi3>3.0.co;2-o"},
		{"doi percent-encoded slash", "https://doi.org/10.1145%2F3292500.3330701", KindDOI, "10.1145/3292500.3330701"},
		{"doi subdivided registrant", "10.1000.10/123456", KindDOI, "10.1000.10/123456"},
		{"arxiv datacite doi", "10.48550/arXiv.1706.03762", KindDOI, "10.48550/arxiv.1706.03762"},

		// ---- DOIs inside publisher URLs
		{"acm", "https://dl.acm.org/doi/10.1145/3292500.3330701", KindDOI, "10.1145/3292500.3330701"},
		{"acm pdf", "https://dl.acm.org/doi/pdf/10.1145/3292500.3330701", KindDOI, "10.1145/3292500.3330701"},
		{"wiley", "https://onlinelibrary.wiley.com/doi/full/10.1002/asi.24301", KindDOI, "10.1002/asi.24301"},
		{"springer", "https://link.springer.com/article/10.1007/s11192-020-03690-4", KindDOI, "10.1007/s11192-020-03690-4"},
		{"tandf", "https://www.tandfonline.com/doi/abs/10.1080/01621459.2017.1285773", KindDOI, "10.1080/01621459.2017.1285773"},
		{"doi url stacked extensions", "https://www.pnas.org/doi/10.1073/pnas.1719367115.full.pdf", KindDOI, "10.1073/pnas.1719367115"},
		{"doi url view segment", "https://onlinelibrary.wiley.com/doi/10.1002/asi.24301/full", KindDOI, "10.1002/asi.24301"},
		{"doi url epdf", "https://onlinelibrary.wiley.com/doi/epdf/10.1002/asi.24301", KindDOI, "10.1002/asi.24301"},
		{"biorxiv versioned", "https://www.biorxiv.org/content/10.1101/2020.03.22.002386v1", KindDOI, "10.1101/2020.03.22.002386"},
		{"biorxiv full pdf", "https://www.biorxiv.org/content/10.1101/2020.03.22.002386v2.full.pdf", KindDOI, "10.1101/2020.03.22.002386"},
		{"medrxiv", "https://www.medrxiv.org/content/10.1101/2020.04.14.20062463v1.full", KindDOI, "10.1101/2020.04.14.20062463"},
		// A DOI the user typed is taken as typed: the tail trimming applies to
		// URLs only.
		{"typed doi ending in v1 kept", "10.5555/abc.v1", KindDOI, "10.5555/abc.v1"},

		// ---- arXiv, current scheme
		{"arxiv bare", "1706.03762", KindArXiv, "1706.03762"},
		{"arxiv five digit", "2310.06825", KindArXiv, "2310.06825"},
		{"arxiv versioned", "1706.03762v5", KindArXiv, "1706.03762"},
		{"arxiv prefixed", "arXiv:1706.03762", KindArXiv, "1706.03762"},
		{"arxiv prefixed versioned", "arXiv:1706.03762v5", KindArXiv, "1706.03762"},
		{"arxiv prefixed with space", "arxiv 1706.03762", KindArXiv, "1706.03762"},
		{"arxiv abs url", "https://arxiv.org/abs/1706.03762", KindArXiv, "1706.03762"},
		{"arxiv abs url versioned", "https://arxiv.org/abs/1706.03762v5", KindArXiv, "1706.03762"},
		{"arxiv pdf url", "https://arxiv.org/pdf/1706.03762v5.pdf", KindArXiv, "1706.03762"},
		{"arxiv pdf url no ext", "https://arxiv.org/pdf/1706.03762", KindArXiv, "1706.03762"},
		{"arxiv url no scheme", "arxiv.org/abs/1706.03762", KindArXiv, "1706.03762"},
		{"arxiv www", "https://www.arxiv.org/abs/1706.03762", KindArXiv, "1706.03762"},
		{"arxiv html", "https://arxiv.org/html/2310.06825v1", KindArXiv, "2310.06825"},

		// ---- arXiv, pre-2007 scheme
		{"arxiv old", "hep-th/9901001", KindArXiv, "hep-th/9901001"},
		{"arxiv old subject class", "math.GT/0309136", KindArXiv, "math.GT/0309136"},
		{"arxiv old versioned", "hep-th/9901001v2", KindArXiv, "hep-th/9901001"},
		{"arxiv old prefixed", "arXiv:cs/0112017", KindArXiv, "cs/0112017"},
		{"arxiv old url", "https://arxiv.org/abs/hep-th/9901001", KindArXiv, "hep-th/9901001"},

		// ---- PMIDs
		{"pmid prefixed", "PMID:29051481", KindPMID, "29051481"},
		{"pmid prefixed lower space", "pmid: 29051481", KindPMID, "29051481"},
		{"pmid short but prefixed", "PMID:1234", KindPMID, "1234"},
		{"pmid bare", "29051481", KindPMID, "29051481"},
		{"pmid bare five digits", "12345", KindPMID, "12345"},
		{"pubmed url", "https://pubmed.ncbi.nlm.nih.gov/29051481/", KindPMID, "29051481"},
		{"old pubmed url", "https://www.ncbi.nlm.nih.gov/pubmed/29051481", KindPMID, "29051481"},

		// ---- titles
		{"title", "Attention Is All You Need", KindTitle, "Attention Is All You Need"},
		{"title whitespace collapsed", "  Attention   Is\tAll You\nNeed ", KindTitle, "Attention Is All You Need"},
		{"title quoted", `"Attention Is All You Need"`, KindTitle, "Attention Is All You Need"},
		{"title with digits", "BERT: Pre-training of Deep Bidirectional Transformers", KindTitle, "BERT: Pre-training of Deep Bidirectional Transformers"},
		{"title starting with a keyword-like word", "Doing research with citation graphs", KindTitle, "Doing research with citation graphs"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse(%q) error = %v", tt.input, err)
			}
			if got.Kind != tt.kind || got.Value != tt.value {
				t.Errorf("Parse(%q) = %s %q, want %s %q",
					tt.input, got.Kind, got.Value, tt.kind, tt.value)
			}
			if got.Raw != tt.input {
				t.Errorf("Raw = %q, want the input unchanged", got.Raw)
			}
		})
	}
}

// TestParseRejects covers input that must be refused rather than guessed at.
// Each of these, if it fell through to a title search instead, would put a
// candidate list in front of the user for something that was never a title —
// or worse, look a real paper up by a number the user did not mean.
func TestParseRejects(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"whitespace", "  \t\n "},
		{"punctuation only", "?!."},
		{"digits too short for a bare PMID", "2017"},
		{"digits with a leading zero", "012345"},
		{"doi prefix only", "10."},
		{"doi no suffix", "10.1145/"},
		{"doi short registrant", "10.12/abc"},
		{"doi with inner space", "10.1145/3292 500"},
		{"doi prefixed but not a doi", "doi:hello"},
		{"doi url with no doi", "https://doi.org/"},
		{"openalex leading zero", "W0123"},
		{"openalex author not work", "https://openalex.org/A5023888391"},
		{"openalex url with no id", "https://openalex.org/"},
		{"arxiv prefixed but not arxiv", "arXiv:hello"},
		{"arxiv bad month", "1713.03762"},
		{"arxiv url with no id", "https://arxiv.org/list/cs.AI/recent"},
		{"pmid prefixed but not digits", "PMID:abc"},
		{"pmid prefixed zero", "PMID:0"},
		{"pubmed url with no id", "https://pubmed.ncbi.nlm.nih.gov/?term=attention"},
		{"unrelated url", "https://www.nature.com/articles/s41586-020-2649-2"},
		{"unrelated url no scheme", "example.com/paper"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Parse(tt.input)
			if !errors.Is(err, errs.ErrInvalidInput) {
				t.Errorf("Parse(%q) = %s %q, %v; want ErrInvalidInput",
					tt.input, got.Kind, got.Value, err)
			}
		})
	}
}

// A refusal has to tell the user what was received, or they cannot see which
// of several pasted arguments was the bad one.
func TestParseErrorNamesTheInput(t *testing.T) {
	t.Parallel()
	_, err := Parse("2017")
	if err == nil {
		t.Fatal("Parse(\"2017\") succeeded")
	}
	const want = `"2017"`
	if msg := err.Error(); !strings.Contains(msg, want) {
		t.Errorf("error %q does not quote the input %s", msg, want)
	}
	// And a short number says how to force the PMID reading.
	if msg := err.Error(); !strings.Contains(msg, "PMID:") {
		t.Errorf("error %q does not say how to write it as a PMID", msg)
	}
}

// Only a title may be ambiguous (ADR-005). Everything else resolves silently,
// so this is the one flag the CLI checks before accepting a result.
func TestDeterministic(t *testing.T) {
	t.Parallel()
	for input, want := range map[string]bool{
		"W2741809807":               true,
		"10.1145/3292500.3330701":   true,
		"1706.03762":                true,
		"PMID:29051481":             true,
		"Attention Is All You Need": false,
	} {
		id, err := Parse(input)
		if err != nil {
			t.Fatalf("Parse(%q): %v", input, err)
		}
		if got := id.Deterministic(); got != want {
			t.Errorf("Parse(%q).Deterministic() = %v, want %v", input, got, want)
		}
	}
}

func TestArXivDOI(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input, want string
	}{
		{"1706.03762v5", "10.48550/arxiv.1706.03762"},
		{"hep-th/9901001", "10.48550/arxiv.hep-th/9901001"},
		{"math.GT/0309136", "10.48550/arxiv.math.gt/0309136"},
	}
	for _, tt := range tests {
		id, err := Parse(tt.input)
		if err != nil {
			t.Fatalf("Parse(%q): %v", tt.input, err)
		}
		if got := id.ArXivDOI(); got != tt.want {
			t.Errorf("ArXivDOI(%q) = %q, want %q", tt.input, got, tt.want)
		}
		// The minted DOI must itself parse to the same normalised form, or a
		// paper added once by arXiv ID and once by its DOI becomes two lookups.
		viaDOI, err := Parse(id.ArXivDOI())
		if err != nil {
			t.Fatalf("Parse(ArXivDOI(%q)): %v", tt.input, err)
		}
		if viaDOI.Kind != KindDOI || viaDOI.Value != tt.want {
			t.Errorf("Parse(ArXivDOI(%q)) = %s %q, want DOI %q",
				tt.input, viaDOI.Kind, viaDOI.Value, tt.want)
		}
	}

	notArXiv, _ := Parse("10.1145/3292500.3330701")
	if got := notArXiv.ArXivDOI(); got != "" {
		t.Errorf("ArXivDOI on a DOI = %q, want the empty string", got)
	}
}

// NormaliseOpenAlexID is what sources/openalex runs over every ID in an API
// response, where the URL form is the norm rather than the exception.
func TestNormaliseOpenAlexID(t *testing.T) {
	t.Parallel()
	good := map[string]string{
		"W2741809807":                                "W2741809807",
		"https://openalex.org/W2741809807":           "W2741809807",
		"https://openalex.org/works/W2741809807":     "W2741809807",
		"https://api.openalex.org/works/w2741809807": "W2741809807",
	}
	for in, want := range good {
		got, err := NormaliseOpenAlexID(in)
		if err != nil || got != want {
			t.Errorf("NormaliseOpenAlexID(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "W", "W0", "A5023888391", "https://openalex.org/A5023888391", "10.1145/1"} {
		if got, err := NormaliseOpenAlexID(in); !errors.Is(err, errs.ErrInvalidInput) {
			t.Errorf("NormaliseOpenAlexID(%q) = %q, %v; want ErrInvalidInput", in, got, err)
		}
	}
}

// NormaliseDOI is used on DOIs arriving from OpenAlex as well as from the user,
// and the two must meet in the same form or the UNIQUE index cannot do its job.
func TestNormaliseDOI(t *testing.T) {
	t.Parallel()
	good := map[string]string{
		"10.1145/3292500.3330701":                       "10.1145/3292500.3330701",
		"https://doi.org/10.1145/3292500.3330701":       "10.1145/3292500.3330701",
		"https://doi.org/10.1016/S0140-6736(97)11096-0": "10.1016/s0140-6736(97)11096-0",
	}
	for in, want := range good {
		got, err := NormaliseDOI(in)
		if err != nil || got != want {
			t.Errorf("NormaliseDOI(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "10.", "10.1145/", "hello", "W2741809807"} {
		if got, err := NormaliseDOI(in); !errors.Is(err, errs.ErrInvalidInput) {
			t.Errorf("NormaliseDOI(%q) = %q, %v; want ErrInvalidInput", in, got, err)
		}
	}
}

func TestKindString(t *testing.T) {
	t.Parallel()
	for k, want := range map[Kind]string{
		KindOpenAlex: "OpenAlex ID",
		KindDOI:      "DOI",
		KindArXiv:    "arXiv ID",
		KindPMID:     "PMID",
		KindTitle:    "title",
	} {
		if got := k.String(); got != want {
			t.Errorf("Kind(%d).String() = %q, want %q", int(k), got, want)
		}
	}
}
