package model

import (
	"fmt"
	"time"
)

// Work is one row of the work table, plus the fields that only exist in flight.
//
// A Work is in one of two states (ADR-003):
//
//	stub      Hydrated == false. The OpenAlex ID is known and almost nothing else.
//	          Created the moment some other paper is seen to reference it.
//	hydrated  Hydrated == true. Metadata has been fetched.
//
// Stubs are the common case, not the exception: a 500-node graph reached from one
// seed is mostly stubs until the budget is spent on them. Every read path meets
// them. That is why the optional fields are pointers — the compiler asks the
// question rather than handing back a zero value that reads like real data.
//
// Nullable columns are modelled as pointers only where NULL and the zero value
// mean different things. Two cases matter:
//
//   - DOI. The column is UNIQUE. SQLite allows many NULLs in a unique index but
//     only one empty string, so a string-typed DOI would make the second stub
//     without one fail to insert. This one is a correctness requirement.
//   - Year, Depth. Zero is a value a caller could believe. Absent is not.
//
// Where the empty string is an honest way to say "not set" and nothing downstream
// can misread it — Abstract, Venue — a plain string is used and stored as NULL by
// the store layer.
type Work struct {
	// OpenAlexID is the primary key and the deduplication key (hard rule 6).
	// Bare form, "W2741809807" — not the https://openalex.org/... URL OpenAlex
	// returns. Normalising that is internal/identity's job, and the store should
	// reject anything else rather than silently create a second node for the same
	// paper under a different spelling.
	OpenAlexID string

	// External identifiers. All absent on a stub, and often absent afterwards:
	// roughly a third of hydrated works have no DOI.
	DOI     *string
	ArXivID *string
	PMID    *string

	// Bibliographic metadata. All zero on a stub.
	Title    *string
	Abstract string
	Year     *int
	Venue    string
	Type     WorkType

	// CitedByCount is OpenAlex's global count — how often the whole world cites
	// this work. It is not the in-graph in-degree, which is computed from cites
	// and is the better ranking signal because it is relative to this library.
	// See FrontierItem.InDegree.
	CitedByCount int

	// Open access. OAURL is the only sanctioned route to a full text (hard rule 1).
	OAStatus OAStatus
	OAURL    *string

	// Full text on disk. Both nil until M3.
	PDFSHA256  *string
	PDFLicense *string

	// State flags, all persisted so that expansion resumes from the frontier
	// rather than starting over.
	Hydrated    bool // metadata fetched
	FetchedRefs bool // this work's own references have been recorded
	Unresolved  bool // OpenAlex has no record of it — stop retrying

	// Provenance.
	Depth   *int // hops from the nearest seed; nil until the work is first reached
	IsSeed  bool
	Source  Source
	AddedAt time.Time

	// ---- not columns on the work row ----

	// Authors comes from the authorship join and is populated only when a caller
	// asks for it, so the nil-versus-empty distinction carries meaning here the
	// same way it does for the pointer fields above:
	//
	//	nil            not loaded — this query did not ask for authors
	//	[]Author{}     loaded, and the work genuinely has none
	//
	// Use AuthorsLoaded rather than len() to tell them apart. A byline hidden on
	// len(Authors) == 0 disappears for every work whose authors were simply never
	// queried.
	Authors []Author
}

// IsStub reports whether only the identity of this work is known.
func (w *Work) IsStub() bool { return !w.Hydrated }

// DisplayTitle is the safe way to name a work in output meant for a human. It
// never returns the empty string, so a CLI table or an export cannot end up with
// a blank row that looks like a bug in the tool rather than a work not yet fetched.
func (w *Work) DisplayTitle() string {
	if w.Title != nil && *w.Title != "" {
		return *w.Title
	}
	if w.Unresolved {
		return fmt.Sprintf("[not in OpenAlex: %s]", w.OpenAlexID)
	}
	return fmt.Sprintf("[not fetched: %s]", w.OpenAlexID)
}

