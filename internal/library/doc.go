// Package library is the core of Filiation. Every user-visible operation —
// add, expand, search, ask, path — is a function here.
//
// Everything else is arranged around this package:
//
//   - The front doors (cmd/, internal/mcpsrv, internal/web) are translation
//     layers. Each parses a request, calls one function here, and formats the
//     result. If a front door imports internal/store, that is a bug.
//   - This package orchestrates: it calls store, sources, graph, retrieve and
//     jobs. Those packages do not call back into it.
//   - It is the composition root: the one place that builds a store and a
//     source side by side. It never moves data between them — that is
//     internal/graph's job — so each stays swappable on its own.
//
// Nothing a front door needs from here is a store type. Results come back as
// model types or types declared in this package, and errors as errs sentinels,
// so cmd/ can branch on every outcome without importing store.
//
// The practical test: adding a command to the CLI, an MCP tool and a web
// endpoint for the same feature should mean one function here and three thin
// wrappers, not three implementations.
package library
