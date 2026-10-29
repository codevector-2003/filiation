package mcpsrv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/codevector-2003/filiation/internal/config"
	"github.com/codevector-2003/filiation/internal/library"
	"github.com/codevector-2003/filiation/internal/text/pdftest"
)

// connectWithPDF serves a library folder holding the M0 seed and its PDF,
// read: the seed cites Laakso 2011 author–year on page 1.
func connectWithPDF(t *testing.T) *client {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{MaxNodes: config.DefaultMaxNodes, MaxDepth: config.DefaultMaxDepth,
		MaxRefsPerWork: config.DefaultMaxRefsPerWork}
	if err := cfg.UseFolder(filepath.Join(dir, "Lib")); err != nil {
		t.Fatal(err)
	}
	lib, err := library.Open(t.Context(), cfg, library.Options{
		CacheDir: filepath.Join(dir, "cache"), Transport: replay{t: t, routes: peerjExpansion(t)}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lib.Close() })
	ctx := t.Context()
	if _, err := lib.Add(ctx, "10.7717/peerj.4375", library.AddOptions{}); err != nil {
		t.Fatal(err)
	}
	f, err := lib.FolderFor(ctx, "10.7717/peerj.4375")
	if err != nil {
		t.Fatal(err)
	}
	pdf := pdftest.Make("Introduction\n" +
		"Open access journals grew steadily for two decades (Laakso et al., 2011).\n" +
		"We estimate that at least 28 percent of the scholarly literature is open access today,\n" +
		"and that the share is growing fastest among recent articles in every field we measured.")
	if err := os.WriteFile(filepath.Join(f.Path, "oa.pdf"), pdf, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := lib.ExtractText(ctx, library.ExtractOptions{}); err != nil {
		t.Fatal(err)
	}

	st, ct := mcp.NewInMemoryTransports()
	ss, err := New(lib, "test").Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })
	c := &client{t: t}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	c.cs = cs
	return c
}

func TestSearchLibrary(t *testing.T) {
	t.Parallel()
	c := connectWithPDF(t)

	var out searchOut
	c.call("search_library", map[string]any{"query": "28 percent"}, &out)
	if len(out.Passages) != 1 || out.Passages[0].Page != 1 || !strings.Contains(out.Passages[0].Text, "[[28]] [[percent]]") ||
		!strings.Contains(out.Passages[0].Paper.Title, "The state of OA") || out.Passages[0].PDFPath == "" {
		t.Errorf("search_library = %+v", out)
	}
	var partial searchOut
	c.call("search_library", map[string]any{"query": "percent zyxwv"}, &partial)
	if !partial.Partial || len(partial.Passages) != 1 {
		t.Errorf("partial = %+v", partial)
	}
	if msg := c.fails("search_library", map[string]any{"query": " - "}); !strings.Contains(msg, "at least one word") {
		t.Errorf("empty query: %s", msg)
	}
}

func TestGetPaper(t *testing.T) {
	t.Parallel()
	c := connectWithPDF(t)
	var out paperOut
	c.call("get_paper", map[string]any{"identifier": "10.7717/peerj.4375"}, &out)
	if out.PDF != "on_disk" || out.Text != "searchable" || out.Cites != 54 || out.Contexts != 1 ||
		len(out.Sentences) != 1 || !strings.Contains(out.Sentences[0].Cited.Title, "Open Access Journal Publishing") {
		t.Errorf("get_paper = %+v", out)
	}

	var n neighboursOut
	c.call("neighbours", map[string]any{"identifier": "10.7717/peerj.4375", "direction": "cites", "limit": 100}, &n)
	if len(n.Sentences) != 1 || !strings.HasPrefix(n.Sentences[0].Sentence, "Open access journals grew steadily") {
		t.Errorf("neighbours sentences = %+v", n.Sentences)
	}
}

// A library that is only a database file cannot hold PDFs, and says what to
// do about it.
func TestFetchPDFsNeedsAFolder(t *testing.T) {
	t.Parallel()
	c := connect(t)
	var added addOut
	c.call("add_paper", map[string]any{"identifier": "10.7717/peerj.4375"}, &added)
	if msg := c.fails("fetch_pdfs", map[string]any{}); !strings.Contains(msg, "fil library move") {
		t.Errorf("fetch_pdfs without a folder: %s", msg)
	}
	var p paperOut
	c.call("get_paper", map[string]any{"identifier": "10.7717/peerj.4375"}, &p)
	if p.PDF != "not_looked_up" || p.Text != "not_read" {
		t.Errorf("get_paper with no PDF = %+v", p)
	}
}

// library_stats reports the PDFs too: how many, how many searchable, and the
// papers without text with the reason — found missing by a real assistant,
// which had no way to list them (M3 step 10).
func TestLibraryStatsReportsPDFs(t *testing.T) {
	t.Parallel()
	c := connectWithPDF(t)
	var out statsOut
	c.call("library_stats", map[string]any{}, &out)
	if out.PDFs == nil || out.PDFs.WithPDF != 1 || out.PDFs.Searchable != 1 || out.PDFs.CitationSentences != 1 || len(out.PDFs.WithoutText) != 0 {
		t.Errorf("library_stats pdfs = %+v", out.PDFs)
	}
	db := connect(t)
	var none statsOut
	db.call("library_stats", map[string]any{}, &none)
	if none.PDFs != nil {
		t.Errorf("a library with no folder reports PDFs: %+v", none.PDFs)
	}
}
