// Package mcpsrv exposes the library over the Model Context Protocol. M2.
//
// A translation layer, not a home for logic: each tool definition validates its
// arguments, calls one function in internal/library, and marshals the result.
// It must never import internal/store.
//
// M2 is cheap because M1 already did the work, and it is the part no existing
// citation-map tool has — an assistant that can answer "what connects these two
// papers in my library" against a graph the user owns.
package mcpsrv
