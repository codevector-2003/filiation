// Command validation-m3 is the M3 validation run: ARCHITECTURE_PHASE3.md's
// step 11, the 20 seeds of M1's run (spikes/8-validation) again, now for the
// papers on disk — PDFs, their text, search, citation context and import.
//
// Each seed gets a library folder of its own. For each: add it, expand to 200
// works (M1 used 500; the PDFs are what is under test here), fetch at most 25
// PDFs — open access only, a refusal is recorded and never worked around
// (hard rule 1) — which reads them too. Then, for every paper with text,
// search for its own title's longest words across the library and see
// whether it comes back in the first five papers.
//
// After every seed, an import check per field: up to 8 of the field's
// downloaded PDFs, copied under meaningless names into a fresh library's
// Inbox/, imported, and the paper each was filed as compared with the paper
// it was downloaded for.
//
// Last, a sample of 10 citation sentences per field is written to
// mentions-sample.json for checking by hand.
//
// No contact email is sent. Everything is written under -root, which should
// not be a temporary folder: the hand check reads it afterwards.
//
// Usage:
//
//	go run ./spikes/12-validation-m3 -root <folder> > report.md
//	go run ./spikes/12-validation-m3 -root <folder> -recheck-imports
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/codevector-2003/filiation/internal/config"
	"github.com/codevector-2003/filiation/internal/library"
	"github.com/codevector-2003/filiation/internal/model"
)

type seed struct {
	field, id, note string
}

// The seeds of M1's run, unchanged, so the two runs compare.
var seeds = []seed{
	{"Medicine", "10.1056/NEJMoa2034577", "BNT162b2 vaccine trial, 2020"},
	{"Medicine", "10.1016/S0140-6736(20)30183-5", "COVID-19 clinical features, Wuhan, 2020"},
	{"Medicine", "10.1001/jama.2020.1585", "138 hospitalised COVID-19 patients, 2020"},
	{"Medicine", "10.1371/journal.pmed.0020124", "Why most published research findings are false, 2005"},

	{"Physics", "10.1103/PhysRevLett.116.061102", "LIGO GW150914, 2016"},
	{"Physics", "10.1103/PhysRev.47.777", "Einstein-Podolsky-Rosen, 1935"},
	{"Physics", "10.1038/s41586-019-1666-5", "Quantum supremacy, 2019"},
	{"Physics", "10.1103/RevModPhys.81.109", "Electronic properties of graphene, 2009"},

	{"Computer Science", "arXiv:1706.03762", "Attention is all you need, 2017"},
	{"Computer Science", "10.1109/CVPR.2016.90", "Deep residual learning, 2016"},
	{"Computer Science", "10.1038/nature14539", "Deep learning, 2015"},
	{"Computer Science", "10.1145/3292500.3330701", "Optuna, 2019"},

	{"Social Sciences", "10.7717/peerj.4375", "The state of OA, 2018"},
	{"Social Sciences", "10.1086/225469", "The strength of weak ties, 1973"},
	{"Social Sciences", "10.1126/science.aac4716", "Reproducibility of psychological science, 2015"},
	{"Social Sciences", "10.1257/aer.91.5.1369", "Colonial origins of comparative development, 2001"},

	{"Arts and Humanities", "10.2307/412243", "Turn-taking for conversation, 1974"},
	{"Arts and Humanities", "10.1177/030631289019003001", "Boundary objects, 1989"},
	{"Arts and Humanities", "10.1098/rspb.1979.0086", "The spandrels of San Marco, 1979"},
	{"Arts and Humanities", "W2266294403", "Meeting the Universe Halfway, 2007"},
}

const (
	maxNodes   = 200
	maxPDFs    = 25
	importEach = 8  // PDFs per field for the import check
	sampleEach = 10 // citation sentences per field for the hand check
)

type result struct {
	seed  seed
	err   error
	works int
	links int // works with a legal PDF link, after fetch looked
	exp   model.ExpansionResult
	fetch library.Fetched
	text  map[string]int // by status
	ctx   library.ContextCounts
	// Papers with text whose text gave no citation fil could tie to a
	// reference: superscript citers, mostly, and papers with no list found.
	silent     int
	mismatches []string
	searched   int // papers with text whose title was searched for
	found      int // ... and came back in the first five papers
	misses     []string
	pdfs       []pdf // downloaded, for the import check
	mentions   []mention
}

