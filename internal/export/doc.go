// Package export writes the library out as GraphML, JSON and BibTeX. M1.
//
// No lock-in is a product commitment, not a feature: everything the tool knows
// must be extractable in a format something else can read. GraphML is the one
// that matters first, because the M1 acceptance test is a 500-node graph that
// opens cleanly in Gephi.
//
// Every exporter has to survive a graph that is mostly stubs — works with an ID
// and no title. Filter them or label them, but never crash on them.
package export
