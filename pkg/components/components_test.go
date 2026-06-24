package components

import (
	"testing"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

func TestAnalyze_SingleComponent(t *testing.T) {
	// Build a triangle: A -- B -- C -- A
	g := mgraph.New()
	g.AddNode(mgraph.Node{ID: "A", Kind: mgraph.NodeType})
	g.AddNode(mgraph.Node{ID: "B", Kind: mgraph.NodeType})
	g.AddNode(mgraph.Node{ID: "C", Kind: mgraph.NodeType})
	_, _ = g.AddEdge(mgraph.Edge{From: "A", To: "B", Kind: mgraph.EdgeDependsOn})
	_, _ = g.AddEdge(mgraph.Edge{From: "B", To: "C", Kind: mgraph.EdgeDependsOn})
	_, _ = g.AddEdge(mgraph.Edge{From: "C", To: "A", Kind: mgraph.EdgeDependsOn})

	result := Analyze(g, nil)

	if result.NodeCount != 3 {
		t.Errorf("NodeCount: got %d, want 3", result.NodeCount)
	}
	if result.EdgeCount != 3 {
		t.Errorf("EdgeCount: got %d, want 3", result.EdgeCount)
	}
	if result.ComponentCount != 1 {
		t.Errorf("ComponentCount: got %d, want 1", result.ComponentCount)
	}
	if result.SizeHistogram[3] != 1 {
		t.Errorf("SizeHistogram[3]: got %d, want 1", result.SizeHistogram[3])
	}
	if len(result.Components) != 1 {
		t.Fatalf("len(Components): got %d, want 1", len(result.Components))
	}
	comp := result.Components[0]
	if comp.Size != 3 {
		t.Errorf("Component.Size: got %d, want 3", comp.Size)
	}
	if comp.CenterNodeID == "" {
		t.Error("Component.CenterNodeID is empty")
	}
	if comp.Centrality <= 0 {
		t.Errorf("Component.Centrality: got %f, want > 0", comp.Centrality)
	}
}

func TestAnalyze_MultipleComponents(t *testing.T) {
	// Build two disconnected pairs: A--B and C--D, plus isolated E
	g := mgraph.New()
	g.AddNode(mgraph.Node{ID: "A", Kind: mgraph.NodeType})
	g.AddNode(mgraph.Node{ID: "B", Kind: mgraph.NodeType})
	g.AddNode(mgraph.Node{ID: "C", Kind: mgraph.NodeType})
	g.AddNode(mgraph.Node{ID: "D", Kind: mgraph.NodeType})
	g.AddNode(mgraph.Node{ID: "E", Kind: mgraph.NodeType}) // isolated
	_, _ = g.AddEdge(mgraph.Edge{From: "A", To: "B", Kind: mgraph.EdgeDependsOn})
	_, _ = g.AddEdge(mgraph.Edge{From: "C", To: "D", Kind: mgraph.EdgeDependsOn})

	result := Analyze(g, nil)

	if result.NodeCount != 5 {
		t.Errorf("NodeCount: got %d, want 5", result.NodeCount)
	}
	if result.ComponentCount != 3 {
		t.Errorf("ComponentCount: got %d, want 3", result.ComponentCount)
	}
	// Histogram: two size-2 components, one size-1 component
	if result.SizeHistogram[2] != 2 {
		t.Errorf("SizeHistogram[2]: got %d, want 2", result.SizeHistogram[2])
	}
	if result.SizeHistogram[1] != 1 {
		t.Errorf("SizeHistogram[1]: got %d, want 1", result.SizeHistogram[1])
	}
	// Components sorted by size descending.
	if len(result.Components) != 3 {
		t.Fatalf("len(Components): got %d, want 3", len(result.Components))
	}
	if result.Components[0].Size != 2 {
		t.Errorf("Components[0].Size: got %d, want 2", result.Components[0].Size)
	}
	if result.Components[2].Size != 1 {
		t.Errorf("Components[2].Size: got %d, want 1", result.Components[2].Size)
	}
}

func TestAnalyze_SingleNode(t *testing.T) {
	g := mgraph.New()
	g.AddNode(mgraph.Node{ID: "X", Kind: mgraph.NodeType})

	result := Analyze(g, nil)

	if result.NodeCount != 1 {
		t.Errorf("NodeCount: got %d, want 1", result.NodeCount)
	}
	if result.ComponentCount != 1 {
		t.Errorf("ComponentCount: got %d, want 1", result.ComponentCount)
	}
	if len(result.Components) != 1 {
		t.Fatalf("len(Components): got %d, want 1", len(result.Components))
	}
	comp := result.Components[0]
	if comp.CenterNodeID != "X" {
		t.Errorf("CenterNodeID: got %s, want X", comp.CenterNodeID)
	}
	if comp.Centrality != 1.0 {
		t.Errorf("Centrality: got %f, want 1.0", comp.Centrality)
	}
}

func TestAnalyze_EmptyGraph(t *testing.T) {
	g := mgraph.New()
	result := Analyze(g, nil)

	if result.NodeCount != 0 {
		t.Errorf("NodeCount: got %d, want 0", result.NodeCount)
	}
	if result.ComponentCount != 0 {
		t.Errorf("ComponentCount: got %d, want 0", result.ComponentCount)
	}
}

func TestAnalyze_NilGraph(t *testing.T) {
	result := Analyze(nil, nil)

	if result.NodeCount != 0 {
		t.Errorf("NodeCount: got %d, want 0", result.NodeCount)
	}
	if result.SizeHistogram == nil {
		t.Error("SizeHistogram should not be nil")
	}
}

func TestAnalyze_Subgraph(t *testing.T) {
	// Build: A--B--C--D--E (chain)
	g := mgraph.New()
	g.AddNode(mgraph.Node{ID: "A", Kind: mgraph.NodeType})
	g.AddNode(mgraph.Node{ID: "B", Kind: mgraph.NodeType})
	g.AddNode(mgraph.Node{ID: "C", Kind: mgraph.NodeType})
	g.AddNode(mgraph.Node{ID: "D", Kind: mgraph.NodeType})
	g.AddNode(mgraph.Node{ID: "E", Kind: mgraph.NodeType})
	_, _ = g.AddEdge(mgraph.Edge{From: "A", To: "B", Kind: mgraph.EdgeDependsOn})
	_, _ = g.AddEdge(mgraph.Edge{From: "B", To: "C", Kind: mgraph.EdgeDependsOn})
	_, _ = g.AddEdge(mgraph.Edge{From: "C", To: "D", Kind: mgraph.EdgeDependsOn})
	_, _ = g.AddEdge(mgraph.Edge{From: "D", To: "E", Kind: mgraph.EdgeDependsOn})

	// Analyze only A, B, D (disconnected in the subgraph: A--B and D alone)
	result := Analyze(g, []string{"A", "B", "D"})

	if result.NodeCount != 3 {
		t.Errorf("NodeCount: got %d, want 3", result.NodeCount)
	}
	// Only edge A--B survives; B--C and C--D are excluded.
	if result.EdgeCount != 1 {
		t.Errorf("EdgeCount: got %d, want 1", result.EdgeCount)
	}
	if result.ComponentCount != 2 {
		t.Errorf("ComponentCount: got %d, want 2", result.ComponentCount)
	}
}
