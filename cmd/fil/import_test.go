package main

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codevector-2003/filiation/internal/text/pdftest"
)

// seedPage is the M0 seed's first page as PeerJ sets it, with title as its
// title: the DOI in the header, the authors, and a cited DOI in the text.
func seedPage(title string) []byte { return pdftest.Make(seedPageText(title)) }

func seedPageText(title string) string {
	return "PeerJ\nDOI 10.7717/peerj.4375\n" + title + "\nHeather Piwowar, Jason Priem, Vincent Lariviere\n" +
		"Despite growing interest in open access, there is an unmet need for large-scale studies\n" +
		"of its prevalence. Earlier work (doi:10.1000/earlier.1) estimated it from one sample only.\n" +
		"We estimate that at least 28 percent of the scholarly literature is open access today."
}

const seedTitle = "The state of OA: a large-scale analysis of the prevalence and impact of Open Access articles"

func (h *harness) inbox(name string, pdf []byte) string {
	h.t.Helper()
	p := filepath.Join(h.documentsDir, "Filiation", "My Library", "Inbox", name)
	if err := os.WriteFile(p, pdf, 0o644); err != nil {
		h.t.Fatal(err)
	}
	return p
}

func TestImportCommand(t *testing.T) {
	routes := maps.Clone(peerj)
	routes["/works/doi:10.1000/earlier.1"] = "404"
	h := newHarness(t, routes)
	if code, _, errOut := h.run(false, "", "add", "10.7717/peerj.4375"); code != exitOK {
		t.Fatalf("add: exit %d\n%s", code, errOut)
	}

	// Dropped in Inbox/: identified by the DOI in the header, filed, read.
	h.inbox("download.pdf", seedPage(seedTitle))
	code, out, errOut := h.run(false, "", "import")
	if code != exitOK {
		t.Fatalf("import: exit %d\n%s\n%s", code, out, errOut)
	}
	for _, want := range []string{"✓ download.pdf", "The state of OA", "identified by a DOI label in page 1's header",
		"Text:  searchable", "Imported: 1 PDF."} {
		if !strings.Contains(out, want) {
			t.Errorf("import lacks %q:\n%s", want, out)
		}
	}

	// The DOI names another paper: not filed, and the way to file it said.
	bees := h.inbox("bees.pdf", seedPage("A field guide to the solitary bees of northern Europe"))
	_, out, _ = h.run(false, "", "import", "--searches", "0")
	for _, want := range []string{"· bees.pdf", "not filed: its identifier names a work whose title is not in the PDF",
		"--as W2741809807", "fil-report.txt"} {
		if !strings.Contains(out, want) {
			t.Errorf("import of a wrong DOI lacks %q:\n%s", want, out)
		}
	}
	if _, out, _ := h.run(false, "", "stats"); !strings.Contains(out, "Inbox:      1 PDF waiting") {
		t.Errorf("stats does not count the waiting PDF:\n%s", out)
	}
	// Named by hand, at a terminal: asked, and filed on yes.
	code, out, _ = h.run(true, "y\n", "import", bees, "--as", "10.7717/peerj.4375")
	if code != exitOK || !strings.Contains(out, "is not in the PDF") || !strings.Contains(out, "✓ bees.pdf") {
		t.Errorf("--as with a mismatch, confirmed: exit %d\n%s", code, out)
	}

	// A local document.
	notes := filepath.Join(t.TempDir(), "notes.pdf")
	os.WriteFile(notes, pdftest.Make("Field notes on solitary bees, kept over three summers behind the lab.\n"+
		"Nest counts rose each year after the hedgerow was replanted along the eastern edge."), 0o644)
	code, out, _ = h.run(false, "", "import", notes, "--local", "Field notes on solitary bees", "--year", "2025")
	if code != exitOK || !strings.Contains(out, "✓ notes.pdf") || !strings.Contains(out, "Field notes on solitary bees (2025") {
		t.Errorf("--local: exit %d\n%s", code, out)
	}
	if _, err := os.Stat(notes); err != nil {
		t.Errorf("the original of a copied file is gone: %v", err)
	}
	if _, out, _ := h.run(false, "", "search", "hedgerow"); !strings.Contains(out, "Field notes on solitary bees") {
		t.Errorf("the local document is not searchable:\n%s", out)
	}

	// Flags that do not go together.
	if code, _, errOut := h.run(false, "", "import", "--as", "10.7717/peerj.4375"); code != exitInvalidInput || !strings.Contains(errOut, "name one file") {
		t.Errorf("--as with no file: exit %d\n%s", code, errOut)
	}
}

