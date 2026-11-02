//go:build !windows

package opener

import (
	"os/exec"
	"runtime"
)

// Open opens a file in its default program, or a folder in the file manager:
// open on macOS, xdg-open elsewhere.
func Open(path string) error {
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	return launch(exec.Command(opener, path))
}
