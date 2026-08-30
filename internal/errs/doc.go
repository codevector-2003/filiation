// Package errs holds the sentinel errors shared across Filiation.
//
// Callers compare with errors.Is; everything else wraps with
// fmt.Errorf("...: %w", err). The sentinels live here rather than in the
// package that returns them, so that checking for one does not force an import
// of a package the caller has no other business knowing about.
package errs
