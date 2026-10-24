package openalex

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/codevector-2003/filiation/internal/identity"
	"github.com/codevector-2003/filiation/internal/model"
)

// selectFields is the top-level fields every request asks for. OpenAlex's
// select takes only top-level names, so primary_location and authorships come
// whole; everything not listed — topics, concepts, counts by year — is left
// behind, and it is most of a full work object.
//
// best_oa_location and locations carry the open-access PDF links (ADR-013).
// A list request costs one credit whatever it returns, so asking for them
// while hydrating is free: a work fetched from now on knows its links.
const selectFields = "id,doi,ids,title,display_name,publication_year,type,cited_by_count," +
	"open_access,primary_location,authorships,abstract_inverted_index," +
	"referenced_works,referenced_works_count,best_oa_location,locations"

// locationFields is what a lookup of links alone asks for.
const locationFields = "id,open_access,best_oa_location,locations"

// work is the subset of an OpenAlex work object this package reads. Every
// optional field is a pointer or a nil-able slice because OpenAlex sends null
// freely, and a zero value read as data is exactly the mistake model.Work's
// pointer fields exist to prevent.
type work struct {
	ID          string  `json:"id"`
	DOI         *string `json:"doi"`
	Title       *string `json:"title"`
	DisplayName *string `json:"display_name"`
	IDs         struct {
		PMID *string `json:"pmid"`
	} `json:"ids"`
	PublicationYear *int    `json:"publication_year"`
	Type            *string `json:"type"`
	CitedByCount    int     `json:"cited_by_count"`
	OpenAccess      struct {
		IsOA     bool    `json:"is_oa"`
		OAStatus *string `json:"oa_status"`
		OAURL    *string `json:"oa_url"`
	} `json:"open_access"`
	// Pointers, so that a response without these fields — one cached before
	// they were asked for — reads as "not known" rather than "no open copy".
	BestOALocation *oaLocation    `json:"best_oa_location"`
	Locations      *[]oaLocation  `json:"locations"`
	PrimaryLocation *location `json:"primary_location"`
	Authorships     []struct {
		Author struct {
			ID          *string `json:"id"`
			DisplayName *string `json:"display_name"`
			ORCID       *string `json:"orcid"`
		} `json:"author"`
	} `json:"authorships"`
	AbstractInvertedIndex map[string][]int `json:"abstract_inverted_index"`
	ReferencedWorks       []string         `json:"referenced_works"`
}

// location is where a copy of a work lives. Only the primary one is read here;
// open-access locations for PDFs are M3's concern.
type location struct {
	LandingPageURL *string `json:"landing_page_url"`
	Source         *struct {
		DisplayName *string `json:"display_name"`
	} `json:"source"`
}

// oaLocation is one copy of a work, as the locations list describes it.
type oaLocation struct {
	IsOA           bool    `json:"is_oa"`
	PDFURL         *string `json:"pdf_url"`
	LandingPageURL *string `json:"landing_page_url"`
	Version        *string `json:"version"`
	License        *string `json:"license"`
}

// list is a list response: a page of works and its metadata.
type list struct {
	Meta struct {
		Count int `json:"count"`
	} `json:"meta"`
	Results []work `json:"results"`
}

// hydrated maps an OpenAlex work onto the model.
//
// Identifiers are normalised through internal/identity on the way in, because
// OpenAlex returns every one of them as a URL — https://openalex.org/W...,
// https://doi.org/... — and store keys on the bare form. A work whose own ID
// cannot be normalised is an error; a reference that cannot be is skipped, so
// that one malformed entry in a list of eighty does not cost the other
// seventy-nine edges.
//
// Provenance — depth, seed, source — is left zero. This package does not know
// how a work was reached; the caller does.
func (w *work) hydrated() (model.HydratedWork, error) {
	id, err := identity.NormaliseOpenAlexID(w.ID)
	if err != nil {
		return model.HydratedWork{}, fmt.Errorf("openalex: work with id %q: %w", w.ID, err)
	}

	h := model.HydratedWork{Work: model.Work{
		OpenAlexID:   id,
		Title:        firstNonEmpty(w.Title, w.DisplayName),
		Year:         w.PublicationYear,
		CitedByCount: w.CitedByCount,
		Abstract:     abstract(w.AbstractInvertedIndex),
		Hydrated:     true,
		// The response carried referenced_works, so this work's references
		// are known — including when there are none. That is what lets
		// IsDeadEnd answer honestly (D12).
		FetchedRefs: true,
	}}

	if w.DOI != nil {
		if doi, err := identity.NormaliseDOI(*w.DOI); err == nil {
			h.DOI = &doi
		}
	}
	if w.IDs.PMID != nil {
		if p, err := identity.Parse(*w.IDs.PMID); err == nil && p.Kind == identity.KindPMID {
			h.PMID = &p.Value
		}
	}
	h.ArXivID = arXivID(h.DOI, w.PrimaryLocation)

	if w.Type != nil {
		// Stored as it arrived. OpenAlex's vocabulary is open-ended — it has
		// "conference-paper" and "paratext" that the model has no constant for —
		// and mapping an unknown type to a default would be a lie.
		h.Type = model.WorkType(*w.Type)
	}
	if w.OpenAccess.OAStatus != nil {
		h.OAStatus = model.OAStatus(*w.OpenAccess.OAStatus)
	}
	h.OAURL = w.OpenAccess.OAURL
	if pl := w.PrimaryLocation; pl != nil && pl.Source != nil && pl.Source.DisplayName != nil {
		h.Venue = *pl.Source.DisplayName
	}

	// Loaded, so never nil: a work with no authors is an empty slice, which
	// is how model.Work tells "has none" from "not asked".
	h.Authors = make([]model.Author, 0, len(w.Authorships))
	for i, a := range w.Authorships {
		if a.Author.ID == nil || a.Author.DisplayName == nil {
			continue
		}
		author := model.Author{
			OpenAlexID: lastSegment(*a.Author.ID),
			Name:       *a.Author.DisplayName,
			Position:   i,
		}
		if a.Author.ORCID != nil && *a.Author.ORCID != "" {
			author.ORCID = model.Ptr(lastSegment(*a.Author.ORCID))
		}
		h.Authors = append(h.Authors, author)
	}

	h.PDF = w.pdfLinks()

	h.ReferencedWorks = make([]string, 0, len(w.ReferencedWorks))
	for _, ref := range w.ReferencedWorks {
		if r, err := identity.NormaliseOpenAlexID(ref); err == nil {
			h.ReferencedWorks = append(h.ReferencedWorks, r)
		}
	}
	return h, nil
}

