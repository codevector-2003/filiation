package blobs

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The parts of a library folder (ADR-011).
const (
	PapersDir  = "papers"
	InboxDir   = "Inbox"
	ExportsDir = "exports"
	// tmpDir sits inside the library folder, on the same drive as papers/,
	// so the final rename is atomic. A rename across drives is a copy.
	tmpDir = ".filiation/tmp"

	// staleTmp is how old a temporary file must be before Open clears it. A
	// day, not zero: an MCP server and the CLI may share a library, and one
	// must not delete the other's download in progress.
	staleTmp = 24 * time.Hour
)

// ErrNotPDF is returned when bytes meant to be a PDF are not one: a publisher's
// HTML landing page served under a .pdf link is the usual case.
var ErrNotPDF = errors.New("not a PDF")

// Folder is a library folder on disk.
type Folder struct {
	root string
}

// Open prepares the library folder at root: it creates papers/, Inbox/,
// exports/ and the temporary directory if they are missing, and clears
// temporary files a crash left behind. It creates nothing else, and touches
// nothing the researcher put there.
func Open(root string) (*Folder, error) {
	if root == "" {
		return nil, errors.New("blobs: empty library folder")
	}
	f := &Folder{root: root}
	for _, d := range []string{PapersDir, InboxDir, ExportsDir, tmpDir} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(d)), 0o755); err != nil {
			return nil, fmt.Errorf("blobs: prepare library folder: %w", err)
		}
	}
	f.clearStale(time.Now())
	return f, nil
}

// Root is the library folder.
func (f *Folder) Root() string { return f.root }

// Abs turns a stored path, relative to the library folder, into a real one.
func (f *Folder) Abs(rel string) string {
	return filepath.Join(f.root, filepath.FromSlash(rel))
}

// Rel turns a real path inside the library folder into the stored form.
func (f *Folder) Rel(abs string) (string, error) {
	rel, err := filepath.Rel(f.root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("blobs: %s is outside the library folder %s", abs, f.root)
	}
	return Clean(rel), nil
}

// MakeWorkFolder creates papers/<name> and returns its stored path. A folder
// already there is used as it is.
func (f *Folder) MakeWorkFolder(name string) (string, error) {
	rel := PapersDir + "/" + name
	if err := os.MkdirAll(f.Abs(rel), 0o755); err != nil {
		return "", fmt.Errorf("blobs: create %s: %w", rel, err)
	}
	return Clean(rel), nil
}

// RenameFolder moves a folder fil named to a new name in the same parent, and
// returns the new stored path. It refuses to overwrite.
func (f *Folder) RenameFolder(rel, name string) (string, error) {
	to := Clean(pathDir(rel) + "/" + name)
	if _, err := os.Lstat(f.Abs(to)); err == nil {
		return "", fmt.Errorf("blobs: rename %s: %s already exists", rel, to)
	}
	if err := os.Rename(f.Abs(rel), f.Abs(to)); err != nil {
		return "", fmt.Errorf("blobs: rename %s: %w", rel, err)
	}
	return to, nil
}

// File is a file fil has written or read: where it is, and what is in it.
type File struct {
	Path   string // relative to the library folder, as stored
	SHA256 string // hex
	Size   int64
}

// WriteOptions control Write.
type WriteOptions struct {
	// PDF requires the bytes to start like a PDF.
	PDF bool
	// SHA256, when set, is the hash the bytes must have.
	SHA256 string
	// MaxSize stops a runaway download. Zero means no limit.
	MaxSize int64
}

