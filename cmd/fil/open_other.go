//go:build !windows

package main

import (
	"os/exec"
	"runtime"
)

// openWithSystem opens a file in its default program, or a folder in the
// file manager: open on macOS, xdg-open elsewhere. No shell is involved.
func openWithSystem(path string) error {
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	return exec.Command(opener, path).Start()
}
