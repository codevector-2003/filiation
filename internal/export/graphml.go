package export

import (
	"bufio"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"

	"github.com/codevector-2003/filiation/internal/model"
)

// GraphML writes a citation graph as GraphML (http://graphml.graphdrawing.org),
// the format Gephi, Cytoscape, yEd, NetworkX and igraph all read.
//
// It streams: nodes and edges are written as they are handed over, so a
// library of any size exports in constant memory. Call WriteNode for every
// node, then WriteEdge for every edge, then Close. GraphML requires nodes and
// edges inside one <graph> element and does not require nodes first, but
// several readers resolve edges against nodes already seen, so nodes go first.
//
// Edges point from the citing work to the cited one: an arrow means "cites",
// which is also the direction fil path calls lineage.
type GraphML struct {
	w      *bufio.Writer
	err    error
	nodes  int
	edges  int
	closed bool
}

// nodeKeys are the attributes every node may carry. "label" is the one Gephi
// shows on the canvas; the rest are there to size, colour and filter by.
var nodeKeys = []struct{ id, typ string }{
	{"label", "string"},
	{"title", "string"},
	{"year", "int"},
	{"type", "string"},
	{"venue", "string"},
	{"doi", "string"},
	{"oa_status", "string"},
	{"cited_by_count", "int"},
	{"depth", "int"},
	{"seed", "boolean"},
	{"fetched", "boolean"},
	{"unresolved", "boolean"},
}

// NewGraphML writes the GraphML header to w and returns a writer for the
// graph's nodes and edges.
func NewGraphML(w io.Writer) *GraphML {
	g := &GraphML{w: bufio.NewWriter(w)}
	g.printf("%s", `<?xml version="1.0" encoding="UTF-8"?>
<graphml xmlns="http://graphml.graphdrawing.org/xmlns"
         xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
         xsi:schemaLocation="http://graphml.graphdrawing.org/xmlns http://graphml.graphdrawing.org/xmlns/1.0/graphml.xsd">
`)
	for _, k := range nodeKeys {
		g.printf(`  <key id="%s" for="node" attr.name="%s" attr.type="%s"/>`+"\n", k.id, k.id, k.typ)
	}
	g.printf("%s", `  <graph id="citations" edgedefault="directed">`+"\n")
	return g
}

// WriteNode writes one work. Absent values are left out rather than written as
// zero or empty: a stub has no year, and "year 0" would put it at the start of
// every timeline.
func (g *GraphML) WriteNode(w *model.Work) {
	g.printf(`    <node id="%s">`+"\n", attr(w.OpenAlexID))
	g.data("label", w.DisplayTitle())
	if w.Title != nil {
		g.data("title", *w.Title)
	}
	if w.Year != nil {
		g.data("year", strconv.Itoa(*w.Year))
	}
	if w.Type != model.TypeUnknown {
		g.data("type", string(w.Type))
	}
	if w.Venue != "" {
		g.data("venue", w.Venue)
	}
	if w.DOI != nil {
		g.data("doi", *w.DOI)
	}
	if w.OAStatus != model.OAUnknown {
		g.data("oa_status", string(w.OAStatus))
	}
	if w.Hydrated {
		g.data("cited_by_count", strconv.Itoa(w.CitedByCount))
	}
	if w.Depth != nil {
		g.data("depth", strconv.Itoa(*w.Depth))
	}
	g.data("seed", strconv.FormatBool(w.IsSeed))
	g.data("fetched", strconv.FormatBool(w.Hydrated))
	g.data("unresolved", strconv.FormatBool(w.Unresolved))
	g.printf("%s", "    </node>\n")
	g.nodes++
}

// WriteEdge writes one citation: from cites to.
func (g *GraphML) WriteEdge(from, to string) {
	g.printf(`    <edge source="%s" target="%s"/>`+"\n", attr(from), attr(to))
	g.edges++
}

// Close ends the document and flushes it, returning the first error seen at
// any point in the write.
func (g *GraphML) Close() error {
	if !g.closed {
		g.closed = true
		g.printf("%s", "  </graph>\n</graphml>\n")
		if g.err == nil {
			g.err = g.w.Flush()
		}
	}
	return g.err
}

// Counts reports how many nodes and edges were written.
func (g *GraphML) Counts() (nodes, edges int) { return g.nodes, g.edges }

func (g *GraphML) data(key, value string) {
	g.printf(`      <data key="%s">%s</data>`+"\n", key, text(value))
}

func (g *GraphML) printf(format string, args ...any) {
	if g.err != nil {
		return
	}
	_, g.err = fmt.Fprintf(g.w, format, args...)
}

// text escapes character data. xml.EscapeText also replaces characters XML
// cannot carry at all — control characters turn up in real titles — with
// U+FFFD, so one bad title cannot make the whole file unreadable.
func text(s string) string {
	var b bufWriter
	_ = xml.EscapeText(&b, []byte(s))
	return string(b)
}

// attr escapes an attribute value, which may not contain a raw quote either.
func attr(s string) string { return text(s) }

type bufWriter []byte

func (b *bufWriter) Write(p []byte) (int, error) {
	*b = append(*b, p...)
	return len(p), nil
}
