package main

import (
	"os"
	"os/exec"
)

// openWithSystem opens a file in its default program, or a folder in File
// Explorer. No shell is involved, so nothing in the name can run as a
// command.
func openWithSystem(path string) error {
	cmd := exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", path)
	if fi, err := os.Stat(path); err == nil && fi.IsDir() {
		cmd = exec.Command("explorer.exe", path)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() // collects it once it exits, however long a session runs
	return nil
}
