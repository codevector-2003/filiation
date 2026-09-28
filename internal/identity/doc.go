// Package identity classifies and normalises whatever the user typed.
//
// Input arrives as a DOI in five formats, an arXiv ID in two generations, a
// bare OpenAlex ID, a URL, a PMID, or a title typed from memory. The first five
// resolve deterministically and silently.
//
// A title does not. Title search returns candidates and requires confirmation —
// interactively in the CLI, or via an explicit --accept-first in scripts
// (ADR-005). The asymmetry is deliberate: a wrong seed is not a small error, it
// poisons everything expanded from it, and the user may not notice for weeks.
//
// A malformed identifier is refused with errs.ErrInvalidInput, never passed on
// to a title search: that would ask the user to pick a candidate for something
// that was never a title.
//
// It also owns fil IDs (D17): Filiation's own name for a work, "F" and eight
// Crockford base32 characters, generated here and parsed here like every other
// identifier. A fil ID names a work already in the library; OpenAlex has never
// heard of one.
//
// Pure functions over strings, except NewFilID, which reads crypto/rand. A leaf package: no I/O, and one internal import,
// errs, so that a refusal reaches the CLI as a sentinel it can branch on. errs
// imports nothing, so no cycle is possible — the same exception config takes.
package identity
