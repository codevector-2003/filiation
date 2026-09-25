package mcpsrv

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/codevector-2003/filiation/internal/errs"
	"github.com/codevector-2003/filiation/internal/library"
	"github.com/codevector-2003/filiation/internal/model"
)

// Limits on what one call may ask for. Each exists so that an assistant cannot,
// by one enthusiastic argument, fill its own context or hold a call open for
// minutes.
const (
	defaultListLimit = 20
	maxListLimit     = 100

	// An expansion runs inside the call (D16). 500 works took ~21 s live, well
	// inside a client's timeout, and progress notifications keep it alive.
	defaultExpandNodes = 200
	maxExpandNodes     = 500

	maxPathHops       = 10
	maxDuplicateShown = 20

	// Three names, then "et al.": enough to credit a paper, not so many that a
	// consortium paper's thousand authors swamp the answer.
	maxAuthorsShown = 3
)

const instructions = `Filiation is the user's own citation graph: papers they added, and the papers
those cite, stored on their machine. Every work is identified by its OpenAlex ID
(W followed by digits); pass that ID between tools.

Start with library_stats to see what is there. add_paper adds a paper by DOI, arXiv
ID, PMID, OpenAlex ID, link or title. expand_graph follows references outward,
most-cited first. neighbours and find_path answer questions about the graph.

Works marked stub: true are known only by ID — something cites them, but they have
not been fetched. Never describe a stub as if its content were known. A missing
reference list is usually the data, not a fault: it is common outside STEM.`

// New returns an MCP server exposing lib. version is the fil version, reported
// to the client.
func New(lib *library.Library, version string) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "filiation", Title: "Filiation", Version: version},
		&mcp.ServerOptions{Instructions: instructions})
	t := tools{lib: lib}

	readOnly := func(title string) *mcp.ToolAnnotations {
		// Reads the library only; never the network.
		return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true, OpenWorldHint: ptr(false)}
	}
	grows := func(title string) *mcp.ToolAnnotations {
		// Additive and repeatable: adding a paper twice adds nothing.
		return &mcp.ToolAnnotations{Title: title, DestructiveHint: ptr(false), IdempotentHint: true, OpenWorldHint: ptr(true)}
	}

	mcp.AddTool(s, &mcp.Tool{
		Name:  "add_paper",
		Title: "Add a paper",
		Description: "Add a paper to the library, plus a record for every work it cites. Takes a DOI, " +
			"arXiv ID, PMID, OpenAlex ID, a link to any of those, or a title. A title can match " +
			"several papers: then nothing is added and the candidates come back — call add_paper " +
			"again with the id of the one the user means. Adding a paper twice changes nothing.",
		Annotations: grows("Add a paper"),
	}, t.addPaper)

	mcp.AddTool(s, &mcp.Tool{
		Name:  "expand_graph",
		Title: "Expand the graph",
		Description: "Fetch works the library cites but has not fetched yet, most-cited first, and " +
			"record what they cite in turn. Bounded by max_nodes; call again to continue from where " +
			"it stopped. Reports reference coverage: how many fetched works came with a reference list.",
		Annotations: grows("Expand the graph"),
	}, t.expandGraph)

	mcp.AddTool(s, &mcp.Tool{
		Name:  "neighbours",
		Title: "Neighbours",
		Description: "What a paper in the library cites, and what in the library cites it. Lists are " +
			"ranked — fetched works first, then by how widely cited — and capped by limit.",
		Annotations: readOnly("Neighbours"),
	}, t.neighbours)

	mcp.AddTool(s, &mcp.Tool{
		Name:  "find_path",
		Title: "Find a path",
		Description: "The shortest chain of citations between two papers in the library. By default it " +
			"looks for lineage — one paper reaching the other by following references, in either " +
			"order — which is how an idea descends. any_direction also allows mixed chains.",
		Annotations: readOnly("Find a path"),
	}, t.findPath)

	mcp.AddTool(s, &mcp.Tool{
		Name:  "library_stats",
		Title: "Library stats",
		Description: "The library at a glance: works, citations, seeds, and reference coverage. " +
			"include_duplicates also lists fetched works that share a title — probably one paper " +
			"held twice, but not certainly: a book review carries its book's title.",
		Annotations: readOnly("Library stats"),
	}, t.libraryStats)

	return s
}

