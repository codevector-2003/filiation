package mcpsrv

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/codevector-2003/filiation/internal/config"
	"github.com/codevector-2003/filiation/internal/library"
)

// These tests drive the real server through a real MCP client over an
// in-memory connection, against a real library. Only the network is replaced:
// OpenAlex answers from the responses recorded for sources/openalex.

const fixtureDir = "../sources/openalex/testdata"

type replay struct {
	t      *testing.T
	routes map[string]string // path plus filter -> fixture file, or "404"
}

func (r replay) RoundTrip(req *http.Request) (*http.Response, error) {
	key := req.URL.Path
	if f := req.URL.Query().Get("filter"); f != "" {
		key += "?filter=" + f
	}
	status, body := 200, ""
	switch file, ok := r.routes[key]; {
	case !ok:
		r.t.Errorf("unexpected request %s", key)
		status = 400
	case file == "404":
		status = 404
	default:
		raw, err := os.ReadFile(filepath.Join(fixtureDir, file))
		if err != nil {
			r.t.Fatalf("read fixture %s: %v", file, err)
		}
		body = string(raw)
	}
	return &http.Response{StatusCode: status, Header: http.Header{},
		Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

// peerjExpansion routes the M0 seed and one expansion hop from it: 54
// references in one batch, 44 returned by the filter, the other ten confirmed
// one at a time. Recorded 24 Sept 2026; see library_test.go.
func peerjExpansion(t *testing.T) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(fixtureDir, "work_W2741809807.json"))
	if err != nil {
		t.Fatal(err)
	}
	var seed struct {
		ReferencedWorks []string `json:"referenced_works"`
	}
	if err := json.Unmarshal(raw, &seed); err != nil {
		t.Fatal(err)
	}
	ids := make([]string, len(seed.ReferencedWorks))
	for i, r := range seed.ReferencedWorks {
		ids[i] = r[strings.LastIndexByte(r, '/')+1:]
	}
	sort.Strings(ids)
	routes := map[string]string{
		"/works/doi:10.7717/peerj.4375":                        "work_W2741809807.json",
		"/works?filter=openalex_id:" + strings.Join(ids, "|"):  "batch_peerj_refs.json",
		"/works/W6887727194":                                   "single_W6887727194.json",
		"/works/W6948399261":                                   "single_W6948399261.json",
		"/works?filter=title.search:Attention is all you need": "search_attention.json",
	}
	for _, id := range []string{"W2611818942", "W6637734586", "W6640061894", "W6640335369",
		"W6640848857", "W6687877900", "W6725637556", "W6744027076"} {
		routes["/works/"+id] = "404"
	}
	return routes
}

type client struct {
	t  *testing.T
	cs *mcp.ClientSession

	mu       sync.Mutex
	progress []float64
}

