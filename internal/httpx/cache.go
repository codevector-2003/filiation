package httpx

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Cache stores successful response bodies by request key, so that re-running an
// expansion while debugging does not re-pay for requests already made (ADR-004).
//
// A cache must never be the reason a request fails. The client treats a Get
// error as a miss and ignores a Put error: a full disk or an unreadable file
// costs a refetch, not a failed expansion.
type Cache interface {
	// Get returns the body stored for key, if there is one no older than
	// maxAge. A maxAge of zero means any age.
	Get(key string, maxAge time.Duration) (body []byte, ok bool, err error)
	Put(key string, body []byte) error
}

// DirCache is a Cache kept as one file per response under a directory.
//
// ADR-004 specified a separate SQLite file. A directory was chosen instead, for
// two reasons. Every SQL statement in Filiation lives in internal/store, and a
// second database owned by this package would break that rule for a key-value
// lookup that needs no SQL. And every connection in the WASM driver is a
// separate instance, which is a real cost to pay for a cache. A directory keeps
// what the ADR wanted — the cache is separate from the library and can be
// deleted at any time without touching the user's data — and a person can
// inspect it with ls.
//
// Each file starts with the key on its own line, then the body. The key is
// checked on read, so the file name being a hash is never trusted on its own.
type DirCache struct {
	dir string
}

// NewDirCache opens, creating if needed, a cache rooted at dir.
func NewDirCache(dir string) (*DirCache, error) {
	if dir == "" {
		return nil, errors.New("httpx: cache directory is empty")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("httpx: create cache directory: %w", err)
	}
	return &DirCache{dir: dir}, nil
}

// Dir is where the cache lives, for `fil cache clear` to report.
func (c *DirCache) Dir() string { return c.dir }

// Get implements Cache.
func (c *DirCache) Get(key string, maxAge time.Duration) ([]byte, bool, error) {
	path := c.path(key)
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("httpx: cache stat: %w", err)
	}
	if maxAge > 0 && time.Since(info.ModTime()) > maxAge {
		return nil, false, nil
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, false, fmt.Errorf("httpx: cache read: %w", err)
	}
	storedKey, body, found := bytes.Cut(raw, []byte("\n"))
	if !found || string(storedKey) != key {
		// A hash collision, or a file truncated mid-write by a crash before
		// the rename below existed. Either way it is not this key's response.
		return nil, false, nil
	}
	return body, true, nil
}

// Put implements Cache. It writes to a temporary file and renames it into
// place, so a reader never sees a half-written response and two writers of the
// same key cannot interleave.
func (c *DirCache) Put(key string, body []byte) error {
	path := c.path(key)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("httpx: cache mkdir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".put-*")
	if err != nil {
		return fmt.Errorf("httpx: cache temp file: %w", err)
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck // gone after a successful rename

	_, err = tmp.Write(append(append([]byte(key), '\n'), body...))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("httpx: cache write: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("httpx: cache rename: %w", err)
	}
	return nil
}

// Clear deletes every cached response. It is `fil cache clear`, which ADR-004
// requires from day one: a stale cache can hide a bug, and the user needs a way
// to rule it out.
func (c *DirCache) Clear() error {
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return fmt.Errorf("httpx: read cache directory: %w", err)
	}
	var failed []error
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(c.dir, e.Name())); err != nil {
			failed = append(failed, err)
		}
	}
	if err := errors.Join(failed...); err != nil {
		return fmt.Errorf("httpx: clear cache: %w", err)
	}
	return nil
}

// path shards by the first two hex characters of the key's hash, so a large
// cache does not put tens of thousands of files in one directory.
func (c *DirCache) path(key string) string {
	sum := sha256.Sum256([]byte(key))
	name := hex.EncodeToString(sum[:])
	return filepath.Join(c.dir, name[:2], name)
}
