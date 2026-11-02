# The desktop app

A native window showing the interface in [`internal/ui`](../internal/ui), in the same process and with no port open. It opens the same library `fil` uses. D24 and D25 in [`docs/DECISIONS.md`](../docs/DECISIONS.md) say why it is built this way.

This is its own Go module, and the one place cgo is allowed.

## Build and run

| System | Needs | Command |
| --- | --- | --- |
| Windows | WebView2, present on Windows 10 and 11 | `go build -tags desktop,production -ldflags "-H windowsgui" -o Filiation.exe .` |
| macOS | Xcode command line tools | `CGO_LDFLAGS="-framework UniformTypeIdentifiers" go build -tags desktop,production -o Filiation .` |
| Linux | `libgtk-3-dev`, `libwebkit2gtk-4.1-dev` | `go build -tags desktop,production,webkit2_41 -o Filiation .` |

The screens need no separate build step: they are plain files in `internal/ui/assets`, embedded in the binary.