type pdf struct {
	file, openalexID, title string
}

// mention is one citation sentence, with what the hand check needs to judge
// it: the citing paper, the marker, and the work it was tied to.
type mention struct {
	Field    string `json:"field"`
	Citing   string `json:"citing"`
	Page     int    `json:"page"`
	Marker   string `json:"marker"`
	Method   string `json:"method"`
	Sentence string `json:"sentence"`
	Cited    string `json:"cited"` // title (year) — first author
	CitedID  string `json:"cited_fil_id"`
}

type importResult struct {
	field              string
	tried, filed, good int
	counts             map[string]int
	wrong              []string
}

func main() {
	root := flag.String("root", "", "folder for the libraries, the cache and the hand-check sample")
	recheck := flag.Bool("recheck-imports", false, "only run the import check again, on a finished run under -root")
	flag.Parse()
	if *root == "" {
		fatal(fmt.Errorf("-root is required"))
	}
	ctx := context.Background()
	if *recheck {
		recheckImports(ctx, *root)
		return
	}
	cache := filepath.Join(*root, "cache")

	start := time.Now()
	var results []result
	var quota library.Quota
	for i, s := range seeds {
		fmt.Fprintf(os.Stderr, "[%2d/%d] %-20s %s ... ", i+1, len(seeds), s.field, s.id)
		r, q := run(ctx, filepath.Join(*root, fmt.Sprintf("lib%02d", i)), cache, s)
		if q.Limit > 0 {
			quota = q
		}
		if r.err != nil {
			fmt.Fprintf(os.Stderr, "FAILED: %v\n", r.err)
		} else {
			fmt.Fprintf(os.Stderr, "%d works, %d PDFs, %d with text\n", r.works, r.fetch.Downloaded, r.text["ok"])
		}
		results = append(results, r)
	}

	var imports []importResult
	for _, f := range fields(results) {
		fmt.Fprintf(os.Stderr, "import check: %s ... ", f)
		ir := importCheck(ctx, filepath.Join(*root, "import-"+slug(f)), cache, f, results)
		fmt.Fprintf(os.Stderr, "%d of %d filed, %d correct\n", ir.filed, ir.tried, ir.good)
		imports = append(imports, ir)
	}

	if err := writeSample(filepath.Join(*root, "mentions-sample.json"), results); err != nil {
		fatal(err)
	}
	report(results, imports, time.Since(start), quota)
}

func run(ctx context.Context, dir, cache string, s seed) (result, library.Quota) {
	r := result{seed: s}
	cfg := &config.Config{MaxNodes: maxNodes, MaxDepth: config.DefaultMaxDepth,
		MaxRefsPerWork: config.DefaultMaxRefsPerWork, FolderCollections: true}
	if err := cfg.UseFolder(dir); err != nil {
		r.err = err
		return r, library.Quota{}
	}
	lib, err := library.Open(ctx, cfg, library.Options{CacheDir: cache})
	if err != nil {
		r.err = err
		return r, library.Quota{}
	}
	defer lib.Close()

	if _, err := lib.Add(ctx, s.id, library.AddOptions{}); err != nil {
		r.err = fmt.Errorf("add: %w", err)
		return r, quota(lib)
	}
	if r.exp, err = lib.Expand(ctx, library.ExpandOptions{}); err != nil {
		r.err = fmt.Errorf("expand: %w", err)
		return r, quota(lib)
	}
	if r.fetch, err = lib.Fetch(ctx, library.FetchOptions{Max: maxPDFs}); err != nil {
		r.err = fmt.Errorf("fetch: %w", err)
		return r, quota(lib)
	}
	pc, err := lib.PDFCounts(ctx)
	if err != nil {
		r.err = fmt.Errorf("pdf counts: %w", err)
		return r, quota(lib)
	}
	r.links = pc[model.PDFLink]
	if r.text, err = lib.TextCounts(ctx); err != nil {
		r.err = fmt.Errorf("text counts: %w", err)
		return r, quota(lib)
	}
	if r.ctx, err = lib.ContextCounts(ctx); err != nil {
		r.err = fmt.Errorf("context counts: %w", err)
		return r, quota(lib)
	}

	rows, err := lib.List(ctx, library.ListOptions{})
	if err != nil {
		r.err = fmt.Errorf("list: %w", err)
		return r, quota(lib)
	}
	r.works = len(rows)
	byID := map[string]library.Listed{}
	for _, row := range rows {
		byID[row.FilID] = row
	}
	for _, row := range rows {
		if row.Mismatch {
			r.mismatches = append(r.mismatches, row.FilID+" "+row.Title)
		}
		if row.TextStatus != "ok" {
			continue
		}
		if row.Contexts == 0 {
			r.silent++
		}
		if err := r.searchTitle(ctx, lib, row); err != nil {
			r.err = fmt.Errorf("search: %w", err)
			return r, quota(lib)
		}
		if err := r.collectMentions(ctx, lib, row, byID); err != nil {
			r.err = fmt.Errorf("mentions: %w", err)
			return r, quota(lib)
		}
	}
	for _, p := range r.fetch.Papers {
		if p.File != "" && !p.Already && p.Work.OpenAlexID != "" {
			r.pdfs = append(r.pdfs, pdf{file: p.File, openalexID: p.Work.OpenAlexID, title: p.Work.DisplayTitle()})
		}
	}
	return r, quota(lib)
}

