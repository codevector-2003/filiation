package text

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/codevector-2003/filiation/internal/text/pdftest"
)

func TestClean(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"windows line ends", "one\r\ntwo\r\n", "one\ntwo"},
		{"spaces collapse, lines trimmed", "  a   b \t c  \n  d  ", "a b c\nd"},
		{"line-end hyphen already joined", "we de\x02scribe it", "we describe it"},
		{"U+0002 not between letters is a space", "Copyright: \x02 2011", "Copyright: 2011"},
		{"soft hyphen inside a line is printed", "Ninety­seven percent", "Ninety-seven percent"},
		{"soft hyphen at a line end goes", "exam­\nple", "exam\nple"},
		{"a range broken across lines", "in 1992-\n2003 and", "in 1992-2003 and"},
		{"a hyphen before a word keeps its line", "well-\nknown", "well-\nknown"},
		{"ligatures spelt out", "ﬁnd the ﬂow", "find the flow"},
		{"control and private-use characters separate words", "Sample\x05citations\x06#x", "Sample citations # x"},
		{"blank lines collapse to one", "a\n\n\n\n\nb", "a\n\nb"},
		{"leading and trailing blank lines go", "\n\n a \n\n", "a"},
		{"NFC", "Montréal", "Montréal"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Clean(tt.in); got != tt.want {
				t.Errorf("Clean(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// prose is plain English, the kind of page Judge must call ok.
const prose = "In 2001, Lawrence found that articles in computer science that were openly " +
	"accessible on the Web were cited substantially more than those that were not. We have " +
	"since replicated this effect in physics. To test its generality, we used articles " +
	"published across twelve years in ten disciplines. "

func TestJudge(t *testing.T) {
	german := strings.Repeat("Die Ergebnisse zeigen, dass frei zugängliche Artikel häufiger zitiert werden als andere. ", 5)
	chinese := strings.Repeat("开放获取文章的引用次数明显高于其他文章，这一结果在多个学科中都得到了验证。", 6)
	formulas := prose + strings.Repeat("x = 2.45 ± 0.52 (r = .76, N = 12, p < 0.005) ", 10)
	symbols := strings.Repeat("#$%& ()*+ ,-./ 0123 :;<= >?@[ \\]^_ ", 20)
	consonants := strings.Repeat("Tbc rcsnlts shqw thst qpcn sccrss srtjclrs wcrc cjtcd mqrc qftcn ", 6)
	swapped := strings.Repeat("Xqv tqzbmkx pjfw kzqm qwvxl jhrtk nmpq zxcv bnmq wrtp ", 6)
	// A font with no character map: the letters themselves are gone.
	unmapped := strings.Repeat("The ������ �� ���� �������� articles ", 20)
	// A formula-heavy physics review, as read live: 7% of characters
	// unmapped, all in the equations, the prose intact. Text, not gibberish.
	formulaSymbols := strings.Repeat(prose+"\n���� �� (��) = −ℱ ����\n", 3)

	tests := []struct {
		name  string
		pages []string
		want  Status
	}{
		{"English prose", []string{prose, prose}, StatusOK},
		{"German", []string{german}, StatusOK},
		{"Chinese", []string{chinese}, StatusOK},
		{"prose with formulas", []string{formulas}, StatusOK},
		{"prose with unmapped formula symbols", []string{formulaSymbols}, StatusOK},
		{"no pages", nil, StatusNoText},
		{"a scan: a stamped line per page", []string{"Downloaded from example.org", "Downloaded from example.org", "Downloaded from example.org"}, StatusNoText},
		{"empty pages", []string{"", ""}, StatusNoText},
		{"symbols", []string{symbols}, StatusGibberish},
		{"words with no vowels", []string{consonants}, StatusGibberish},
		{"letters swapped for others", []string{swapped}, StatusGibberish},
		{"glyphs with no character", []string{unmapped}, StatusGibberish},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := Judge(tt.pages)
			if v.Status != tt.want {
				t.Errorf("Judge = %s (%s), want %s", v.Status, v.Reason, tt.want)
			}
			if v.Status != StatusOK && v.Reason == "" {
				t.Error("a page without text must say why")
			}
		})
	}
}

// sentences returns n numbered sentences of nine words each.
func sentences(n int) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "Sentence %d says that open access raises citation counts. ", i)
	}
	return strings.TrimSpace(b.String())
}

