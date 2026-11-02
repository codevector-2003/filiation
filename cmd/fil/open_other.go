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
	cmd := exec.Command(opener, path)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() // collects it once it exits, however long a session runs
	return nil
}
