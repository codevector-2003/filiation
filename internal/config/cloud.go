package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// CloudService names the sync service whose folder path is inside — OneDrive,
// iCloud Drive, Dropbox or Google Drive — or returns "" for an ordinary folder.
//
// It matters for one reason. SQLite in WAL mode keeps a library in three files,
// and sync tools copy them separately, which corrupts the database. So when a
// library folder syncs, its database is kept in the per-user application
// directory instead, and the folder holds everything else (ADR-011). PDFs sync
// safely; the database does not.
//
// Detection reads the path, and on Windows the variables OneDrive sets. It is a
// guard, not a guarantee: a sync tool configured somewhere unusual will not be
// seen, and fil says where the database is so a person can check.
func CloudService(path string) string {
	return cloudService(path, os.Getenv)
}

func cloudService(path string, getenv func(string) string) string {
	if path == "" {
		return ""
	}
	p := filepath.Clean(path)
	for _, v := range []string{"OneDrive", "OneDriveConsumer", "OneDriveCommercial"} {
		if root := getenv(v); root != "" && within(p, root) {
			return "OneDrive"
		}
	}
	parts := strings.FieldsFunc(filepath.ToSlash(p), func(r rune) bool { return r == '/' })
	for i, part := range parts {
		l := strings.ToLower(part)
		switch {
		case l == "onedrive" || strings.HasPrefix(l, "onedrive - ") || strings.HasPrefix(l, "onedrive-"):
			return "OneDrive"
		case l == "dropbox" || strings.HasPrefix(l, "dropbox ("):
			return "Dropbox"
		case l == "google drive" || l == "googledrive" || strings.HasPrefix(l, "googledrive-"):
			return "Google Drive"
		case l == "icloud drive" || l == "iclouddrive":
			return "iCloud Drive"
		case l == "mobile documents" && i > 0 && strings.EqualFold(parts[i-1], "Library"):
			return "iCloud Drive"
		case l == "cloudstorage" && i > 0 && strings.EqualFold(parts[i-1], "Library") && i+1 < len(parts):
			// macOS File Provider: ~/Library/CloudStorage/OneDrive-Personal,
			// Dropbox, GoogleDrive-name@example.com.
			return cloudService(filepath.Join(parts[i+1:]...), func(string) string { return "" })
		}
	}
	return ""
}

// within reports whether p is root or inside it, ignoring case where the file
// system does.
func within(p, root string) bool {
	root = filepath.Clean(root)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		p, root = strings.ToLower(p), strings.ToLower(root)
	}
	return p == root || strings.HasPrefix(p, root+string(filepath.Separator))
}
