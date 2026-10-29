package blobs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ReportName is the note fil leaves in Inbox/ saying why each file still
// there was not filed, so a researcher who only looks at the folder still
// finds out.
const ReportName = "fil-report.txt"

// Inbox lists the PDFs in Inbox/, at any depth, oldest first by path.
// Hidden files and system litter are skipped, as in papers/.
func (f *Folder) Inbox() ([]Entry, error) {
	root := f.Abs(InboxDir)
	var out []Entry
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == root {
				return err
			}
			return nil
		}
		if p == root {
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, ".") || junk[strings.ToLower(name)] {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() || !strings.EqualFold(filepath.Ext(name), ".pdf") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, err := f.Rel(p)
		if err != nil {
			return err
		}
		out = append(out, Entry{Path: rel, Size: info.Size(), ModTime: info.ModTime()})
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("blobs: list %s: %w", root, err)
	}
	return out, nil
}

// MoveInto moves the file at rel into the folder dir, keeping its name — or
// "name-2.pdf" and on if the name is taken: nothing is ever overwritten. It
// returns the new path. Both must be inside the library folder, so the move
// is a rename on one volume.
func (f *Folder) MoveInto(rel, dir string) (string, error) {
	src := f.Abs(Clean(rel))
	ext := filepath.Ext(rel)
	stem := strings.TrimSuffix(Base(Clean(rel)), ext)
	for n := 1; n <= 100; n++ {
		name := stem + ext
		if n > 1 {
			name = fmt.Sprintf("%s-%d%s", stem, n, ext)
		}
		to := Clean(dir + "/" + name)
		dst := f.Abs(to)
		if _, err := os.Lstat(dst); err == nil {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return "", fmt.Errorf("blobs: move %s: %w", rel, err)
		}
		if err := os.Rename(src, dst); err != nil {
			return "", fmt.Errorf("blobs: move %s: %w", rel, err)
		}
		return to, nil
	}
	return "", fmt.Errorf("blobs: move %s into %s: %w", rel, dir, fs.ErrExist)
}

// WriteReport replaces Inbox/fil-report.txt with text, or removes it when
// text is empty. It is fil's own file, so replacing it is allowed.
func (f *Folder) WriteReport(text string) error {
	p := f.Abs(InboxDir + "/" + ReportName)
	if text == "" {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("blobs: remove %s: %w", p, err)
		}
		return nil
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, []byte(text), 0o644); err != nil {
		return fmt.Errorf("blobs: write %s: %w", p, err)
	}
	if err := os.Rename(tmp, p); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("blobs: write %s: %w", p, err)
	}
	return nil
}