// searchTitle searches the whole library for the four longest words of a
// paper's title and records whether the paper is among the first five
// papers the hits come from.
func (r *result) searchTitle(ctx context.Context, lib *library.Library, row library.Listed) error {
	q := titleQuery(row.Title)
	if q == "" {
		return nil
	}
	got, err := lib.Search(ctx, q, library.SearchOptions{Limit: 15})
	if err != nil {
		return err
	}
	r.searched++
	var papers []string
	for _, h := range got.Hits {
		if len(papers) == 5 {
			break
		}
		if len(papers) == 0 || papers[len(papers)-1] != h.Work.FilID {
			papers = append(papers, h.Work.FilID)
		}
	}
	for _, p := range papers {
		if p == row.FilID {
			r.found++
			return nil
		}
	}
	r.misses = append(r.misses, fmt.Sprintf("%s %q — searched %q", row.FilID, short(row.Title), q))
	return nil
}

func (r *result) collectMentions(ctx context.Context, lib *library.Library, row library.Listed, byID map[string]library.Listed) error {
	ms, err := lib.CitationContexts(ctx, row.FilID)
	if err != nil {
		return err
	}
	for to, list := range ms {
		cited := byID[to]
		desc := cited.Title
		if desc == "" {
			desc = "[no title: " + cited.OpenAlexID + "]"
		}
		if cited.Year > 0 {
			desc += fmt.Sprintf(" (%d)", cited.Year)
		}
		if cited.Author != "" {
			desc += " — " + cited.Author
		}
		for _, m := range list {
			r.mentions = append(r.mentions, mention{Field: r.seed.field, Citing: row.FilID + " " + short(row.Title),
				Page: m.Page, Marker: m.Marker, Method: m.Method, Sentence: m.Sentence, Cited: desc, CitedID: to})
		}
	}
	return nil
}

