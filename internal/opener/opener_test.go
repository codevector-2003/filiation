package opener

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWithinRefusesALinkLeadingOut(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root, outside := filepath.Join(dir, "lib"), filepath.Join(dir, "secret.pdf")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(root, "own.pdf")
	for _, f := range []string{outside, inside} {
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := Within(root, inside); err != nil {
		t.Errorf("a file in the library was refused: %v", err)
	}
	if err := Within(root, outside); err == nil {
		t.Error("a file outside the library was allowed")
	}
	link := filepath.Join(root, "paper.pdf")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if err := Within(root, link); err == nil {
		t.Error("a link leading out of the library was allowed")
	}
}