// ServeStdio serves lib over stdin and stdout until the client disconnects or
// ctx is cancelled. Nothing else may write to stdout while it runs.
func ServeStdio(ctx context.Context, lib *library.Library, version string) error {
	return New(lib, version).Run(ctx, &mcp.StdioTransport{})
}

type tools struct{ lib *library.Library }

// ---- add_paper ----

type addIn struct {
	Identifier string `json:"identifier" jsonschema:"a DOI, arXiv ID, PMID, OpenAlex ID, a link to any of those, or a title"`
	Candidates int    `json:"candidates,omitempty" jsonschema:"for a title, how many candidates to offer (default 10)"`
}

type addOut struct {
	Status     string      `json:"status" jsonschema:"added, already_in_library, or choose (a title matched several papers and nothing was added)"`
	Paper      *Work       `json:"paper,omitempty"`
	References int         `json:"references,omitempty" jsonschema:"how many works the paper cites"`
	NewWorks   int         `json:"new_works,omitempty" jsonschema:"of those, how many were new to the library (recorded as stubs)"`
	Candidates []Candidate `json:"candidates,omitempty"`
	Note       string      `json:"note,omitempty"`
}

func (t tools) addPaper(ctx context.Context, _ *mcp.CallToolRequest, in addIn) (*mcp.CallToolResult, addOut, error) {
	res, err := t.lib.Add(ctx, in.Identifier, library.AddOptions{SearchLimit: min(max(in.Candidates, 0), maxListLimit)})
	var amb *library.AmbiguousError
	if errors.As(err, &amb) {
		return nil, addOut{
			Status:     "choose",
			Candidates: candidates(amb.Candidates),
			Note:       "Nothing was added. Ask the user which paper they mean, then call add_paper with its id.",
		}, nil
	}
	if err != nil {
		return nil, addOut{}, toolError(err)
	}

	w := toWork(res.Work)
	out := addOut{Status: "added", Paper: &w, References: res.Refs, NewWorks: res.NewStubs}
	switch {
	case res.AlreadySeed:
		out.Status = "already_in_library"
	case res.DeadEnd:
		out.Note = "OpenAlex has no reference list for this paper, so the graph cannot grow from it. " +
			"That is common for books and outside STEM; it is the data, not a fault."
	default:
		out.Note = "Its references are recorded as stubs. Call expand_graph to fetch them."
	}
	return nil, out, nil
}

// ---- expand_graph ----

type expandIn struct {
	MaxNodes int `json:"max_nodes,omitempty" jsonschema:"how many works to fetch at most (default 200, at most 500)"`
	MaxDepth int `json:"max_depth,omitempty" jsonschema:"how many citation steps from the user's own papers to go (default from the user's settings, usually 3)"`
}

type expandOut struct {
	Fetched           int     `json:"fetched" jsonschema:"works fetched in this call"`
	NewCitations      int     `json:"new_citations"`
	ReferenceCoverage float64 `json:"reference_coverage" jsonschema:"share of the works fetched that came with a reference list, 0 to 1"`
	NotInOpenAlex     int     `json:"not_in_openalex" jsonschema:"cited IDs OpenAlex has no record of"`
	Merged            int     `json:"merged" jsonschema:"works OpenAlex holds twice, folded into one"`
	Skipped           int     `json:"skipped" jsonschema:"works left for next time after a failed request"`
	StoppedBecause    string  `json:"stopped_because" jsonschema:"budget-exhausted, frontier-empty, max-depth or cancelled"`
	Seconds           float64 `json:"seconds"`
	Library           stats   `json:"library"`
	Note              string  `json:"note,omitempty"`
}

