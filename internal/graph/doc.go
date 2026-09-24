// Package graph grows and traverses the citation graph.
//
// Two jobs live here:
//
//   - Expansion. Follow referenced_works outward from a seed under a node
//     budget and a priority order. At ~40 references per paper, depth 3 is
//     ~64,000 nodes, so breadth-first without a limit is never acceptable.
//   - Identity resolution. Deduplicate on openalex_id, and reconcile the DOI,
//     arXiv ID and PMID that arrive attached to the same work.
//
// This is the only package that knows about both internal/store and
// internal/sources. Keeping that pairing here is what lets store stay
// swappable.
//
// Traversal (neighbours, shortest path) is SQL — a breadth-first search, one
// set-based query per step, not a recursive CTE (see store.ShortestPath) — so it lives in
// store. Whole-graph algorithms (PageRank, communities) load edges into memory
// through gonum/graph and live here.
package graph