// Write stores r at rel atomically. The bytes go to a temporary file in the
// library folder, are synced and checked, and only then renamed into place;
// any failure removes the temporary file and leaves rel untouched. Write
// refuses to replace a file that is already there — whatever is at rel is
// either fil's own earlier copy or the researcher's, and neither is for
// overwriting.
func (f *Folder) Write(rel string, r io.Reader, opts WriteOptions) (File, error) {
	rel = Clean(rel)
	dst := f.Abs(rel)
	if _, err := os.Lstat(dst); err == nil {
		return File{}, fmt.Errorf("blobs: write %s: %w", rel, fs.ErrExist)
	}
	tmp, err := os.CreateTemp(f.Abs(tmpDir), "write-*.part")
	if err != nil {
		return File{}, fmt.Errorf("blobs: write %s: %w", rel, err)
	}
	done := false
	defer func() {
		if !done {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()

	h := sha256.New()
	var src io.Reader = r
	if opts.MaxSize > 0 {
		src = io.LimitReader(r, opts.MaxSize+1)
	}
	head := &headBuffer{limit: 1024}
	n, err := io.Copy(io.MultiWriter(tmp, h, head), src)
	if err != nil {
		return File{}, fmt.Errorf("blobs: write %s: %w", rel, err)
	}
	if opts.MaxSize > 0 && n > opts.MaxSize {
		return File{}, fmt.Errorf("blobs: write %s: larger than %d bytes", rel, opts.MaxSize)
	}
	if n == 0 {
		return File{}, fmt.Errorf("blobs: write %s: no bytes", rel)
	}
	if opts.PDF && !IsPDF(head.b) {
		return File{}, fmt.Errorf("blobs: write %s: %w", rel, ErrNotPDF)
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if opts.SHA256 != "" && !strings.EqualFold(opts.SHA256, sum) {
		return File{}, fmt.Errorf("blobs: write %s: hash %s, expected %s", rel, sum, opts.SHA256)
	}
	if err := tmp.Sync(); err != nil {
		return File{}, fmt.Errorf("blobs: write %s: %w", rel, err)
	}
	if err := tmp.Close(); err != nil {
		return File{}, fmt.Errorf("blobs: write %s: %w", rel, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return File{}, fmt.Errorf("blobs: write %s: %w", rel, err)
	}
	// The check above and this rename are not one step, but only fil writes
	// through here and only into folders it named.
	if err := os.Rename(tmp.Name(), dst); err != nil {
		return File{}, fmt.Errorf("blobs: write %s: %w", rel, err)
	}
	done = true
	return File{Path: rel, SHA256: sum, Size: n}, nil
}

// IsPDF reports whether b, the start of a file, is a PDF. The header may follow
// a little junk — the specification allows it within the first 1024 bytes, and
// real files take advantage.
func IsPDF(b []byte) bool {
	return bytes.Contains(b[:min(len(b), 1024)], []byte("%PDF-"))
}

// headBuffer keeps the first limit bytes written to it.
type headBuffer struct {
	b     []byte
	limit int
}

func (h *headBuffer) Write(p []byte) (int, error) {
	if room := h.limit - len(h.b); room > 0 {
		h.b = append(h.b, p[:min(room, len(p))]...)
	}
	return len(p), nil
}

// Hash reads the file at rel and returns its hash and size.
func (f *Folder) Hash(rel string) (File, error) {
	fh, err := os.Open(f.Abs(rel))
	if err != nil {
		return File{}, fmt.Errorf("blobs: hash %s: %w", rel, err)
	}
	defer fh.Close()
	h := sha256.New()
	n, err := io.Copy(h, fh)
	if err != nil {
		return File{}, fmt.Errorf("blobs: hash %s: %w", rel, err)
	}
	return File{Path: Clean(rel), SHA256: hex.EncodeToString(h.Sum(nil)), Size: n}, nil
}

// clearStale removes temporary files older than staleTmp. Failure is not an
// error: the worst case is a leftover file nobody sees.
func (f *Folder) clearStale(now time.Time) {
	dir := f.Abs(tmpDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || e.IsDir() || now.Sub(info.ModTime()) < staleTmp {
			continue
		}
		os.Remove(filepath.Join(dir, e.Name()))
	}
}

func pathDir(rel string) string {
	if i := strings.LastIndexByte(rel, '/'); i >= 0 {
		return rel[:i]
	}
	return "."
}
