// Package model holds the types shared across Filiation: Work, HydratedWork,
// Edge, Author, FrontierItem, ExpansionResult.
//
// A leaf package — it imports nothing else in internal/, which is what lets
// store and sources both use these types without importing each other.
//
// The one thing to get right here is that a Work may be a stub (ADR-003): the
// OpenAlex ID is known and nothing else is. Two conventions carry that through
// the codebase, and both are narrower than "the compiler prevents mistakes", so
// it is worth being exact about what each one buys:
//
//   - Optional fields are pointers where NULL and the zero value mean different
//     things. This does not stop anyone constructing a nonsensical Work. What it
//     does, at every call site, is force a reader of Title or Year or DOI to
//     acknowledge that the value may be absent, rather than handing back a zero
//     that reads like data. Use DisplayTitle for anything a person will see.
//
//   - State a Work cannot answer for itself does not live on Work. A reference
//     list belongs to HydratedWork, because a Work loaded from the store has no
//     reference list and would answer the dead-end question wrongly rather than
//     refusing it. In-graph in-degree is likewise on FrontierItem and computed
//     from cites, not carried on the work.
package model
