package export

import (
	"bytes"
	"encoding/xml"
	"errors"
	"strings"
	"testing"

	"github.com/codevector-2003/filiation/internal/model"
)

// parsed is a GraphML document read back with encoding/xml — an independent
// parser, so a test fails on a file other tools would reject.
type parsed struct {
	Keys []struct {
		ID   string `xml:"id,attr"`
		For  string `xml:"for,attr"`
		Name string `xml:"attr.name,attr"`
		Type string `xml:"attr.type,attr"`
	} `xml:"key"`
	Graph struct {
		EdgeDefault string `xml:"edgedefault,attr"`
		Nodes       []struct {
			ID   string `xml:"id,attr"`
			Data []struct {
				Key   string `xml:"key,attr"`
				Value string `xml:",chardata"`
			} `xml:"data"`
		} `xml:"node"`
		Edges []struct {
			Source string `xml:"source,attr"`
			Target string `xml:"target,attr"`
		} `xml:"edge"`
	} `xml:"graph"`
}

func parse(t *testing.T, doc []byte) parsed {
	t.Helper()
	var p parsed
	if err := xml.Unmarshal(doc, &p); err != nil {
		t.Fatalf("output is not well-formed XML: %v\n%s", err, doc)
	}
	return p
}

func nodeData(p parsed, id string) map[string]string {
	for _, n := range p.Graph.Nodes {
		if n.ID == id {
			m := map[string]string{}
			for _, d := range n.Data {
				m[d.Key] = d.Value
			}
			return m
		}
	}
	return nil
}

func TestGraphML(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	g := NewGraphML(&buf)
	g.WriteNode(&model.Work{
		OpenAlexID: "W2741809807", Title: model.Ptr("The state of OA"), Year: model.Ptr(2018),
		Type: model.TypeArticle, Venue: "PeerJ", DOI: model.Ptr("10.7717/peerj.4375"),
		OAStatus: model.OAGold, CitedByCount: 1259, Depth: model.Ptr(0), IsSeed: true, Hydrated: true,
	})
	// A stub: an ID and a depth, nothing else. It must not crash the export
	// or be given values it does not have (export/doc.go).
	g.WriteNode(&model.Work{OpenAlexID: "W1503178185", Depth: model.Ptr(2)})
	g.WriteEdge("W2741809807", "W1503178185")
	if err := g.Close(); err != nil {
		t.Fatal(err)
	}
	if n, e := g.Counts(); n != 2 || e != 1 {
		t.Errorf("Counts = %d, %d", n, e)
	}

	p := parse(t, buf.Bytes())
	if p.Graph.EdgeDefault != "directed" {
		t.Errorf("edgedefault = %q, want directed — citations have a direction", p.Graph.EdgeDefault)
	}
	if len(p.Keys) != len(nodeKeys) {
		t.Errorf("keys = %d, want %d", len(p.Keys), len(nodeKeys))
	}

	seed := nodeData(p, "W2741809807")
	for k, want := range map[string]string{
		"label": "The state of OA", "year": "2018", "type": "article", "venue": "PeerJ",
		"doi": "10.7717/peerj.4375", "oa_status": "gold", "cited_by_count": "1259",
		"depth": "0", "seed": "true", "fetched": "true", "unresolved": "false",
	} {
		if seed[k] != want {
			t.Errorf("seed %s = %q, want %q", k, seed[k], want)
		}
	}

	stub := nodeData(p, "W1503178185")
	if stub["label"] != "[not fetched: W1503178185]" || stub["fetched"] != "false" || stub["depth"] != "2" {
		t.Errorf("stub = %v", stub)
	}
	for _, absent := range []string{"title", "year", "type", "doi", "cited_by_count"} {
		if _, ok := stub[absent]; ok {
			t.Errorf("stub carries %s = %q; absent values must be left out", absent, stub[absent])
		}
	}

	if e := p.Graph.Edges[0]; e.Source != "W2741809807" || e.Target != "W1503178185" {
		t.Errorf("edge = %+v, want citing -> cited", e)
	}
}

func TestGraphMLEscapesTitles(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	g := NewGraphML(&buf)
	// Real titles carry markup characters, quotes, non-Latin letters and,
	// occasionally, control characters XML cannot represent at all.
	title := "Cats & dogs: <b>\"bold\"</b> — naïve 猫\x01\x0b end"
	g.WriteNode(&model.Work{OpenAlexID: "W1", Title: model.Ptr(title), Hydrated: true})
	if err := g.Close(); err != nil {
		t.Fatal(err)
	}
	got := nodeData(parse(t, buf.Bytes()), "W1")["title"]
	if !strings.HasPrefix(got, `Cats & dogs: <b>"bold"</b> — naïve 猫`) || !strings.HasSuffix(got, " end") {
		t.Errorf("title round-tripped as %q", got)
	}
}

func TestGraphMLEmpty(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := NewGraphML(&buf).Close(); err != nil {
		t.Fatal(err)
	}
	if p := parse(t, buf.Bytes()); len(p.Graph.Nodes) != 0 {
		t.Errorf("empty graph has %d nodes", len(p.Graph.Nodes))
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestGraphMLReportsWriteErrors(t *testing.T) {
	t.Parallel()
	g := NewGraphML(failingWriter{})
	for range 1000 {
		g.WriteNode(&model.Work{OpenAlexID: "W1", Title: model.Ptr(strings.Repeat("x", 100))})
	}
	if err := g.Close(); err == nil {
		t.Error("Close reported success writing to a full disk")
	}
}
