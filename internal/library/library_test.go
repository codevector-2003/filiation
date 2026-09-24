package library

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/codevector-2003/filiation/internal/config"
	"github.com/codevector-2003/filiation/internal/errs"
	"github.com/codevector-2003/filiation/internal/model"
)

// These are end-to-end tests of M0: input string in, rows out, with every
// package real except the network. OpenAlex answers from the responses
// recorded for sources/openalex on 24 Sept 2026.

const fixtureDir = "../sources/openalex/testdata"

// replay serves recorded responses keyed on path plus filter. An unexpected
// request fails the test, so each test also pins down which requests M0 makes.
type replay struct {
	t      *testing.T
	mu     sync.Mutex
	routes map[string]string // key -> fixture file, "404", or a literal JSON body
	calls  []string
}

func (r *replay) RoundTrip(req *http.Request) (*http.Response, error) {
	key := req.URL.Path
	if f := req.URL.Query().Get("filter"); f != "" {
		key += "?filter=" + f
	}
	r.mu.Lock()
	r.calls = append(r.calls, key)
	file, ok := r.routes[key]
	r.mu.Unlock()

	status, body := 200, ""
	switch {
	case !ok:
		r.t.Errorf("unexpected request %s", key)
		status = 400
	case file == "404":
		status = 404
	case strings.HasPrefix(file, "{"):
		body = file
	default:
		raw, err := os.ReadFile(filepath.Join(fixtureDir, file))
		if err != nil {
			r.t.Fatalf("read fixture %s: %v", file, err)
		}
		body = string(raw)
	}
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}

func (r *replay) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

// openTest opens a library in a temporary directory, with its own cache, that
// answers from the recorded responses.
func openTest(t *testing.T, routes map[string]string, opts Options) (*Library, *replay) {
	t.Helper()
	r := &replay{t: t, routes: routes}
	dir := t.TempDir()
	if opts.CacheDir == "" {
		opts.CacheDir = filepath.Join(dir, "cache")
	}
	opts.Transport = r
	cfg := &config.Config{
		DBPath:         filepath.Join(dir, "My Library", "library.db"),
		MaxNodes:       config.DefaultMaxNodes,
		MaxDepth:       config.DefaultMaxDepth,
		MaxRefsPerWork: config.DefaultMaxRefsPerWork,
	}
	l, err := Open(t.Context(), cfg, opts)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	return l, r
}

// peerj is M0's acceptance seed: "The state of OA", 54 references.
var peerj = map[string]string{"/works/doi:10.7717/peerj.4375": "work_W2741809807.json"}

