package mcpsrv

import (
	"cmp"
	"slices"

	"github.com/codevector-2003/filiation/internal/library"
	"github.com/codevector-2003/filiation/internal/model"
)

// Work is a paper as an assistant sees it. Two fields are never omitted,
// whatever else is missing (ARCHITECTURE.md §6): the OpenAlex ID, so that every
// answer can be followed up with another call, and the open-access status, so
// that an assistant does not claim it can read a paper it cannot.
type Work struct {
	ID       string `json:"id" jsonschema:"OpenAlex ID; pass it to any other tool"`
	OAStatus string `json:"oa_status" jsonschema:"diamond, gold, green, hybrid or bronze are free to read; closed is not; unknown until fetched"`

	// Stub marks a work known only by its ID: something in the library cites
	// it, but its metadata has not been fetched. It must never be presented as
	// if the library knew what it is (§6, rule 3).
	Stub bool `json:"stub" jsonschema:"true when only the ID is known: the library has not fetched this work, so it has no title yet"`

	Title string `json:"title,omitempty"`

	// Authors is the byline, so that an answer can credit a paper from the
	// library rather than from the model's memory. Capped like every list.
	Authors     []string `json:"authors,omitempty" jsonschema:"the first authors, in byline order; absent when not recorded"`
	MoreAuthors int      `json:"more_authors,omitempty" jsonschema:"how many further authors the byline has (et al.)"`

	Year         int    `json:"year,omitempty"`
	Type         string `json:"type,omitempty"`
	Venue        string `json:"venue,omitempty"`
	DOI          string `json:"doi,omitempty"`
	OAURL        string `json:"oa_url,omitempty" jsonschema:"a free-to-read copy, when OpenAlex knows one"`
	CitedByCount int    `json:"cited_by_count_global,omitempty" jsonschema:"how often the whole world cites it, per OpenAlex; not a count within this library"`
	Seed         bool   `json:"seed,omitempty" jsonschema:"the user added this paper themselves"`
	Unresolved   bool   `json:"unresolved,omitempty" jsonschema:"OpenAlex has no record of this ID; it will never be fetched"`
}

func toWork(w model.Work) Work {
	out := Work{
		ID:           w.OpenAlexID,
		OAStatus:     string(w.OAStatus),
		Stub:         w.IsStub(),
		Type:         string(w.Type),
		Venue:        w.Venue,
		CitedByCount: w.CitedByCount,
		Seed:         w.IsSeed,
		Unresolved:   w.Unresolved,
	}
	for _, a := range w.Authors[:min(len(w.Authors), maxAuthorsShown)] {
		out.Authors = append(out.Authors, a.Name)
	}
	out.MoreAuthors = len(w.Authors) - len(out.Authors)
	if out.OAStatus == "" {
		out.OAStatus = "unknown"
	}
	if w.Title != nil {
		out.Title = *w.Title
	}
	if w.Year != nil {
		out.Year = *w.Year
	}
	if w.DOI != nil {
		out.DOI = *w.DOI
	}
	if w.OAURL != nil {
		out.OAURL = *w.OAURL
	}
	return out
}

// Page is a capped list: the works returned, and how many were left out. An
// assistant handed five thousand works spends its context on them and loses the
// thread, so no tool returns an unbounded list (§6, rule 1).
type Page struct {
	Total   int    `json:"total" jsonschema:"how many there are in the library"`
	Fetched int    `json:"fetched" jsonschema:"how many of them have been fetched; the rest are stubs"`
	Omitted int    `json:"omitted" jsonschema:"how many were left out of works; raise limit to see more"`
	Works   []Work `json:"works"`
}

// page ranks works — fetched before stubs, then by how widely each is cited —
// and keeps the first limit of them.
func page(works []model.Work, limit int) Page {
	ranked := slices.Clone(works)
	slices.SortStableFunc(ranked, func(a, b model.Work) int {
		if a.IsStub() != b.IsStub() {
			if a.IsStub() {
				return 1
			}
			return -1
		}
		return cmp.Compare(b.CitedByCount, a.CitedByCount)
	})
	p := Page{Total: len(ranked), Works: []Work{}}
	for _, w := range ranked {
		if !w.IsStub() {
			p.Fetched++
		}
	}
	for _, w := range ranked[:min(limit, len(ranked))] {
		p.Works = append(p.Works, toWork(w))
	}
	p.Omitted = p.Total - len(p.Works)
	return p
}

// Candidate is one work a title could mean.
type Candidate struct {
	Work
	TitleMatch bool `json:"title_match,omitempty" jsonschema:"the title matches the one given, allowing for case, punctuation and a typo"`
}

func candidates(cs []library.Candidate) []Candidate {
	out := make([]Candidate, len(cs))
	for i, c := range cs {
		out[i] = Candidate{Work: toWork(c.Work), TitleMatch: c.TitleMatch}
	}
	return out
}
