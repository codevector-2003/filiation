// Command validation is the M1 validation run: ARCHITECTURE_PHASE1.md §10's
// "20-paper / 5-field" check, run against live OpenAlex before v0.1.
//
// Each seed gets a library of its own, so every number below belongs to one
// seed and nothing else. For each: add it, expand once with the default budget
// (500) and depth (3), count probable title duplicates (D14), and export
// GraphML — then check the export reads back with every edge resolving to a
// node in the file. One HTTP cache is shared, so a re-run is nearly free.
//
// Seeds in medicine, physics, computer science and the social sciences are
// hand-picked: well-known papers a researcher would plausibly start from. The
// four humanities seeds are chosen the way spike 4 chose them — the most-cited
// arts-and-humanities articles with more than 20 references — because
// humanities DOIs are the ones least safely recalled. Two of OpenAlex's top
// "Arts and Humanities" results (a 1949 population-genetics paper, a sociology
// classic filed under Music) were skipped as misclassified.
//
// No contact email is sent.
//
// Usage:
//
//	go run ./spikes/8-validation > docs/validation-run.md
package main

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/codevector-2003/filiation/internal/config"
	"github.com/codevector-2003/filiation/internal/library"
	"github.com/codevector-2003/filiation/internal/model"
)

type seed struct {
	field, id, note string
}

var seeds = []seed{
	{"Medicine", "10.1056/NEJMoa2034577", "BNT162b2 vaccine trial, 2020"},
	{"Medicine", "10.1016/S0140-6736(20)30183-5", "COVID-19 clinical features, Wuhan, 2020"},
	{"Medicine", "10.1001/jama.2020.1585", "138 hospitalised COVID-19 patients, 2020"},
	{"Medicine", "10.1371/journal.pmed.0020124", "Why most published research findings are false, 2005"},

	{"Physics", "10.1103/PhysRevLett.116.061102", "LIGO GW150914, 2016"},
	{"Physics", "10.1103/PhysRev.47.777", "Einstein-Podolsky-Rosen, 1935"},
	{"Physics", "10.1038/s41586-019-1666-5", "Quantum supremacy, 2019"},
	{"Physics", "10.1103/RevModPhys.81.109", "Electronic properties of graphene, 2009"},

	{"Computer Science", "arXiv:1706.03762", "Attention is all you need, 2017 (by arXiv ID)"},
	{"Computer Science", "10.1109/CVPR.2016.90", "Deep residual learning, 2016"},
	{"Computer Science", "10.1038/nature14539", "Deep learning, 2015"},
	{"Computer Science", "10.1145/3292500.3330701", "Optuna, 2019"},

	{"Social Sciences", "10.7717/peerj.4375", "The state of OA, 2018 (the M0 seed)"},
	{"Social Sciences", "10.1086/225469", "The strength of weak ties, 1973"},
	{"Social Sciences", "10.1126/science.aac4716", "Reproducibility of psychological science, 2015"},
	{"Social Sciences", "10.1257/aer.91.5.1369", "Colonial origins of comparative development, 2001"},

	{"Arts and Humanities", "10.2307/412243", "Turn-taking for conversation, 1974"},
	{"Arts and Humanities", "10.1177/030631289019003001", "Boundary objects, 1989"},
	{"Arts and Humanities", "10.1098/rspb.1979.0086", "The spandrels of San Marco, 1979"},
	{"Arts and Humanities", "W2266294403", "Meeting the Universe Halfway, 2007 (no DOI)"},
}

type result struct {
	seed      seed
	title     string
	err       error
	refs      int
	exp       model.ExpansionResult
	dups      int
	nodes     int
	edges     int
	exportOK  bool
	exportErr string
}

func main() {
	ctx := context.Background()
	root, err := os.MkdirTemp("", "fil-validation-*")
	if err != nil {
		fatal(err)
	}
	defer os.RemoveAll(root)
	cache := filepath.Join(root, "cache")

	start := time.Now()
	var results []result
	var quota library.Quota
	for i, s := range seeds {
		fmt.Fprintf(os.Stderr, "[%2d/%d] %-20s %s ... ", i+1, len(seeds), s.field, s.id)
		r, q := run(ctx, filepath.Join(root, fmt.Sprintf("lib%02d", i)), cache, s)
		if q.Limit > 0 {
			quota = q
		}
		if r.err != nil {
			fmt.Fprintf(os.Stderr, "FAILED: %v\n", r.err)
		} else {
			fmt.Fprintf(os.Stderr, "%d fetched in %s\n", r.exp.Hydrated, r.exp.Duration.Round(time.Second))
		}
		results = append(results, r)
	}
	report(results, time.Since(start), quota)
}

