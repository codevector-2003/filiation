package zotero

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestReadExport(t *testing.T) {
	t.Parallel()
	dir := filepath.Join("testdata", "Exported Items")
	ex, err := ReadExport(filepath.Join(dir, "Exported Items.rdf"))
	if err != nil {
		t.Fatal(err)
	}
	if len(ex.Collections) != 2 || ex.Collections[0].Name != "Thesis" || ex.Collections[0].Parent != "" ||
		ex.Collections[1].Name != "Chapter 2" || ex.Collections[1].Parent != ex.Collections[0].Key {
		t.Fatalf("collections = %+v", ex.Collections)
	}
	thesis, chapter := ex.Collections[0].Key, ex.Collections[1].Key
	if len(ex.Items) != 4 {
		t.Fatalf("items = %+v", ex.Items)
	}
	byTitle := map[string]Item{}
	for _, it := range ex.Items {
		byTitle[it.Title] = it
	}

	oa := byTitle["The state of OA"]
	if oa.DOI != "10.7717/peerj.4375" || oa.Year != 2018 || !slices.Equal(oa.Creators, []string{"Heather Piwowar", "Jason Priem"}) ||
		!slices.Equal(oa.Tags, []string{"key paper"}) || !slices.Equal(oa.Collections, []string{chapter}) {
		t.Errorf("article with its DOI on its journal = %+v", oa)
	}
	if len(oa.PDFs) != 1 || oa.PDFs[0].Path != filepath.Join(dir, "files", "13", "Piwowar 2018.pdf") {
		t.Errorf("its PDF = %+v; the snapshot is left out and the escaped name found", oa.PDFs)
	}
	growth := byTitle["Growth rates of modern science"]
	if growth.DOI != "10.1007/s11192-015-1765-5" || !slices.Equal(growth.Collections, []string{thesis}) ||
		len(growth.PDFs) != 1 || growth.PDFs[0].Path != filepath.Join(dir, "files", "21", "Bornmann.pdf") {
		t.Errorf("article with a nested journal = %+v", growth)
	}
	if _, err := os.Stat(growth.PDFs[0].Path); err == nil {
		t.Error("Bornmann.pdf is meant to be missing from the export")
	}
	if a := byTitle["Attention Is All You Need"]; a.ArXiv != "1706.03762" || a.Type != "preprint" {
		t.Errorf("preprint = %+v", a)
	}
	if k := byTitle["The Structure of Scientific Revolutions"]; k.DOI != "" || k.ArXiv != "" || k.PMID != "" || k.Year != 1962 ||
		!slices.Equal(k.Creators, []string{"Thomas S. Kuhn"}) || len(k.Collections) != 0 {
		t.Errorf("book = %+v", k)
	}

	// The same export read again names everything the same.
	again, _ := ReadExport(filepath.Join(dir, "Exported Items.rdf"))
	for i := range ex.Items {
		if again.Items[i].Key != ex.Items[i].Key || ex.Items[i].Key == "" {
			t.Errorf("item %d key %q then %q", i, ex.Items[i].Key, again.Items[i].Key)
		}
	}
}

func TestReadExportRefusesOtherFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for name, body := range map[string]string{
		"library.bib": "@article{x, title={A}}",
		"other.rdf":   `<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description rdf:about="x"/></rdf:RDF>`,
		"page.xml":    `<html><body/></html>`,
	} {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte(body), 0o644)
		if _, err := ReadExport(p); !errors.Is(err, ErrNotExport) {
			t.Errorf("%s: err = %v, want ErrNotExport", name, err)
		}
	}
}