// importCheck copies up to importEach of a field's downloaded PDFs, under
// meaningless names, into a fresh library's Inbox/ and imports them.
func importCheck(ctx context.Context, dir, cache, field string, results []result) importResult {
	ir := importResult{field: field, counts: map[string]int{}}
	var pool []pdf
	for _, r := range results {
		if r.seed.field == field {
			pool = append(pool, r.pdfs...)
		}
	}
	rng := rand.New(rand.NewPCG(11, uint64(len(field))))
	rng.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
	seen := map[string]bool{}

	cfg := &config.Config{MaxNodes: maxNodes, MaxDepth: config.DefaultMaxDepth,
		MaxRefsPerWork: config.DefaultMaxRefsPerWork, FolderCollections: true}
	if err := cfg.UseFolder(dir); err != nil {
		ir.wrong = append(ir.wrong, "set up: "+err.Error())
		return ir
	}
	inbox := filepath.Join(dir, "Inbox")
	if err := os.MkdirAll(inbox, 0o755); err != nil {
		ir.wrong = append(ir.wrong, "set up: "+err.Error())
		return ir
	}
	want := map[string]pdf{} // by the name it was given
	for _, p := range pool {
		if len(want) == importEach {
			break
		}
		if seen[p.openalexID] {
			continue
		}
		seen[p.openalexID] = true
		name := filepath.Join(inbox, fmt.Sprintf("scan%03d.pdf", len(want)+1))
		if err := copyFile(p.file, name); err != nil {
			ir.wrong = append(ir.wrong, "copy: "+err.Error())
			return ir
		}
		want[filepath.Base(name)] = p
	}
	ir.tried = len(want)
	if ir.tried == 0 {
		return ir
	}

	lib, err := library.Open(ctx, cfg, library.Options{CacheDir: cache})
	if err != nil {
		ir.wrong = append(ir.wrong, "open: "+err.Error())
		return ir
	}
	defer lib.Close()
	got, err := lib.Import(ctx, library.ImportOptions{})
	if err != nil {
		ir.wrong = append(ir.wrong, "import: "+err.Error())
		return ir
	}
	for _, f := range got.Files {
		ir.counts[f.Outcome]++
		if f.Outcome != library.ImportImported {
			continue
		}
		ir.filed++
		p := want[filepath.Base(f.File)]
		if f.Work.OpenAlexID == p.openalexID {
			ir.good++
		} else {
			ir.wrong = append(ir.wrong, fmt.Sprintf("%q filed as %s %q", short(p.title), f.Work.OpenAlexID, short(f.Work.DisplayTitle())))
		}
	}
	return ir
}

