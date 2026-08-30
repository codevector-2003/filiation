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
//
// The practical test: adding a command to the CLI, an MCP tool and a web
// endpoint for the same feature should mean one function here and three thin
// wrappers, not three implementations.
package library
