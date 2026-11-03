package zotero

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// A Zotero RDF export (File, Export Library, Zotero RDF, with Export Files) is
// the way in for a Zotero that is not running here: an older one, or one on
// another computer. It is RDF/XML as Zotero's own "Zotero RDF" translator
// writes it, with each attached file copied beside it under files/.
//
// What the translator does that matters here:
//
//   - An item is a node with z:itemType. Its subject is its ISBN, its URL or
//     "#item_" and Zotero's local row number, none of which is Zotero's key,
//     so an item is known again by what it says (exportKey).
//   - A journal article's DOI is on its journal: the node dcterms:isPartOf
//     points at, as dc:identifier "DOI 10...". Other DOIs are on the item.
//   - Extra is dc:description; a URL is a dc:identifier node's rdf:value.
//   - Tags are dc:subject: a literal is the user's own, a node typed
//     z:AutomaticTag one Zotero or a publisher added.
//   - An attachment is a node linked by link:link, with link:type and z:path
//     (rdf:resource in older versions) relative to the export.
//   - A collection is a z:Collection node whose dcterms:hasPart are its items
//     and the collections inside it.

// ErrNotExport: the file is not a Zotero RDF export.
var ErrNotExport = errors.New("not a Zotero RDF export")

const (
	nsRDF     = "http://www.w3.org/1999/02/22-rdf-syntax-ns#"
	nsZ       = "http://www.zotero.org/namespaces/export#"
	nsDC      = "http://purl.org/dc/elements/1.1/"
	nsDCTerms = "http://purl.org/dc/terms/"
	nsBib     = "http://purl.org/net/biblio#"
	nsFOAF    = "http://xmlns.com/foaf/0.1/"
	nsLink    = "http://purl.org/rss/1.0/modules/link/"
)

// Export is a Zotero library read from an export file.
type Export struct {
	Collections []Collection
	Items       []Item
}

// ReadExport reads the Zotero RDF export at path. Each PDF's Path is where
// the export says its file is, whether or not it is there.
func ReadExport(path string) (Export, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Export{}, err
	}
	g, err := parseRDF(data)
	if err != nil {
		return Export{}, fmt.Errorf("%w: %v", ErrNotExport, err)
	}
	if !g.zotero {
		return Export{}, ErrNotExport
	}
	return g.export(filepath.Dir(path)), nil
}

// arc is one statement about a subject: a literal, or another node.
type arc struct {
	pred    string
	obj     string
	literal bool
}

type rdfGraph struct {
	order  []string         // subjects, in the order first seen
	arcs   map[string][]arc // by subject
	blanks int
	zotero bool // the file uses Zotero's namespace
}

func (g *rdfGraph) add(s, p, o string, literal bool) {
	if _, ok := g.arcs[s]; !ok {
		g.order = append(g.order, s)
	}
	g.arcs[s] = append(g.arcs[s], arc{p, o, literal})
}

func (g *rdfGraph) blank() string {
	g.blanks++
	return fmt.Sprintf("_:b%d", g.blanks)
}

// parseRDF reads RDF/XML into statements. It handles what Zotero writes:
// nested node elements, rdf:about, rdf:nodeID, rdf:resource, property
// attributes and rdf:parseType="Resource".
func parseRDF(data []byte) (*rdfGraph, error) {
	d := xml.NewDecoder(bytes.NewReader(data))
	g := &rdfGraph{arcs: map[string][]arc{}}
	for {
		tok, err := d.Token()
		if err == io.EOF {
			return nil, errors.New("no rdf:RDF element")
		}
		if err != nil {
			return nil, err
		}
		if se, ok := tok.(xml.StartElement); ok {
			if se.Name.Space != nsRDF || se.Name.Local != "RDF" {
				return nil, fmt.Errorf("the file starts with <%s>, not rdf:RDF", se.Name.Local)
			}
			for _, a := range se.Attr {
				if a.Value == nsZ {
					g.zotero = true
				}
			}
			for {
				tok, err := d.Token()
				if err != nil {
					return nil, err
				}
				switch t := tok.(type) {
				case xml.StartElement:
					if _, err := g.node(d, t); err != nil {
						return nil, err
					}
				case xml.EndElement:
					return g, nil
				}
			}
		}
	}
}

func attr(se xml.StartElement, space, local string) (string, bool) {
	for _, a := range se.Attr {
		if a.Name.Space == space && a.Name.Local == local {
			return a.Value, true
		}
	}
	return "", false
}

