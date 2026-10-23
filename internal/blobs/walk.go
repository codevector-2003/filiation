package blobs

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"time"
)

// Listing is what is in papers/ right now, before anyone has decided what any
// of it means.
type Listing struct {
	// WorkFolders are folders whose name ends in a fil ID, wherever they sit.
	WorkFolders []WorkFolder
	// Loose are files that are not inside a work folder: PDFs dropped in by
	// the researcher, or the contents of a work folder whose fil ID was taken
	// off its name.
	Loose []Entry
	// Folders are the researcher's own folders — every folder that is not a
	// work folder or inside one — relative to the library folder.
	Folders []string
}

// WorkFolder is a folder named for one work.
type WorkFolder struct {
	Path  string // relative to the library folder
	FilID string // as written in the name, normalised; may be an alias
	Files []Entry
}

// Entry is one file.
type Entry struct {
	Path    string // relative to the library folder
	Size    int64
	ModTime time.Time
}

// IsPDF reports whether the entry is named as a PDF.
func (e Entry) IsPDF() bool { return strings.EqualFold(filepath.Ext(e.Path), ".pdf") }

// Walk lists papers/. It reads directory entries only — no file is opened —
// so it stays well under a second for tens of thousands of folders.
//
// Hidden files and folders, and the litter operating systems leave behind,
// are skipped. Symbolic links are not followed. Everything inside a work
// folder belongs to that work, at any depth; a work folder inside another
// work folder is treated as part of the outer one.
func (f *Folder) Walk() (Listing, error) {
	var l Listing
	papers := f.Abs(PapersDir)
	var current *WorkFolder
	err := filepath.WalkDir(papers, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == papers {
				return err
			}
			return nil // a folder we cannot read is skipped, not fatal
		}
		if p == papers {
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, ".") || junk[strings.ToLower(name)] {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		rel, err := f.Rel(p)
		if err != nil {
			return err
		}
		if current != nil && !strings.HasPrefix(rel, current.Path+"/") {
			current = nil
		}
		if d.IsDir() {
			if current != nil {
				return nil
			}
			if id, ok := FolderID(name); ok {
				l.WorkFolders = append(l.WorkFolders, WorkFolder{Path: rel, FilID: id})
				current = &l.WorkFolders[len(l.WorkFolders)-1]
				return nil
			}
			l.Folders = append(l.Folders, rel)
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		e := Entry{Path: rel, Size: info.Size(), ModTime: info.ModTime()}
		if current != nil {
			current.Files = append(current.Files, e)
		} else {
			l.Loose = append(l.Loose, e)
		}
		return nil
	})
	if err != nil {
		return Listing{}, fmt.Errorf("blobs: list %s: %w", papers, err)
	}
	return l, nil
}

// junk is what operating systems and sync tools leave in folders.
var junk = map[string]bool{
	"desktop.ini": true, "thumbs.db": true, "icon\r": true,
}

// Dir is the folder a stored path is in, "." for one at the top.
func Dir(rel string) string { return pathDir(rel) }

// Base is the last element of a stored path.
func Base(rel string) string {
	if i := strings.LastIndexByte(rel, '/'); i >= 0 {
		return rel[i+1:]
	}
	return rel
}
