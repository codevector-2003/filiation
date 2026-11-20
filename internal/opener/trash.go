package opener

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Trash moves a file or folder to the system's Recycle Bin, or Trash, where
// the person can restore it (D36). Nothing is deleted outright. Each system
// has its own way, in trash_*.go.
func Trash(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	// A drive or the root is never something fil means to throw away.
	if abs == filepath.VolumeName(abs)+string(filepath.Separator) || filepath.Dir(abs) == abs {
		return fmt.Errorf("will not move %s to the bin", abs)
	}
	if _, err := os.Lstat(abs); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil // already gone: nothing to move
		}
		return err
	}
	if err := trash(abs); err != nil {
		return fmt.Errorf("move %s to the bin: %w", filepath.Base(abs), err)
	}
	return nil
}

// freeName is a name in dir not taken yet: name, then name.2, name.3 and on,
// as the bins themselves number a second item of the same name.
func freeName(dir, name string) string {
	try := name
	for i := 2; ; i++ {
		if _, err := os.Lstat(filepath.Join(dir, try)); errors.Is(err, os.ErrNotExist) {
			return try
		}
		try = fmt.Sprintf("%s.%d", name, i)
	}
}