func TestImportCommandChoosesACandidate(t *testing.T) {
	routes := maps.Clone(peerj)
	maps.Copy(routes, attention)
	h := newHarness(t, routes)
	if code, _, errOut := h.run(false, "", "add", "10.7717/peerj.4375"); code != exitOK {
		t.Fatalf("add: exit %d\n%s", code, errOut)
	}
	h.inbox("transformer.pdf", pdftest.Doc{Title: "Attention is all you need",
		Pages: []string{"Ashish Vaswani, Noam Shazeer, Niki Parmar\nThe dominant sequence transduction models are based on\n" +
			"complex recurrent or convolutional neural networks that include an encoder and a decoder."}}.Bytes())

	code, out, errOut := h.run(true, "1\n", "import")
	if code != exitOK {
		t.Fatalf("import: exit %d\n%s\n%s", code, out, errOut)
	}
	for _, want := range []string{"Which paper is it?", "1. ✓ Attention Is All You Need", "✓ transformer.pdf",
		"Imported: 1 PDF; 1 title searched (10 credits)."} {
		if !strings.Contains(out, want) {
			t.Errorf("import lacks %q:\n%s", want, out)
		}
	}
}

// Importing the PDFs you have is a way to start: on a first run, fil import
// with a file makes the library, as fil add does. A missing file makes
// nothing.
func TestImportOnAFirstRun(t *testing.T) {
	h := newHarness(t, maps.Clone(peerj))
	if code, _, errOut := h.run(false, "", "import", filepath.Join(t.TempDir(), "missing.pdf")); code == exitOK ||
		!strings.Contains(errOut, "missing.pdf") {
		t.Errorf("a missing file: exit %d\n%s", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(h.documentsDir, "Filiation")); !os.IsNotExist(err) {
		t.Errorf("a typo made a library: %v", err)
	}

	file := filepath.Join(t.TempDir(), "oa.pdf")
	os.WriteFile(file, seedPage(seedTitle), 0o644)
	code, out, errOut := h.run(false, "", "import", file)
	if code != exitOK || !strings.Contains(out, "✓ oa.pdf") || !strings.Contains(errOut, "Created your library") {
		t.Errorf("first run: exit %d\n%s\n%s", code, out, errOut)
	}
}

// fil neighbours --context shows the sentence each reference is cited in,
// once the paper's PDF has been read; fil stats counts them.
func TestNeighboursWithContext(t *testing.T) {
	h := newHarness(t, maps.Clone(peerj))
	if code, _, errOut := h.run(false, "", "add", "10.7717/peerj.4375"); code != exitOK {
		t.Fatalf("add: exit %d\n%s", code, errOut)
	}
	_, out, _ := h.run(false, "", "neighbours", "10.7717/peerj.4375", "--context")
	if !strings.Contains(out, "No citation sentences") {
		t.Errorf("before reading:\n%s", out)
	}
	file := filepath.Join(t.TempDir(), "oa.pdf")
	os.WriteFile(file, pdftest.Make(seedPageText(seedTitle)+
		"\nOpen access journals grew steadily for two decades (Laakso et al., 2011)."), 0o644)
	if code, out, errOut := h.run(false, "", "import", file); code != exitOK {
		t.Fatalf("import: exit %d\n%s\n%s", code, out, errOut)
	}
	code, out, errOut := h.run(false, "", "neighbours", "10.7717/peerj.4375", "--context")
	if code != exitOK || !strings.Contains(out, "The Development of Open Access Journal Publishing") ||
		!strings.Contains(out, "p. 1: “Open access journals grew steadily for two decades") ||
		!strings.Contains(out, "Sentences found for 1 of 54 references.") {
		t.Errorf("neighbours --context: exit %d\n%s\n%s", code, out, errOut)
	}
	if _, out, _ := h.run(false, "", "stats"); !strings.Contains(out, "Context:    1 of 54 citations") {
		t.Errorf("stats:\n%s", out)
	}
}