// node reads a node element and returns its subject.
func (g *rdfGraph) node(d *xml.Decoder, se xml.StartElement) (string, error) {
	subj := ""
	if v, ok := attr(se, nsRDF, "about"); ok {
		subj = v
	} else if v, ok := attr(se, nsRDF, "nodeID"); ok {
		subj = "_:" + v
	} else {
		subj = g.blank()
	}
	if se.Name.Space == nsZ {
		g.zotero = true
	}
	if !(se.Name.Space == nsRDF && se.Name.Local == "Description") {
		g.add(subj, nsRDF+"type", se.Name.Space+se.Name.Local, false)
	} else if _, ok := g.arcs[subj]; !ok {
		g.order = append(g.order, subj)
		g.arcs[subj] = nil
	}
	for _, a := range se.Attr {
		if a.Name.Space == nsRDF || a.Name.Space == "xmlns" || a.Name.Space == "" || a.Name.Space == "http://www.w3.org/XML/1998/namespace" {
			continue
		}
		g.add(subj, a.Name.Space+a.Name.Local, a.Value, true)
	}
	return subj, g.properties(d, subj)
}

// properties reads property elements up to the end of the node holding them.
func (g *rdfGraph) properties(d *xml.Decoder, subj string) error {
	for {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if err := g.property(d, subj, t); err != nil {
				return err
			}
		case xml.EndElement:
			return nil
		}
	}
}

func (g *rdfGraph) property(d *xml.Decoder, subj string, se xml.StartElement) error {
	pred := se.Name.Space + se.Name.Local
	if v, ok := attr(se, nsRDF, "resource"); ok {
		g.add(subj, pred, v, false)
		return d.Skip()
	}
	if v, ok := attr(se, nsRDF, "nodeID"); ok {
		g.add(subj, pred, "_:"+v, false)
		return d.Skip()
	}
	if v, _ := attr(se, nsRDF, "parseType"); v == "Resource" {
		b := g.blank()
		g.add(subj, pred, b, false)
		return g.properties(d, b)
	}
	var text strings.Builder
	for {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.CharData:
			text.Write(t)
		case xml.StartElement:
			obj, err := g.node(d, t)
			if err != nil {
				return err
			}
			g.add(subj, pred, obj, false)
			// Whatever follows the nested node is only white space.
			if err := d.Skip(); err != nil {
				return err
			}
			return nil
		case xml.EndElement:
			g.add(subj, pred, strings.TrimSpace(text.String()), true)
			return nil
		}
	}
}

// first is the first value of pred on subj, and whether it is a literal.
func (g *rdfGraph) first(subj, pred string) (string, bool) {
	for _, a := range g.arcs[subj] {
		if a.pred == pred {
			return a.obj, a.literal
		}
	}
	return "", false
}

func (g *rdfGraph) all(subj, pred string) []arc {
	var out []arc
	for _, a := range g.arcs[subj] {
		if a.pred == pred {
			out = append(out, a)
		}
	}
	return out
}

func (g *rdfGraph) is(subj, typ string) bool {
	for _, a := range g.all(subj, nsRDF+"type") {
		if a.obj == typ {
			return true
		}
	}
	return false
}

func (g *rdfGraph) literal(subj, pred string) string {
	for _, a := range g.all(subj, pred) {
		if a.literal {
			return a.obj
		}
	}
	return ""
}

// export turns the statements into Zotero's items and collections. dir is
// the export's folder, which attachment paths are relative to.
func (g *rdfGraph) export(dir string) Export {
	var out Export
	itemAt := map[string]int{}
	for _, s := range g.order {
		t := g.literal(s, nsZ+"itemType")
		if t == "" || t == "attachment" || t == "note" || g.is(s, nsZ+"Attachment") || g.is(s, nsBib+"Memo") {
			continue
		}
		it := g.item(s, t, dir)
		itemAt[s] = len(out.Items)
		out.Items = append(out.Items, it)
	}

	// Collections: the tree first, then what is in each.
	isCol := map[string]bool{}
	for _, s := range g.order {
		if g.is(s, nsZ+"Collection") {
			isCol[s] = true
		}
	}
	parent := map[string]string{}
	for s := range isCol {
		for _, a := range g.all(s, nsDCTerms+"hasPart") {
			if isCol[a.obj] {
				parent[a.obj] = s
			}
		}
	}
	keys := map[string]string{}
	var keyOf func(s string, depth int) string
	keyOf = func(s string, depth int) string {
		if k, ok := keys[s]; ok {
			return k
		}
		path := g.literal(s, nsDC+"title")
		if p, ok := parent[s]; ok && depth < 64 {
			path = keyOf(p, depth+1) + "\x00" + path
		}
		keys[s] = "rdf:" + shortHash(path)
		return keys[s]
	}
	for _, s := range g.order {
		if !isCol[s] {
			continue
		}
		c := Collection{Key: keyOf(s, 0), Name: g.literal(s, nsDC+"title")}
		if p, ok := parent[s]; ok {
			c.Parent = keyOf(p, 0)
		}
		out.Collections = append(out.Collections, c)
		for _, a := range g.all(s, nsDCTerms+"hasPart") {
			if i, ok := itemAt[a.obj]; ok {
				out.Items[i].Collections = append(out.Items[i].Collections, c.Key)
			}
		}
	}
	return out
}

