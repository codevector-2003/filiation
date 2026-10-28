// Package pdftest writes small, valid PDFs for tests: real files PDFium
// opens, made in code, so no binary sits in testdata and every test says
// exactly what its PDF contains.
package pdftest

import (
	"bytes"
	"fmt"
	"strings"
)

// Make returns a PDF with one page per element of pages. Each line of a
// page's text is set as one line in Helvetica; an empty page has no text at
// all, only a grey box — what a scanned page looks like to a text extractor.
// Text must be ASCII.
func Make(pages ...string) []byte {
	var objs []string
	// 1: catalog, 2: page tree, 3: font; then a page and its content stream
	// for each page.
	kids := make([]string, len(pages))
	for i := range pages {
		kids[i] = fmt.Sprintf("%d 0 R", 4+2*i)
	}
	objs = append(objs,
		"<< /Type /Catalog /Pages 2 0 R >>",
		fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(pages)),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
	)
	for i, text := range pages {
		objs = append(objs,
			fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] "+
				"/Resources << /Font << /F1 3 0 R >> >> /Contents %d 0 R >>", 5+2*i),
			stream(content(text)))
	}

	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return b.Bytes()
}

func content(text string) string {
	if text == "" {
		return "0.8 g 72 72 468 648 re f"
	}
	var c strings.Builder
	c.WriteString("BT /F1 10 Tf 12 TL 72 740 Td\n")
	for i, line := range strings.Split(text, "\n") {
		if i > 0 {
			c.WriteString("T*\n")
		}
		fmt.Fprintf(&c, "(%s) Tj\n", escape(line))
	}
	c.WriteString("ET")
	return c.String()
}

func stream(s string) string {
	return fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(s), s)
}

func escape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `(`, `\(`, `)`, `\)`).Replace(s)
}
