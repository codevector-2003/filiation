// Command fil is the Filiation command-line interface.
//
// Architecture rule: this package is a translation layer. It parses input,
// calls one function in internal/library, and formats the result. It must
// never import internal/store — imports_test.go fails the build if it does.
package main

import (
	"context"
	"os"
	"os/signal"
)

// Version is set at build time:
//
//	go build -ldflags "-X main.Version=$(git describe --tags)" ./cmd/fil
var Version = "0.0.0-dev"

func main() {
	// Ctrl-C cancels the context rather than killing the process, so a write in
	// flight rolls back and the library is never left half-written.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	a := &app{
		stdin:       os.Stdin,
		stdout:      os.Stdout,
		stderr:      os.Stderr,
		interactive: isTerminal(os.Stdin) && isTerminal(os.Stdout),
	}
	os.Exit(a.run(ctx, os.Args[1:]))
}

// isTerminal reports whether f is attached to a terminal, which decides
// whether fil may ask a question or must fail with instructions instead. A
// pipe or a script gets no prompt it cannot answer.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
