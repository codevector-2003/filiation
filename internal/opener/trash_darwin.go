//go:build darwin

package opener

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// trash moves path into the Trash. On the same disk as the home folder that
// is a rename into ~/.Trash; elsewhere it asks Finder, which knows each
// disk's own Trash. The path is handed to Finder as an argument, never as
// part of the script, so nothing in a name can run.
func trash(path string) error {
	home, err := os.UserHomeDir()
	if err == nil {
		bin := filepath.Join(home, ".Trash")
		if os.Rename(path, filepath.Join(bin, freeName(bin, filepath.Base(path)))) == nil {
			return nil
		}
	}
	out, err := exec.Command("osascript",
		"-e", "on run argv",
		"-e", `tell application "Finder" to delete (POSIX file (item 1 of argv) as alias)`,
		"-e", "end run",
		path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("Finder: %s", strings.TrimSpace(string(out)))
	}
	return nil
}
