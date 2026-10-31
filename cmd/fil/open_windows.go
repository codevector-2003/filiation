package main

import (
	"os"
	"os/exec"
)

// openWithSystem opens a file in its default program, or a folder in File
// Explorer. No shell is involved, so nothing in the name can run as a
// command.
func openWithSystem(path string) error {
	if fi, err := os.Stat(path); err == nil && fi.IsDir() {
		return exec.Command("explorer.exe", path).Start()
	}
	return exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", path).Start()
}
