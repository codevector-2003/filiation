package text

import (
	"context"
	"strings"
	"testing"

	"github.com/codevector-2003/filiation/internal/text/pdftest"
)

func clueIDs(cs []Clue) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.ID.Kind.String() + ":" + c.ID.Value
	}
	return out
}

func TestClues(t *testing.T) {
	body := strings.Repeat("Body text of the paper. ", 20)
	tests := []struct {
		name  string
		file  string
		front Front
		first string // the first clue, or "" for none
		also  string // another clue that must be found, if set
	}{
		{"the paper's DOI in the header beats a cited one in the body",
			"paper.pdf", Front{Pages: []string{"PeerJ\nDOI 10.7717/peerj.4375\n" + body + "\nAs shown by doi:10.1000/cited.1, open access helps.\n" + body}},
			"DOI:10.7717/peerj.4375", "DOI:10.1000/cited.1"},
		{"new-style arXiv stamp",
			"x.pdf", Front{Pages: []string{"arXiv:1706.03762v5 [cs.CL] 6 Dec 2017\nAttention Is All You Need\n" + body}},
			"arXiv ID:1706.03762", ""},
		{"old-style arXiv stamp",
			"x.pdf", Front{Pages: []string{body + "\narXiv:cond-mat/0604601v1 [cond-mat.mes-hall] 26 Apr 2006"}},
			"arXiv ID:cond-mat/0604601", ""},
		{"an arXiv ID cited in the text is not a stamp",
			"x.pdf", Front{Pages: []string{body + " as in arXiv:1706.03762, attention works. " + body}},
			"", ""},
		{"an arXiv download's file name",
			"1706.03762v5.pdf", Front{Pages: []string{body}},
			"arXiv ID:1706.03762", ""},
		{"a DOI in the metadata",
			"x.pdf", Front{Pages: []string{body}, Meta: map[string]string{"Subject": "Living Reviews, https://doi.org/10.1007/s41114-020-00026-9"}},
			"DOI:10.1007/s41114-020-00026-9", ""},
		{"Springer's hidden text after a DOI is cut off",
			"x.pdf", Front{Pages: []string{"https://doi.org/10.1007/s41114-020-00026-9(0123456789().,-volV)(0123456789().,-volV)\n" + body}},
			"DOI:10.1007/s41114-020-00026-9", ""},
		{"a DOI's own parentheses are kept",
			"x.pdf", Front{Pages: []string{"doi: 10.1016/S0140-6736(20)30183-5\n" + body}},
			"DOI:10.1016/s0140-6736(20)30183-5", ""},
		{"a DOI broken across lines is joined",
			"x.pdf", Front{Pages: []string{body + "\nDOI: 10.1371/journal.\npmed.0020124\n" + body}},
			"DOI:10.1371/journal.pmed.0020124", ""},
		{"a bracket set around a DOI is not part of it",
			"x.pdf", Front{Pages: []string{"⟨10.1103/RevModPhys.79.677⟩\n" + body}},
			"DOI:10.1103/revmodphys.79.677", ""},
		{"a labelled PMID",
			"x.pdf", Front{Pages: []string{"PMID: 16060722\n" + body}},
			"PMID:16060722", ""},
		{"a DOI in the file name",
			"10.1371_journal.pmed.0020124.pdf", Front{Pages: []string{body}},
			"DOI:10.1371/journal.pmed.0020124", ""},
		{"nothing", "notes.pdf", Front{Pages: []string{body}}, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clueIDs(Clues(tt.file, tt.front))
			if tt.first == "" {
				if len(got) != 0 {
					t.Errorf("Clues = %v, want none", got)
				}
				return
			}
			if len(got) == 0 || !strings.EqualFold(got[0], tt.first) {
				t.Errorf("Clues = %v, want %s first", got, tt.first)
			}
			if tt.also != "" && !strings.Contains(strings.ToLower(strings.Join(got, " ")), strings.ToLower(tt.also)) {
				t.Errorf("Clues = %v, want %s among them", got, tt.also)
			}
		})
	}
}

