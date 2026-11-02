// Package ui is the interface of the Filiation desktop app: its screens,
// embedded in the binary, and the JSON handlers that feed them.
//
// It is an http.Handler, and no network is involved: the desktop shell in
// desktop/ calls it in the same process, with no port open (D25). A browser
// version can serve the same handler later, and must add the lock-down a
// listening port needs first.
//
// Same rule as the other front doors. Handlers call internal/library and format
// the result. No SQL here.
package ui
