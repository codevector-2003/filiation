// Package web serves the local single-page app: graph view, search, reader,
// notes. M5.
//
// The SPA is built from web/ui into internal/web/dist and compiled into the
// binary with a //go:embed directive, which is why M5 is shorter here than it
// would be elsewhere — there are no assets to ship and no paths to configure.
// That directive lands with the first real build: dist/ is gitignored, and
// //go:embed fails to compile against a missing directory.
//
// Same rule as the other front doors. Handlers call internal/library and format
// the result. No SQL here.
package web
