package graphmlx

import (
	"strings"
	"testing"
)

const minimalGraphML = `<?xml version="1.0" encoding="UTF-8"?>
<graphml xmlns="http://graphml.graphdrawing.org/xmlns">
  <key id="n_label" for="node" attr.name="label" attr.type="string"/>
  <key id="n_kind" for="node" attr.name="kind" attr.type="string"/>
  <key id="n_id" for="node" attr.name="archmotif_id" attr.type="string"/>
  <key id="e_kind" for="edge" attr.name="kind" attr.type="string"/>
  <graph id="G" edgedefault="directed">
    <node id="n0">
      <data key="n_id">pkg:foo</data>
      <data key="n_label">foo</data>
      <data key="n_kind">package</data>
    </node>
    <node id="n1">
      <data key="n_id">pkg:bar</data>
      <data key="n_label">bar</data>
      <data key="n_kind">package</data>
    </node>
    <edge id="e0" source="n0" target="n1">
      <data key="e_kind">depends_on</data>
    </edge>
  </graph>
</graphml>`

func TestRead_BasicShape(t *testing.T) {
	g, err := Read(strings.NewReader(minimalGraphML))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got, want := len(g.Nodes), 2; got != want {
		t.Fatalf("nodes: got %d want %d", got, want)
	}
	if got, want := len(g.Edges), 1; got != want {
		t.Fatalf("edges: got %d want %d", got, want)
	}
	bar, ok := g.Node("pkg:bar")
	if !ok {
		t.Fatal("expected node pkg:bar")
	}
	if bar.Label != "bar" {
		t.Errorf("label: got %q want %q", bar.Label, "bar")
	}
	if bar.Kind != "package" {
		t.Errorf("kind: got %q want %q", bar.Kind, "package")
	}
	if g.Edges[0].Kind != "depends_on" {
		t.Errorf("edge kind: got %q want %q", g.Edges[0].Kind, "depends_on")
	}
	if g.Edges[0].From != "pkg:bar" || g.Edges[0].To != "pkg:foo" && g.Edges[0].From != "pkg:foo" {
		// after sort, the only edge ordering is by (From,To,Kind); verify endpoints either way
		if !(g.Edges[0].From == "pkg:foo" && g.Edges[0].To == "pkg:bar") {
			t.Errorf("unexpected edge: %+v", g.Edges[0])
		}
	}
}

func TestRead_RejectsMultiGraph(t *testing.T) {
	doc := `<?xml version="1.0"?>
<graphml xmlns="http://graphml.graphdrawing.org/xmlns">
  <graph id="A" edgedefault="directed"></graph>
  <graph id="B" edgedefault="directed"></graph>
</graphml>`
	if _, err := Read(strings.NewReader(doc)); err == nil {
		t.Fatal("expected multi-graph error, got nil")
	}
}

func TestRead_FallsBackToXMLID(t *testing.T) {
	doc := `<?xml version="1.0"?>
<graphml xmlns="http://graphml.graphdrawing.org/xmlns">
  <graph id="G" edgedefault="directed">
    <node id="alpha"></node>
    <node id="beta"></node>
    <edge id="e0" source="alpha" target="beta"></edge>
  </graph>
</graphml>`
	g, err := Read(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if _, ok := g.Node("alpha"); !ok {
		t.Fatal("expected node alpha (fell back to XMLID)")
	}
}

func TestSortFindings_StableOrder(t *testing.T) {
	fs := []Finding{
		{Detector: "b", Severity: SeverityLow, Score: 1, PrimaryID: "z"},
		{Detector: "a", Severity: SeverityHigh, Score: 5, PrimaryID: "z"},
		{Detector: "a", Severity: SeverityHigh, Score: 5, PrimaryID: "a"},
		{Detector: "c", Severity: SeverityMedium, Score: 3, PrimaryID: "m"},
	}
	SortFindings(fs)
	if fs[0].PrimaryID != "a" || fs[0].Detector != "a" {
		t.Errorf("first: got %+v", fs[0])
	}
	if fs[1].PrimaryID != "z" || fs[1].Detector != "a" {
		t.Errorf("second: got %+v", fs[1])
	}
	if fs[2].Detector != "c" {
		t.Errorf("third: got %+v", fs[2])
	}
	if fs[3].Detector != "b" {
		t.Errorf("fourth: got %+v", fs[3])
	}
}

// graphFromInline is a tiny helper so per-detector tests can build
// fixtures without verbose XML literals.
func graphFromInline(t *testing.T, doc string) *Graph {
	t.Helper()
	g, err := Read(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("Read fixture: %v", err)
	}
	return g
}
