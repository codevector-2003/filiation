// Package text turns a stored PDF into searchable text. M3.
//
// Three stages: extract text with layout hints, split into chunks that keep
// stable offsets back into the source page, and capture citation context — the
// sentence around each citation marker in the citing paper.
//
// The citation context is why this package has the shape it does. Nobody hands
// it to you, and it is what makes retrieval better than plain search, so it is
// captured while the document is already parsed rather than by re-parsing
// later.
//
// PDF extraction is the weakest part of the Go choice (ADR-009). Keep the
// extractor behind an interface so it can be replaced without disturbing
// chunking or context capture.
package text
