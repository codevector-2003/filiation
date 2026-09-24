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
// Pure functions over strings. A leaf package: no I/O, and one internal import,
// errs, so that a refusal reaches the CLI as a sentinel it can branch on. errs
// imports nothing, so no cycle is possible — the same exception config takes.
package identity