// AuthorsLoaded reports whether the authorship join was performed for this work.
// It distinguishes a work with no authors from one whose authors were not asked
// for; see the Authors field.
func (w *Work) AuthorsLoaded() bool { return w.Authors != nil }

// HydratedWork is a Work as it comes back from OpenAlex, with the reference list
// still attached.
//
// The reference list is the reason this type exists. It arrives inside the work
// object, which is what lets RecordEdgesAndStubs write the edges and the stub
// rows in the same transaction that hydrates the work (ADR-003, §4). But it is
// never a column: the cites table is the record, and nothing reads a reference
// list back out of the database.
//
// Keeping it off Work is not tidiness. A Work loaded from the store has
// FetchedRefs true and no reference list, which is indistinguishable from a work
// that was asked and had none — so a dead-end test living on Work would report
// every persisted work as a dead end. Splitting the types makes that
// unrepresentable rather than merely documented.
//
// Only sources/openalex constructs one, and only the hydration path consumes it.
// Everything downstream takes the embedded Work.
type HydratedWork struct {
	Work
	ReferencedWorks []string
}

// IsDeadEnd reports whether this work can never extend the graph: its references
// were fetched and there were none.
//
// This is not a defect in the work or in the tool. Reference coverage is a
// property of the field — 6% dead ends in medicine against 84% in arts and
// humanities (D12) — and it has to be surfaced per node from day one, the same
// way OA status is, or users conclude the tool is broken when it is the data.
//
// This answers the question only for a work in flight. For a work already in the
// library the answer comes from its out-degree in cites, computed by store at
// query time — the same way FrontierItem.InDegree is, and for the same reason:
// the edges are already stored, so the signal is free.
func (h *HydratedWork) IsDeadEnd() bool {
	return h.FetchedRefs && len(h.ReferencedWorks) == 0
}

// Author is one row of author, carrying its position from the authorship join.
type Author struct {
	OpenAlexID string
	Name       string
	ORCID      *string
	Position   int // 0-based; first author is 0
}

// WorkType mirrors work.type. OpenAlex's vocabulary is open-ended and it has
// added values before; an unrecognised type is stored as it arrived, never
// rejected and never mapped to a default.
type WorkType string

const (
	TypeUnknown      WorkType = ""
	TypeArticle      WorkType = "article"
	TypePreprint     WorkType = "preprint"
	TypeReview       WorkType = "review"
	TypeBook         WorkType = "book"
	TypeBookChapter  WorkType = "book-chapter"
	TypeDataset      WorkType = "dataset"
	TypeDissertation WorkType = "dissertation"
)

// OAStatus mirrors work.oa_status.
type OAStatus string

const (
	OAUnknown OAStatus = ""
	OADiamond OAStatus = "diamond" // open in a venue that charges authors nothing either
	OAGold    OAStatus = "gold"    // published open in a fully OA venue
	OAGreen   OAStatus = "green"   // author copy in a repository
	OAHybrid  OAStatus = "hybrid"  // open article in a subscription venue
	OABronze  OAStatus = "bronze"  // free to read on the publisher site, no open licence
	OAClosed  OAStatus = "closed"
)

// IsOpen reports whether OpenAlex considers a free-to-read copy to exist.
//
// It is a display and filtering predicate, not a download gate. Nothing may fetch
// a PDF on the strength of a status string: the only sanctioned input is an actual
// OA location, Work.OAURL. Bronze is the reason the distinction is not pedantic —
// free to read on the publisher's own site, with no licence permitting anything
// else. See hard rule 1.
func (s OAStatus) IsOpen() bool {
	switch s {
	case OADiamond, OAGold, OAGreen, OAHybrid, OABronze:
		return true
	default:
		return false
	}
}

// Source records how a work entered the library, mirroring work.source.
type Source string

const (
	SourceUnknown   Source = ""
	SourceSeed      Source = "seed"      // named by the user
	SourceExpansion Source = "expansion" // reached by following references
	SourceUpload    Source = "upload"
	SourceZotero    Source = "zotero"
)
