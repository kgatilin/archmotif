package graph

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"strings"
	"testing"
)

func TestMakeID_FilePosition(t *testing.T) {
	id := MakeID(NodeFunction, Position{File: "internal/parser/build.go", Line: 12, Col: 1}, "Build", 0)
	want := "internal/parser/build.go:12:1:function:Build"
	if id != want {
		t.Fatalf("got %q, want %q", id, want)
	}
}

func TestMakeID_PackageNoPosition(t *testing.T) {
	id := MakeID(NodePackage, Position{}, "github.com/foo/bar", 0)
	want := "pkg:github.com/foo/bar"
	if id != want {
		t.Fatalf("got %q, want %q", id, want)
	}
}

func TestMakeID_OrdinalDisambiguation(t *testing.T) {
	pos := Position{File: "x.go", Line: 1, Col: 1}
	a := MakeID(NodeLoop, pos, "", 0)
	b := MakeID(NodeLoop, pos, "", 1)
	if a == b {
		t.Fatalf("expected ordinal to differentiate; got %q twice", a)
	}
	if !strings.HasSuffix(b, "#1") {
		t.Fatalf("ordinal not appended: %q", b)
	}
}

func TestGraph_AddNodeAddEdge(t *testing.T) {
	g := New()
	a, _ := g.AddNode(Node{ID: "a", Kind: NodeFunction, Name: "A"})
	b, _ := g.AddNode(Node{ID: "b", Kind: NodeFunction, Name: "B"})
	added, err := g.AddEdge(Edge{From: a.ID, To: b.ID, Kind: EdgeCalls})
	if err != nil {
		t.Fatal(err)
	}
	if !added {
		t.Fatal("expected new edge")
	}
	if g.NodeCount() != 2 || g.EdgeCount() != 1 {
		t.Fatalf("counts wrong: %d nodes, %d edges", g.NodeCount(), g.EdgeCount())
	}
}

func TestGraph_DuplicateNode(t *testing.T) {
	g := New()
	g.AddNode(Node{ID: "a", Kind: NodeFunction, Name: "A"})
	_, inserted := g.AddNode(Node{ID: "a", Kind: NodeFunction, Name: "A2"})
	if inserted {
		t.Fatal("duplicate ID should not insert")
	}
	got, _ := g.Node("a")
	if got.Name != "A" {
		t.Fatalf("expected first AddNode to win: got Name=%q", got.Name)
	}
}

func TestGraph_DuplicateEdge(t *testing.T) {
	g := New()
	g.AddNode(Node{ID: "a", Kind: NodeFunction})
	g.AddNode(Node{ID: "b", Kind: NodeFunction})
	_, _ = g.AddEdge(Edge{From: "a", To: "b", Kind: EdgeCalls})
	added, _ := g.AddEdge(Edge{From: "a", To: "b", Kind: EdgeCalls})
	if added {
		t.Fatal("duplicate edge should not insert")
	}
	// Different kind same endpoints should still insert.
	added, err := g.AddEdge(Edge{From: "a", To: "b", Kind: EdgeReturns})
	if err != nil {
		t.Fatal(err)
	}
	if !added {
		t.Fatal("different kind should insert")
	}
}

func TestGraph_AddEdgeMissingEndpoint(t *testing.T) {
	g := New()
	g.AddNode(Node{ID: "a", Kind: NodeFunction})
	if _, err := g.AddEdge(Edge{From: "a", To: "missing", Kind: EdgeCalls}); err == nil {
		t.Fatal("expected error for missing endpoint")
	}
}

func TestGraph_NodesByKind(t *testing.T) {
	g := New()
	g.AddNode(Node{ID: "p", Kind: NodePackage, Name: "p"})
	g.AddNode(Node{ID: "f", Kind: NodeFile, Name: "f"})
	g.AddNode(Node{ID: "fn1", Kind: NodeFunction, Name: "Fn1"})
	g.AddNode(Node{ID: "fn2", Kind: NodeFunction, Name: "Fn2"})
	got := g.NodesByKind(NodeFunction)
	if len(got) != 2 {
		t.Fatalf("got %d functions, want 2", len(got))
	}
}

