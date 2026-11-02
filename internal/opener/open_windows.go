package opener

import (
	"os"
	"os/exec"
)

// Open opens a file in its default program, or a folder in File Explorer.
func Open(path string) error {
	if fi, err := os.Stat(path); err == nil && fi.IsDir() {
		return exec.Command("explorer.exe", path).Start()
	}
	return exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", path).Start()
}
