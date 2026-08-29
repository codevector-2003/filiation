// Command fil is the Filiation command-line interface.
//
// This is a placeholder entry point so the module builds from day one and the
// cross-compile can be verified in week 1. Real command wiring lands in M0,
// using cobra; the flag parsing below is deliberately throwaway.
//
// Architecture rule: this package is a translation layer. It parses input,
// calls one function in internal/library, and formats the result. It must
// never import internal/store.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Version is set at build time:
//
//	go build -ldflags "-X main.Version=$(git describe --tags)" ./cmd/fil
var Version = "0.0.0-dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "version", "-v", "--version":
		fmt.Printf("fil %s (%s/%s, %s)\n",
			Version, runtime.GOOS, runtime.GOARCH, runtime.Version())
	case "where":
		// ADR-006: one library per user, not one per directory. The CLI must
		// print this on first run or nobody will find their library.
		fmt.Println(defaultLibraryPath())
	default:
		fmt.Fprintf(os.Stderr, "fil: unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `fil — a citation graph and retrieval engine you run yourself

Usage:
  fil version     print the version
  fil where       print the library location

Not implemented yet (see docs/PRODUCT_PLAN.md):
  fil add         add a paper by DOI, arXiv ID or title        M0
  fil expand      follow references and grow the graph         M1
  fil path        shortest citation chain between two papers   M1
  fil search      search the library                           M4
  fil ask         ask a question, get cited answers            M4
`)
}

// defaultLibraryPath is a stand-in. M0 replaces this with github.com/adrg/xdg,
// which gets the per-OS conventions right rather than approximating them.
func defaultLibraryPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return filepath.Join(".", "filiation")
	}
	return filepath.Join(dir, "filiation")
}
