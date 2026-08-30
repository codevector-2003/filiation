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
// Pure functions over strings. A leaf package: no I/O, no internal imports.
package identity
