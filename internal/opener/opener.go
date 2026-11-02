// Package opener hands a file or folder to the operating system: a file to
// its default program, a folder to the file manager. No shell is involved,
// so nothing in a name can run as a command.
package opener

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Within refuses a path that leads out of root: a link inside the folder can
// point anywhere, so it compares where each really is.
func Within(root, path string) error {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(realRoot, realPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("%s leads outside the library folder", filepath.Base(path))
	}
	return nil
}

// launch starts cmd without waiting for it, and collects it once it exits,
// however long the program that started it runs.
func launch(cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}