func run(ctx context.Context, dir, cache string, s seed) (result, library.Quota) {
	r := result{seed: s}
	cfg := &config.Config{
		DBPath:         filepath.Join(dir, "library.db"),
		MaxNodes:       config.DefaultMaxNodes,
		MaxDepth:       config.DefaultMaxDepth,
		MaxRefsPerWork: config.DefaultMaxRefsPerWork,
	}
	lib, err := library.Open(ctx, cfg, library.Options{CacheDir: cache})
	if err != nil {
		r.err = err
		return r, library.Quota{}
	}
	defer lib.Close()

	added, err := lib.Add(ctx, s.id, library.AddOptions{})
	if err != nil {
		r.err = fmt.Errorf("add: %w", err)
		return r, quota(lib)
	}
	r.title, r.refs = added.Work.DisplayTitle(), added.Refs

	r.exp, err = lib.Expand(ctx, library.ExpandOptions{})
	if err != nil {
		r.err = fmt.Errorf("expand: %w", err)
		return r, quota(lib)
	}

	dups, err := lib.ProbableDuplicates(ctx)
	if err != nil {
		r.err = fmt.Errorf("duplicates: %w", err)
		return r, quota(lib)
	}
	r.dups = len(dups)

	var buf bytes.Buffer
	got, err := lib.ExportGraphML(ctx, &buf, library.ExportOptions{})
	if err != nil {
		r.err = fmt.Errorf("export: %w", err)
		return r, quota(lib)
	}
	r.nodes, r.edges = got.Nodes, got.Edges
	r.exportOK, r.exportErr = checkGraphML(buf.Bytes(), got)
	return r, quota(lib)
}

// checkGraphML reads the export back with an independent parser and confirms
// it holds what was reported, with no edge pointing outside the file.
func checkGraphML(doc []byte, want library.Exported) (bool, string) {
	var g struct {
		Nodes []struct {
			ID string `xml:"id,attr"`
		} `xml:"graph>node"`
		Edges []struct {
			Source string `xml:"source,attr"`
			Target string `xml:"target,attr"`
		} `xml:"graph>edge"`
	}
	if err := xml.Unmarshal(doc, &g); err != nil {
		return false, "not well-formed: " + err.Error()
	}
	if len(g.Nodes) != want.Nodes || len(g.Edges) != want.Edges {
		return false, fmt.Sprintf("file has %d/%d, reported %d/%d", len(g.Nodes), len(g.Edges), want.Nodes, want.Edges)
	}
	ids := make(map[string]bool, len(g.Nodes))
	for _, n := range g.Nodes {
		if ids[n.ID] {
			return false, "duplicate node " + n.ID
		}
		ids[n.ID] = true
	}
	for _, e := range g.Edges {
		if !ids[e.Source] || !ids[e.Target] {
			return false, "edge to a missing node"
		}
	}
	return true, ""
}

func quota(l *library.Library) library.Quota {
	q, _ := l.Quota()
	return q
}

func report(results []result, took time.Duration, q library.Quota) {
	fmt.Printf("# Validation run — %s\n\n", time.Now().Format("2 January 2006"))
	fmt.Printf("`go run ./spikes/8-validation`. %d seeds, 5 fields, one library each; default budget 500, "+
		"depth 3. Took %s.", len(results), took.Round(time.Second))
	if q.Limit > 0 {
		fmt.Printf(" OpenAlex allowance at the end: %d of %d credits.", q.Remaining, q.Limit)
	}
	fmt.Print("\n\n")

	fmt.Println("| Field | Seed | Refs | Fetched | Coverage | Not found | Merged | Title dups | Stopped | Export (nodes / edges) | Time |")
	fmt.Println("| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |")
	failures := 0
	for _, r := range results {
		if r.err != nil {
			failures++
			fmt.Printf("| %s | %s — %s | **failed:** %s |||||||||\n", r.seed.field, r.seed.id, r.seed.note, escape(r.err.Error()))
			continue
		}
		export := fmt.Sprintf("%d / %d", r.nodes, r.edges)
		if !r.exportOK {
			failures++
			export += " **" + r.exportErr + "**"
		}
		fmt.Printf("| %s | %s — %s | %d | %d | %.0f%% | %d | %d | %d | %s | %s | %s |\n",
			r.seed.field, r.seed.id, r.seed.note, r.refs, r.exp.Hydrated, 100*r.exp.ReferenceCoverage(),
			r.exp.Unresolved, r.exp.Merged, r.dups, r.exp.StoppedBecause, export,
			r.exp.Duration.Round(time.Second))
	}

	fmt.Print("\n## By field\n\n")
	fmt.Println("| Field | Seeds OK | Fetched | Dead ends | Coverage | Not found | Title dups per 100 fetched |")
	fmt.Println("| --- | --- | --- | --- | --- | --- | --- |")
	var order []string
	byField := map[string][]result{}
	for _, r := range results {
		if _, seen := byField[r.seed.field]; !seen {
			order = append(order, r.seed.field)
		}
		byField[r.seed.field] = append(byField[r.seed.field], r)
	}
	for _, f := range order {
		ok, fetched, dead, unresolved, dups := 0, 0, 0, 0, 0
		for _, r := range byField[f] {
			if r.err != nil {
				continue
			}
			ok++
			fetched += r.exp.Hydrated
			dead += r.exp.DeadEnds
			unresolved += r.exp.Unresolved
			dups += r.dups
		}
		cov, dupRate := 0.0, 0.0
		if fetched > 0 {
			cov = 100 * float64(fetched-dead) / float64(fetched)
			dupRate = 100 * float64(dups) / float64(fetched)
		}
		fmt.Printf("| %s | %d / %d | %d | %d | %.0f%% | %d | %.1f |\n",
			f, ok, len(byField[f]), fetched, dead, cov, unresolved, dupRate)
	}

	fmt.Printf("\n%d of %d seeds completed every step.\n", len(results)-failures, len(results))
	if failures > 0 {
		os.Exit(1)
	}
}

func escape(s string) string {
	return string(bytes.ReplaceAll([]byte(s), []byte("|"), []byte(`\|`)))
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