func TestMentions(t *testing.T) {
	page := "PLoS Medicine | Essay\nWhy Most Published Research\nFi ndings Are False\nJohn P. A. Ioannidis\n" +
		"Summary. There is increasing concern that most current published research findings are false."
	tests := []struct {
		name, title, surname string
		want                 bool
	}{
		{"title across lines, a ligature spaced apart", "Why Most Published Research Findings Are False", "Ioannidis", true},
		{"no author known", "Why Most Published Research Findings Are False", "", true},
		{"the wrong first author", "Why Most Published Research Findings Are False", "Piwowar", false},
		{"a short surname found only inside other words", "Why Most Published Research Findings Are False", "Ma", false},
		{"another paper's title", "The state of OA: a large-scale analysis", "Ioannidis", false},
		{"a subtitle the PDF leaves off", "Why Most Published Research Findings Are False: an essay on bias", "Ioannidis", true},
		{"a longer title that starts with this one, no author to check", "Why Most Published Research Findings Are False: a reply", "", false},
		{"too short to tell", "Why", "Ioannidis", false},
	}
	for _, tt := range tests {
		if got := Mentions([]string{page}, tt.title, tt.surname); got != tt.want {
			t.Errorf("%s: Mentions = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestResembles(t *testing.T) {
	page := "arXiv:cond-mat/0510830v2 [cond-mat.dis-nn] 10 Apr 2006\nInteraction effects on 2D fermions with random hopping\n" +
		"Matthew S. Foster∗ and Andreas W. W. Ludwig\nDepartment of Physics, University of California\n" +
		strings.Repeat("We study the effects of generic short-ranged interactions.\n", 20) +
		"[1] A. Author, Quantum Hall ferromagnets in bilayer graphene, Phys. Rev. B (2005)."
	tests := []struct {
		name, title, surname string
		want                 bool
	}{
		{"the published title of this preprint", "Interaction effects on two-dimensional fermions with random hopping", "Foster", true},
		{"its exact title fails Mentions but passes here", "Interaction effects on 2D fermions with random hopping", "Foster", true},
		{"the wrong author", "Interaction effects on two-dimensional fermions with random hopping", "Ludwigsen", false},
		{"a paper cited at the bottom", "Quantum Hall ferromagnets in bilayer graphene", "Author", false},
		{"no author known", "Interaction effects on two-dimensional fermions with random hopping", "", false},
	}
	for _, tt := range tests {
		if got := Resembles([]string{page}, tt.title, tt.surname); got != tt.want {
			t.Errorf("%s: Resembles = %v, want %v", tt.name, got, tt.want)
		}
	}
	// An affiliation mark against a name does not hide it.
	if !Mentions([]string{"Graphene integer quantum Hall effect\nJason Alicea1 and Matthew Fisher2"}, "Graphene integer quantum Hall effect", "Alicea") {
		t.Error("Alicea1 was not read as Alicea")
	}
}

// Found in the M3 validation run: a surname of two words was looked for as
// one, so every paper by Aspuru-Guzik or Martínez-Cantín failed the check.
func TestMentionsASurnameOfSeveralWords(t *testing.T) {
	page := "Simulated Quantum Computation of Molecular Energies\nAlán Aspuru-Guzik*a, Anthony D. Dutoi*a and Peter J. Love\n" +
		"Department of Chemistry, University of California, Berkeley"
	tests := []struct {
		name, surname string
		want          bool
	}{
		{"OpenAlex's own hyphen, U+2010, against the PDF's", "Aspuru‐Guzik", true},
		{"accents OpenAlex has and the PDF lacks", "Aspurú-Guzík", true},
		{"half of the name alone", "Guzik", true},
		{"both halves, but not in a row", "Aspuru-Love", false},
		{"the halves the other way round", "Guzik-Aspuru", false},
	}
	for _, tt := range tests {
		if got := Mentions([]string{page}, "Simulated Quantum Computation of Molecular Energies", tt.surname); got != tt.want {
			t.Errorf("%s: Mentions = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestFrontReadsTitleAndInfo(t *testing.T) {
	pdf := pdftest.Doc{
		Title: "Attention is all you need",
		Pages: []string{"Ashish Vaswani, Noam Shazeer\nThe dominant sequence transduction models are based on recurrent networks.", "Page two."},
		Info:  map[string]string{"Title": "untitled", "doi": "10.48550/arXiv.1706.03762"},
	}.Bytes()
	f, err := Shared().Front(context.Background(), pdf)
	if err != nil {
		t.Fatal(err)
	}
	if f.Title != "Attention is all you need" {
		t.Errorf("Title = %q", f.Title)
	}
	if f.Meta["doi"] != "10.48550/arXiv.1706.03762" || f.Meta["Title"] != "untitled" {
		t.Errorf("Meta = %v", f.Meta)
	}
	if len(f.Pages) != 2 || !strings.Contains(f.Pages[0], "Vaswani") {
		t.Errorf("Pages = %q", f.Pages)
	}
}

func TestLargestTextStopsAtTheAuthors(t *testing.T) {
	for _, tt := range []struct {
		runs []run
		want string
	}{
		{[]run{{text: "Unconventional quantum Hall effect", size: 14, top: 700}, {text: "in bilayer graphene", size: 14, top: 684},
			{text: "K. S. Novoselov , E. McCann , S. V. Morozov", size: 14, top: 660}, {text: "Abstract text in a smaller size here", size: 9, top: 600}},
			"Unconventional quantum Hall effect in bilayer graphene"},
		{[]run{{text: "Two Dimensional Electron and Hole Gases", size: 16, top: 700}, {text: "S.V. Morozov, K.S. Novoselov & A.K. Geim", size: 16, top: 670}},
			"Two Dimensional Electron and Hole Gases"},
		{[]run{{text: "arXiv:1706.03762v5 [cs.CL] 6 Dec 2017", size: 20, top: 400}, {text: "Attention Is All You Need", size: 17, top: 700},
			{text: "Ashish Vaswani", size: 11, top: 650}},
			"Attention Is All You Need"},
	} {
		if got := largestText(tt.runs); got != tt.want {
			t.Errorf("largestText = %q, want %q", got, tt.want)
		}
	}
}

// Accents printed as glyphs of their own, and names with and without them,
// still match (found live, M3 step 10: "Bjo¨ rk1" in a PLoS ONE paper).
func TestAccentsMatch(t *testing.T) {
	if got := Clean("Bo-Christer Bjo¨ rk1 , Patrik Welling1 and V. Larivie`re"); got != "Bo-Christer Björk1 , Patrik Welling1 and V. Larivière" {
		t.Errorf("Clean = %q", got)
	}
	page := Clean("Open Access to the Scientific Journal Literature: Situation 2009\nBo-Christer Bjo¨ rk1 , Patrik Welling1")
	if !Mentions([]string{page}, "Open Access to the Scientific Journal Literature: Situation 2009", "Björk") {
		t.Error("Björk was not found in a page that prints him with a spaced accent")
	}
	if !Mentions([]string{"The state of OA\nVincent Lariviere"}, "The state of OA", "Larivière") {
		t.Error("an accent in the record but not in the PDF blocked the match")
	}
}
