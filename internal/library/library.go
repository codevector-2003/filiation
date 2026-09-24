package library

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/codevector-2003/filiation/internal/config"
	"github.com/codevector-2003/filiation/internal/errs"
	"github.com/codevector-2003/filiation/internal/graph"
	"github.com/codevector-2003/filiation/internal/httpx"
	"github.com/codevector-2003/filiation/internal/identity"
	"github.com/codevector-2003/filiation/internal/model"
	"github.com/codevector-2003/filiation/internal/sources/openalex"
	"github.com/codevector-2003/filiation/internal/store"
)

// Options are the choices a front door makes that are not configuration.
type Options struct {
	// CacheDir is where HTTP responses are cached. Empty means
	// config.DefaultCacheDir.
	CacheDir string

	// NoCache turns the cache off: --no-cache, which ADR-004 requires from day
	// one because a stale cache can hide a bug.
	NoCache bool

	// Transport replaces the network. Tests replay recorded responses through
	// it; front doors leave it nil.
	Transport http.RoundTripper
}

// Library is an open library: the store, and the sources that grow it.
//
// This is the composition root. It is the one place that builds both a store
// and a source — but it never moves data between them. That is internal/graph's
// job, so the two stay swappable independently.
type Library struct {
	db    *store.DB
	graph *graph.Graph
	http  *httpx.Client
	cfg   config.Config
}

// Open opens the library cfg names, creating and migrating it on first use.
func Open(ctx context.Context, cfg *config.Config, opts Options) (*Library, error) {
	db, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		return nil, err
	}
	if err := db.Migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}

	var cache httpx.Cache
	if !opts.NoCache {
		dir := opts.CacheDir
		if dir == "" {
			if dir, err = config.DefaultCacheDir(); err != nil {
				db.Close()
				return nil, err
			}
		}
		dc, err := httpx.NewDirCache(dir)
		if err != nil {
			db.Close()
			return nil, err
		}
		cache = dc
	}

	hopts := openalex.HTTPOptions(cfg.ContactEmail, cache)
	hopts.Transport = opts.Transport
	h, err := httpx.New(hopts)
	if err != nil {
		db.Close()
		return nil, err
	}

	return &Library{
		db:    db,
		graph: graph.New(db, openalex.New(h)),
		http:  h,
		cfg:   *cfg,
	}, nil
}

// Close closes the library.
func (l *Library) Close() error { return l.db.Close() }

// Path is where the library lives on disk.
func (l *Library) Path() string { return l.db.Path() }

// AddOptions controls Add.
type AddOptions struct {
	// AcceptFirst takes the best title-search candidate without asking. It is
	// --accept-first, for scripts, and it is the only way a title seeds a
	// graph unattended (ADR-005).
	AcceptFirst bool

	// SearchLimit is how many title candidates to offer. Zero means the
	// source's default.
	SearchLimit int
}

// Added reports what Add did.
type Added struct {
	// Work is the seed as stored.
	Work model.Work

	// AlreadySeed reports the work was a seed before this call, so nothing
	// about it changed. The CLI says "already in your library" rather than
	// printing zeros that look like a failure.
	AlreadySeed bool

	// Refs is how many works the seed cites. NewStubs of them were not yet in
	// the library, and NewEdges citation edges were recorded.
	Refs     int
	NewStubs int
	NewEdges int

	// DeadEnd reports the seed cites nothing OpenAlex could resolve, so the
	// graph cannot grow from it. In arts and humanities that is most works
	// (D12). It is reported, not raised: it is the data, not the tool.
	DeadEnd bool
}

// Validate reports whether input is something Add could look up, without
// touching the network or the library. A front door calls it first, so that a
// typo is refused before anything is created — including, on a first run, the
// library itself.
func Validate(input string) error {
	_, err := identity.Parse(input)
	return err
}

// Add adds a paper to the library as a seed: the work, a stub for everything it
// cites, and the edges between them.
//
// input is whatever the user typed. A DOI, arXiv ID, PMID or OpenAlex ID
// resolves directly. A title is searched, and unless opts.AcceptFirst is set the
// candidates come back in an *AmbiguousError and nothing is written — the user
// chooses and the front door calls AddCandidate. Errors to expect, all compared
// with errors.Is:
//
//	errs.ErrInvalidInput  the input is not something that can be looked up
//	errs.ErrUnresolved    OpenAlex has no such work (or no title candidates)
//	errs.ErrAmbiguous     a title matched candidates; see *AmbiguousError
//	errs.ErrTransient     the network failed; trying again later may work
func (l *Library) Add(ctx context.Context, input string, opts AddOptions) (Added, error) {
	id, err := identity.Parse(input)
	if err != nil {
		return Added{}, err
	}

	if id.Deterministic() {
		s, err := l.graph.AddSeed(ctx, id)
		if err != nil {
			return Added{}, err
		}
		return added(s), nil
	}

	found, err := l.graph.SearchTitle(ctx, id.Value, opts.SearchLimit)
	if err != nil {
		return Added{}, err
	}
	if len(found) == 0 {
		return Added{}, fmt.Errorf("no work titled %q: %w", id.Value, errs.ErrUnresolved)
	}
	candidates := make([]Candidate, len(found))
	for i, c := range found {
		candidates[i] = Candidate{Work: c.Work.Work, TitleMatch: c.TitleMatch, hydrated: c.Work}
	}
	if !opts.AcceptFirst {
		return Added{}, &AmbiguousError{Title: id.Value, Candidates: candidates}
	}
	return l.AddCandidate(ctx, candidates[0])
}

