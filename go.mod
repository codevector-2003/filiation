module github.com/codevector-2003/filiation

go 1.26.0

// Added with `go get` as each milestone needs it. Check the licence of every
// one before adding it, and remember: no dependency may require cgo (D9).
//
// In:
//   M0  github.com/ncruces/go-sqlite3                SQLite, WASM, no cgo (ADR-007) · MIT
//   M0  github.com/BurntSushi/toml                   config file (step 3) · MIT, no cgo
//   M0  golang.org/x/time/rate                       token bucket (step 6, ADR-004) · BSD-3, no cgo.
//                                                    Pinned at v0.15.0 while fil was on Go 1.25.
//   M0  github.com/spf13/cobra                       CLI (step 9) · Apache-2.0, no cgo. Brings
//                                                    spf13/pflag (BSD-3) and, on Windows only,
//                                                    inconshreveable/mousetrap (Apache-2.0).
//   M2  github.com/modelcontextprotocol/go-sdk       MCP server · MIT, moving to Apache-2.0,
//                                                    no cgo. Brings google/jsonschema-go and
//                                                    segmentio/encoding (MIT), uritemplate,
//                                                    x/oauth2 and x/sync (BSD-3).
//   M3  github.com/klippa-app/go-pdfium              PDF text: PDFium compiled to WebAssembly
//                                                    (D20) · MIT, no cgo. Its webassembly
//                                                    package alone is linked: wazero (Apache-2.0),
//                                                    go-commons-pool (Apache-2.0), google/uuid
//                                                    (BSD-3). Requires Go 1.26. Adds ~10 MB.
//   M3  github.com/tetratelabs/wazero                runs PDFium · Apache-2.0, pure Go. Named
//                                                    directly for its runtime and cache config.
//
// Planned:
//   M1  gonum.org/v1/gonum/graph                     PageRank, communities
//   M4  github.com/asg017/sqlite-vec-go-bindings     vectors, pairs with the driver above
//
// Not taken:
//   github.com/adrg/xdg — suggested by ADR-006 for the per-OS library path, but
//   os.UserConfigDir is stdlib and already correct on all three targets. xdg
//   only earns its place if the full spec (separate data, cache and state
//   directories) is ever needed. It is not.

require (
	github.com/BurntSushi/toml v1.6.0
	github.com/charmbracelet/bubbles v1.0.0
	github.com/charmbracelet/bubbletea v1.3.10
	github.com/charmbracelet/lipgloss v1.1.0
	github.com/klippa-app/go-pdfium v1.21.3
	github.com/modelcontextprotocol/go-sdk v1.8.0
	github.com/ncruces/go-sqlite3 v0.35.3
	github.com/spf13/cobra v1.10.2
	github.com/tetratelabs/wazero v1.12.0
	golang.org/x/sys v0.48.0
	golang.org/x/text v0.42.0
	golang.org/x/time v0.15.0
)

require (
	github.com/atotto/clipboard v0.1.4 // indirect
	github.com/aymanbagabas/go-osc52/v2 v2.0.1 // indirect
	github.com/charmbracelet/colorprofile v0.4.1 // indirect
	github.com/charmbracelet/x/ansi v0.11.6 // indirect
	github.com/charmbracelet/x/cellbuf v0.0.15 // indirect
	github.com/charmbracelet/x/term v0.2.2 // indirect
	github.com/clipperhouse/displaywidth v0.9.0 // indirect
	github.com/clipperhouse/stringish v0.1.1 // indirect
	github.com/clipperhouse/uax29/v2 v2.5.0 // indirect
	github.com/erikgeiser/coninput v0.0.0-20211004153227-1c3628e74d0f // indirect
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/jolestar/go-commons-pool/v2 v2.1.2 // indirect
	github.com/lucasb-eyer/go-colorful v1.3.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-localereader v0.0.1 // indirect
	github.com/mattn/go-runewidth v0.0.19 // indirect
	github.com/muesli/ansi v0.0.0-20230316100256-276c6243b2f6 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/muesli/termenv v0.16.0 // indirect
	github.com/ncruces/go-sqlite3-wasm/v3 v3.2.35304 // indirect
	github.com/ncruces/julianday v1.0.0 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/segmentio/asm v1.1.3 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/spf13/pflag v1.0.9 // indirect
	github.com/xo/terminfo v0.0.0-20220910002029-abceb7e1c41e // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	golang.org/x/oauth2 v0.35.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
)
