package contracts

import (
	"testing"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

func TestApplyExcludes_RemovesConfiguredNodesAndIncidentEdges(t *testing.T) {
	g := mgraph.New()
	add := func(n mgraph.Node) {
		t.Helper()
		if _, inserted := g.AddNode(n); !inserted {
			t.Fatalf("duplicate node %q", n.ID)
		}
	}
	add(mgraph.Node{ID: "root", Kind: mgraph.NodeFunction, Name: "Root", QName: "example/root.Root"})
	add(mgraph.Node{ID: "errorf", Kind: mgraph.NodeFunction, Name: "Errorf", QName: "fmt.Errorf"})
	add(mgraph.Node{ID: "trim", Kind: mgraph.NodeFunction, Name: "TrimSpace", QName: "strings.TrimSpace"})
	add(mgraph.Node{ID: "test", Kind: mgraph.NodeFunction, Name: "Helper", QName: "testing.Helper"})
	add(mgraph.Node{ID: "method", Kind: mgraph.NodeMethod, Name: "Do", QName: "(*testing.T).Run"})
	add(mgraph.Node{ID: "branch", Kind: mgraph.NodeBranch, Name: "branch"})
	add(mgraph.Node{ID: "keep", Kind: mgraph.NodeFunction, Name: "Keep", QName: "encoding/json.NewDecoder"})
	for _, to := range []string{"errorf", "trim", "test", "method", "branch", "keep"} {
		if _, err := g.AddEdge(mgraph.Edge{From: "root", To: to, Kind: mgraph.EdgeCalls}); err != nil {
			t.Fatalf("add edge root -> %s: %v", to, err)
		}
	}

	out := ApplyExcludes(g, Exclude{
		QNames:        []string{"fmt.Errorf"},
		QNamePrefixes: []string{"strings."},
		Packages:      []string{"testing"},
		Kinds:         []string{"branch"},
	})

	for _, id := range []string{"errorf", "trim", "test", "method", "branch"} {
		if out.HasNode(id) {
			t.Fatalf("excluded node %q still present", id)
		}
	}
	for _, id := range []string{"root", "keep"} {
		if !out.HasNode(id) {
			t.Fatalf("kept node %q missing", id)
		}
	}
	if got := out.EdgeCount(); got != 1 {
		t.Fatalf("edge count = %d, want 1", got)
	}
	edges := out.IncidentEdges("root", mgraph.DirectionOut, mgraph.EdgeCalls)
	if len(edges) != 1 || edges[0].To != "keep" {
		t.Fatalf("remaining root calls = %+v, want only keep", edges)
	}
}

func TestApplyExcludes_EmptyConfigReturnsSameGraph(t *testing.T) {
	g := mgraph.New()
	g.AddNode(mgraph.Node{ID: "n", Kind: mgraph.NodeFunction, QName: "fmt.Errorf"})
	if got := ApplyExcludes(g, Exclude{}); got != g {
		t.Fatal("empty exclude config should return original graph")
	}
}