func (t tools) expandGraph(ctx context.Context, req *mcp.CallToolRequest, in expandIn) (*mcp.CallToolResult, expandOut, error) {
	nodes := in.MaxNodes
	if nodes <= 0 {
		nodes = defaultExpandNodes
	}
	nodes = min(nodes, maxExpandNodes)

	opts := library.ExpandOptions{MaxNodes: nodes, MaxDepth: max(in.MaxDepth, 0)}
	if token := req.Params.GetProgressToken(); token != nil {
		opts.Progress = func(r model.ExpansionResult) {
			// Best effort: a lost notification must not fail the expansion.
			_ = req.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
				ProgressToken: token,
				Progress:      float64(r.Hydrated),
				Total:         float64(nodes),
				Message:       fmt.Sprintf("fetched %d of up to %d works", r.Hydrated, nodes),
			})
		}
	}

	res, err := t.lib.Expand(ctx, opts)
	if err != nil {
		// Whatever was committed before the failure is kept.
		return nil, expandOut{}, fmt.Errorf("%w (%d works were fetched and saved before it failed; "+
			"calling expand_graph again continues from there)", toolError(err), res.Hydrated)
	}

	out := expandOut{
		Fetched:           res.Hydrated,
		NewCitations:      res.Edges,
		ReferenceCoverage: round2(res.ReferenceCoverage()),
		NotInOpenAlex:     res.Unresolved,
		Merged:            res.Merged,
		Skipped:           res.Skipped,
		StoppedBecause:    string(res.StoppedBecause),
		Seconds:           round2(res.Duration.Seconds()),
	}
	if out.Library, err = t.stats(ctx); err != nil {
		return nil, expandOut{}, toolError(err)
	}
	switch res.StoppedBecause {
	case model.StopBudgetExhausted:
		out.Note = "Stopped at max_nodes. Call expand_graph again to continue."
	case model.StopFrontierEmpty:
		if res.Hydrated == 0 && out.Library.Works == 0 {
			out.Note = "The library is empty. Add a paper with add_paper first."
		} else {
			out.Note = "Everything the library cites has been fetched, within the depth limit."
		}
	case model.StopMaxDepth:
		out.Note = "Reached the depth limit. Raise max_depth to go further."
	}
	return nil, out, nil
}

// ---- neighbours ----

type neighboursIn struct {
	Identifier string `json:"identifier" jsonschema:"the paper: an OpenAlex ID, DOI, arXiv ID, PMID or title, already in the library"`
	Direction  string `json:"direction,omitempty" jsonschema:"cites (its references), cited_by (what in the library cites it), or both (default)"`
	Limit      int    `json:"limit,omitempty" jsonschema:"how many works to list each way (default 20, at most 100)"`
}

type neighboursOut struct {
	Paper      Work   `json:"paper"`
	References *Page  `json:"references,omitempty" jsonschema:"what the paper cites"`
	CitedBy    *Page  `json:"cited_by,omitempty" jsonschema:"what in this library cites the paper — not a global count"`
	Note       string `json:"note,omitempty"`
}

func (t tools) neighbours(ctx context.Context, _ *mcp.CallToolRequest, in neighboursIn) (*mcp.CallToolResult, neighboursOut, error) {
	dir := strings.ToLower(strings.TrimSpace(in.Direction))
	switch dir {
	case "":
		dir = "both"
	case "both", "cites", "cited_by":
	default:
		return nil, neighboursOut{}, fmt.Errorf("direction must be cites, cited_by or both, not %q", in.Direction)
	}
	limit := listLimit(in.Limit)

	n, err := t.lib.Neighbours(ctx, in.Identifier)
	if err != nil {
		return nil, neighboursOut{}, toolError(err)
	}
	out := neighboursOut{Paper: toWork(n.Work)}
	if dir != "cited_by" {
		p := page(n.Cites, limit)
		out.References = &p
	}
	if dir != "cites" {
		p := page(n.CitedBy, limit)
		out.CitedBy = &p
	}
	switch {
	case n.Work.IsStub():
		out.Note = "This work has not been fetched, so its references are unknown. expand_graph fetches it in turn."
	case len(n.Cites) == 0:
		out.Note = "OpenAlex has no reference list for this work. That is common for books and outside STEM."
	}
	return nil, out, nil
}

