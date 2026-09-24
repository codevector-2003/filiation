package openalex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/codevector-2003/filiation/internal/errs"
	"github.com/codevector-2003/filiation/internal/httpx"
	"github.com/codevector-2003/filiation/internal/identity"
	"github.com/codevector-2003/filiation/internal/model"
)

const (
	// BaseURL is the OpenAlex API.
	BaseURL = "https://api.openalex.org"

	// MaxBatch is the most IDs one filter may carry. Measured exactly: 100
	// returns all of them, 101 is a hard 400 (spike 1). A hard chunk size, not
	// a hopeful one.
	MaxBatch = 100

	// RatePerSecond is the token bucket for OpenAlex: half the ~11 req/s at
	// which the first 429 appeared (spike 3).
	RatePerSecond = 5

	// DefaultSearchLimit is how many title-search candidates are asked for. A
	// title search costs 10 credits whatever the page size, so this is about
	// what a person can choose between, not about cost.
	DefaultSearchLimit = 10

	maxSearchLimit = 25
)

// HTTPOptions is how an httpx.Client for OpenAlex must be configured. The rate
// and the name of the contact parameter are facts about OpenAlex, so they live
// here rather than with whoever builds the client.
func HTTPOptions(contact string, cache httpx.Cache) httpx.Options {
	return httpx.Options{
		RatePerSecond: RatePerSecond,
		Contact:       contact,
		ContactParam:  "mailto",
		Cache:         cache,
	}
}

// Client reads works from OpenAlex.
type Client struct {
	http *httpx.Client
}

// New wraps an httpx.Client built from HTTPOptions. Build one, share it: the
// rate limit is only a limit if every OpenAlex request waits on the same bucket.
func New(h *httpx.Client) *Client {
	return &Client{http: h}
}

// Resolve fetches the one work a deterministic identifier names.
//
// OpenAlex IDs, DOIs and PMIDs are single-work lookups, which are free. arXiv
// IDs are harder than they look, because OpenAlex does not index arXiv IDs as
// such: arXiv mints a DataCite DOI for every paper, but OpenAlex does not
// always hold the work under it — 1706.03762, "Attention Is All You Need", is a
// 404 by its arXiv DOI (recorded 24 Sept 2026). So the DOI is tried first, free,
// and on a 404 the work is found by its arXiv landing page instead, for one
// credit.
//
// A title is refused: it names candidates, not a work, and SearchByTitle is
// the call for it (ADR-005). OpenAlex having no record wraps errs.ErrUnresolved.
func (c *Client) Resolve(ctx context.Context, id identity.ID) (model.HydratedWork, error) {
	switch id.Kind {
	case identity.KindOpenAlex:
		return c.getOne(ctx, id.Value, id)
	case identity.KindDOI:
		return c.getOne(ctx, "doi:"+escapeDOI(id.Value), id)
	case identity.KindPMID:
		return c.getOne(ctx, "pmid:"+id.Value, id)
	case identity.KindArXiv:
		return c.resolveArXiv(ctx, id)
	case identity.KindTitle:
		return model.HydratedWork{}, fmt.Errorf(
			"openalex: %q is a title, which names candidates rather than a work — use SearchByTitle: %w",
			id.Value, errs.ErrInvalidInput)
	default:
		return model.HydratedWork{}, fmt.Errorf("openalex: cannot resolve %q: %w", id.Raw, errs.ErrInvalidInput)
	}
}

