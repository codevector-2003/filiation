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
// A leaf package: it imports nothing else in internal/.
package config