func writeSample(path string, results []result) error {
	var out []mention
	for _, f := range fields(results) {
		var pool []mention
		for _, r := range results {
			if r.seed.field == f {
				pool = append(pool, r.mentions...)
			}
		}
		sort.Slice(pool, func(i, j int) bool {
			if pool[i].Citing != pool[j].Citing {
				return pool[i].Citing < pool[j].Citing
			}
			return pool[i].Sentence < pool[j].Sentence
		})
		rng := rand.New(rand.NewPCG(12, uint64(len(f))))
		rng.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
		out = append(out, pool[:min(sampleEach, len(pool))]...)
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func report(results []result, imports []importResult, took time.Duration, q library.Quota) {
	fmt.Printf("# M3 validation run — %s\n\n", time.Now().Format("2 January 2006"))
	fmt.Printf("`go run ./spikes/12-validation-m3`. %d seeds, 5 fields, one library each; expansion to %d works, "+
		"at most %d PDFs each. Took %s.", len(results), maxNodes, maxPDFs, took.Round(time.Second))
	if q.Limit > 0 {
		fmt.Printf(" OpenAlex allowance at the end: %d of %d credits.", q.Remaining, q.Limit)
	}
	fmt.Print("\n\n")

	fmt.Println("| Field | Seed | Works | With a PDF link | Tried | PDFs | Failed (why) | Text: ok / no text / gibberish | Context | Silent | Title search | Mismatch | Fetch time |")
	fmt.Println("| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |")
	failures := 0
	for _, r := range results {
		if r.err != nil {
			failures++
			fmt.Printf("| %s | %s — %s | **failed:** %s |||||||||||\n", r.seed.field, r.seed.id, r.seed.note, escape(r.err.Error()))
			continue
		}
		fmt.Printf("| %s | %s — %s | %d | %d | %d | %d | %s | %d / %d / %d | %s | %d | %s | %d | %s |\n",
			r.seed.field, r.seed.id, r.seed.note, r.works, r.links, r.fetch.Tried, r.fetch.Downloaded,
			outcomes(r.fetch.Outcomes), r.text["ok"], r.text["no-text"], r.text["gibberish"],
			ratio(r.ctx.WithContext, r.ctx.Citations), r.silent, ratio(r.found, r.searched), len(r.mismatches),
			r.fetch.Duration.Round(time.Second))
	}

	fmt.Print("\n## By field\n\n")
	fmt.Println("| Field | Seeds OK | PDFs | Failed downloads | Text ok | No text / gibberish | Citations with a sentence | Papers with text, no citation found | Title search in top 5 | Import: filed / tried, correct |")
	fmt.Println("| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |")
	for _, f := range fields(results) {
		ok, pdfs, failed, text, bad, with, cites, silent, found, searched := 0, 0, 0, 0, 0, 0, 0, 0, 0, 0
		for _, r := range results {
			if r.seed.field != f || r.err != nil {
				continue
			}
			ok++
			pdfs += r.fetch.Downloaded
			for _, n := range r.fetch.Outcomes {
				failed += n
			}
			text += r.text["ok"]
			bad += r.text["no-text"] + r.text["gibberish"] + r.text["unreadable"] + r.text["timed-out"]
			with += r.ctx.WithContext
			cites += r.ctx.Citations
			silent += r.silent
			found += r.found
			searched += r.searched
		}
		imp := ""
		for _, ir := range imports {
			if ir.field == f {
				imp = fmt.Sprintf("%d / %d, %d correct", ir.filed, ir.tried, ir.good)
			}
		}
		fmt.Printf("| %s | %d / %d | %d | %d | %d | %d | %s | %d | %s | %s |\n",
			f, ok, count(results, f), pdfs, failed, text, bad, ratio(with, cites), silent, ratio(found, searched), imp)
	}

	fmt.Print("\n## Details\n\n")
	for _, r := range results {
		if len(r.mismatches) == 0 && len(r.misses) == 0 {
			continue
		}
		fmt.Printf("**%s** (%s)\n\n", r.seed.note, r.seed.field)
		for _, m := range r.mismatches {
			fmt.Printf("- download check, mismatch: %s\n", m)
		}
		for _, m := range r.misses {
			fmt.Printf("- title search, not in top 5: %s\n", m)
		}
		fmt.Println()
	}
	for _, ir := range imports {
		fmt.Printf("**Import, %s:** %s", ir.field, outcomes(ir.counts))
		for _, w := range ir.wrong {
			fmt.Printf("; %s", w)
		}
		fmt.Println()
	}

	fmt.Printf("\n%d of %d seeds completed every step.\n", len(results)-failures, len(results))
	if failures > 0 {
		os.Exit(1)
	}
}

// titleQuery is the four longest words of a title, longest first: what a
// person half-remembering it might type.
func titleQuery(title string) string {
	words := strings.FieldsFunc(strings.ToLower(title), func(r rune) bool { return !unicode.IsLetter(r) })
	seen := map[string]bool{}
	var uniq []string
	for _, w := range words {
		if len([]rune(w)) >= 4 && !seen[w] {
			seen[w] = true
			uniq = append(uniq, w)
		}
	}
	sort.SliceStable(uniq, func(i, j int) bool { return len([]rune(uniq[i])) > len([]rune(uniq[j])) })
	return strings.Join(uniq[:min(4, len(uniq))], " ")
}

func fields(results []result) []string {
	var out []string
	seen := map[string]bool{}
	for _, r := range results {
		if !seen[r.seed.field] {
			seen[r.seed.field] = true
			out = append(out, r.seed.field)
		}
	}
	return out
}

func count(results []result, field string) int {
	n := 0
	for _, r := range results {
		if r.seed.field == field {
			n++
		}
	}
	return n
}

func outcomes(m map[string]int) string {
	if len(m) == 0 {
		return "—"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s %d", k, m[k])
	}
	return strings.Join(parts, ", ")
}

func ratio(a, b int) string {
	if b == 0 {
		return "—"
	}
	return fmt.Sprintf("%d / %d (%.0f%%)", a, b, 100*float64(a)/float64(b))
}

func short(s string) string {
	if r := []rune(s); len(r) > 60 {
		return string(r[:59]) + "…"
	}
	return s
}

func slug(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, " ", "-"))
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(to)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func quota(l *library.Library) library.Quota {
	q, _ := l.Quota()
	return q
}

func escape(s string) string {
	return strings.ReplaceAll(s, "|", `\|`)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
