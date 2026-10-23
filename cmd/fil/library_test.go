package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codevector-2003/filiation/internal/config"
)

// M3 step 3 from the command line: the library is a folder, a paper's folder
// is made on request, and one moved by hand is found again.
func TestLibraryFolderCommands(t *testing.T) {
	h := newHarness(t, peerj)
	if code, _, errOut := h.run(false, "", "add", "10.7717/peerj.4375"); code != exitOK {
		t.Fatalf("add: exit %d\n%s", code, errOut)
	}
	folder := filepath.Join(h.documentsDir, "Filiation", "My Library")

	code, out, errOut := h.run(false, "", "library", "folder", "10.7717/peerj.4375")
	if code != exitOK || !strings.Contains(errOut, "Made a folder for The state of OA") {
		t.Fatalf("library folder: exit %d\n%s\n%s", code, out, errOut)
	}
	made := strings.TrimSpace(out)
	if filepath.Dir(made) != filepath.Join(folder, "papers") || !strings.HasPrefix(filepath.Base(made), "Piwowar-2018-The-state-of-OA") {
		t.Fatalf("folder = %q", made)
	}
	if err := os.WriteFile(filepath.Join(made, "published.pdf"), []byte("%PDF-1.7\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	moved := filepath.Join(folder, "papers", "Chapter 2", filepath.Base(made))
	if err := os.MkdirAll(filepath.Dir(moved), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(made, moved); err != nil {
		t.Fatal(err)
	}
	code, out, _ = h.run(false, "", "library", "scan")
	if code != exitOK || !strings.Contains(out, "1 with a folder, 1 file") || !strings.Contains(out, "moved: papers/") {
		t.Errorf("library scan: exit %d\n%s", code, out)
	}

	code, out, _ = h.run(false, "", "library")
	for _, want := range []string{"Library:  My Library", "Folder:   " + folder, "Collections: Chapter 2 (1)"} {
		if !strings.Contains(out, want) {
			t.Errorf("library lacks %q (exit %d):\n%s", want, code, out)
		}
	}
	if _, out, _ = h.run(false, "", "library", "folder", "10.7717/peerj.4375"); strings.TrimSpace(out) != moved {
		t.Errorf("folder after the move = %q, want %q", strings.TrimSpace(out), moved)
	}
}

// A v0.2 library is a database with no folder. It keeps working, explains
// what it lacks, and moves into a folder on request.
func TestLibraryMoveFromV02(t *testing.T) {
	h := newHarness(t, peerj)
	old := filepath.Join(t.TempDir(), "library.db")
	if err := os.MkdirAll(h.configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.configDir, config.FileName), []byte("db_path = '"+old+"'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := h.run(false, "", "add", "10.7717/peerj.4375"); code != exitOK {
		t.Fatalf("add: exit %d\n%s", code, errOut)
	}
	if code, _, errOut := h.run(false, "", "library", "scan"); code == exitOK || !strings.Contains(errOut, "fil library move") {
		t.Errorf("scan of a v0.2 library: exit %d\n%s", code, errOut)
	}

	dest := filepath.Join(h.documentsDir, "Filiation", "Thesis")
	code, out, errOut := h.run(false, "", "library", "move", dest)
	if code != exitOK || !strings.Contains(out, "Your library is now at "+dest) || !strings.Contains(out, old) {
		t.Fatalf("library move: exit %d\n%s\n%s", code, out, errOut)
	}
	_, out, _ = h.run(false, "", "stats")
	if !strings.Contains(out, "Library:    "+dest) || !strings.Contains(out, "55") {
		t.Errorf("stats after the move:\n%s", out)
	}
}
