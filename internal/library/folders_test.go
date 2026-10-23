package library

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codevector-2003/filiation/internal/config"
	"github.com/codevector-2003/filiation/internal/model"
)

const pdfBytes = "%PDF-1.7\n1 0 obj\n<<>>\nendobj\n%%EOF\n"

// openFolderTest opens a library folder in a temporary directory, with the
// seed of M0 added.
func openFolderTest(t *testing.T) (*Library, model.Work) {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		MaxNodes:          config.DefaultMaxNodes,
		MaxDepth:          config.DefaultMaxDepth,
		MaxRefsPerWork:    config.DefaultMaxRefsPerWork,
		FolderCollections: true,
	}
	if err := cfg.UseFolder(filepath.Join(dir, "My Library")); err != nil {
		t.Fatal(err)
	}
	l, err := Open(t.Context(), cfg, Options{CacheDir: filepath.Join(dir, "cache"), Transport: &replay{t: t, routes: peerj}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	added, err := l.Add(t.Context(), "10.7717/peerj.4375", AddOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return l, added.Work
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func rename(t *testing.T, from, to string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(from, to); err != nil {
		t.Fatal(err)
	}
}

func rescan(t *testing.T, l *Library) ScanReport {
	t.Helper()
	r, err := l.Scan(t.Context())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	return r
}

// M3 step 3, done when: a work folder can be created, moved by hand and found
// again (ARCHITECTURE_PHASE3.md §9).
func TestWorkFolderIsCreatedMovedAndFoundAgain(t *testing.T) {
	t.Parallel()
	l, seed := openFolderTest(t)
	ctx := t.Context()
	papers := filepath.Join(l.Folder(), "papers")
	for _, d := range []string{"papers", "Inbox", "exports"} {
		if _, err := os.Stat(filepath.Join(l.Folder(), d)); err != nil {
			t.Errorf("%s not created: %v", d, err)
		}
	}

	f, err := l.FolderFor(ctx, "10.7717/peerj.4375")
	if err != nil {
		t.Fatal(err)
	}
	name := "Piwowar-2018-The-state-of-OA-a-large-scale-analysis-of-the-prevalence-and--" + seed.FilID
	if !f.Created || f.Path != filepath.Join(papers, name) {
		t.Fatalf("FolderFor = %+v; want %s created", f, name)
	}
	writeFile(t, filepath.Join(f.Path, "published.pdf"), pdfBytes)
	writeFile(t, filepath.Join(f.Path, "my notes.txt"), "read p. 4")

	if r := rescan(t, l); r.Folders != 1 || r.Files != 2 || r.NewFiles != 2 {
		t.Errorf("first scan = %+v", r)
	}
	again, _ := l.FolderFor(ctx, seed.FilID)
	if again.Created || again.Path != f.Path || len(again.Files) != 2 {
		t.Errorf("second FolderFor = %+v; want the same folder, with both files", again)
	}
	if again.Files[1].Version != "published" {
		t.Errorf("files = %+v; published.pdf is the published version", again.Files)
	}

	// The researcher files it under a chapter.
	chapter := filepath.Join(papers, "Chapter 2 - Open access", name)
	rename(t, f.Path, chapter)
	r := rescan(t, l)
	if len(r.Moved) != 1 || r.Moved[0].ByHash || r.NewFiles != 0 || len(r.Missing) != 0 {
		t.Errorf("scan after a move = %+v", r)
	}
	if found, _ := l.FolderFor(ctx, seed.FilID); found.Path != chapter || found.Created || len(found.Files) != 2 {
		t.Errorf("FolderFor after a move = %+v", found)
	}
	cs, _ := l.Collections(ctx)
	if len(cs) != 1 || cs[0].Name != "Chapter 2 - Open access" || cs[0].Works != 1 || !cs[0].FromFolder {
		t.Errorf("collections = %+v; the folder is a collection of one", cs)
	}

	// Renamed by hand, keeping the fil ID: found, and not renamed back.
	mine := filepath.Join(papers, "Chapter 2 - Open access", "Piwowar OA study--"+strings.ToLower(seed.FilID))
	rename(t, chapter, mine)
	if found, _ := l.FolderFor(ctx, seed.FilID); found.Path != mine || found.Renamed != "" {
		t.Errorf("FolderFor after a rename = %+v; a person's name is kept", found)
	}

	// The fil ID taken off the name: found by the hash of what is inside.
	stripped := filepath.Join(papers, "Piwowar OA study")
	rename(t, mine, stripped)
	r = rescan(t, l)
	if len(r.Moved) != 1 || !r.Moved[0].ByHash || len(r.Missing) != 0 || len(r.Loose) != 0 || r.MissingFiles != 0 {
		t.Errorf("scan after the ID was removed = %+v", r)
	}
	if found, _ := l.FolderFor(ctx, seed.FilID); found.Path != stripped || len(found.Files) != 2 {
		t.Errorf("FolderFor after the ID was removed = %+v", found)
	}
	if r := rescan(t, l); len(r.Moved) != 0 || r.Folders != 1 || r.Files != 2 {
		t.Errorf("a second scan with nothing changed = %+v", r)
	}

	// Deleted: missing, and the paper is still in the graph.
	if err := os.RemoveAll(stripped); err != nil {
		t.Fatal(err)
	}
	r = rescan(t, l)
	if len(r.Missing) != 1 || r.MissingFiles != 2 {
		t.Errorf("scan after a delete = %+v", r)
	}
	if s, _ := l.Stats(ctx); s.Works != 55 {
		t.Errorf("works after the folder was deleted = %d, want 55", s.Works)
	}
	if r := rescan(t, l); len(r.Missing) != 0 || r.MissingFiles != 2 {
		t.Errorf("missing is reported once, then counted: %+v", r)
	}
}

// A dropped PDF is left where it is and reported for import (ADR-014); a
// folder named for a work in some other library is reported, not adopted.
func TestScanReportsWhatItCannotPlace(t *testing.T) {
	t.Parallel()
	l, _ := openFolderTest(t)
	papers := filepath.Join(l.Folder(), "papers")
	writeFile(t, filepath.Join(papers, "Chapter 3", "downloaded.pdf"), pdfBytes)
	writeFile(t, filepath.Join(papers, "Someone-2020-Else--F3QD8WN2T", "published.pdf"), pdfBytes)
	writeFile(t, filepath.Join(papers, ".trash", "x.pdf"), pdfBytes)

	r := rescan(t, l)
	if len(r.Loose) != 1 || !strings.HasSuffix(r.Loose[0], "downloaded.pdf") {
		t.Errorf("loose = %v", r.Loose)
	}
	if len(r.Unknown) != 1 || r.Folders != 0 || r.Files != 0 {
		t.Errorf("scan = %+v", r)
	}
	if _, err := os.Stat(filepath.Join(papers, "Chapter 3", "downloaded.pdf")); err != nil {
		t.Error("a dropped PDF was moved")
	}
}

// fil renames a folder it named when the name would change — a stub has been
// fetched and has a title — and never one a person named.
func TestFolderIsRenamedOnlyWhileItHasFilsName(t *testing.T) {
	t.Parallel()
	l, _ := openFolderTest(t)
	ctx := t.Context()

	n, err := l.Neighbours(ctx, "10.7717/peerj.4375")
	if err != nil || !n.Cites[len(n.Cites)-1].IsStub() {
		t.Fatalf("Neighbours = %v; want a stub to name a folder for", err)
	}
	stub := &n.Cites[len(n.Cites)-1]
	f, err := l.FolderFor(ctx, stub.FilID)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(f.Path) != "Unknown-nd-Untitled--"+stub.FilID {
		t.Fatalf("stub folder = %s", f.Path)
	}

	title, year := "Anatomy of green open access", 2014
	stub.Title, stub.Year, stub.Hydrated = &title, &year, true
	stub.Authors = []model.Author{{OpenAlexID: "A1", Name: "Bo-Christer Björk"}}
	if err := l.db.UpsertWork(ctx, stub); err != nil {
		t.Fatal(err)
	}
	f, err = l.FolderFor(ctx, stub.FilID)
	if err != nil {
		t.Fatal(err)
	}
	want := "Bjork-2014-Anatomy-of-green-open-access--" + stub.FilID
	if filepath.Base(f.Path) != want || f.Renamed != "Unknown-nd-Untitled--"+stub.FilID {
		t.Errorf("after fetching = %+v; want renamed to %s", f, want)
	}

	// A person renames it; a corrected title must not undo their choice.
	theirs := filepath.Join(filepath.Dir(f.Path), "Björk green OA--"+stub.FilID)
	rename(t, f.Path, theirs)
	title = "Anatomy of Green Open Access"
	if err := l.db.UpsertWork(ctx, stub); err != nil {
		t.Fatal(err)
	}
	if f, _ := l.FolderFor(ctx, stub.FilID); f.Path != theirs || f.Renamed != "" {
		t.Errorf("FolderFor = %+v; want the person's name kept", f)
	}
}

func TestFolderNeedsALibraryFolder(t *testing.T) {
	t.Parallel()
	l, _ := openTest(t, peerj, Options{})
	if _, err := l.Scan(t.Context()); !errors.Is(err, ErrNoFolder) {
		t.Errorf("Scan = %v, want ErrNoFolder", err)
	}
	if l.Folder() != "" {
		t.Errorf("Folder = %q", l.Folder())
	}
}

// A v0.2 library — a database with no folder — moves into a library folder:
// the database is copied and checked, the settings switched, the old file
// kept.
func TestMoveGivesAV02LibraryAFolder(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	root := t.TempDir()
	old := filepath.Join(root, "AppData", "library.db")
	settings := filepath.Join(root, "settings")
	writeFile(t, filepath.Join(settings, config.FileName), "db_path = '"+old+"'\n")
	load := func() *config.Config {
		t.Helper()
		cfg, err := config.Load(config.Options{Dir: settings, DocumentsDir: filepath.Join(root, "Documents")})
		if err != nil {
			t.Fatal(err)
		}
		return cfg
	}

	cfg := load()
	l, err := Open(ctx, cfg, Options{CacheDir: filepath.Join(root, "cache"), Transport: &replay{t: t, routes: peerj}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Add(ctx, "10.7717/peerj.4375", AddOptions{}); err != nil {
		t.Fatal(err)
	}
	l.Close()

	dest := filepath.Join(root, "Documents", "Filiation", "Thesis")
	moved, err := Move(ctx, cfg, dest)
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if moved.Folder != dest || moved.Database != filepath.Join(dest, config.LibraryDBName) || moved.OldDatabase != old {
		t.Errorf("Move = %+v", moved)
	}
	if _, err := os.Stat(old); err != nil {
		t.Errorf("the old database was not kept: %v", err)
	}

	cfg = load()
	if cfg.Folder != dest || cfg.Name != "Thesis" {
		t.Fatalf("settings after the move open %+v", cfg)
	}
	l, err = Open(ctx, cfg, Options{NoCache: true})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if s, _ := l.Stats(ctx); s.Works != 55 || s.Edges != 54 {
		t.Errorf("moved library = %+v", s)
	}
	if _, err := os.Stat(filepath.Join(dest, "papers")); err != nil {
		t.Errorf("papers/ not created: %v", err)
	}

}

func TestMoveAFolderLibrary(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	root := t.TempDir()
	settings := filepath.Join(root, "settings")
	cfg, err := config.Load(config.Options{Dir: settings, DocumentsDir: filepath.Join(root, "Documents")})
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	l, err := Open(ctx, cfg, Options{CacheDir: filepath.Join(root, "cache"), Transport: &replay{t: t, routes: peerj}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Add(ctx, "10.7717/peerj.4375", AddOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.FolderFor(ctx, "10.7717/peerj.4375"); err != nil {
		t.Fatal(err)
	}
	l.Close()

	dest := filepath.Join(root, "Research", "My Library")
	if _, err := Move(ctx, cfg, dest); err != nil {
		t.Fatalf("Move: %v", err)
	}
	cfg, err = config.Load(config.Options{Dir: settings})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Folder != dest || cfg.Name != "My Library" || len(cfg.Libraries) != 1 {
		t.Fatalf("after the move = %+v", cfg)
	}
	l, err = Open(ctx, cfg, Options{NoCache: true})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	f, err := l.FolderFor(ctx, "10.7717/peerj.4375")
	if err != nil || f.Created || !strings.HasPrefix(f.Path, dest) {
		t.Errorf("work folder after the move = %+v, %v", f, err)
	}
}

func TestCreateRefusesAFolderInUse(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	cfg, err := config.Load(config.Options{Dir: filepath.Join(root, "settings"), DocumentsDir: filepath.Join(root, "Documents")})
	if err != nil {
		t.Fatal(err)
	}
	busy := filepath.Join(root, "Busy")
	writeFile(t, filepath.Join(busy, "thesis.docx"), "x")
	if _, err := Create(t.Context(), cfg, busy, false); err == nil {
		t.Error("Create used a folder with someone's files in it")
	}
	made, err := Create(t.Context(), cfg, filepath.Join(root, "Side project"), true)
	if err != nil {
		t.Fatal(err)
	}
	again, err := config.Load(config.Options{Dir: filepath.Join(root, "settings")})
	if err != nil || again.Folder != made.Folder || again.Name != "Side project" {
		t.Errorf("after Create = %+v, %v", again, err)
	}
}
