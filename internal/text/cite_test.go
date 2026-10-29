package text

import (
	"fmt"
	"strings"
	"testing"
)

var citeRefs = []Ref{
	{ID: "F1", Title: "The state of OA: a large-scale analysis of open access articles", Year: 2018, Surname: "Piwowar", DOI: "10.7717/peerj.4375"},
	{ID: "F2", Title: "Ten-year cross-disciplinary comparison of the growth of open access", Year: 2006, Surname: "Hajjem"},
	{ID: "F3", Title: "The development of open access journal publishing from 1993 to 2009", Year: 2011, Surname: "Laakso"},
	{ID: "F4", Title: "Is free inevitable in scholarly communication", Year: 2011, Surname: "Sutton"},
	{ID: "F5", Title: "Looking into Pandora's box: the content of Sci-Hub and its usage", Year: 2017, Surname: "Tzovaras"},
	{ID: "F6", Title: "Scientists in Germany, Peru and Taiwan to lose access to Elsevier journals", Year: 2016, Surname: "Schiermeier"},
}

func byRef(ms []Mention) map[string][]Mention {
	out := map[string][]Mention{}
	for _, m := range ms {
		out[m.Ref] = append(out[m.Ref], m)
	}
	return out
}

func TestContextsNumbered(t *testing.T) {
	body := "Introduction\n" +
		"Open access has grown fast [1]. Earlier counts were smaller [2, 3], and the\n" +
		"growth differs by field [3-5]. A marker with no entry [9] gives nothing.\n\n" +
		"Methods\n" +
		"We follow the approach of Fig. 2 in [6]. The equation (2) is not a citation.\n"
	refs := "References\n" +
		"[1] Piwowar H, Priem J. The state of OA: a large-scale analysis of open access articles. PeerJ. 2018. doi:10.7717/peerj.4375\n" +
		"[2] Hajjem C. Ten-year cross-disciplinary comparison of the growth of open access. 2006.\n" +
		"[3] Laakso M. The development of open access journal publishing from 1993 to 2009. PLoS ONE. 2011.\n" +
		"[4] Sutton C. Is free inevitable in scholarly communication? 2011.\n" +
		"[5] Tzovaras BG. Looking into Pandora's box: the content of Sci-Hub and its usage. 2017.\n" +
		"[6] Schiermeier Q. Scientists in Germany, Peru and Taiwan to lose access to Elsevier journals. Nature. 2016.\n" +
		"[9] Unknown A. A paper OpenAlex did not list. 1999.\n"
	got := byRef(Contexts([]string{body, refs}, citeRefs))

	if m := got["F1"]; len(m) != 1 || m[0].Sentence != "Open access has grown fast [1]." || m[0].Confidence != confNumericDOI ||
		m[0].Section != "introduction" || m[0].Page != 1 || m[0].Method != MethodNumeric {
		t.Errorf("F1 = %+v", m)
	}
	// [2, 3] and [3-5] are in one sentence: one mention of 3, not two.
	if m := got["F3"]; len(m) != 1 || m[0].Marker != "[2, 3]" {
		t.Errorf("F3, cited twice in one sentence: %+v", m)
	}
	for _, id := range []string{"F2", "F4", "F5"} {
		if len(got[id]) != 1 {
			t.Errorf("%s = %+v", id, got[id])
		}
	}
	if m := got["F6"]; len(m) != 1 || m[0].Sentence != "We follow the approach of Fig. 2 in [6]." || m[0].Section != "methods" {
		t.Errorf("F6 = %+v; want the whole sentence, not cut at \"Fig.\"", m)
	}
	for _, ms := range got {
		for _, m := range ms {
			if m.Page != 1 {
				t.Errorf("a mention in the reference list itself: %+v", m)
			}
			if body[m.Start:m.End] == "" || oneLine(body[m.Start:m.End]) != m.Sentence {
				t.Errorf("offsets %d:%d do not give the sentence %q", m.Start, m.End, m.Sentence)
			}
		}
	}
}

func TestContextsAuthorYear(t *testing.T) {
	body := "Open access is now common (Piwowar et al., 2018). Hajjem and Harnad (2006) found\n" +
		"an advantage in every field, as did others (Laakso & Bjork, 2011; Sutton 2011).\n" +
		"In 2011 Sutton wrote a column, which is not a citation of anything.\n" +
		"Two papers by one author in one year cannot be told apart (Tzovaras, 2017).\n"
	refs := append([]Ref{}, citeRefs...)
	refs = append(refs, Ref{ID: "F7", Title: "Another paper", Year: 2017, Surname: "Tzovaras"})
	got := byRef(Contexts([]string{body}, refs))

	if m := got["F1"]; len(m) != 1 || m[0].Marker != "Piwowar et al., 2018" || m[0].Method != MethodAuthorYear {
		t.Errorf("F1 = %+v", m)
	}
	if m := got["F2"]; len(m) != 1 || !strings.HasPrefix(m[0].Sentence, "Hajjem and Harnad (2006) found an advantage") {
		t.Errorf("F2 = %+v", m)
	}
	if len(got["F3"]) != 1 || len(got["F4"]) != 1 {
		t.Errorf("a parenthesis citing two: F3 %+v, F4 %+v", got["F3"], got["F4"])
	}
	for _, m := range got["F4"] {
		if strings.Contains(m.Sentence, "column") {
			t.Errorf("\"In 2011 Sutton wrote\" taken for a citation: %+v", m)
		}
	}
	if len(got["F5"]) != 0 || len(got["F7"]) != 0 {
		t.Errorf("an ambiguous author and year was given to a ref: %+v %+v", got["F5"], got["F7"])
	}
}

