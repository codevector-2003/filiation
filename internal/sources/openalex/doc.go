// Package openalex is the client for the OpenAlex API — the source of works,
// metadata, resolved reference lists and open-access locations.
//
// No API key is required. The cost model was measured, not taken from the
// documentation (D11, spike 3), and it is what shapes this package:
//
//   - A single work by ID is free. A list request costs 1 credit whatever the
//     page size, and a title search costs 10. The daily allowance is 1,000.
//   - At most 100 IDs may be piped into a filter; 101 is a hard 400. So batches
//     are cut at MaxBatch, never "as many as fit".
//   - The request rate runs out long before the credits do: the first 429
//     appeared around 11 req/s, so HTTPOptions sets the bucket at 5.
//
// Batch by default, for round trips rather than credits. Send mailto because it
// is asked for; it bought no measurable throughput (D11).
//
// referenced_works arrives already resolved, inside the work object, complete.
// Do not write a reference parser (hard rule 2).
//
// This package returns model types and never imports store. Deciding what to
// write, and in which transaction, is internal/graph's job.
package openalex
