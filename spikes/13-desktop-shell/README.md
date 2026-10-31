# Spike 13: the desktop app shell (D24)

Which native webview should the desktop app shell use? Two candidates, built as the same tiny shell:
a native window whose page is served by a plain Go `http.Handler` in the same process, which is how
fil's interface would be shown. The page times a call to the Go side, then reports that it is up.

- `wails2/`: Wails v2.17.0
- `wails3/`: Wails v3.0.0

Each is its own Go module, so Wails never enters fil's `go.mod`. `webview_go` was not tried: its
last change was in August 2024.

Run one with `SPIKE_RESULT=<file> SPIKE_AUTOQUIT=1` to get a JSON result and have it close itself.
The workflow `.github/workflows/spike-desktop-shell.yml` builds and runs both on Windows, macOS
and Linux (x86 and Arm). Results: `docs/SPIKES.md`, spike 13.