func (c *Client) resolveArXiv(ctx context.Context, id identity.ID) (model.HydratedWork, error) {
	h, err := c.getOne(ctx, "doi:"+escapeDOI(id.ArXivDOI()), id)
	if !errors.Is(err, errs.ErrUnresolved) {
		return withArXiv(h, id), err
	}

	// OpenAlex records arXiv landing pages with either scheme; ask for both.
	filter := "locations.landing_page_url:" +
		"http://arxiv.org/abs/" + id.Value + "|https://arxiv.org/abs/" + id.Value
	works, err := c.list(ctx, filter, 5)
	if err != nil {
		return model.HydratedWork{}, fmt.Errorf("openalex: resolve arXiv %s: %w", id.Value, err)
	}
	if len(works) == 0 {
		return model.HydratedWork{}, fmt.Errorf("openalex: no work for arXiv %s: %w", id.Value, errs.ErrUnresolved)
	}
	// One landing page, one work — OpenAlex merges versions of a preprint. If
	// it ever returns more, the first is its best match and the rest are noise
	// this lookup cannot adjudicate.
	return withArXiv(works[0], id), nil
}

// withArXiv records the arXiv ID the work was found by, which OpenAlex itself
// may not report when the work's DOI is not arXiv's.
func withArXiv(h model.HydratedWork, id identity.ID) model.HydratedWork {
	if h.OpenAlexID != "" && h.ArXivID == nil {
		h.ArXivID = model.Ptr(id.Value)
	}
	return h
}

// getOne fetches /works/{key}.
func (c *Client) getOne(ctx context.Context, key string, id identity.ID) (model.HydratedWork, error) {
	body, err := c.http.Get(ctx, BaseURL+"/works/"+key+"?select="+selectFields)
	if httpx.StatusCode(err) == http.StatusNotFound {
		return model.HydratedWork{}, fmt.Errorf("openalex: no work for %s %s: %w",
			id.Kind, id.Value, errs.ErrUnresolved)
	}
	if err != nil {
		return model.HydratedWork{}, fmt.Errorf("openalex: get %s %s: %w", id.Kind, id.Value, err)
	}

	var w work
	if err := json.Unmarshal(body, &w); err != nil {
		return model.HydratedWork{}, fmt.Errorf("openalex: decode %s %s: %w", id.Kind, id.Value, err)
	}
	return w.hydrated()
}

// Batch is what GetWorksBatch found.
type Batch struct {
	// Works holds every work found, in the order first requested, each once.
	Works []model.HydratedWork

	// Aliases maps a requested ID to the different ID OpenAlex returned for it.
	// OpenAlex merges duplicate records and redirects the retired ID to the
	// surviving one; the caller must fold the old node into the new, or the
	// same paper sits in the graph twice (hard rule 6).
	Aliases map[string]string

	// Missing lists requested IDs OpenAlex has no record of, each confirmed by
	// a single lookup rather than inferred from its absence in a list. The
	// caller marks them unresolved and stops spending budget on them.
	Missing []string
}

