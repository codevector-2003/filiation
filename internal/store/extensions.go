package store

import (
	"sync"

	"github.com/ncruces/go-sqlite3"
	_ "github.com/ncruces/go-sqlite3/driver" // registers the "sqlite3" driver with database/sql
	"github.com/ncruces/go-sqlite3/ext/fts5"
)

// FTS5 is not compiled into the WASM build of SQLite that this driver ships
// (spike 5). It arrives as a loadable extension instead, and it has to be
// registered on every connection.
//
// This is one of the two silent failures in the storage layer, and it is silent
// in a way that misleads: a connection opened without FTS5 cannot even *read* a
// table that was created with it, and the error surfaces at query time rather
// than at open time. So it reads as a broken query, not as a missing extension.
//
// The registration therefore lives in init rather than in Open. sqlite3.AutoExtension
// applies to connections opened after it is called, so anything that opens a
// connection before registering gets a handle that is quietly wrong for the
// life of the process. Package initialisation is the only point guaranteed to
// come first, and since store owns every SQL statement in Filiation, importing
// store is the one thing every caller already does.
//
// vec1 is deliberately not registered yet. The vector table in schema.sql is
// still commented out, so there is nothing a connection could fail to read, and
// carrying the extension for a milestone that has not started buys nothing.
// **When the vec table is uncommented at M4, its Register must be added here**
// — not at the call site, or the same silent failure comes back wearing a
// different hat.
func init() {
	registerExtensions()
}

var registerOnce sync.Once

// registerExtensions is idempotent. AutoExtension appends to a global list that
// runs against every new connection, so calling it twice would run the same
// registration twice per connection. Tests call this to state the dependency
// out loud rather than relying on init having happened.
func registerExtensions() {
	registerOnce.Do(func() {
		sqlite3.AutoExtension(fts5.Register)
	})
}