// Physics layouts end with numbered notes and no "References" heading
// (spike 11): the list is found by its numbering.
func TestContextsListWithNoHeading(t *testing.T) {
	body := "We study open access [1] and its growth [2]. Journals followed [3], [4], [5].\n"
	notes := "[1] H. Piwowar et al., The state of OA: a large-scale analysis of open access articles, PeerJ (2018).\n" +
		"[2] C. Hajjem, Ten-year cross-disciplinary comparison of the growth of open access (2006).\n" +
		"[3] M. Laakso, The development of open access journal publishing from 1993 to 2009 (2011).\n" +
		"[4] C. Sutton, Is free inevitable in scholarly communication (2011).\n" +
		"[5] B. Tzovaras, Looking into Pandora's box: the content of Sci-Hub and its usage (2017).\n"
	got := byRef(Contexts([]string{body, notes}, citeRefs))
	for _, id := range []string{"F1", "F2", "F3", "F4", "F5"} {
		if len(got[id]) != 1 {
			t.Errorf("%s = %+v", id, got[id])
		}
	}
}

// ADR-015: no match, no context. A numbered entry that does not match a
// reference OpenAlex gave — wrong title, or right title and nothing to
// confirm it — attaches nothing.
func TestContextsNoGuess(t *testing.T) {
	body := "Open access grew [1]. Its growth differs [2].\n"
	refs := "References\n" +
		"1. Somebody X. A completely different paper about bees. 1990.\n" +
		"2. Hajjem C. Ten-year cross-disciplinary comparison of the growth of open access. 2006.\n" +
		"3. a. b. 4. c. 5. d.\n"
	got := Contexts([]string{body, refs}, []Ref{citeRefs[0], {ID: "F2", Title: citeRefs[1].Title}})
	if len(got) != 0 {
		t.Errorf("Contexts = %+v; want none: [1] matches no reference, [2] has no year or author to confirm it", got)
	}
}

func TestExpand(t *testing.T) {
	for in, want := range map[string]string{
		"1": "[1]", "2, 3": "[2 3]", "3-5": "[3 4 5]", "3–5,9": "[3 4 5 9]", "1;4": "[1 4]", "12": "[12]",
		"5-3": "[]", "1-99": "[]", "x": "[]",
	} {
		if got := fmt.Sprint(expand(in)); got != want {
			t.Errorf("expand(%q) = %s, want %s", in, got, want)
		}
	}
}

// The four mistakes the first hand check found (M3 step 9, live), each
// kept as a case.
func TestContextsHandCheckFindings(t *testing.T) {
	// A second author is not the cited paper's first author.
	refs := []Ref{
		{ID: "BW", Title: "BayesWave", Year: 2015, Surname: "Cornish"},
		{ID: "LN", Title: "Spectral estimation of detector noise", Year: 2015, Surname: "Littenberg"},
	}
	got := byRef(Contexts([]string{"Glitches are modelled separately (Cornish and Littenberg 2015).\n"}, refs))
	if len(got["BW"]) != 1 || len(got["LN"]) != 0 {
		t.Errorf("Cornish and Littenberg 2015: %+v", got)
	}

	list := "References\n" +
		"[1] Jian-Xin Zhu and C.S. Ting, Phys. Rev. B 63, 020506 (2001).\n" +
		"[2] B. Abbott et al., Phys. Rev. Lett. 116, 061102 (2016).\n" +
		"[3] B. Abbott et al., Phys. Rev. D 93, 122003 (2016).\n" +
		"9. 16. Peres, N.M.R. Electronic properties of two-dimensional carbon (2006).\n" +
		"[4] S. Khan et al., Phys. Rev. D 93, 044007 (2016).\n" +
		"[5] A. Einstein, Sitzungsber. 1, 688 (1916).\n"
	refs = []Ref{
		{ID: "C", Title: "Multichannel pseudogap Kondo model", Year: 2001, Surname: "C"},       // an initial for a surname
		{ID: "A1", Title: "Observation of gravitational waves", Year: 2016, Surname: "Abbott"}, // two Abbott 2016s
		{ID: "A2", Title: "GW150914: first results", Year: 2016, Surname: "Abbott"},
		{ID: "K", Title: "Frequency-domain gravitational waves", Year: 2016, Surname: "Khan"},
	}
	body := "The model fails [1]. Detection was reported [2], with detail in [3]. Waveforms follow [4].\n"
	got = byRef(Contexts([]string{body, list}, refs))
	if len(got["C"]) != 0 {
		t.Errorf("an initial matched as a surname: %+v", got["C"])
	}
	if len(got["A1"]) != 0 || len(got["A2"]) != 0 {
		t.Errorf("two Abbott 2016 references told apart by guesswork: %+v %+v", got["A1"], got["A2"])
	}
	if len(got["K"]) != 1 {
		t.Errorf("Khan 2016, the one of its kind: %+v", got["K"])
	}

	// Two columns read across interleave entries: "9. 16. Peres" is dropped.
	entries := numberedEntries([]string{"1. A. B. One. 2001.\n2. C. D. Two. 2002.\n3. E. F. Three. 2003.\n4. G. H. Four. 2004.\n5. 16. Peres N. Five. 2005.\n6. I. J. Six. 2006.\n"}, 1, 0)
	if _, ok := entries[5]; ok || len(entries) != 5 {
		t.Errorf("entries = %v; want 5 interleaved with 16 dropped", entries)
	}
}