func TestChunks(t *testing.T) {
	pages := []string{
		"Title of the paper\n\n" + sentences(60), // ~540 words: two or three passages
		sentences(3),                             // one short passage, alone on its page
		"Discussion\n" + sentences(10) + "\n\nReferences\n1. Smith J. A paper. 2001.\n2. Jones K. Another. 2003.",
		"3. Brown L. A third. 2005.\n\nAppendix A: extra tables\n" + sentences(8),
	}
	got := Chunks(pages)
	if len(got) < 5 {
		t.Fatalf("got %d chunks, want at least 5", len(got))
	}
	pagesSeen := map[int]bool{}
	for i, c := range got {
		if c.Ordinal != i {
			t.Errorf("chunk %d has ordinal %d", i, c.Ordinal)
		}
		if c.Text != pages[c.Page-1][c.Start:c.End] {
			t.Errorf("chunk %d: text is not page[%d:%d]", i, c.Start, c.End)
		}
		if strings.TrimSpace(c.Text) != c.Text || c.Text == "" {
			t.Errorf("chunk %d is not trimmed: %q", i, c.Text)
		}
		if n := wordCount(c.Text); n > chunkMax {
			t.Errorf("chunk %d has %d words, over %d", i, n, chunkMax)
		}
		pagesSeen[c.Page] = true
	}
	for p := 1; p <= len(pages); p++ {
		if !pagesSeen[p] {
			t.Errorf("page %d has no chunk", p)
		}
	}

	// Page 1's passages end at sentences.
	for _, c := range got {
		if c.Page == 1 && !strings.HasSuffix(c.Text, ".") {
			t.Errorf("page 1 passage does not end a sentence: …%q", c.Text[max(0, len(c.Text)-30):])
		}
	}

	// The references are their own section, from the heading to the
	// appendix; the discussion before them and the appendix after are not.
	sections := map[string]string{}
	for _, c := range got {
		switch {
		case strings.HasPrefix(c.Text, "References"):
			sections["references"] = c.Section
		case strings.HasPrefix(c.Text, "3. Brown"):
			sections["references continued"] = c.Section
		case strings.HasPrefix(c.Text, "Discussion"):
			sections["discussion"] = c.Section
		case strings.HasPrefix(c.Text, "Appendix"):
			sections["appendix"] = c.Section
		}
	}
	want := map[string]string{
		"references":           SectionReferences,
		"references continued": SectionReferences,
		"discussion":           "",
		"appendix":             "",
	}
	for k, w := range want {
		if s, ok := sections[k]; !ok || s != w {
			t.Errorf("%s: section %q (found %v), want %q", k, s, ok, w)
		}
	}
}

func TestChunksCutsARunOnSentence(t *testing.T) {
	long := strings.Repeat("word ", 1000) // a table, or references with no full stops
	got := Chunks([]string{long})
	if len(got) < 3 {
		t.Fatalf("1,000 words with no sentence end gave %d chunks", len(got))
	}
	for _, c := range got {
		if n := wordCount(c.Text); n > chunkMax {
			t.Errorf("chunk of %d words, over %d", n, chunkMax)
		}
	}
}

func TestChunksReferencesHeadingNumbered(t *testing.T) {
	for _, heading := range []string{"References", "7. REFERENCES", "IV. Bibliography", "Literature Cited", "Works cited:"} {
		got := Chunks([]string{sentences(5), heading + "\n1. Smith J. A paper. 2001."})
		last := got[len(got)-1]
		if last.Section != SectionReferences {
			t.Errorf("heading %q: last chunk section %q", heading, last.Section)
		}
		if got[0].Section != "" {
			t.Errorf("heading %q: body marked %q", heading, got[0].Section)
		}
	}
	// A sentence mentioning references is not a heading.
	got := Chunks([]string{"The references we used are listed below.\n" + sentences(5)})
	for _, c := range got {
		if c.Section != "" {
			t.Errorf("a sentence was taken for a heading: %q", c.Text[:40])
		}
	}
}