// ---- find_path ----

type pathIn struct {
	From         string `json:"from" jsonschema:"the first paper, already in the library"`
	To           string `json:"to" jsonschema:"the second paper, already in the library"`
	MaxHops      int    `json:"max_hops,omitempty" jsonschema:"longest chain to look for (default 6, at most 10)"`
	AnyDirection bool   `json:"any_direction,omitempty" jsonschema:"allow chains that mix citing and being cited, not only lineage"`
}

// Step is one paper on a chain, and how it relates to the next.
type Step struct {
	Work
	Next string `json:"next,omitempty" jsonschema:"cites: this paper cites the next one; cited_by: the next one cites this paper"`
}

type pathOut struct {
	Found bool   `json:"found"`
	Steps int    `json:"steps,omitempty" jsonschema:"citations in the chain"`
	Chain []Step `json:"chain,omitempty" jsonschema:"from the first paper to the second"`
	From  *Work  `json:"from,omitempty"`
	To    *Work  `json:"to,omitempty"`
	Note  string `json:"note,omitempty"`
}

func (t tools) findPath(ctx context.Context, _ *mcp.CallToolRequest, in pathIn) (*mcp.CallToolResult, pathOut, error) {
	hops := in.MaxHops
	if hops <= 0 {
		hops = library.DefaultMaxHops
	}
	hops = min(hops, maxPathHops)

	p, err := t.lib.PathBetween(ctx, in.From, in.To, library.PathOptions{MaxHops: hops, AnyDirection: in.AnyDirection})
	if err != nil {
		return nil, pathOut{}, toolError(err)
	}
	if !p.Found {
		from, to := toWork(p.Works[0]), toWork(p.Works[1])
		note := fmt.Sprintf("No chain of references within %d steps. Not finding one is an answer: "+
			"neither descends from the other in this library.", hops)
		if !in.AnyDirection {
			note += " any_direction: true looks for any connection."
		}
		if p.Works[0].IsStub() || p.Works[1].IsStub() {
			note += " One of them has not been fetched yet, so its references are unknown; expand_graph may connect them."
		}
		return nil, pathOut{From: &from, To: &to, Note: note}, nil
	}

	out := pathOut{Found: true, Steps: len(p.Cites), Chain: make([]Step, len(p.Works))}
	for i, w := range p.Works {
		out.Chain[i] = Step{Work: toWork(w)}
		if i < len(p.Cites) {
			out.Chain[i].Next = "cited_by"
			if p.Cites[i] {
				out.Chain[i].Next = "cites"
			}
		}
	}
	return nil, out, nil
}

// ---- library_stats ----

type statsIn struct {
	IncludeDuplicates bool `json:"include_duplicates,omitempty" jsonschema:"also list fetched works that share a title"`
}

type stats struct {
	Works             int     `json:"works" jsonschema:"every work, stubs included"`
	Fetched           int     `json:"fetched"`
	Stubs             int     `json:"stubs" jsonschema:"known only by ID so far"`
	NotInOpenAlex     int     `json:"not_in_openalex"`
	Seeds             int     `json:"seeds" jsonschema:"papers the user added"`
	Citations         int     `json:"citations"`
	ReferenceCoverage float64 `json:"reference_coverage" jsonschema:"share of fetched works that have a reference list, 0 to 1"`
}

// DuplicateGroup is fetched works that share a title.
type DuplicateGroup struct {
	Title string `json:"title"`
	Works []Work `json:"works"`
}

