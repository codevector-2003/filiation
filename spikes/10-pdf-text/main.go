// Command pdf-text is spike 10 (ARCHITECTURE_PHASE3.md §6, ADR-009): which PDF
// text extractor does fil use?
//
// Candidates, both free of cgo:
//
//	ledongthuc  github.com/ledongthuc/pdf — pure Go
//	pdfium      github.com/klippa-app/go-pdfium/webassembly — PDFium compiled to
//	            WebAssembly, run by wazero
//
// The baseline is Poppler's pdftotext, which is not a candidate: it is a
// separate program, and fil ships as one binary. It stands in for the truth.
//
// For each PDF spike 9 downloaded (up to -per-field from each field), every
// candidate extracts every page, under a time limit and a recover. The text is
// compared with the baseline's:
//
//   - words: F1 of the multiset of words, case-folded and NFKC-normalised.
//     Fidelity — are the right words there at all?
//   - order: the share of the baseline's 6-word sequences that appear in the
//     candidate. Two columns read across, or a page read bottom-up, scores low
//     here while keeping its words.
//   - ligatures and junk: raw U+FB00–FB06 ligature characters, U+FFFD and
//     private-use characters left in the output.
//   - glued: words longer than 25 letters per 1,000, which is what missing
//     spaces between words look like.
//   - pages: whether the page count matches.
//
// Then each candidate is fed five broken files, and must fail cleanly.
//
// Usage:
//
//	go run . -pdfs <spike 9 dir>/pdfs -pdftotext <path> -out <dir> > spike10.md
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// An extractor returns the text of each page.
type extractor struct {
	name string
	run  func(path string) ([]string, error)
	// stop, if set, ends a run that has outlived the time limit. Without it
	// the run keeps going after guarded has given up on it, holding whatever
	// it holds — for PDFium, an instance from a small pool.
	stop func()
}

// Result is one extractor on one file.
type Result struct {
	Pages []string
	Err   error
	Took  time.Duration
}

const timeLimit = 60 * time.Second

func main() {
	pdfs := flag.String("pdfs", "", "spike 9's pdfs directory, one subdirectory per field")
	pdftotext := flag.String("pdftotext", "pdftotext", "path to Poppler's pdftotext")
	out := flag.String("out", "", "where to write extracted text and broken files")
	perField := flag.Int("per-field", 4, "PDFs per field")
	flag.Parse()
	if *pdfs == "" || *out == "" {
		fatal(errors.New("-pdfs and -out are required"))
	}

	files := pick(*pdfs, *perField)
	if len(files) == 0 {
		fatal(errors.New("no PDFs found"))
	}
	pool, err := newPdfium()
	if err != nil {
		fatal(fmt.Errorf("pdfium: %w", err))
	}
	defer pool.Close()

	// pdftotext runs under exec.CommandContext with the same limit, which kills it.
	baseline := extractor{name: "pdftotext", run: func(p string) ([]string, error) { return runPdftotext(*pdftotext, p) }}
	candidates := []extractor{
		// Pure Go: a stuck run leaks a goroutine but holds nothing shared.
		{name: "ledongthuc", run: ledongthuc},
		{name: "pdfium", run: pool.extract, stop: pool.kill},
	}

	var rows []row
	for i, f := range files {
		fmt.Fprintf(os.Stderr, "[%2d/%d] %s\n", i+1, len(files), f.name)
		base := guarded(baseline, f.path)
		save(*out, "pdftotext", f, base)
		r := row{file: f, base: base}
		for _, c := range candidates {
			got := guarded(c, f.path)
			save(*out, c.name, f, got)
			r.cands = append(r.cands, got)
			r.scores = append(r.scores, compare(base, got))
		}
		rows = append(rows, r)
	}

	broken := makeBroken(*out, files)
	var brokenRows [][]Result
	for _, b := range broken {
		var rs []Result
		for _, c := range append([]extractor{baseline}, candidates...) {
			rs = append(rs, guarded(c, b.path))
		}
		brokenRows = append(brokenRows, rs)
	}

	report(rows, candidates, broken, brokenRows)
}

type file struct {
	field, name, path string
	size              int64
}

// pick takes up to n PDFs from each field directory, at random but the same
// every run.
func pick(dir string, n int) []file {
	fields, err := os.ReadDir(dir)
	if err != nil {
		fatal(err)
	}
	rng := rand.New(rand.NewSource(10))
	var out []file
	for _, fd := range fields {
		if !fd.IsDir() {
			continue
		}
		entries, _ := filepath.Glob(filepath.Join(dir, fd.Name(), "*.pdf"))
		sort.Strings(entries)
		rng.Shuffle(len(entries), func(i, j int) { entries[i], entries[j] = entries[j], entries[i] })
		for _, p := range entries[:min(n, len(entries))] {
			info, _ := os.Stat(p)
			out = append(out, file{field: fd.Name(), name: strings.TrimSuffix(filepath.Base(p), ".pdf"), path: p, size: info.Size()})
		}
	}
	return out
}

