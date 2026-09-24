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
