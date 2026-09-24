package openalex

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/codevector-2003/filiation/internal/errs"
	"github.com/codevector-2003/filiation/internal/httpx"
	"github.com/codevector-2003/filiation/internal/identity"
	"github.com/codevector-2003/filiation/internal/model"
)

// The fixtures in testdata/ are real OpenAlex responses, recorded 24 Sept 2026
// with the same select this package sends. Where a test needs a response
// OpenAlex would not reliably produce on demand — a merged record — it says so
// and builds one from a recorded work, rather than inventing a shape.

// route answers one request: the fixture to serve, and the status.
type route struct {
	status  int
	fixture string // file in testdata/, or "" for an empty body
	body    string // literal body, when no fixture fits
}

// fixtures is a stub transport keyed on what identifies an OpenAlex request:
// the path, plus the filter for list requests. Anything unexpected fails the
// test, so a test also pins down exactly which requests the code makes.
type fixtures struct {
	t      *testing.T
	mu     sync.Mutex
	routes map[string]route
	calls  []string
}

func (f *fixtures) RoundTrip(req *http.Request) (*http.Response, error) {
	key := req.URL.Path
	if filter := req.URL.Query().Get("filter"); filter != "" {
		key += "?filter=" + filter
	}
	if got := req.URL.Query().Get("select"); got != selectFields {
		f.t.Errorf("request %s: select = %q, want selectFields", key, got)
	}

	f.mu.Lock()
	f.calls = append(f.calls, key)
	r, ok := f.routes[key]
	f.mu.Unlock()
	if !ok {
		f.t.Errorf("unexpected request %s", key)
		return &http.Response{StatusCode: 599, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	}

	body := r.body
	if r.fixture != "" {
		raw, err := os.ReadFile(filepath.Join("testdata", r.fixture))
		if err != nil {
			f.t.Fatalf("read fixture: %v", err)
		}
		body = string(raw)
	}
	return &http.Response{
		StatusCode: r.status,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}

func newTest(t *testing.T, routes map[string]route) (*Client, *fixtures) {
	t.Helper()
	f := &fixtures{t: t, routes: routes}
	opts := HTTPOptions("", nil)
	opts.Transport = f
	opts.RatePerSecond = 1000 // the real bucket is httpx's to test
	opts.MaxRetries = -1
	h, err := httpx.New(opts)
	if err != nil {
		t.Fatalf("httpx.New: %v", err)
	}
	return New(h), f
}

func parse(t *testing.T, s string) identity.ID {
	t.Helper()
	id, err := identity.Parse(s)
	if err != nil {
		t.Fatalf("identity.Parse(%q): %v", s, err)
	}
	return id
}

func TestResolveByOpenAlexID(t *testing.T) {
	t.Parallel()
	c, _ := newTest(t, map[string]route{
		"/works/W2741809807": {status: 200, fixture: "work_W2741809807.json"},
	})

	h, err := c.Resolve(t.Context(), parse(t, "https://openalex.org/W2741809807"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	checks := []struct {
		field     string
		got, want any
	}{
		{"OpenAlexID", h.OpenAlexID, "W2741809807"},
		{"DOI", model.Deref(h.DOI, ""), "10.7717/peerj.4375"},
		{"PMID", model.Deref(h.PMID, ""), "29456894"},
		{"Year", model.Deref(h.Year, 0), 2018},
		{"Type", h.Type, model.TypeArticle},
		{"OAStatus", h.OAStatus, model.OAGold},
		{"OAURL", model.Deref(h.OAURL, ""), "https://doi.org/10.7717/peerj.4375"},
		{"Venue", h.Venue, "PeerJ"},
		{"CitedByCount", h.CitedByCount, 1259},
		{"Hydrated", h.Hydrated, true},
		{"FetchedRefs", h.FetchedRefs, true},
		{"refs", len(h.ReferencedWorks), 54},
		{"authors", len(h.Authors), 9},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.field, c.got, c.want)
		}
	}

	if !strings.HasPrefix(model.Deref(h.Title, ""), "The state of OA") {
		t.Errorf("Title = %v", h.Title)
	}
	// Every reference must be in the bare form store keys on (hard rule 6).
	for _, r := range h.ReferencedWorks {
		if !strings.HasPrefix(r, "W") || strings.Contains(r, "/") {
			t.Fatalf("reference %q is not a bare OpenAlex ID", r)
		}
	}
	first := h.Authors[0]
	if first.Name != "Heather Piwowar" || first.OpenAlexID != "A5048491430" || first.Position != 0 {
		t.Errorf("first author = %+v", first)
	}
	if model.Deref(first.ORCID, "") != "0000-0003-1613-5981" {
		t.Errorf("ORCID = %v, want the bare form", first.ORCID)
	}
	// The abstract ships as an inverted index and must come back as text.
	if !strings.HasPrefix(h.Abstract, "Despite growing interest in Open Access") {
		t.Errorf("Abstract starts %q", h.Abstract[:min(60, len(h.Abstract))])
	}
	// Provenance is the caller's to set.
	if h.Depth != nil || h.IsSeed || h.Source != model.SourceUnknown {
		t.Errorf("provenance set by the source: depth %v, seed %v, source %q", h.Depth, h.IsSeed, h.Source)
	}
}

func TestResolveByDOIAndPMID(t *testing.T) {
	t.Parallel()
	c, f := newTest(t, map[string]route{
		"/works/doi:10.1145/3292500.3330701": {status: 200, fixture: "work_doi_kdd.json"},
		"/works/pmid:29051481":               {status: 200, fixture: "work_pmid.json"},
	})

	kdd, err := c.Resolve(t.Context(), parse(t, "https://doi.org/10.1145/3292500.3330701"))
	if err != nil {
		t.Fatalf("Resolve DOI: %v", err)
	}
	if kdd.OpenAlexID != "W2949676527" || model.Deref(kdd.Title, "") != "Optuna" {
		t.Errorf("DOI resolved to %s %v", kdd.OpenAlexID, kdd.Title)
	}
	// "conference-paper" has no model constant and is kept as it arrived.
	if kdd.Type != "conference-paper" {
		t.Errorf("Type = %q, want conference-paper stored as-is", kdd.Type)
	}
	// A closed work has no OA URL, and no source name here: both absent, not
	// empty strings pretending to be data.
	if kdd.OAURL != nil || kdd.OAStatus != model.OAClosed || kdd.Venue != "" {
		t.Errorf("OA = %v %q, venue %q", kdd.OAURL, kdd.OAStatus, kdd.Venue)
	}

	pm, err := c.Resolve(t.Context(), parse(t, "PMID:29051481"))
	if err != nil {
		t.Fatalf("Resolve PMID: %v", err)
	}
	if pm.OpenAlexID != "W2952060487" || model.Deref(pm.PMID, "") != "29051481" {
		t.Errorf("PMID resolved to %s, PMID %v", pm.OpenAlexID, pm.PMID)
	}
	if len(f.calls) != 2 {
		t.Errorf("calls = %v, want exactly two single lookups", f.calls)
	}
}

func TestResolveProceedingsVolumeIsADeadEnd(t *testing.T) {
	t.Parallel()
	// 10.1145/3292500 is the DOI in M0's definition of done. It is the KDD 2019
	// proceedings volume — type paratext, no references — so it resolves, but
	// it cannot seed a graph. Pinned here so that fact is not rediscovered.
	c, _ := newTest(t, map[string]route{
		"/works/doi:10.1145/3292500": {status: 200, fixture: "work_doi_proceedings.json"},
	})
	h, err := c.Resolve(t.Context(), parse(t, "10.1145/3292500"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if h.Type != "paratext" || !h.IsDeadEnd() {
		t.Errorf("Type = %q, IsDeadEnd = %v; want paratext and a dead end", h.Type, h.IsDeadEnd())
	}
	if len(h.Authors) != 0 || h.Authors == nil {
		t.Errorf("Authors = %v, want loaded and empty", h.Authors)
	}
}

func TestResolveNotInOpenAlex(t *testing.T) {
	t.Parallel()
	// OpenAlex answers a 404 with an HTML page, not JSON.
	c, _ := newTest(t, map[string]route{
		"/works/W99999999999": {status: 404, fixture: "work_404.json"},
	})
	_, err := c.Resolve(t.Context(), parse(t, "W99999999999"))
	if !errors.Is(err, errs.ErrUnresolved) {
		t.Fatalf("error = %v, want ErrUnresolved", err)
	}
	if errors.Is(err, errs.ErrTransient) {
		t.Errorf("a 404 is transient — expansion would retry a dead identifier forever")
	}
	if strings.Contains(err.Error(), "<html") {
		t.Errorf("error %q carries OpenAlex's HTML 404 page", err)
	}
}

func TestResolveArXivFallsBackToLandingPage(t *testing.T) {
	t.Parallel()
	// Recorded: OpenAlex 404s "Attention Is All You Need" by its arXiv DOI, and
	// finds it by landing page.
	landing := "locations.landing_page_url:http://arxiv.org/abs/1706.03762|https://arxiv.org/abs/1706.03762"
	c, f := newTest(t, map[string]route{
		"/works/doi:10.48550/arxiv.1706.03762": {status: 404, fixture: "work_arxiv_doi.json"},
		"/works?filter=" + landing:             {status: 200, fixture: "arxiv_landing_1706.03762.json"},
	})

	h, err := c.Resolve(t.Context(), parse(t, "https://arxiv.org/abs/1706.03762v5"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if h.OpenAlexID != "W2626778328" || model.Deref(h.Title, "") != "Attention Is All You Need" {
		t.Errorf("resolved to %s %v", h.OpenAlexID, h.Title)
	}
	if model.Deref(h.ArXivID, "") != "1706.03762" {
		t.Errorf("ArXivID = %v, want the ID it was found by", h.ArXivID)
	}
	if len(f.calls) != 2 {
		t.Errorf("calls = %v, want the free DOI lookup then the landing-page filter", f.calls)
	}
}

func TestResolveArXivByDOIWhenItExists(t *testing.T) {
	t.Parallel()
	// When the arXiv DOI does resolve, no credit is spent on the fallback.
	c, f := newTest(t, map[string]route{
		"/works/doi:10.48550/arxiv.1706.03762": {status: 200, fixture: "work_W2741809807.json"},
	})
	if _, err := c.Resolve(t.Context(), parse(t, "1706.03762")); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(f.calls) != 1 {
		t.Errorf("calls = %v, want one", f.calls)
	}
}

func TestResolveArXivNowhere(t *testing.T) {
	t.Parallel()
	landing := "locations.landing_page_url:http://arxiv.org/abs/2401.00001|https://arxiv.org/abs/2401.00001"
	c, _ := newTest(t, map[string]route{
		"/works/doi:10.48550/arxiv.2401.00001": {status: 404},
		"/works?filter=" + landing:             {status: 200, body: `{"meta":{"count":0},"results":[]}`},
	})
	_, err := c.Resolve(t.Context(), parse(t, "2401.00001"))
	if !errors.Is(err, errs.ErrUnresolved) {
		t.Errorf("error = %v, want ErrUnresolved", err)
	}
}

func TestResolveRefusesTitles(t *testing.T) {
	t.Parallel()
	c, f := newTest(t, nil)
	_, err := c.Resolve(t.Context(), parse(t, "Attention Is All You Need"))
	if !errors.Is(err, errs.ErrInvalidInput) {
		t.Errorf("error = %v, want ErrInvalidInput — a title names candidates, not a work", err)
	}
	if len(f.calls) != 0 {
		t.Errorf("a request was made for a title: %v", f.calls)
	}
}

func TestGetWorksBatchConfirmsWhatIsMissing(t *testing.T) {
	t.Parallel()
	// Recorded: three IDs asked for, one returned. The other two are each
	// confirmed by a free single lookup, and both are genuinely gone.
	c, f := newTest(t, map[string]route{
		"/works?filter=openalex_id:W2741809807|W2963403868|W99999999999": {status: 200, fixture: "batch_3.json"},
		"/works/W2963403868":  {status: 404, fixture: "work_404.json"},
		"/works/W99999999999": {status: 404, fixture: "work_404.json"},
	})

	b, err := c.GetWorksBatch(t.Context(), []string{
		"W2741809807", "https://openalex.org/W2963403868", "W99999999999", "W2741809807",
	})
	if err != nil {
		t.Fatalf("GetWorksBatch: %v", err)
	}
	if len(b.Works) != 1 || b.Works[0].OpenAlexID != "W2741809807" {
		t.Errorf("Works = %d, want only W2741809807", len(b.Works))
	}
	if strings.Join(b.Missing, ",") != "W2963403868,W99999999999" {
		t.Errorf("Missing = %v", b.Missing)
	}
	if len(b.Aliases) != 0 {
		t.Errorf("Aliases = %v, want none", b.Aliases)
	}
	if len(f.calls) != 3 {
		t.Errorf("calls = %v — the repeated ID was requested twice, or a lookup was skipped", f.calls)
	}
}

func TestGetWorksBatchFollowsMergedRecords(t *testing.T) {
	t.Parallel()
	// Built from a recorded work, not recorded as a merge: OpenAlex documents
	// that a retired ID redirects to the surviving record, and a merge cannot
	// be produced on demand. The filter omits the old ID; the single lookup,
	// which follows the redirect, returns the survivor under its own ID.
	c, _ := newTest(t, map[string]route{
		"/works?filter=openalex_id:W1111111111": {status: 200, body: `{"meta":{"count":0},"results":[]}`},
		"/works/W1111111111":                    {status: 200, fixture: "work_W2741809807.json"},
	})

	b, err := c.GetWorksBatch(t.Context(), []string{"W1111111111"})
	if err != nil {
		t.Fatalf("GetWorksBatch: %v", err)
	}
	if got := b.Aliases["W1111111111"]; got != "W2741809807" {
		t.Errorf("Aliases = %v, want W1111111111 -> W2741809807", b.Aliases)
	}
	if len(b.Works) != 1 || len(b.Missing) != 0 {
		t.Errorf("Works = %d, Missing = %v; want the survivor and nothing missing", len(b.Works), b.Missing)
	}
}

func TestGetWorksBatchChunksAtOneHundred(t *testing.T) {
	t.Parallel()
	ids := make([]string, 150)
	for i := range ids {
		ids[i] = fmt.Sprintf("W%d", 1000+i)
	}
	// Expect exactly two filters: the first 100 IDs and the last 50. Both
	// answer with nothing, so every ID goes on to a 404 single lookup — the
	// point here is only where the chunk boundary falls.
	routes := map[string]route{
		"/works?filter=openalex_id:" + strings.Join(ids[:100], "|"): {status: 200, body: `{"results":[]}`},
		"/works?filter=openalex_id:" + strings.Join(ids[100:], "|"): {status: 200, body: `{"results":[]}`},
	}
	for _, id := range ids {
		routes["/works/"+id] = route{status: 404}
	}
	c, f := newTest(t, routes)

	b, err := c.GetWorksBatch(t.Context(), ids)
	if err != nil {
		t.Fatalf("GetWorksBatch: %v", err)
	}
	if len(b.Missing) != 150 {
		t.Errorf("Missing = %d, want 150", len(b.Missing))
	}
	lists := 0
	for _, call := range f.calls {
		if strings.Contains(call, "filter=") {
			lists++
		}
	}
	if lists != 2 {
		t.Errorf("list requests = %d, want 2 — 101 IDs in one filter is a hard 400", lists)
	}
}

func TestGetWorksBatchReturnsPartialResultsOnFailure(t *testing.T) {
	t.Parallel()
	c, _ := newTest(t, map[string]route{
		"/works?filter=openalex_id:W2741809807": {status: 503},
	})
	_, err := c.GetWorksBatch(t.Context(), []string{"W2741809807"})
	if !errors.Is(err, errs.ErrTransient) {
		t.Errorf("error = %v, want ErrTransient so the expander skips the batch and carries on", err)
	}
}

func TestGetWorksBatchRejectsBadIDs(t *testing.T) {
	t.Parallel()
	c, f := newTest(t, nil)
	if _, err := c.GetWorksBatch(t.Context(), []string{"W1", "10.1145/3292500"}); !errors.Is(err, errs.ErrInvalidInput) {
		t.Errorf("error = %v, want ErrInvalidInput", err)
	}
	if len(f.calls) != 0 {
		t.Errorf("requests made before the input was checked: %v", f.calls)
	}
}

func TestSearchByTitle(t *testing.T) {
	t.Parallel()
	c, _ := newTest(t, map[string]route{
		"/works?filter=title.search:Attention is all you need": {status: 200, fixture: "search_attention.json"},
	})

	got, err := c.SearchByTitle(t.Context(), "Attention is all you need", 0)
	if err != nil {
		t.Fatalf("SearchByTitle: %v", err)
	}
	if len(got) != 10 {
		t.Fatalf("candidates = %d, want 10", len(got))
	}
	// The exact title is flagged and first. "Attention Is All You Need In
	// Speech Separation" contains it and is not flagged — that is the case a
	// substring match gets wrong.
	if !got[0].TitleMatch || model.Deref(got[0].Work.Title, "") != "Attention Is All You Need" {
		t.Errorf("first candidate = %v (match %v)", got[0].Work.Title, got[0].TitleMatch)
	}
	matches := 0
	for _, cand := range got {
		if cand.TitleMatch {
			matches++
		}
	}
	if matches != 1 {
		t.Errorf("%d candidates flagged as matching, want 1", matches)
	}
	// Candidates arrive hydrated, so the chosen one needs no second fetch.
	if !got[0].Work.Hydrated || !got[0].Work.FetchedRefs {
		t.Errorf("candidate not hydrated")
	}
}

func TestSearchByTitleCleansTheQuery(t *testing.T) {
	t.Parallel()
	c, _ := newTest(t, map[string]route{
		"/works?filter=title.search:Cats dogs and more": {status: 200, body: `{"results":[]}`},
	})
	got, err := c.SearchByTitle(t.Context(), "  Cats,  dogs | and\tmore ", 5)
	if err != nil {
		t.Fatalf("SearchByTitle: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("candidates = %v, want an empty non-nil slice", got)
	}

	if _, err := c.SearchByTitle(t.Context(), " , | ", 5); !errors.Is(err, errs.ErrInvalidInput) {
		t.Errorf("empty search error = %v, want ErrInvalidInput", err)
	}
}

func TestEscapeDOI(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"10.1145/3292500.3330701":       "10.1145/3292500.3330701",
		"10.1016/s0140-6736(97)11096-0": "10.1016/s0140-6736%2897%2911096-0",
		"10.1002/(sici)1097-4571;2-o":   "10.1002/%28sici%291097-4571%3B2-o",
		"10.1000/a<b>#c?d":              "10.1000/a%3Cb%3E%23c%3Fd",
		"10.48550/arxiv.hep-th/9901001": "10.48550/arxiv.hep-th/9901001",
	}
	for in, want := range tests {
		if got := escapeDOI(in); got != want {
			t.Errorf("escapeDOI(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAbstractReconstruction(t *testing.T) {
	t.Parallel()
	got := abstract(map[string][]int{"the": {0, 3}, "cat": {1}, "sat": {2}, "mat": {4}})
	if got != "the cat sat the mat" {
		t.Errorf("abstract = %q", got)
	}
	if abstract(nil) != "" {
		t.Errorf("abstract(nil) should be empty")
	}
}