func TestAddByDOI(t *testing.T) {
	t.Parallel()
	l, _ := openTest(t, peerj, Options{})

	got, err := l.Add(t.Context(), "https://doi.org/10.7717/peerj.4375", AddOptions{})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if got.Work.OpenAlexID != "W2741809807" || !strings.HasPrefix(got.Work.DisplayTitle(), "The state of OA") {
		t.Errorf("added %s %q", got.Work.OpenAlexID, got.Work.DisplayTitle())
	}
	if !got.Work.IsSeed || model.Deref(got.Work.Depth, -1) != 0 {
		t.Errorf("seed %v, depth %v", got.Work.IsSeed, got.Work.Depth)
	}
	if got.AlreadySeed || got.DeadEnd {
		t.Errorf("AlreadySeed %v, DeadEnd %v; want both false", got.AlreadySeed, got.DeadEnd)
	}
	if got.Refs != 54 || got.NewStubs != 54 || got.NewEdges != 54 {
		t.Errorf("refs %d, stubs %d, edges %d; want 54 of each", got.Refs, got.NewStubs, got.NewEdges)
	}

	s, err := l.Stats(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if s != (Stats{Works: 55, Stubs: 54, Edges: 54}) {
		t.Errorf("Stats = %+v, want 55 works of which 54 stubs, 54 edges", s)
	}
}

func TestAddTwiceAddsNothing(t *testing.T) {
	t.Parallel()
	// M0's definition of done, all three clauses: writes a row and has a title,
	// running it twice adds nothing, and the references are stubs with edges.
	l, r := openTest(t, peerj, Options{})

	if _, err := l.Add(t.Context(), "10.7717/peerj.4375", AddOptions{}); err != nil {
		t.Fatal(err)
	}
	again, err := l.Add(t.Context(), "10.7717/peerj.4375", AddOptions{})
	if err != nil {
		t.Fatalf("second Add: %v", err)
	}
	if !again.AlreadySeed || again.NewStubs != 0 || again.NewEdges != 0 {
		t.Errorf("second Add = %+v, want already a seed with nothing new", again)
	}
	if s, _ := l.Stats(t.Context()); s != (Stats{Works: 55, Stubs: 54, Edges: 54}) {
		t.Errorf("Stats = %+v after two adds", s)
	}
	// And the second one came from the cache.
	if r.count() != 1 {
		t.Errorf("requests = %d, want 1 — the repeat should be served from the cache", r.count())
	}
}

func TestAddServedFromCacheAcrossSessions(t *testing.T) {
	t.Parallel()
	cache := filepath.Join(t.TempDir(), "shared cache")
	first, _ := openTest(t, peerj, Options{CacheDir: cache})
	if _, err := first.Add(t.Context(), "10.7717/peerj.4375", AddOptions{}); err != nil {
		t.Fatal(err)
	}

	// A new library, same cache, and a network that answers nothing.
	second, r := openTest(t, map[string]string{}, Options{CacheDir: cache})
	if _, err := second.Add(t.Context(), "10.7717/peerj.4375", AddOptions{}); err != nil {
		t.Fatalf("Add from cache: %v", err)
	}
	if r.count() != 0 {
		t.Errorf("requests = %d, want 0", r.count())
	}
	if _, ok := second.Quota(); ok {
		t.Errorf("Quota reported although nothing reached OpenAlex")
	}
}

func TestAddWithoutCache(t *testing.T) {
	t.Parallel()
	cache := filepath.Join(t.TempDir(), "never created")
	l, r := openTest(t, peerj, Options{CacheDir: cache, NoCache: true})
	for range 2 {
		if _, err := l.Add(t.Context(), "10.7717/peerj.4375", AddOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	if r.count() != 2 {
		t.Errorf("requests = %d, want 2 with the cache off", r.count())
	}
	if _, err := os.Stat(cache); !os.IsNotExist(err) {
		t.Errorf("cache directory created with --no-cache")
	}
}

var attention = map[string]string{
	"/works?filter=title.search:Attention is all you need": "search_attention.json",
}

func TestAddTitleAsksBeforeWriting(t *testing.T) {
	t.Parallel()
	l, _ := openTest(t, attention, Options{})

	_, err := l.Add(t.Context(), "Attention is all you need", AddOptions{})
	if !errors.Is(err, errs.ErrAmbiguous) {
		t.Fatalf("error = %v, want ErrAmbiguous", err)
	}
	var amb *AmbiguousError
	if !errors.As(err, &amb) {
		t.Fatalf("error is not an *AmbiguousError: %T", err)
	}
	if len(amb.Candidates) != 10 {
		t.Errorf("candidates = %d, want 10", len(amb.Candidates))
	}
	first := amb.Candidates[0]
	if !first.TitleMatch || first.Work.DisplayTitle() != "Attention Is All You Need" {
		t.Errorf("first candidate = %q (match %v)", first.Work.DisplayTitle(), first.TitleMatch)
	}
	// ADR-005: a title never seeds a graph until someone chooses.
	if s, _ := l.Stats(t.Context()); s != (Stats{}) {
		t.Errorf("Stats = %+v, want nothing written before a choice", s)
	}
}

func TestAddChosenCandidateMakesNoRequest(t *testing.T) {
	t.Parallel()
	l, r := openTest(t, attention, Options{})

	_, err := l.Add(t.Context(), "Attention is all you need", AddOptions{})
	var amb *AmbiguousError
	if !errors.As(err, &amb) {
		t.Fatalf("error = %v", err)
	}
	before := r.count()

	got, err := l.AddCandidate(t.Context(), amb.Candidates[0])
	if err != nil {
		t.Fatalf("AddCandidate: %v", err)
	}
	if got.Work.OpenAlexID != "W2626778328" || got.Refs == 0 {
		t.Errorf("added %s with %d refs", got.Work.OpenAlexID, got.Refs)
	}
	if r.count() != before {
		t.Errorf("AddCandidate made %d requests, want 0", r.count()-before)
	}
}

func TestAddCandidateRejectsForgedCandidate(t *testing.T) {
	t.Parallel()
	l, _ := openTest(t, nil, Options{})
	if _, err := l.AddCandidate(t.Context(), Candidate{Work: model.Work{OpenAlexID: "W1"}}); err == nil {
		t.Error("AddCandidate accepted a candidate that did not come from Add")
	}
}

func TestAddTitleAcceptFirst(t *testing.T) {
	t.Parallel()
	l, _ := openTest(t, attention, Options{})
	got, err := l.Add(t.Context(), "Attention is all you need", AddOptions{AcceptFirst: true})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if got.Work.OpenAlexID != "W2626778328" {
		t.Errorf("accepted %s %q, want the exact title match", got.Work.OpenAlexID, got.Work.DisplayTitle())
	}
}

func TestAddArXiv(t *testing.T) {
	t.Parallel()
	l, _ := openTest(t, map[string]string{
		"/works/doi:10.48550/arxiv.1706.03762": "404",
		"/works?filter=locations.landing_page_url:http://arxiv.org/abs/1706.03762|https://arxiv.org/abs/1706.03762": "arxiv_landing_1706.03762.json",
	}, Options{})

	got, err := l.Add(t.Context(), "arXiv:1706.03762v5", AddOptions{})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if got.Work.OpenAlexID != "W2626778328" || model.Deref(got.Work.ArXivID, "") != "1706.03762" {
		t.Errorf("added %s, arXiv %v", got.Work.OpenAlexID, got.Work.ArXivID)
	}
}

func TestAddProceedingsVolumeIsADeadEnd(t *testing.T) {
	t.Parallel()
	// The original M0 acceptance DOI. It adds cleanly and is reported as a dead
	// end, which is what the CLI must tell the user instead of looking broken.
	l, _ := openTest(t, map[string]string{"/works/doi:10.1145/3292500": "work_doi_proceedings.json"}, Options{})
	got, err := l.Add(t.Context(), "10.1145/3292500", AddOptions{})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if !got.DeadEnd || got.Refs != 0 {
		t.Errorf("DeadEnd %v, Refs %d; want a dead end", got.DeadEnd, got.Refs)
	}
}

func TestAddErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		input  string
		routes map[string]string
		want   error
	}{
		{"not an identifier", "2017", nil, errs.ErrInvalidInput},
		{"not in OpenAlex", "W99999999999", map[string]string{"/works/W99999999999": "404"}, errs.ErrUnresolved},
		{"no title candidates", "zzqx", map[string]string{
			"/works?filter=title.search:zzqx": `{"meta":{"count":0},"results":[]}`,
		}, errs.ErrUnresolved},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			l, _ := openTest(t, tt.routes, Options{})
			_, err := l.Add(t.Context(), tt.input, AddOptions{})
			if !errors.Is(err, tt.want) {
				t.Errorf("Add(%q) = %v, want %v", tt.input, err, tt.want)
			}
			if s, _ := l.Stats(t.Context()); s != (Stats{}) {
				t.Errorf("Stats = %+v after a failed add, want nothing written", s)
			}
		})
	}
}

