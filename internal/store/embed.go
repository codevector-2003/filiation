// Package store owns every SQL statement in Filiation.
//
// This package is the swap seam (ADR-001): replacing SQLite with Postgres for
// server mode means rewriting this package and nothing else. Two rules keep
// that true:
//
//   - store never imports internal/sources, and sources never imports store.
//     Only internal/graph knows about both.
//   - No front door (cmd/, internal/mcpsrv, internal/web) may import store.
//     They call internal/library, which calls this.
//
// Writes are single-threaded. SQLite allows one writer, and Go makes that easy
// to violate by accident. M0 opens two handles: a read pool, and a write handle
// pinned to SetMaxOpenConns(1), with WAL and busy_timeout set.
package store

import _ "embed"

// Schema is the full DDL, compiled into the binary. Applied on first open and
// on version upgrade; see the meta table's schema_version row.
//
//go:embed schema.sql
var Schema string
