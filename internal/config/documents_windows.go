package config

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// DocumentsDir is the user's Documents folder. Windows is asked for the known
// folder rather than assuming %USERPROFILE%\Documents, because Documents is
// often redirected — into OneDrive most of all.
func DocumentsDir() (string, error) {
	if d, err := windows.KnownFolderPath(windows.FOLDERID_Documents, windows.KF_FLAG_DEFAULT); err == nil && d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Documents"), nil
}