type statsOut struct {
	stats
	Duplicates        []DuplicateGroup `json:"duplicates,omitempty"`
	DuplicatesOmitted int              `json:"duplicates_omitted,omitempty"`
	Note              string           `json:"note,omitempty"`
}

func (t tools) libraryStats(ctx context.Context, _ *mcp.CallToolRequest, in statsIn) (*mcp.CallToolResult, statsOut, error) {
	s, err := t.stats(ctx)
	if err != nil {
		return nil, statsOut{}, toolError(err)
	}
	out := statsOut{stats: s}
	if s.Works == 0 {
		out.Note = "The library is empty. Add a paper with add_paper."
	}
	if !in.IncludeDuplicates {
		return nil, out, nil
	}
	groups, err := t.lib.ProbableDuplicates(ctx)
	if err != nil {
		return nil, statsOut{}, toolError(err)
	}
	for _, g := range groups[:min(len(groups), maxDuplicateShown)] {
		d := DuplicateGroup{Title: g.Title}
		for _, w := range g.Works {
			d.Works = append(d.Works, toWork(w))
		}
		out.Duplicates = append(out.Duplicates, d)
	}
	out.DuplicatesOmitted = len(groups) - len(out.Duplicates)
	out.Note = "Works sharing a title are probably one paper held twice, but not certainly. They are never merged automatically."
	return nil, out, nil
}

func (t tools) stats(ctx context.Context) (stats, error) {
	s, err := t.lib.Summarise(ctx)
	if err != nil {
		return stats{}, err
	}
	out := stats{
		Works: s.Works, Fetched: s.Fetched, Stubs: s.Stubs, NotInOpenAlex: s.Unresolved,
		Seeds: s.Seeds, Citations: s.Citations,
	}
	if s.Fetched > 0 {
		out.ReferenceCoverage = round2(float64(s.Fetched-s.DeadEnds) / float64(s.Fetched))
	}
	return out, nil
}

// ---- errors ----

// toolError turns a library error into one an assistant can act on. The
// library's own messages are already written for a person; this adds what to
// do next.
func toolError(err error) error {
	var amb *library.AmbiguousError
	if errors.As(err, &amb) {
		// Only the read tools get here: add_paper returns candidates instead.
		var b strings.Builder
		fmt.Fprintf(&b, "%q matches %d works in the library; call again with one of these ids:", amb.Title, len(amb.Candidates))
		for _, c := range amb.Candidates[:min(len(amb.Candidates), 10)] {
			w := toWork(c.Work)
			fmt.Fprintf(&b, "\n  %s  %s", w.ID, w.Title)
			if w.Year != 0 {
				fmt.Fprintf(&b, " (%d)", w.Year)
			}
		}
		return errors.New(b.String())
	}
	switch {
	case errors.Is(err, errs.ErrNotFound):
		return fmt.Errorf("%v. Only papers already in the library can be looked up; add it with add_paper first", err)
	case errors.Is(err, errs.ErrInvalidInput):
		return fmt.Errorf("%v. Give a DOI, arXiv ID, PMID, OpenAlex ID (W followed by digits), a link, or a title", err)
	case errors.Is(err, errs.ErrUnresolved):
		return fmt.Errorf("%v. Check the identifier; OpenAlex has no such work", err)
	case errors.Is(err, errs.ErrTransient):
		if d, ok := library.RetryAfter(err); ok {
			return fmt.Errorf("%v. OpenAlex asked to wait %s before trying again", err, d.Round(1e9))
		}
		return fmt.Errorf("%v. OpenAlex could not be reached; trying again later may work", err)
	}
	return err
}

func listLimit(n int) int {
	if n <= 0 {
		return defaultListLimit
	}
	return min(n, maxListLimit)
}

func round2(f float64) float64 { return float64(int(f*100+0.5)) / 100 }

func ptr[T any](v T) *T { return &v }
