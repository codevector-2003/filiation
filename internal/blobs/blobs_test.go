package blobs

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codevector-2003/filiation/internal/model"
)

func work(author, title string, year int) model.Work {
	w := model.Work{FilID: "F7K2M9QXA"}
	if author != "" {
		w.Authors = []model.Author{{Name: author}}
	}
	if title != "" {
		w.Title = &title
	}
	if year != 0 {
		w.Year = &year
	}
	return w
}

func TestFolderName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		w    model.Work
		want string
	}{
		{"the seed", work("Heather Piwowar", "The state of OA: a large-scale analysis of the prevalence and impact of Open Access articles", 2018),
			"Piwowar-2018-The-state-of-OA-a-large-scale-analysis-of-the-prevalence-and--F7K2M9QXA"},
		{"accents fold", work("Vincent Larivière", "Étude sur l'accès ouvert", 2014),
			"Lariviere-2014-Etude-sur-l-acces-ouvert--F7K2M9QXA"},
		{"letters with no decomposition", work("Łukasz Großmann", "Ærøskøbing", 2020),
			"Grossmann-2020-AEroskobing--F7K2M9QXA"},
		{"comma form", work("Piwowar, Heather", "Data", 2011), "Piwowar-2011-Data--F7K2M9QXA"},
		{"characters Windows forbids", work("A B", `What? <is> "this": a/b\c|d*`, 2001),
			"B-2001-What-is-this-a-b-c-d--F7K2M9QXA"},
		{"nothing known", work("", "", 0), "Unknown-nd-Untitled--F7K2M9QXA"},
		{"a title with no Latin letters", work("李 王", "注意力机制", 2019), "Unknown-2019-Untitled--F7K2M9QXA"},
		{"one very long word", work("X Y", strings.Repeat("a", 90), 1999),
			"Y-1999-" + strings.Repeat("a", 60) + "--F7K2M9QXA"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FolderName(tt.w)
			if got != tt.want {
				t.Errorf("FolderName = %q\n             want %q", got, tt.want)
			}
			if strings.ContainsAny(got, `<>:"/\|?* `) {
				t.Errorf("%q holds a character Windows forbids", got)
			}
			if id, ok := FolderID(got); !ok || id != "F7K2M9QXA" {
				t.Errorf("FolderID(%q) = %q, %v", got, id, ok)
			}
		})
	}
	// The seed's sample from ADR-012 fits the 60-character title budget.
	if n := len(FolderName(tests[0].w)); n > 100 {
		t.Errorf("seed folder name is %d characters", n)
	}
}

func TestFolderID(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]string{
		"Piwowar-2018-The-state-of-OA--F7K2M9QXA": "F7K2M9QXA",
		"renamed by hand--f7k2m9qxa":              "F7K2M9QXA",
		"Piwowar-2018-The-state-of-OA":            "",
		"Chapter 2 - Open access":                 "",
		"Notes--FREQUENTS":                        "", // a word, not an ID: no digit
		"a--F7K2M9QXA--extra":                     "",
	} {
		got, ok := FolderID(name)
		if got != want || ok != (want != "") {
			t.Errorf("FolderID(%q) = %q, %v; want %q", name, got, ok, want)
		}
	}
}

func TestClean(t *testing.T) {
	t.Parallel()
	decomposed := "papers/Larivieré" // e + combining acute, as macOS stores it
	for in, want := range map[string]string{
		`papers\Chapter 2\x.pdf`: "papers/Chapter 2/x.pdf",
		"./papers/a/../b":        "papers/b",
		decomposed:               "papers/Larivieré",
	} {
		if got := Clean(in); got != want {
			t.Errorf("Clean(%q) = %q, want %q", in, got, want)
		}
	}
}

const pdf = "%PDF-1.7\n1 0 obj\n<<>>\nendobj\n%%EOF\n"

