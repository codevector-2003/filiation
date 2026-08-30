// Package openalex is the client for the OpenAlex API — the source of works,
// metadata, resolved reference lists and open-access locations.
//
// No API key is required. Always send mailto=<contact> to stay in the polite
// pool. Billing shapes the design: a list request costs 10 credits and returns
// up to 50 works, while a single lookup costs 1, so fetching works one at a
// time costs roughly 5x what batching by ID filter costs. Batch by default.
//
// referenced_works arrives already resolved. Do not write a reference parser.
package openalex
