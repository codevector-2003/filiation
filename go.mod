module github.com/codevector-2003/filiation

go 1.25.0

// Added with `go get` as each milestone needs it. Check the licence of every
// one before adding it, and remember: no dependency may require cgo (D9).
//
// In:
//   M0  github.com/ncruces/go-sqlite3                SQLite, WASM, no cgo (ADR-007) · MIT
//   M0  github.com/BurntSushi/toml                   config file (step 3) · MIT, no cgo
//   M0  golang.org/x/time/rate                       token bucket (step 6, ADR-004) · BSD-3, no cgo.
//                                                    Pinned at v0.15.0: v0.16.0 requires Go 1.26.
//
// Planned:
//   M0  github.com/spf13/cobra                       CLI
//   M1  gonum.org/v1/gonum/graph                     PageRank, communities
//   M2  github.com/modelcontextprotocol/go-sdk       MCP server
//   M4  github.com/asg017/sqlite-vec-go-bindings     vectors, pairs with the driver above
//
// Not taken:
//   github.com/adrg/xdg — suggested by ADR-006 for the per-OS library path, but
//   os.UserConfigDir is stdlib and already correct on all three targets. xdg
//   only earns its place if the full spec (separate data, cache and state
//   directories) is ever needed. It is not.

require (
	github.com/BurntSushi/toml v1.6.0
	github.com/ncruces/go-sqlite3 v0.35.3
	golang.org/x/time v0.15.0
)

require (
	github.com/ncruces/go-sqlite3-wasm/v3 v3.2.35304 // indirect
	github.com/ncruces/julianday v1.0.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
)
