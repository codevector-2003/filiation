//go:build !windows && !darwin

package opener

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// trash moves path to the desktop's Trash. gio, part of GLib on most Linux
// desktops, does it as the file manager would, including on other disks.
// Without it, the freedesktop.org trash layout is written directly: the item
// under Trash/files, and a note under Trash/info of where it came from, which
// is what lets the file manager restore it.
func trash(path string) error {
	if gio, err := exec.LookPath("gio"); err == nil {
		if exec.Command(gio, "trash", "--", path).Run() == nil {
			return nil
		}
		// gio failed; the layout below may still work.
	}
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		data = filepath.Join(home, ".local", "share")
	}
	files, info := filepath.Join(data, "Trash", "files"), filepath.Join(data, "Trash", "info")
	for _, dir := range []string{files, info} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	name := freeName(files, filepath.Base(path))
	note := fmt.Sprintf("[Trash Info]\nPath=%s\nDeletionDate=%s\n",
		(&url.URL{Path: path}).EscapedPath(), time.Now().Format("2006-01-02T15:04:05"))
	notePath := filepath.Join(info, name+".trashinfo")
	if err := os.WriteFile(notePath, []byte(note), 0o600); err != nil {
		return err
	}
	if err := os.Rename(path, filepath.Join(files, name)); err != nil {
		os.Remove(notePath)
		if strings.Contains(err.Error(), "cross-device") {
			return fmt.Errorf("it is on another disk from the Trash, and gio is not installed")
		}
		return err
	}
	return nil
}
