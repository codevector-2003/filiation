package model

// Edge is one row of the cites table: from_work cited to_work.
//
// The direction is the direction of the citation, and it is the opposite of the
// direction time runs — FromWork is the newer paper. Getting this backwards is
// the easiest mistake in the codebase to make and the hardest to see, because a
// reversed graph still looks like a graph.
//
// An edge is ground truth. It comes from OpenAlex's resolved referenced_works,
// not from parsing a reference list (hard rule 2), so it is not a guess and
// nothing in this codebase is permitted to infer one.
type Edge struct {
	FromWork string // the citing work — the newer one
	ToWork   string // the cited work

	// The fields below are the point of the whole project, and all of them are
	// empty until M3 puts PDFs on disk and M6 classifies them. An edge with none
	// of them set is still a complete, useful edge.

	// Context is the sentence around the citation marker in the citing paper.
	// Nobody provides this for free; it is what makes retrieval better than
	// keyword search over abstracts.
	Context string

	// Intent and Section are extracted, therefore fallible — unlike the edge
	// itself. Confidence exists so a caller can tell one from the other.
	Intent  Intent
	Section Section

	// Confidence in the Intent and Section extraction, 0..1. nil means nothing
	// has tried yet, which is different from having tried and scored zero.
	Confidence *float64
}

// Intent classifies why one paper cited another (M6). Uncategorised is the
// honest default and must stay distinguishable from "not yet classified", which
// is IntentUnknown.
type Intent string

const (
	IntentUnknown       Intent = "" // not yet classified
	IntentBackground    Intent = "background"
	IntentMethod        Intent = "method"
	IntentComparison    Intent = "comparison"
	IntentContradiction Intent = "contradiction"
	IntentUncategorised Intent = "uncategorised" // classified, and none of the above
)

// Section is where in the citing paper the citation appeared. It is a useful
// prior on intent: a citation in Methods is far more likely to be a method
// citation than one in the introduction.
type Section string

const (
	SectionUnknown     Section = ""
	SectionIntro       Section = "intro"
	SectionRelatedWork Section = "related-work"
	SectionMethods     Section = "methods"
	SectionResults     Section = "results"
	SectionDiscussion  Section = "discussion"
)
