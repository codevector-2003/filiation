//go:build !windows

package config

import (
	"os"
	"path/filepath"
	"strings"
)

// DocumentsDir is the user's Documents folder. Linux reads XDG_DOCUMENTS_DIR,
// which user-dirs sets, and falls back to ~/Documents; macOS has only the
// latter.
func DocumentsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if d := strings.TrimSpace(os.Getenv("XDG_DOCUMENTS_DIR")); d != "" {
		d = strings.ReplaceAll(d, "$HOME", home)
		if filepath.IsAbs(d) {
			return d, nil
		}
	}
	return filepath.Join(home, "Documents"), nil
}
