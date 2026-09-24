package main

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// frontDoors are the translation layers. Each takes a request, calls one
// function in internal/library, and formats the result. None of them may
// import internal/store: that would put SQL-shaped decisions in a place that
// has three copies, and make the storage backend impossible to swap (CLAUDE.md,
// "the rule that matters most").
var frontDoors = []string{"cmd", "internal/mcpsrv", "internal/web"}

const forbidden = "github.com/codevector-2003/filiation/internal/store"

// TestFrontDoorsDoNotImportStore enforces the rule on every go test run.
// .golangci.yml carries the same rule for linting, but a lint rule only binds
// people who run the linter; this binds everyone who runs the tests.
func TestFrontDoorsDoNotImportStore(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, dir := range frontDoors {
		violations, err := importsOf(filepath.Join(root, dir), forbidden)
		if err != nil {
			t.Fatalf("scan %s: %v", dir, err)
		}
		for _, v := range violations {
			t.Errorf("%s imports %s — front doors must go through internal/library", v, forbidden)
		}
	}
}

// The check is only worth having if it can fail, so prove it does.
func TestImportCheckCatchesAViolation(t *testing.T) {
	dir := t.TempDir()
	src := "package x\n\nimport _ \"" + forbidden + "\"\n"
	if err := os.WriteFile(filepath.Join(dir, "bad.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	violations, err := importsOf(dir, forbidden)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 1 {
		t.Errorf("violations = %v, want the one planted", violations)
	}
}

// importsOf lists the Go files under dir that import pkg, or any package
// beneath it. Test files count: a test that reaches into store from a front
// door is the first step to production code doing the same.
func importsOf(dir, pkg string) ([]string, error) {
	var found []string
	fset := token.NewFileSet()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			p, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				return err
			}
			if p == pkg || strings.HasPrefix(p, pkg+"/") {
				found = append(found, path)
			}
		}
		return nil
	})
	return found, err
}