func TestReadGeneratedPDF(t *testing.T) {
	pdf := pdftest.Make(
		"Open access and citation impact\nArticles that were openly accessible were cited more.\n"+wrap(prose),
		"",
		"The effect holds in physics.\nIt holds in ten other disciplines.\n"+wrap(prose),
	)
	res, err := Read(context.Background(), Shared(), pdf)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusOK || res.Pages != 3 {
		t.Fatalf("got %s (%s), %d pages; want ok, 3", res.Status, res.Reason, res.Pages)
	}
	pages := map[int]string{}
	for _, c := range res.Chunks {
		pages[c.Page] += c.Text
	}
	if !strings.Contains(pages[1], "openly accessible were cited more") {
		t.Errorf("page 1 = %q", pages[1])
	}
	if pages[2] != "" {
		t.Errorf("the blank page has text %q", pages[2])
	}
	if !strings.Contains(pages[3], "ten other disciplines") {
		t.Errorf("page 3 = %q", pages[3])
	}
}

// wrap breaks text into lines of about 80 characters, as a PDF sets them.
func wrap(s string) string {
	var b strings.Builder
	n := 0
	for _, w := range strings.Fields(s) {
		if n > 0 && n+len(w) > 80 {
			b.WriteByte('\n')
			n = 0
		} else if n > 0 {
			b.WriteByte(' ')
			n++
		}
		b.WriteString(w)
		n += len(w)
	}
	return b.String()
}

func TestReadScannedPDF(t *testing.T) {
	res, err := Read(context.Background(), Shared(), pdftest.Make("", "", ""))
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusNoText || len(res.Chunks) != 0 {
		t.Errorf("got %s with %d chunks, want no-text and none", res.Status, len(res.Chunks))
	}
}

func TestReadBrokenFiles(t *testing.T) {
	good := pdftest.Make("Some text on a page.")
	for name, data := range map[string][]byte{
		"empty":          {},
		"cut off":        good[:len(good)/3],
		"not a PDF":      []byte("<html><body>Access denied</body></html>"),
		"random bytes":   append([]byte("%PDF-1.4\n"), bytes(4096)...),
		"truncated head": []byte("%PDF-"),
	} {
		t.Run(name, func(t *testing.T) {
			res, err := Read(context.Background(), Shared(), data)
			if err != nil {
				t.Fatalf("a broken file must be a result, not an error: %v", err)
			}
			if res.Status != StatusUnreadable && res.Status != StatusNoText {
				t.Errorf("got %s, want unreadable or no-text", res.Status)
			}
		})
	}
}

func bytes(n int) []byte {
	b := make([]byte, n)
	x := uint32(2463534242)
	for i := range b {
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
		b[i] = byte(x)
	}
	return b
}

// bigPDF takes PDFium long enough to read that a short time limit lands
// mid-extraction.
func bigPDF() []byte {
	page := strings.TrimSpace(strings.Repeat("Open access articles are cited more often than others.\n", 50))
	pages := make([]string, 3000)
	for i := range pages {
		pages[i] = page
	}
	return pdftest.Make(pages...)
}

// TestTimeLimitStopsTheExtraction is spike 10's finding as a test: at the
// time limit the extraction stops, rather than running on and keeping its
// PDFium instance, and the next file reads normally.
func TestTimeLimitStopsTheExtraction(t *testing.T) {
	if testing.Short() {
		t.Skip("reads a 3,000-page PDF")
	}
	big := bigPDF()
	// PDFium starts on first use, which takes seconds; start it outside the
	// time limit, so the limit lands on the extraction.
	if _, err := Read(context.Background(), Shared(), pdftest.Make(wrap(prose))); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	res, err := Read(ctx, Shared(), big)
	took := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusTimedOut {
		t.Fatalf("got %s after %v, want timed-out", res.Status, took)
	}
	if took > 3*time.Second {
		t.Errorf("returned %v after a 50 ms limit: the extraction was not stopped", took)
	}

	// Both instances are free again: two reads at once both finish.
	errc := make(chan error, 2)
	for range 2 {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			res, err := Read(ctx, Shared(), pdftest.Make(wrap(prose+prose)))
			if err == nil && res.Status != StatusOK {
				err = fmt.Errorf("got %s (%s)", res.Status, res.Reason)
			}
			errc <- err
		}()
	}
	for range 2 {
		if err := <-errc; err != nil {
			t.Errorf("after a killed extraction: %v", err)
		}
	}
}

func TestCancelIsAnError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Read(ctx, Shared(), pdftest.Make(wrap(prose)))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want context.Canceled: cancelling stops the run, it is not a property of the PDF", err)
	}
}
