// Package retrieve answers questions over the library. M4.
//
// The pipeline: seed with hybrid search (FTS5 for keywords, sqlite-vec for
// meaning), expand along weighted citation edges, rerank, then assemble an
// answer whose citations point at passages the user actually holds.
//
// Edge weighting is not polish. Citation edges are topically noisy — a paper
// often cites another for a dataset, not an idea — so blind expansion degrades
// results with distance. Weight by co-citation before anything cleverer.
package retrieve