// pdfLinks reads a work's open-access PDF links in the order to try them, or
// returns nil when the response did not include its locations.
//
// The order is the version first — published, accepted, then preprint, the
// copy a reader would cite before the one they could get — then OpenAlex's
// best location, then the order OpenAlex lists them in. Only open locations
// with an http or https PDF link count; a landing page is never a PDF.
func (w *work) pdfLinks() *model.PDFLinks {
	if w.Locations == nil && w.BestOALocation == nil {
		return nil
	}
	var all []oaLocation
	if w.BestOALocation != nil {
		all = append(all, *w.BestOALocation)
	}
	if w.Locations != nil {
		all = append(all, *w.Locations...)
	}

	out := &model.PDFLinks{Availability: model.PDFNoOpenCopy}
	if w.OpenAccess.IsOA {
		out.Availability = model.PDFOpenNoLink
	}
	seen := map[string]bool{}
	for _, l := range all {
		if !l.IsOA {
			continue
		}
		out.Availability = model.PDFOpenNoLink
		if l.PDFURL == nil {
			continue
		}
		u, err := url.Parse(strings.TrimSpace(*l.PDFURL))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || seen[u.String()] {
			continue
		}
		seen[u.String()] = true
		out.Links = append(out.Links, model.Location{
			PDFURL:     u.String(),
			LandingURL: deref(l.LandingPageURL),
			Version:    version(deref(l.Version)),
			License:    deref(l.License),
			Host:       strings.TrimPrefix(strings.ToLower(u.Hostname()), "www."),
		})
	}
	sort.SliceStable(out.Links, func(i, j int) bool {
		return versionRank[out.Links[i].Version] < versionRank[out.Links[j].Version]
	})
	if len(out.Links) > 0 {
		out.Availability = model.PDFLink
	}
	return out
}

// version maps OpenAlex's version names onto the ones fil names files by
// (ADR-012): published.pdf, accepted.pdf, preprint.pdf.
func version(v string) string {
	switch v {
	case "publishedVersion":
		return "published"
	case "acceptedVersion":
		return "accepted"
	case "submittedVersion":
		return "preprint"
	}
	return ""
}

var versionRank = map[string]int{"published": 0, "accepted": 1, "preprint": 2, "": 3}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}

// arXivID recovers the arXiv ID OpenAlex does not report directly: from an
// arXiv DataCite DOI if the work has one, or else from an arXiv landing page.
func arXivID(doi *string, pl *location) *string {
	const prefix = "10.48550/arxiv."
	if doi != nil && strings.HasPrefix(*doi, prefix) {
		if id, err := identity.Parse("arXiv:" + strings.TrimPrefix(*doi, prefix)); err == nil {
			return &id.Value
		}
	}
	if pl != nil && pl.LandingPageURL != nil {
		if id, err := identity.Parse(*pl.LandingPageURL); err == nil && id.Kind == identity.KindArXiv {
			return &id.Value
		}
	}
	return nil
}

// abstract rebuilds the text OpenAlex ships as an inverted index — word to the
// positions it occupies — which it does for licensing reasons. Positions that
// no word claims are dropped rather than left as gaps.
func abstract(index map[string][]int) string {
	if len(index) == 0 {
		return ""
	}
	type placed struct {
		pos  int
		word string
	}
	var words []placed
	for word, positions := range index {
		for _, p := range positions {
			words = append(words, placed{p, word})
		}
	}
	sort.Slice(words, func(i, j int) bool { return words[i].pos < words[j].pos })

	var b strings.Builder
	for i, w := range words {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(w.word)
	}
	return b.String()
}

func firstNonEmpty(ptrs ...*string) *string {
	for _, p := range ptrs {
		if p != nil && strings.TrimSpace(*p) != "" {
			return p
		}
	}
	return nil
}

func lastSegment(s string) string {
	s = strings.TrimRight(s, "/")
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		return s[i+1:]
	}
	return s
}