func TestClearCache(t *testing.T) {
	t.Parallel()
	cache := filepath.Join(t.TempDir(), "cache")
	l, _ := openTest(t, peerj, Options{CacheDir: cache})
	if _, err := l.Add(t.Context(), "10.7717/peerj.4375", AddOptions{}); err != nil {
		t.Fatal(err)
	}

	dir, err := ClearCache(cache)
	if err != nil || dir != cache {
		t.Fatalf("ClearCache = %q, %v", dir, err)
	}
	entries, _ := os.ReadDir(cache)
	if len(entries) != 0 {
		t.Errorf("cache still holds %d entries", len(entries))
	}
}

// peerjExpansion routes the recorded responses for one expansion hop from the
// M0 seed: its 54 references in one batch, of which OpenAlex's filter returned
// 44. The other ten were each confirmed by a single lookup — eight are 404s
// (dangling references) and two exist under the same ID but are not matched by
// the filter. Recorded 24 Sept 2026.
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
	// Every reference has in-degree 1 at depth 1, so the frontier orders them
	// by ID — the order the batch filter was recorded in.
	sort.Strings(ids)

	routes := map[string]string{
		"/works/doi:10.7717/peerj.4375":                       "work_W2741809807.json",
		"/works?filter=openalex_id:" + strings.Join(ids, "|"): "batch_peerj_refs.json",
		"/works/W6887727194":                                  "single_W6887727194.json",
		"/works/W6948399261":                                  "single_W6948399261.json",
	}
	for _, id := range []string{"W2611818942", "W6637734586", "W6640061894", "W6640335369",
		"W6640848857", "W6687877900", "W6725637556", "W6744027076"} {
		routes["/works/"+id] = "404"
	}
	return routes
}