// guarded runs an extractor with a time limit, turning a panic into an error.
// One bad file must fail one paper, never the run (ARCHITECTURE_PHASE3.md §7).
func guarded(e extractor, path string) Result {
	done := make(chan Result, 1)
	start := time.Now()
	go func() {
		defer func() {
			if p := recover(); p != nil {
				done <- Result{Err: fmt.Errorf("panic: %v", p), Took: time.Since(start)}
			}
		}()
		pages, err := e.run(path)
		done <- Result{Pages: pages, Err: err, Took: time.Since(start)}
	}()
	select {
	case r := <-done:
		return r
	case <-time.After(timeLimit):
		if e.stop != nil {
			e.stop()
		}
		return Result{Err: errors.New("timed out"), Took: timeLimit}
	}
}

func runPdftotext(bin, path string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeLimit)
	defer cancel()
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, "-enc", "UTF-8", path, "-")
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%v: %s", err, firstLine(stderr.String()))
	}
	pages := strings.Split(string(out), "\f")
	if len(pages) > 0 && strings.TrimSpace(pages[len(pages)-1]) == "" {
		pages = pages[:len(pages)-1]
	}
	return pages, nil
}

func save(dir, who string, f file, r Result) {
	p := filepath.Join(dir, "text", who, f.field, f.name+".txt")
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte(strings.Join(r.Pages, "\f")), 0o644)
}

// Score is one candidate against the baseline on one file.
type Score struct {
	WordF1, Order       float64
	Ligatures, Junk     int
	GluedPer1k          float64
	PagesOK             bool
	CandWords, BaseWord int
}

func compare(base, got Result) Score {
	var s Score
	if got.Err != nil || base.Err != nil {
		return s
	}
	bt, gt := strings.Join(base.Pages, "\n"), strings.Join(got.Pages, "\n")
	bw, gw := words(bt), words(gt)
	s.BaseWord, s.CandWords = len(bw), len(gw)
	s.WordF1 = f1(bw, gw)
	s.Order = shingleRecall(bw, gw, 6)
	for _, r := range gt {
		switch {
		case r >= 0xFB00 && r <= 0xFB06:
			s.Ligatures++
		case r == unicode.ReplacementChar, r >= 0xE000 && r <= 0xF8FF:
			s.Junk++
		}
	}
	glued := 0
	for _, w := range gw {
		if len([]rune(w)) > 25 {
			glued++
		}
	}
	if len(gw) > 0 {
		s.GluedPer1k = 1000 * float64(glued) / float64(len(gw))
	}
	s.PagesOK = len(base.Pages) == len(got.Pages)
	return s
}

// words splits text into lower-case words, with ligatures and other
// compatibility characters folded, so the word score measures fidelity and
// not ligature handling, which is scored on its own. A word broken across a
// line by a hyphen is joined, as both fil and a reader would.
func words(text string) []string {
	text = norm.NFKC.String(text)
	text = strings.ReplaceAll(text, "-\n", "")
	var out []string
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			out = append(out, b.String())
			b.Reset()
		}
	}
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		} else {
			flush()
		}
	}
	flush()
	return out
}

func f1(want, got []string) float64 {
	if len(want) == 0 || len(got) == 0 {
		return 0
	}
	count := map[string]int{}
	for _, w := range want {
		count[w]++
	}
	hit := 0
	for _, w := range got {
		if count[w] > 0 {
			count[w]--
			hit++
		}
	}
	p, r := float64(hit)/float64(len(got)), float64(hit)/float64(len(want))
	if p+r == 0 {
		return 0
	}
	return 2 * p * r / (p + r)
}

func shingleRecall(want, got []string, n int) float64 {
	if len(want) < n {
		return 0
	}
	have := map[string]bool{}
	for i := 0; i+n <= len(got); i++ {
		have[strings.Join(got[i:i+n], " ")] = true
	}
	hit, total := 0, 0
	for i := 0; i+n <= len(want); i++ {
		total++
		if have[strings.Join(want[i:i+n], " ")] {
			hit++
		}
	}
	return float64(hit) / float64(total)
}

type broken struct{ name, path string }

