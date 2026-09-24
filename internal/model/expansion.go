package model

import "time"

// FrontierItem is one candidate for hydration: a stub the expander may spend
// part of its budget on.
//
// It is deliberately not a Work. The frontier query runs once per batch over a
// graph that may hold tens of thousands of stubs, and it needs three columns,
// not thirty.
type FrontierItem struct {
	OpenAlexID string
	Depth      int

	// InDegree is how many works already in this library cite this one.
	//
	// It is the ranking signal that makes expansion best-first rather than
	// breadth-first, and it is free: computed from edges already stored, with no
	// API call. A work cited by five papers the user already has is a better use
	// of the budget than one cited by a single paper, whatever the global count
	// says about either.
	InDegree int
}

// ExpansionResult is what an expansion did. It is a report, not an error type —
// a run that stops on its budget succeeded.
type ExpansionResult struct {
	SeedID string

	Hydrated   int // works fetched during this run
	Stubs      int // works left unhydrated on the frontier
	Edges      int // citation edges recorded during this run
	Unresolved int // IDs OpenAlex had no record of

	// Merged counts works OpenAlex holds twice — a second record under a new
	// ID with the same DOI — folded into the record already in the library
	// rather than added as a duplicate node (hard rule 6).
	Merged int

	// Skipped counts works left as stubs because their batch failed for a
	// reason that may not recur. §7: one bad batch must not kill a 500-node
	// run. They stay on the frontier, so the next run picks them up.
	Skipped int

	// DeadEnds counts hydrated works that came back with no reference list, so
	// the graph cannot continue through them. Reporting it is not optional
	// polish: in arts and humanities it is the great majority of the frontier
	// (D12), and a user who is not told will read a graph that stops after one
	// hop as a broken tool rather than as missing data.
	DeadEnds int

	MaxDepthReached int
	StoppedBecause  StopReason
	Duration        time.Duration
}

// ReferenceCoverage is the fraction of works hydrated in this run that arrived
// with a usable reference list, 0..1. It returns 0 for an empty run rather than
// dividing by zero.
func (r ExpansionResult) ReferenceCoverage() float64 {
	if r.Hydrated <= 0 {
		return 0
	}
	return float64(r.Hydrated-r.DeadEnds) / float64(r.Hydrated)
}

// StopReason says why an expansion ended. It is a named reason rather than a
// bool because three of the four outcomes are entirely normal and the CLI has
// something different and useful to say about each: budget exhausted invites
// --max-nodes, an empty frontier means the graph genuinely ends there, and only
// cancellation is the user's own doing.
type StopReason string

const (
	StopFrontierEmpty   StopReason = "frontier-empty"
	StopBudgetExhausted StopReason = "budget-exhausted"
	StopMaxDepth        StopReason = "max-depth"
	StopCancelled       StopReason = "cancelled"
)
