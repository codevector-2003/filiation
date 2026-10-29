// Package pdftest writes small, valid PDFs for tests: real files PDFium
// opens, made in code, so no binary sits in testdata and every test says
// exactly what its PDF contains.
package pdftest

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
)

// Make returns a PDF with one page per element of pages. Each line of a
// page's text is set as one line in Helvetica; an empty page has no text at
// all, only a grey box — what a scanned page looks like to a text extractor.
// Text must be ASCII.
func Make(pages ...string) []byte { return Doc{Pages: pages}.Bytes() }

// Doc is a PDF to write.
type Doc struct {
	// Title, if set, is set in 20-point type above page 1's text, as a
	// paper's title is.
	Title string
	Pages []string
	// Info is the document information dictionary: Title, Subject, a doi
	// key, as publishers write them.
	Info map[string]string
}

// Bytes writes the PDF.
func (d Doc) Bytes() []byte {
	var objs []string
	// 1: catalog, 2: page tree, 3: font; then a page and its content stream
	// for each page, then the information dictionary.
	kids := make([]string, len(d.Pages))
	for i := range d.Pages {
		kids[i] = fmt.Sprintf("%d 0 R", 4+2*i)
	}
	objs = append(objs,
		"<< /Type /Catalog /Pages 2 0 R >>",
		fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(d.Pages)),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
	)
	for i, text := range d.Pages {
		title := ""
		if i == 0 {
			title = d.Title
		}
		objs = append(objs,
			fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] "+
				"/Resources << /Font << /F1 3 0 R >> >> /Contents %d 0 R >>", 5+2*i),
			stream(content(title, text)))
	}
	info := ""
	if len(d.Info) > 0 {
		keys := make([]string, 0, len(d.Info))
		for k := range d.Info {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b strings.Builder
		b.WriteString("<<")
		for _, k := range keys {
			fmt.Fprintf(&b, " /%s (%s)", k, escape(d.Info[k]))
		}
		b.WriteString(" >>")
		objs = append(objs, b.String())
		info = fmt.Sprintf(" /Info %d 0 R", len(objs))
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
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R%s >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, info, xref)
	return b.Bytes()
}

func content(title, text string) string {
	if title == "" && text == "" {
		return "0.8 g 72 72 468 648 re f"
	}
	var c strings.Builder
	y := 740
	if title != "" {
		fmt.Fprintf(&c, "BT /F1 20 Tf 72 %d Td (%s) Tj ET\n", y, escape(title))
		y -= 36
	}
	fmt.Fprintf(&c, "BT /F1 10 Tf 12 TL 72 %d Td\n", y)
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