func TestGraph_NeighborsByDirection(t *testing.T) {
	g := New()
	g.AddNode(Node{ID: "fn", Kind: NodeFunction, Name: "Fn"})
	g.AddNode(Node{ID: "callee1", Kind: NodeFunction, Name: "C1"})
	g.AddNode(Node{ID: "callee2", Kind: NodeFunction, Name: "C2"})
	g.AddNode(Node{ID: "caller", Kind: NodeFunction, Name: "Caller"})
	_, _ = g.AddEdge(Edge{From: "fn", To: "callee1", Kind: EdgeCalls})
	_, _ = g.AddEdge(Edge{From: "fn", To: "callee2", Kind: EdgeCalls})
	_, _ = g.AddEdge(Edge{From: "caller", To: "fn", Kind: EdgeCalls})

	out := g.Neighbors("fn", DirectionOut, EdgeCalls)
	if len(out) != 2 {
		t.Fatalf("out neighbours = %d, want 2", len(out))
	}
	in := g.Neighbors("fn", DirectionIn, EdgeCalls)
	if len(in) != 1 || in[0].ID != "caller" {
		t.Fatalf("in neighbours wrong: %+v", in)
	}
	both := g.Neighbors("fn", DirectionBoth, "")
	if len(both) != 3 {
		t.Fatalf("both neighbours = %d, want 3", len(both))
	}
}

func TestGraph_Subgraph(t *testing.T) {
	g := New()
	g.AddNode(Node{ID: "T", Kind: NodeType, Name: "T"})
	g.AddNode(Node{ID: "M", Kind: NodeMethod, Name: "M"})
	g.AddNode(Node{ID: "C", Kind: NodeFunction, Name: "Caller"})
	g.AddNode(Node{ID: "X", Kind: NodeFunction, Name: "Unrelated"})
	_, _ = g.AddEdge(Edge{From: "T", To: "M", Kind: EdgeContains})
	_, _ = g.AddEdge(Edge{From: "C", To: "M", Kind: EdgeCalls})

	sub := g.Subgraph([]string{"T"}, 2)
	if !sub.HasNode("T") || !sub.HasNode("M") || !sub.HasNode("C") {
		t.Fatalf("subgraph missing expected nodes: %+v", sub.Nodes())
	}
	if sub.HasNode("X") {
		t.Fatal("subgraph should not include unrelated node")
	}
	if sub.EdgeCount() != 2 {
		t.Fatalf("subgraph edges = %d, want 2", sub.EdgeCount())
	}
}

func TestGraph_JSONRoundTrip(t *testing.T) {
	g := New()
	g.AddNode(Node{ID: "a", Kind: NodeFunction, Name: "A"})
	g.AddNode(Node{ID: "b", Kind: NodeFunction, Name: "B"})
	_, _ = g.AddEdge(Edge{From: "a", To: "b", Kind: EdgeCalls})

	var buf bytes.Buffer
	if err := g.WriteJSON(&buf); err != nil {
		t.Fatal(err)
	}
	var got JSON
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Version != CurrentJSONVersion {
		t.Fatalf("version = %d, want %d", got.Version, CurrentJSONVersion)
	}
	if len(got.Nodes) != 2 || len(got.Edges) != 1 {
		t.Fatalf("counts: %d nodes, %d edges", len(got.Nodes), len(got.Edges))
	}
}

func TestGraph_GraphMLIsWellFormedAndKeepsStableIDs(t *testing.T) {
	g := New()
	g.AddNode(Node{
		ID:    `pkg:example.com/a&b`,
		Kind:  NodePackage,
		Name:  `a&b`,
		Attrs: map[string]any{"foreign": true},
	})
	g.AddNode(Node{
		ID:    `internal/x.go:10:2:function:Run`,
		Kind:  NodeFunction,
		Name:  `Run<Now>`,
		QName: `example.com/a.Run`,
		Pos:   Position{File: "internal/x.go", Line: 10, Col: 2},
	})
	_, _ = g.AddEdge(Edge{From: `pkg:example.com/a&b`, To: `internal/x.go:10:2:function:Run`, Kind: EdgeContains})

	var buf bytes.Buffer
	if err := g.WriteGraphML(&buf); err != nil {
		t.Fatal(err)
	}
	if err := xml.Unmarshal(buf.Bytes(), new(any)); err != nil {
		t.Fatalf("GraphML is not well-formed XML: %v\n%s", err, buf.String())
	}
	out := buf.String()
	for _, want := range []string{
		`<key id="n_id" for="node" attr.name="archmotif_id" attr.type="string"/>`,
		`<data key="n_id">pkg:example.com/a&amp;b</data>`,
		`<data key="n_label">Run&lt;Now&gt;</data>`,
		`<data key="n_layer">behavior</data>`,
		`<data key="n_detail_level">3</data>`,
		`<data key="e_kind">contains</data>`,
		`<data key="e_layer">structure</data>`,
		`<data key="e_detail_level">0</data>`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in GraphML:\n%s", want, out)
		}
	}
}