// GetWorksBatch hydrates works by OpenAlex ID, MaxBatch to a request.
//
// A batch filter silently omits any ID it cannot match, and that absence is
// ambiguous: the work may not exist, or it may have been merged into another
// record, which a filter does not follow but a single lookup does. Each omitted
// ID is therefore looked up on its own — free, and in practice a handful per
// batch — and lands in Aliases or Missing according to the answer.
//
// If a request fails, the works already fetched are returned with the error,
// which wraps errs.ErrTransient when retrying later may help. The expander
// commits what it has and carries on (§7); the rest stay stubs on the frontier.
func (c *Client) GetWorksBatch(ctx context.Context, ids []string) (Batch, error) {
	b := Batch{Aliases: map[string]string{}}

	requested := make([]string, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, raw := range ids {
		id, err := identity.NormaliseOpenAlexID(raw)
		if err != nil {
			return b, fmt.Errorf("openalex: batch: %w", err)
		}
		if !seen[id] {
			seen[id] = true
			requested = append(requested, id)
		}
	}

	found := make(map[string]bool, len(requested))
	for start := 0; start < len(requested); start += MaxBatch {
		chunk := requested[start:min(start+MaxBatch, len(requested))]
		works, err := c.list(ctx, "openalex_id:"+strings.Join(chunk, "|"), len(chunk))
		if err != nil {
			return b, fmt.Errorf("openalex: batch of %d: %w", len(chunk), err)
		}
		for _, w := range works {
			if !found[w.OpenAlexID] {
				found[w.OpenAlexID] = true
				b.Works = append(b.Works, w)
			}
		}
	}

	for _, id := range requested {
		if found[id] {
			continue
		}
		h, err := c.getOne(ctx, id, identity.ID{Kind: identity.KindOpenAlex, Value: id, Raw: id})
		switch {
		case errors.Is(err, errs.ErrUnresolved):
			b.Missing = append(b.Missing, id)
		case err != nil:
			return b, fmt.Errorf("openalex: confirm %s: %w", id, err)
		default:
			if h.OpenAlexID != id {
				b.Aliases[id] = h.OpenAlexID
			}
			if !found[h.OpenAlexID] {
				found[h.OpenAlexID] = true
				b.Works = append(b.Works, h)
			}
		}
	}
	return b, nil
}

// Candidate is one title-search result.
type Candidate struct {
	Work model.HydratedWork

	// TitleMatch reports that the candidate's title is, allowing for case,
	// punctuation and a typo, the one searched for. It orders and flags the
	// list. It is never grounds to accept a candidate: two papers can share a
	// title, and ADR-005 requires the user to choose.
	TitleMatch bool
}

// SearchByTitle returns works whose titles match title, those that match
// closely first and otherwise in OpenAlex's relevance order. No results is an
// empty slice, not an error; the caller decides what that means.
//
// A search costs 10 credits — ten times a list request — which is fine for a
// seed a person typed and would not be for anything in a loop.
func (c *Client) SearchByTitle(ctx context.Context, title string, limit int) ([]Candidate, error) {
	// Commas separate filters and pipes mean OR, so neither may reach the
	// filter value. Neither carries meaning in a title search.
	q := strings.Join(strings.Fields(strings.NewReplacer(",", " ", "|", " ").Replace(title)), " ")
	if q == "" {
		return nil, fmt.Errorf("openalex: search: empty title: %w", errs.ErrInvalidInput)
	}
	if limit <= 0 {
		limit = DefaultSearchLimit
	}
	limit = min(limit, maxSearchLimit)

	works, err := c.list(ctx, "title.search:"+q, limit)
	if err != nil {
		return nil, fmt.Errorf("openalex: search %q: %w", q, err)
	}

	out := make([]Candidate, 0, len(works))
	for _, w := range works {
		t := ""
		if w.Title != nil {
			t = *w.Title
		}
		out = append(out, Candidate{Work: w, TitleMatch: identity.TitlesMatch(title, t)})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].TitleMatch && !out[j].TitleMatch })
	return out, nil
}

// list runs one filtered list request and maps its results.
func (c *Client) list(ctx context.Context, filter string, perPage int) ([]model.HydratedWork, error) {
	q := url.Values{}
	q.Set("filter", filter)
	q.Set("per_page", fmt.Sprint(perPage))
	q.Set("select", selectFields)

	body, err := c.http.Get(ctx, BaseURL+"/works?"+q.Encode())
	if err != nil {
		return nil, err
	}
	var l list
	if err := json.Unmarshal(body, &l); err != nil {
		return nil, fmt.Errorf("decode list: %w", err)
	}

	works := make([]model.HydratedWork, 0, len(l.Results))
	for i := range l.Results {
		h, err := l.Results[i].hydrated()
		if err != nil {
			return nil, err
		}
		works = append(works, h)
	}
	return works, nil
}

// escapeDOI makes a DOI safe as a path segment while leaving its slashes
// alone. DOI suffixes carry characters a path cannot — "<", ">", "#", "?", ";"
// — and OpenAlex expects /works/doi:10.1145/... with the slash literal.
func escapeDOI(doi string) string {
	return strings.ReplaceAll(url.PathEscape(doi), "%2F", "/")
}