func (g *rdfGraph) item(s, typ, dir string) Item {
	it := Item{Type: typ, Title: strings.TrimSpace(g.literal(s, nsDC+"title"))}
	it.Year = year(g.literal(s, nsDC+"date"))
	if it.Year == 0 {
		it.Year = year(g.literal(s, nsDCTerms+"issued"))
	}

	// Creators: authors before editors and the rest, each list in order.
	preds := []string{nsBib + "authors", nsBib + "editors", nsBib + "contributors"}
	for _, a := range g.arcs[s] {
		if !a.literal && !strings.HasPrefix(a.pred, nsBib) && a.pred != nsDCTerms+"isPartOf" && g.is(a.obj, nsRDF+"Seq") {
			preds = append(preds, a.pred)
		}
	}
	seen := map[string]bool{}
	for _, p := range preds {
		for _, a := range g.all(s, p) {
			if seen[a.obj] {
				continue
			}
			seen[a.obj] = true
			for _, li := range g.all(a.obj, nsRDF+"li") {
				name := strings.TrimSpace(g.literal(li.obj, nsFOAF+"givenName") + " " + g.literal(li.obj, nsFOAF+"surname"))
				if name != "" {
					it.Creators = append(it.Creators, name)
				}
			}
		}
	}

	// Identifiers: on the item, and on what it is part of.
	var doiField, link string
	ids := func(n string) {
		for _, a := range g.all(n, nsDC+"identifier") {
			v := a.obj
			if !a.literal {
				v = g.literal(a.obj, nsRDF+"value")
			}
			switch {
			case strings.HasPrefix(v, "DOI "):
				if doiField == "" {
					doiField = strings.TrimPrefix(v, "DOI ")
				}
			case strings.HasPrefix(v, "http://") || strings.HasPrefix(v, "https://"):
				if link == "" {
					link = v
				}
			}
		}
	}
	ids(s)
	for n, depth := s, 0; depth < 3; depth++ {
		up, lit := g.first(n, nsDCTerms+"isPartOf")
		if up == "" || lit {
			break
		}
		ids(up)
		n = up
	}
	if link == "" && (strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")) {
		link = s
	}
	extra := g.literal(s, nsDC+"description")
	it.DOI, it.ArXiv, it.PMID = identifiers(doiField, link, extra, g.literal(s, nsZ+"archiveID"))

	for _, a := range g.all(s, nsDC+"subject") {
		if a.literal {
			if t := strings.TrimSpace(a.obj); t != "" {
				it.Tags = append(it.Tags, t)
			}
		} else if !g.is(a.obj, nsZ+"AutomaticTag") {
			if t := strings.TrimSpace(g.literal(a.obj, nsRDF+"value")); t != "" {
				it.Tags = append(it.Tags, t)
			}
		}
	}

	for _, a := range g.all(s, nsLink+"link") {
		n := a.obj
		mime := g.literal(n, nsLink+"type")
		p, _ := g.first(n, nsZ+"path")
		if p == "" {
			p, _ = g.first(n, nsRDF+"resource")
		}
		if p == "" || (mime != "application/pdf" && !strings.HasSuffix(strings.ToLower(p), ".pdf")) {
			continue
		}
		it.PDFs = append(it.PDFs, Attachment{Key: n, LinkMode: "exported", Title: g.literal(n, nsDC+"title"), Path: exportedPath(dir, p)})
	}

	it.Key = exportKey(it)
	return it
}

// exportedPath is where an attachment the export names is on this computer.
func exportedPath(dir, p string) string {
	if strings.HasPrefix(p, "file:") {
		if f, err := fromFileURL(p); err == nil {
			return f
		}
	}
	raw := p
	if filepath.IsAbs(filepath.FromSlash(raw)) {
		return filepath.FromSlash(raw)
	}
	full := filepath.Join(dir, filepath.FromSlash(raw))
	if _, err := os.Stat(full); err != nil {
		if un, uerr := url.PathUnescape(raw); uerr == nil && un != raw {
			if alt := filepath.Join(dir, filepath.FromSlash(un)); fileThere(alt) {
				return alt
			}
		}
	}
	return full
}

func fileThere(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// exportKey names an item from an export by what it says, since an export
// carries no Zotero key: the same item in a later export has the same key.
func exportKey(it Item) string {
	first := ""
	if len(it.Creators) > 0 {
		first = it.Creators[0]
	}
	return "rdf:" + shortHash(strings.Join([]string{it.Type, strings.ToLower(it.Title), fmt.Sprint(it.Year),
		strings.ToLower(first), strings.ToLower(it.DOI), strings.ToLower(it.ArXiv), it.PMID}, "\x00"))
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}
