// Package config resolves where the library lives and how the tool behaves.
//
// Override order, highest first: the --db flag, FILIATION_DB, the config file,
// then a per-user application data directory (ADR-006). One library per user,
// not one per directory — a database in the working directory produces a
// scattering of half-built graphs, and the whole product thesis is a library
// that accumulates over years.
//
// Because the default location differs per OS, the CLI prints the path on first
// run. Without that, users cannot find their own data.
//
// Configuration is resolved, never prompted for. config has to work from a
// test, an MCP server and a web handler, none of which have a terminal — so it
// reports (FirstRun, Warnings) and the front door does the asking. Save is how
// an answer given once survives to the next run.
//
// A leaf package, with one exception: it imports internal/errs, so that a bad
// config file arrives at the CLI as errs.ErrInvalidConfig rather than as an
// opaque string. errs imports nothing itself, so this cannot create a cycle,
// and without the exception the sentinel package cannot do its job.
package config