func TestExpandOneHopFromTheM0Seed(t *testing.T) {
	t.Parallel()
	l, r := openTest(t, peerjExpansion(t), Options{})
	if _, err := l.Add(t.Context(), "10.7717/peerj.4375", AddOptions{}); err != nil {
		t.Fatal(err)
	}

	var progress []int
	res, err := l.Expand(t.Context(), ExpandOptions{MaxNodes: 54,
		Progress: func(r model.ExpansionResult) { progress = append(progress, r.Hydrated) }})
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}

	// Expected values computed from the fixtures independently of this code.
	if res.Hydrated != 46 || res.Unresolved != 8 || res.DeadEnds != 8 || res.Edges != 1601 {
		t.Errorf("result = %+v; want 46 hydrated, 8 unresolved, 8 dead ends, 1601 edges", res)
	}
	if res.StoppedBecause != model.StopBudgetExhausted || res.MaxDepthReached != 1 {
		t.Errorf("stopped %q at depth %d; want budget-exhausted at depth 1", res.StoppedBecause, res.MaxDepthReached)
	}
	s, _ := l.Stats(t.Context())
	if s != (Stats{Works: 1041, Stubs: 994, Edges: 1655}) {
		t.Errorf("Stats = %+v, want 1041 works (994 stubs), 1655 edges", s)
	}
	if len(progress) != 1 || progress[0] != 46 {
		t.Errorf("progress = %v, want one report of 46", progress)
	}
	// The seed, the batch, and ten confirming lookups.
	if r.count() != 12 {
		t.Errorf("requests = %d, want 12", r.count())
	}
}

func TestExpandUsesConfiguredBudget(t *testing.T) {
	t.Parallel()
	l, _ := openTest(t, peerj, Options{})
	nodes, depth := l.Budget()
	if nodes != config.DefaultMaxNodes || depth != config.DefaultMaxDepth {
		t.Errorf("Budget() = %d, %d; want the configured defaults", nodes, depth)
	}
	// An empty library has nothing to expand from, and says so by stopping
	// on an empty frontier rather than failing.
	res, err := l.Expand(t.Context(), ExpandOptions{})
	if err != nil || res.StoppedBecause != model.StopFrontierEmpty || res.Hydrated != 0 {
		t.Errorf("Expand on an empty library = %+v, %v", res, err)
	}
}