// AddCandidate adds the title-search candidate the user chose. The candidate
// arrived fully fetched, so this makes no request.
func (l *Library) AddCandidate(ctx context.Context, c Candidate) (Added, error) {
	if c.hydrated.OpenAlexID == "" {
		return Added{}, errors.New("library: candidate did not come from Add")
	}
	s, err := l.graph.AddSeedWork(ctx, c.hydrated)
	if err != nil {
		return Added{}, err
	}
	return added(s), nil
}

func added(s graph.Seeded) Added {
	return Added{
		Work:        s.Work,
		AlreadySeed: s.WasSeed,
		Refs:        s.Recorded.Refs,
		NewStubs:    s.Recorded.Stubs,
		NewEdges:    s.Recorded.Edges,
		DeadEnd:     s.Recorded.DeadEnd(),
	}
}

// ExpandOptions bound one expansion. Zero fields take the configured value —
// max_nodes, max_depth and max_refs_per_work from the config file, or their
// defaults — so a front door passes only what the user overrode.
type ExpandOptions struct {
	MaxNodes int
	MaxDepth int

	// Progress, if set, is called after each batch is committed.
	Progress func(model.ExpansionResult)
}

// Expand grows the graph outward from everything already in the library, best
// first, until the budget is spent, the frontier is empty, the depth limit is
// reached, or ctx is cancelled. Each of those is a normal outcome, reported in
// the result's StoppedBecause with a nil error. Run it again to continue: the
// frontier is persisted state, so a stopped run resumes where it left off.
//
// An error means something failed — most often the network, wrapping
// errs.ErrTransient — and the result still reports what was saved before it.
func (l *Library) Expand(ctx context.Context, opts ExpandOptions) (model.ExpansionResult, error) {
	o := graph.ExpandOptions{
		MaxNodes:       l.cfg.MaxNodes,
		MaxDepth:       l.cfg.MaxDepth,
		MaxRefsPerWork: l.cfg.MaxRefsPerWork,
		Progress:       opts.Progress,
	}
	if opts.MaxNodes != 0 {
		o.MaxNodes = opts.MaxNodes
	}
	if opts.MaxDepth != 0 {
		o.MaxDepth = opts.MaxDepth
	}
	return l.graph.Expand(ctx, o)
}

// Budget is the configured expansion budget: the default a front door shows
// when the user has not overridden it.
func (l *Library) Budget() (maxNodes, maxDepth int) { return l.cfg.MaxNodes, l.cfg.MaxDepth }

// Candidate is one title-search result offered to the user.
type Candidate struct {
	Work model.Work

	// TitleMatch flags a candidate whose title is, allowing for case,
	// punctuation and a typo, the one typed. Such candidates come first. It is
	// a hint for the person choosing, never a choice (ADR-005).
	TitleMatch bool

	// hydrated keeps the full response, reference list included, so that
	// AddCandidate needs no second fetch.
	hydrated model.HydratedWork
}

// AmbiguousError is returned by Add when a title matched candidates and no
// choice was made. It matches errs.ErrAmbiguous.
type AmbiguousError struct {
	Title      string
	Candidates []Candidate
}

func (e *AmbiguousError) Error() string {
	return fmt.Sprintf("%q matched %d works; choose one, or pass --accept-first",
		e.Title, len(e.Candidates))
}

// Is makes an *AmbiguousError match errs.ErrAmbiguous.
func (e *AmbiguousError) Is(target error) bool { return target == errs.ErrAmbiguous }

// Stats counts what the library holds.
type Stats struct {
	Works int // every work, stubs included
	Stubs int // works known only by ID so far
	Edges int // citations
}

// Stats reads the library's counts.
func (l *Library) Stats(ctx context.Context) (Stats, error) {
	s, err := l.db.Stats(ctx)
	if err != nil {
		return Stats{}, err
	}
	return Stats{Works: s.Works, Stubs: s.Stubs, Edges: s.Edges}, nil
}