// makeBroken writes five files no extractor should choke on: empty, cut off
// part-way, random bytes behind a PDF header, an HTML page saved as .pdf, and
// a real PDF with its cross-reference table wrecked.
func makeBroken(dir string, files []file) []broken {
	d := filepath.Join(dir, "broken")
	os.MkdirAll(d, 0o755)
	src, err := os.ReadFile(files[0].path)
	if err != nil {
		fatal(err)
	}
	rng := rand.New(rand.NewSource(11))
	junk := make([]byte, 200_000)
	rng.Read(junk)
	xref := bytes.ReplaceAll(src, []byte("startxref"), []byte("startxxxx"))
	xref = bytes.ReplaceAll(xref, []byte("xref"), []byte("xxxx"))
	items := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"truncated at 30%", src[:len(src)*3/10]},
		{"random bytes behind %PDF-", append([]byte("%PDF-1.7\n"), junk...)},
		{"HTML saved as .pdf", []byte("<!doctype html><html><head><title>Sign in</title></head><body>Access denied</body></html>")},
		{"cross-reference table wrecked", xref},
	}
	var out []broken
	for i, it := range items {
		p := filepath.Join(d, fmt.Sprintf("broken%d.pdf", i))
		os.WriteFile(p, it.data, 0o644)
		out = append(out, broken{it.name, p})
	}
	return out
}

type row struct {
	file   file
	base   Result
	cands  []Result
	scores []Score
}

func report(rows []row, cands []extractor, broken []broken, brokenRows [][]Result) {
	fmt.Println("## Spike 10 — PDF text extraction (ADR-009)")
	fmt.Println()
	fmt.Printf("`go run ./spikes/10-pdf-text`. %d PDFs downloaded by spike 9, scored against Poppler's pdftotext.\n\n", len(rows))

	fmt.Println("### Summary")
	fmt.Println()
	fmt.Println("| Candidate | Failed | Words F1 (median) | Order (median) | Order < 0.8 | Ligatures left | Junk chars | Glued /1k (median) | Pages match | Time (total) |")
	fmt.Println("| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |")
	var baseTime time.Duration
	for _, r := range rows {
		baseTime += r.base.Took
	}
	for i, c := range cands {
		var fails, lowOrder, lig, junk, pages int
		var wf, ord, glued []float64
		var took time.Duration
		for _, r := range rows {
			took += r.cands[i].Took
			if r.cands[i].Err != nil {
				fails++
				continue
			}
			s := r.scores[i]
			wf, ord, glued = append(wf, s.WordF1), append(ord, s.Order), append(glued, s.GluedPer1k)
			if s.Order < 0.8 {
				lowOrder++
			}
			lig += s.Ligatures
			junk += s.Junk
			if s.PagesOK {
				pages++
			}
		}
		fmt.Printf("| %s | %d | %.3f | %.3f | %d | %d | %d | %.1f | %d of %d | %s |\n",
			c.name, fails, med(wf), med(ord), lowOrder, lig, junk, med(glued), pages, len(rows), took.Round(time.Millisecond))
	}
	fmt.Printf("| pdftotext (baseline) | %d | 1 | 1 | — | — | — | — | — | %s |\n\n", countErr(rows), baseTime.Round(time.Millisecond))

	fmt.Println("### Per file")
	fmt.Println()
	head := "| Field | Work | Size | Pages |"
	sep := "| --- | --- | --- | --- |"
	for _, c := range cands {
		head += " " + c.name + " F1 / order / ms |"
		sep += " --- |"
	}
	fmt.Println(head)
	fmt.Println(sep)
	for _, r := range rows {
		line := fmt.Sprintf("| %s | %s | %.1f MB | %d |", r.file.field, r.file.name, float64(r.file.size)/(1<<20), len(r.base.Pages))
		for i := range cands {
			if r.cands[i].Err != nil {
				line += " **" + escape(firstLine(r.cands[i].Err.Error())) + "** |"
				continue
			}
			s := r.scores[i]
			line += fmt.Sprintf(" %.2f / %.2f / %d |", s.WordF1, s.Order, r.cands[i].Took.Milliseconds())
		}
		fmt.Println(line)
	}
	fmt.Println()

	fmt.Println("### Broken files")
	fmt.Println()
	head = "| File | pdftotext |"
	sep = "| --- | --- |"
	for _, c := range cands {
		head += " " + c.name + " |"
		sep += " --- |"
	}
	fmt.Println(head)
	fmt.Println(sep)
	for i, b := range broken {
		line := "| " + b.name + " |"
		for _, r := range brokenRows[i] {
			switch {
			case r.Err != nil:
				line += " error: " + escape(trim(firstLine(r.Err.Error()), 70)) + " |"
			default:
				n := len(strings.TrimSpace(strings.Join(r.Pages, "")))
				line += fmt.Sprintf(" no error, %d pages, %d chars |", len(r.Pages), n)
			}
		}
		fmt.Println(line)
	}
	fmt.Println()
}

func countErr(rows []row) int {
	n := 0
	for _, r := range rows {
		if r.base.Err != nil {
			n++
		}
	}
	return n
}

func med(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	return s[len(s)/2]
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func trim(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func escape(s string) string { return strings.ReplaceAll(s, "|", `\|`) }

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "spike 10:", err)
	os.Exit(1)
}
