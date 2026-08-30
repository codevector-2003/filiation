// Package model holds the types shared across Filiation: Work, Edge,
// FrontierItem, ExpansionResult.
//
// A leaf package — it imports nothing else in internal/, which is what lets
// store and sources both use these types without importing each other.
//
// The one thing to get right here is that a Work may be a stub (ADR-003): the
// OpenAlex ID is known and nothing else is. Titles are therefore optional, and
// the type should make that impossible to forget rather than merely documented.
package model