// Find resolves what the user typed to a work already in the library. It never
// asks OpenAlex: a paper that has not been added has no neighbours and no
// paths, and saying so is more useful than fetching it behind the user's back.
//
// A DOI, arXiv ID, PMID or OpenAlex ID is looked up directly. A title is
// matched against the titles in the library, allowing for case, punctuation
// and a typo; one match is used, several come back in an *AmbiguousError. For
// a read-only question that is safe in a way it is not for Add: nothing is
// written, and the answer names the work it used.
//
// A work that is not there wraps errs.ErrNotFound.
func (l *Library) Find(ctx context.Context, input string) (model.Work, error) {
	id, err := identity.Parse(input)
	if err != nil {
		return model.Work{}, err
	}

	by := map[identity.Kind]store.Lookup{
		identity.KindOpenAlex: store.ByOpenAlexID,
		identity.KindDOI:      store.ByDOI,
		identity.KindArXiv:    store.ByArXivID,
		identity.KindPMID:     store.ByPMID,
	}
	if lookup, ok := by[id.Kind]; ok {
		w, err := l.db.FindWork(ctx, lookup, id.Value)
		if errors.Is(err, errs.ErrNotFound) && id.Kind == identity.KindArXiv {
			// An arXiv paper OpenAlex found by its DataCite DOI may be stored
			// under that DOI rather than with its arXiv ID.
			w, err = l.db.FindWork(ctx, store.ByDOI, id.ArXivDOI())
		}
		if errors.Is(err, errs.ErrNotFound) {
			return model.Work{}, fmt.Errorf("%s %s is not in your library: %w", id.Kind, id.Value, errs.ErrNotFound)
		}
		if err != nil {
			return model.Work{}, err
		}
		return *w, nil
	}

	works, err := l.db.TitledWorks(ctx)
	if err != nil {
		return model.Work{}, err
	}
	var exact, near []model.Work
	key := identity.TitleKey(id.Value)
	for _, w := range works {
		switch {
		case identity.TitleKey(*w.Title) == key:
			exact = append(exact, w)
		case identity.TitlesMatch(id.Value, *w.Title):
			near = append(near, w)
		}
	}
	matches := exact
	if len(matches) == 0 {
		matches = near
	}
	switch len(matches) {
	case 0:
		return model.Work{}, fmt.Errorf("no work titled %q in your library: %w", id.Value, errs.ErrNotFound)
	case 1:
		return l.getWork(ctx, matches[0].OpenAlexID)
	}
	candidates := make([]Candidate, len(matches))
	for i, w := range matches {
		candidates[i] = Candidate{Work: w, TitleMatch: true}
	}
	return model.Work{}, &AmbiguousError{Title: id.Value, Candidates: candidates}
}

// Neighbourhood is a work and the works on either side of it.
type Neighbourhood struct {
	Work model.Work

	// Cites is the work's references, fetched ones first.
	Cites []model.Work

	// CitedBy is the works in this library that cite it — never a global
	// count; see Work.CitedByCount for that.
	CitedBy []model.Work
}

// Neighbours returns what a work in the library cites and what cites it.
func (l *Library) Neighbours(ctx context.Context, input string) (Neighbourhood, error) {
	w, err := l.Find(ctx, input)
	if err != nil {
		return Neighbourhood{}, err
	}
	cites, err := l.db.WorksCitedBy(ctx, w.OpenAlexID)
	if err != nil {
		return Neighbourhood{}, err
	}
	citedBy, err := l.db.WorksCiting(ctx, w.OpenAlexID)
	if err != nil {
		return Neighbourhood{}, err
	}
	return Neighbourhood{Work: w, Cites: cites, CitedBy: citedBy}, nil
}

// DefaultMaxHops is how far Path searches by default. Six citation steps
// reaches across almost any library; a chain longer than that says little
// about how two papers are related.
const DefaultMaxHops = 6

// PathOptions control Path.
type PathOptions struct {
	// MaxHops bounds the search. Zero means DefaultMaxHops.
	MaxHops int

	// AnyDirection connects the two works through citations in either
	// direction. Without it, Path looks for lineage: one work reaching the
	// other by following references.
	AnyDirection bool
}

// Path is a chain of citations between two works.
type Path struct {
	// Found reports whether a chain exists within the step limit. Not finding
	// one is an answer, not an error.
	Found bool

	// Works runs from the first work asked about to the second.
	Works []model.Work

	// Cites[i] reports that Works[i] cites Works[i+1]; false means it is the
	// other way round.
	Cites []bool
}

