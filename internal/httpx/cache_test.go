package httpx

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func newCache(t *testing.T) *DirCache {
	t.Helper()
	// A space in the path, because the real default location usually has one.
	c, err := NewDirCache(filepath.Join(t.TempDir(), "fil cache"))
	if err != nil {
		t.Fatalf("NewDirCache: %v", err)
	}
	return c
}

func TestDirCacheRoundTrip(t *testing.T) {
	t.Parallel()
	c := newCache(t)
	const key = "https://api.openalex.org/works/W1"

	if _, ok, err := c.Get(key, 0); ok || err != nil {
		t.Fatalf("Get on an empty cache = %v, %v; want a clean miss", ok, err)
	}
	// A body containing newlines, since the key is stored on the first line.
	body := []byte("{\n  \"id\": \"W1\"\n}\n")
	if err := c.Put(key, body); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, ok, err := c.Get(key, 0)
	if err != nil || !ok || string(got) != string(body) {
		t.Errorf("Get = %q, %v, %v; want the stored body", got, ok, err)
	}

	if err := c.Put(key, []byte("replaced")); err != nil {
		t.Fatalf("Put over an existing entry: %v", err)
	}
	if got, _, _ := c.Get(key, 0); string(got) != "replaced" {
		t.Errorf("Get after overwrite = %q", got)
	}
}

func TestDirCacheEmptyBody(t *testing.T) {
	t.Parallel()
	c := newCache(t)
	if err := c.Put("k", nil); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, ok, err := c.Get("k", 0)
	if !ok || err != nil || len(got) != 0 {
		t.Errorf("Get = %q, %v, %v; want a hit with an empty body", got, ok, err)
	}
}

func TestDirCacheExpires(t *testing.T) {
	t.Parallel()
	c := newCache(t)
	if err := c.Put("k", []byte("old")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(c.path("k"), old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	if _, ok, _ := c.Get("k", 24*time.Hour); ok {
		t.Errorf("a 48h-old entry was served with a 24h max age")
	}
	if _, ok, _ := c.Get("k", 0); !ok {
		t.Errorf("max age zero should mean any age")
	}
}

func TestDirCacheChecksTheKey(t *testing.T) {
	t.Parallel()
	c := newCache(t)
	// Simulate a collision: a file at k's path holding another key's response.
	if err := os.MkdirAll(filepath.Dir(c.path("k")), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.path("k"), []byte("other-key\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := c.Get("k", 0); ok {
		t.Errorf("served a response stored under a different key")
	}
}

func TestDirCacheClear(t *testing.T) {
	t.Parallel()
	c := newCache(t)
	for i := range 10 {
		if err := c.Put(fmt.Sprintf("k%d", i), []byte("x")); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}
	if err := c.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if _, ok, _ := c.Get("k0", 0); ok {
		t.Errorf("an entry survived Clear")
	}
	// The directory itself stays, so the cache is usable straight after.
	if err := c.Put("k0", []byte("x")); err != nil {
		t.Errorf("Put after Clear: %v", err)
	}
}

func TestDirCacheConcurrentWriters(t *testing.T) {
	t.Parallel()
	c := newCache(t)
	// Many goroutines writing the same key must leave one whole response,
	// never an interleaving of two.
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = c.Put("k", []byte(fmt.Sprintf("writer-%02d", i)))
		}()
	}
	wg.Wait()

	got, ok, err := c.Get("k", 0)
	if !ok || err != nil {
		t.Fatalf("Get = %v, %v", ok, err)
	}
	if len(got) != len("writer-00") || string(got[:7]) != "writer-" {
		t.Errorf("Get = %q, want one writer's whole body", got)
	}

	// And no temporary files left behind.
	entries, _ := os.ReadDir(filepath.Dir(c.path("k")))
	for _, e := range entries {
		if e.Name() != filepath.Base(c.path("k")) {
			t.Errorf("stray file %s left in the cache", e.Name())
		}
	}
}

func TestNewDirCacheRejectsEmpty(t *testing.T) {
	t.Parallel()
	if _, err := NewDirCache(""); err == nil {
		t.Error("NewDirCache(\"\") succeeded")
	}
}
