package ui

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

// The screens are plain scripts sharing one global scope, so a function
// declared in two of them is not an error: the later one silently replaces
// the earlier. That once left the AI assistants screen calling the Reader's
// renderSide, and its call log blank.
func TestScriptsDeclareEachFunctionOnce(t *testing.T) {
	decl := regexp.MustCompile(`(?m)^(?:async\s+)?function\s+([A-Za-z0-9_$]+)`)
	where := map[string]string{}
	err := fs.WalkDir(assets, "assets", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".js") || strings.Contains(path, "/pdfjs/") {
			return err
		}
		src, err := fs.ReadFile(assets, path)
		if err != nil {
			return err
		}
		for _, m := range decl.FindAllStringSubmatch(string(src), -1) {
			if other, ok := where[m[1]]; ok {
				t.Errorf("function %s is declared in both %s and %s", m[1], other, path)
			}
			where[m[1]] = path
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(where) == 0 {
		t.Fatal("found no functions; is the pattern wrong?")
	}
}
