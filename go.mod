module github.com/codevector-2003/filiation

go 1.24

// No dependencies yet — `go build ./...` works out of the box.
//
// Planned, added with `go get` as each milestone needs it. Check the licence of
// every one before adding it, and remember: no dependency may require cgo (D9).
//
//   M0  github.com/spf13/cobra                       CLI
//   M0  github.com/ncruces/go-sqlite3                SQLite, WASM, no cgo (ADR-007)
//   M0  golang.org/x/time/rate                       token bucket (ADR-004)
//   M0  github.com/adrg/xdg                          per-OS library path (ADR-006)
//   M1  gonum.org/v1/gonum/graph                     PageRank, communities
//   M2  github.com/modelcontextprotocol/go-sdk       MCP server
//   M4  github.com/asg017/sqlite-vec-go-bindings     vectors, pairs with the driver above