// PathBetween finds the shortest chain of citations between two works in the
// library.
//
// By default it looks for lineage — the project's name for it (D6): a chain of
// references from one work to the other, so that one descends from the other.
// It tries from the first to the second, then the reverse, and always reports
// the chain in the order asked. With AnyDirection it finds any connection.
func (l *Library) PathBetween(ctx context.Context, from, to string, opts PathOptions) (Path, error) {
	a, err := l.Find(ctx, from)
	if err != nil {
		return Path{}, err
	}
	b, err := l.Find(ctx, to)
	if err != nil {
		return Path{}, err
	}
	hops := opts.MaxHops
	if hops <= 0 {
		hops = DefaultMaxHops
	}

	var p store.Path
	if opts.AnyDirection {
		p, err = l.db.ShortestPath(ctx, a.OpenAlexID, b.OpenAlexID, hops, store.Either)
	} else {
		p, err = l.db.ShortestPath(ctx, a.OpenAlexID, b.OpenAlexID, hops, store.Backward)
		if errors.Is(err, errs.ErrNotFound) {
			p, err = l.db.ShortestPath(ctx, b.OpenAlexID, a.OpenAlexID, hops, store.Backward)
			if err == nil {
				p = reversePath(p)
			}
		}
	}
	if errors.Is(err, errs.ErrNotFound) {
		return Path{Works: []model.Work{a, b}}, nil
	}
	if err != nil {
		return Path{}, err
	}

	out := Path{Found: true, Cites: p.Cites}
	for _, id := range p.IDs {
		w, err := l.getWork(ctx, id)
		if err != nil {
			return Path{}, err
		}
		out.Works = append(out.Works, w)
	}
	return out, nil
}

// getWork reads one work by ID and returns it by value.
func (l *Library) getWork(ctx context.Context, id string) (model.Work, error) {
	w, err := l.db.GetWork(ctx, id)
	if err != nil {
		return model.Work{}, err
	}
	return *w, nil
}

// reversePath turns a chain found from b to a into the same chain read from a
// to b.
func reversePath(p store.Path) store.Path {
	n := len(p.IDs)
	r := store.Path{IDs: make([]string, n), Cites: make([]bool, len(p.Cites))}
	for i, id := range p.IDs {
		r.IDs[n-1-i] = id
	}
	for i, c := range p.Cites {
		r.Cites[len(p.Cites)-1-i] = !c
	}
	return r
}

// Summary is the library at a glance.
type Summary struct {
	Works      int // every work, stubs included
	Fetched    int // metadata fetched
	Stubs      int // known only by ID so far
	Unresolved int // of the stubs, those OpenAlex has no record of
	Seeds      int // papers the user added
	Citations  int

	// DeadEnds is how many fetched works have no reference list, so the graph
	// cannot grow through them (D12).
	DeadEnds int
}

// Summarise counts the library.
func (l *Library) Summarise(ctx context.Context) (Summary, error) {
	s, err := l.db.Summary(ctx)
	if err != nil {
		return Summary{}, err
	}
	return Summary{
		Works: s.Works, Fetched: s.Hydrated, Stubs: s.Stubs, Unresolved: s.Unresolved,
		Seeds: s.Seeds, Citations: s.Edges, DeadEnds: s.DeadEnds,
	}, nil
}

// DuplicateGroup is a set of fetched works that share a title: probably one
// paper OpenAlex holds as more than one record.
type DuplicateGroup = graph.DuplicateGroup

// ProbableDuplicates lists fetched works that share a title. It reports and
// never merges: a matching title is evidence, not proof — a book review carries
// the title of the book it reviews (D14).
func (l *Library) ProbableDuplicates(ctx context.Context) ([]DuplicateGroup, error) {
	return l.graph.ProbableDuplicates(ctx)
}

// Quota is the OpenAlex allowance as last reported: requests left today and
// how long until it resets.
type Quota = httpx.Quota

// Quota returns the last OpenAlex allowance seen, and false before any request
// has reported one — including when every answer came from the cache.
func (l *Library) Quota() (Quota, bool) { return l.http.Quota() }

// RetryAfter returns how long OpenAlex asked us to wait, when err is a
// transient failure that carried the request. A front door uses it to say
// "try again in an hour — today's allowance is spent" rather than just "failed".
func RetryAfter(err error) (time.Duration, bool) { return httpx.RetryAfter(err) }

// ClearCache deletes every cached HTTP response under dir, or under
// config.DefaultCacheDir if dir is empty, and returns the directory it cleared.
// It is `fil cache clear`, and does not need the library open.
func ClearCache(dir string) (string, error) {
	if dir == "" {
		var err error
		if dir, err = config.DefaultCacheDir(); err != nil {
			return "", err
		}
	}
	c, err := httpx.NewDirCache(dir)
	if err != nil {
		return "", err
	}
	return dir, c.Clear()
}