// connect serves a fresh library and returns a client connected to it.
func connect(t *testing.T) *client {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		DBPath:         filepath.Join(dir, "My Library", "library.db"),
		MaxNodes:       config.DefaultMaxNodes,
		MaxDepth:       config.DefaultMaxDepth,
		MaxRefsPerWork: config.DefaultMaxRefsPerWork,
	}
	lib, err := library.Open(t.Context(), cfg, library.Options{
		CacheDir: filepath.Join(dir, "cache"), Transport: replay{t: t, routes: peerjExpansion(t)}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lib.Close() })

	st, ct := mcp.NewInMemoryTransports()
	ss, err := New(lib, "test").Connect(t.Context(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })

	c := &client{t: t}
	mc := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, &mcp.ClientOptions{
		ProgressNotificationHandler: func(_ context.Context, req *mcp.ProgressNotificationClientRequest) {
			c.mu.Lock()
			c.progress = append(c.progress, req.Params.Progress)
			c.mu.Unlock()
		},
	})
	cs, err := mc.Connect(t.Context(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	c.cs = cs
	return c
}

// call invokes a tool and decodes its structured result into out. It fails the
// test if the tool reported an error.
func (c *client) call(name string, args map[string]any, out any) {
	c.t.Helper()
	res := c.raw(name, args, nil)
	if res.IsError {
		c.t.Fatalf("%s(%v) failed: %s", name, args, text(res))
	}
	b, err := json.Marshal(res.StructuredContent)
	if err != nil {
		c.t.Fatal(err)
	}
	if err := json.Unmarshal(b, out); err != nil {
		c.t.Fatalf("%s: decode %s: %v", name, b, err)
	}
}

// fails invokes a tool that should report an error, and returns its message.
func (c *client) fails(name string, args map[string]any) string {
	c.t.Helper()
	res := c.raw(name, args, nil)
	if !res.IsError {
		c.t.Fatalf("%s(%v) succeeded; want a tool error", name, args)
	}
	return text(res)
}

func (c *client) raw(name string, args map[string]any, progressToken any) *mcp.CallToolResult {
	c.t.Helper()
	p := &mcp.CallToolParams{Name: name, Arguments: args}
	if progressToken != nil {
		p.SetProgressToken(progressToken)
	}
	res, err := c.cs.CallTool(c.t.Context(), p)
	if err != nil {
		c.t.Fatalf("%s: protocol error: %v", name, err)
	}
	return res
}

func text(res *mcp.CallToolResult) string {
	var parts []string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// grown is a client whose library holds the M0 seed and one hop out from it.
func grown(t *testing.T) *client {
	t.Helper()
	c := connect(t)
	var added addOut
	c.call("add_paper", map[string]any{"identifier": "10.7717/peerj.4375"}, &added)
	var ex expandOut
	c.call("expand_graph", map[string]any{"max_nodes": 54}, &ex)
	return c
}

func TestToolsAreListed(t *testing.T) {
	t.Parallel()
	c := connect(t)
	res, err := c.cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
		readOnly := tool.Annotations != nil && tool.Annotations.ReadOnlyHint
		wantReadOnly := tool.Name != "add_paper" && tool.Name != "expand_graph"
		if readOnly != wantReadOnly {
			t.Errorf("%s: readOnlyHint = %v, want %v", tool.Name, readOnly, wantReadOnly)
		}
	}
	slices.Sort(names)
	want := []string{"add_paper", "expand_graph", "find_path", "library_stats", "neighbours"}
	if !slices.Equal(names, want) {
		t.Errorf("tools = %v, want %v", names, want)
	}
}

func TestAddPaper(t *testing.T) {
	t.Parallel()
	c := connect(t)

	var out addOut
	c.call("add_paper", map[string]any{"identifier": "10.7717/peerj.4375"}, &out)
	if out.Status != "added" || out.Paper == nil || out.Paper.ID != "W2741809807" ||
		out.References != 54 || out.NewWorks != 54 {
		t.Fatalf("add_paper = %+v; want W2741809807 added with 54 new references", out)
	}
	// Every work carries its ID and OA status (§6, rule 2).
	if out.Paper.OAStatus != "gold" || out.Paper.Stub || !out.Paper.Seed {
		t.Errorf("paper = %+v; want a fetched gold OA seed", *out.Paper)
	}
	// Three names, in byline order, then a count: the answer can credit the
	// paper from the library, not from memory (VALIDATION.md, M2).
	if a := out.Paper.Authors; !slices.Equal(a, []string{"Heather Piwowar", "Jason R Priem", "Vincent Larivière"}) ||
		out.Paper.MoreAuthors != 6 {
		t.Errorf("authors = %q + %d more; want the first three and 6 more", a, out.Paper.MoreAuthors)
	}

	var again addOut
	c.call("add_paper", map[string]any{"identifier": "https://doi.org/10.7717/peerj.4375"}, &again)
	if again.Status != "already_in_library" {
		t.Errorf("second add_paper status = %q, want already_in_library", again.Status)
	}
}

// A title never seeds a graph until someone chooses (ADR-005): the candidates
// come back as a normal result, and nothing is written.
func TestAddPaperByTitleOffersCandidates(t *testing.T) {
	t.Parallel()
	c := connect(t)

	var out addOut
	c.call("add_paper", map[string]any{"identifier": "Attention is all you need"}, &out)
	if out.Status != "choose" || len(out.Candidates) != 10 || out.Paper != nil {
		t.Fatalf("add_paper(title) = status %q, %d candidates", out.Status, len(out.Candidates))
	}
	if first := out.Candidates[0]; !first.TitleMatch || first.Title != "Attention Is All You Need" || first.ID == "" {
		t.Errorf("first candidate = %+v", first)
	}
	var s statsOut
	c.call("library_stats", nil, &s)
	if s.Works != 0 {
		t.Errorf("library holds %d works after an unchosen title; want 0", s.Works)
	}
}

func TestAddPaperErrorsSayWhatToDo(t *testing.T) {
	t.Parallel()
	c := connect(t)
	if msg := c.fails("add_paper", map[string]any{"identifier": "2017"}); !strings.Contains(msg, "Give a DOI") {
		t.Errorf("invalid input message = %q", msg)
	}
	// Schema validation rejects a call without the required argument before
	// it reaches the library.
	if msg := c.fails("add_paper", map[string]any{}); !strings.Contains(msg, "identifier") {
		t.Errorf("missing argument message = %q", msg)
	}
}

func TestExpandGraph(t *testing.T) {
	t.Parallel()
	c := connect(t)

	var empty expandOut
	c.call("expand_graph", nil, &empty)
	if empty.Fetched != 0 || empty.StoppedBecause != "frontier-empty" || !strings.Contains(empty.Note, "add_paper") {
		t.Errorf("expand_graph on an empty library = %+v", empty)
	}

	var added addOut
	c.call("add_paper", map[string]any{"identifier": "10.7717/peerj.4375"}, &added)
	res := c.raw("expand_graph", map[string]any{"max_nodes": 54}, "tok-1")
	if res.IsError {
		t.Fatal(text(res))
	}
	var out expandOut
	b, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	// The same recorded hop as library's TestExpandOneHopFromTheM0Seed.
	if out.Fetched != 46 || out.NotInOpenAlex != 8 || out.NewCitations != 1601 ||
		out.StoppedBecause != "budget-exhausted" || out.ReferenceCoverage != 0.83 {
		t.Errorf("expand_graph = %+v; want 46 fetched, 8 not in OpenAlex, 1601 citations, coverage 0.83", out)
	}
	if out.Library.Works != 1041 || out.Library.Fetched != 47 || out.Library.Citations != 1655 {
		t.Errorf("library after expand = %+v; want 1041 works, 47 fetched, 1655 citations", out.Library)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if !slices.Equal(c.progress, []float64{46}) {
		t.Errorf("progress notifications = %v, want one report of 46", c.progress)
	}
}

func TestNeighboursIsCappedAndRanked(t *testing.T) {
	t.Parallel()
	c := grown(t)

	var out neighboursOut
	c.call("neighbours", map[string]any{"identifier": "W2741809807", "limit": 5}, &out)
	refs := out.References
	if refs == nil || refs.Total != 54 || refs.Fetched != 46 || refs.Omitted != 49 || len(refs.Works) != 5 {
		t.Fatalf("references = %+v; want 5 of 54 (46 fetched), 49 omitted", refs)
	}
	for i, w := range refs.Works {
		if len(w.Authors) == 0 {
			t.Errorf("reference %s has no authors", w.ID)
		}
		if w.Stub || w.ID == "" || w.OAStatus == "" {
			t.Errorf("reference %d = %+v; want a fetched work with ID and OA status", i, w)
		}
		if i > 0 && w.CitedByCount > refs.Works[i-1].CitedByCount {
			t.Errorf("references not ranked by citations: %d after %d", w.CitedByCount, refs.Works[i-1].CitedByCount)
		}
	}
	// The Sci-Hub paper cites the seed back: a real cycle.
	if out.CitedBy == nil || out.CitedBy.Total != 1 || out.CitedBy.Works[0].ID != "W2785823074" {
		t.Errorf("cited_by = %+v; want the Sci-Hub paper", out.CitedBy)
	}

	// A stub is labelled as one, and nothing is claimed about it (§6, rule 3).
	var stub neighboursOut
	c.call("neighbours", map[string]any{"identifier": "W1503178185", "direction": "cites"}, &stub)
	if !stub.Paper.Stub || stub.Paper.Title != "" || stub.Paper.OAStatus != "unknown" ||
		stub.CitedBy != nil || !strings.Contains(stub.Note, "not been fetched") {
		t.Errorf("stub neighbours = %+v", stub)
	}
}

func TestNeighboursErrors(t *testing.T) {
	t.Parallel()
	c := grown(t)
	if msg := c.fails("neighbours", map[string]any{"identifier": "W9999999999"}); !strings.Contains(msg, "add_paper") {
		t.Errorf("not-found message = %q; want it to point at add_paper", msg)
	}
	if msg := c.fails("neighbours", map[string]any{"identifier": "W2741809807", "direction": "up"}); !strings.Contains(msg, "cited_by") {
		t.Errorf("bad direction message = %q", msg)
	}
}

func TestFindPath(t *testing.T) {
	t.Parallel()
	c := grown(t)

	var out pathOut
	c.call("find_path", map[string]any{"from": "10.7717/peerj.4375", "to": "W1503178185"}, &out)
	var got []string
	for _, s := range out.Chain {
		got = append(got, s.ID+":"+s.Next)
	}
	want := []string{"W2741809807:cites", "W1560783210:cites", "W1503178185:"}
	if !out.Found || out.Steps != 2 || !slices.Equal(got, want) {
		t.Errorf("find_path = found %v, %d steps, %v; want %v", out.Found, out.Steps, got, want)
	}

	// Not finding one is an answer, not an error — and it says what to try.
	var none pathOut
	c.call("find_path", map[string]any{"from": "W2611818942", "to": "W1503178185"}, &none)
	if none.Found || none.From == nil || none.To == nil || !strings.Contains(none.Note, "any_direction") {
		t.Errorf("find_path with no lineage = %+v", none)
	}
	var mixed pathOut
	c.call("find_path", map[string]any{"from": "W2611818942", "to": "W1503178185", "any_direction": true}, &mixed)
	if !mixed.Found || mixed.Steps != 3 || mixed.Chain[0].Next != "cited_by" {
		t.Errorf("find_path any_direction = %+v", mixed)
	}
}

func TestLibraryStats(t *testing.T) {
	t.Parallel()
	c := grown(t)

	var out statsOut
	c.call("library_stats", map[string]any{"include_duplicates": true}, &out)
	if out.Works != 1041 || out.Fetched != 47 || out.Seeds != 1 || out.Citations != 1655 {
		t.Errorf("library_stats = %+v", out.stats)
	}
	if out.ReferenceCoverage <= 0 || out.ReferenceCoverage > 1 {
		t.Errorf("reference_coverage = %v, want a share between 0 and 1", out.ReferenceCoverage)
	}
	if !strings.Contains(out.Note, "never merged") {
		t.Errorf("note = %q; duplicates must be described as unmerged", out.Note)
	}
}
