# web/ui

Source for the local web interface (M5).

Built output goes to `internal/web/dist`, which is embedded into the binary with
`//go:embed` and is not committed. Nothing here is wired up yet — the milestone
that fills this directory also adds the build step and the embed directive.

Graph rendering needs WebGL. An SVG force layout stalls somewhere under 2,000
nodes, and a 500-node graph is the *small* case here.