func TestWriteIsAtomicAndChecked(t *testing.T) {
	t.Parallel()
	f, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir, err := f.MakeWorkFolder("Piwowar-2018-The-state-of-OA--F7K2M9QXA")
	if err != nil {
		t.Fatal(err)
	}

	got, err := f.Write(dir+"/published.pdf", strings.NewReader(pdf), WriteOptions{PDF: true})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got.Size != int64(len(pdf)) || len(got.SHA256) != 64 || got.Path != dir+"/published.pdf" {
		t.Errorf("Write = %+v", got)
	}
	again, err := f.Hash(got.Path)
	if err != nil || again != got {
		t.Errorf("Hash = %+v, %v; want %+v", again, err, got)
	}

	// Never over what is there.
	if _, err := f.Write(got.Path, strings.NewReader(pdf), WriteOptions{}); !errors.Is(err, fs.ErrExist) {
		t.Errorf("second write = %v, want fs.ErrExist", err)
	}

	// An HTML page under a .pdf link, a wrong hash, an empty body and a
	// runaway download all leave nothing behind.
	bad := []struct {
		body string
		opts WriteOptions
	}{
		{"<!doctype html><title>Sign in</title>", WriteOptions{PDF: true}},
		{pdf, WriteOptions{SHA256: strings.Repeat("0", 64)}},
		{"", WriteOptions{}},
		{pdf, WriteOptions{MaxSize: 10}},
	}
	for i, b := range bad {
		rel := dir + "/accepted.pdf"
		if _, err := f.Write(rel, strings.NewReader(b.body), b.opts); err == nil {
			t.Errorf("case %d: Write succeeded", i)
		}
		if _, err := os.Stat(f.Abs(rel)); !os.IsNotExist(err) {
			t.Errorf("case %d: a file was left at %s", i, rel)
		}
	}
	if left, _ := os.ReadDir(f.Abs(tmpDir)); len(left) != 0 {
		t.Errorf("temporary files left behind: %v", left)
	}
	if _, err := f.Write(dir+"/x.pdf", strings.NewReader("<html>"), WriteOptions{PDF: true}); !errors.Is(err, ErrNotPDF) {
		t.Errorf("HTML as PDF = %v, want ErrNotPDF", err)
	}
}

func TestOpenClearsOnlyStaleTemporaryFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	f, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	old, fresh := f.Abs(tmpDir+"/old.part"), f.Abs(tmpDir+"/fresh.part")
	for _, p := range []string{old, fresh} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("a stale temporary file survived")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Error("a download in progress was deleted")
	}
	for _, d := range []string{PapersDir, InboxDir, ExportsDir} {
		if _, err := os.Stat(filepath.Join(root, d)); err != nil {
			t.Errorf("%s not created: %v", d, err)
		}
	}
}

func TestWalk(t *testing.T) {
	t.Parallel()
	f, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	files := []string{
		"papers/Piwowar-2018-The-state-of-OA--F7K2M9QXA/published.pdf",
		"papers/Piwowar-2018-The-state-of-OA--F7K2M9QXA/notes/margin.txt",
		"papers/Chapter 2/Harnad-2004-Access--F3QD8WN2T/preprint.pdf",
		"papers/Chapter 2/Harnad 2004 annotated.pdf",
		"papers/Chapter 2/Key studies/readme.txt",
		"papers/dropped.PDF",
		"papers/.hidden/x.pdf",
		"papers/Thumbs.db",
	}
	for _, p := range files {
		abs := f.Abs(p)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(pdf), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	l, err := f.Walk()
	if err != nil {
		t.Fatal(err)
	}
	if len(l.WorkFolders) != 2 {
		t.Fatalf("work folders = %+v", l.WorkFolders)
	}
	byID := map[string]WorkFolder{}
	for _, w := range l.WorkFolders {
		byID[w.FilID] = w
	}
	if w := byID["F7K2M9QXA"]; len(w.Files) != 2 || w.Path != "papers/Piwowar-2018-The-state-of-OA--F7K2M9QXA" {
		t.Errorf("seed folder = %+v; want both files, at any depth", w)
	}
	if w := byID["F3QD8WN2T"]; w.Path != "papers/Chapter 2/Harnad-2004-Access--F3QD8WN2T" {
		t.Errorf("nested work folder = %+v", w)
	}
	var loose []string
	for _, e := range l.Loose {
		if e.IsPDF() {
			loose = append(loose, e.Path)
		}
	}
	if len(l.Loose) != 3 {
		t.Errorf("loose files = %+v; want the two PDFs and the readme", l.Loose)
	}
	if strings.Join(loose, "|") != "papers/Chapter 2/Harnad 2004 annotated.pdf|papers/dropped.PDF" {
		t.Errorf("loose = %v", loose)
	}
	if strings.Join(l.Folders, "|") != "papers/Chapter 2|papers/Chapter 2/Key studies" {
		t.Errorf("folders = %v", l.Folders)
	}
}
