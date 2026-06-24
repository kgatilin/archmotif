package trophic

import (
	"testing"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

func addFn(g *mgraph.Graph, id string) {
	g.AddNode(mgraph.Node{ID: id, Kind: mgraph.NodeFunction, Name: id})
}
func call(g *mgraph.Graph, from, to string) {
	_, _ = g.AddEdge(mgraph.Edge{From: from, To: to, Kind: mgraph.EdgeCalls})
}

// A clean dependency chain top -> mid -> base is perfectly layered: F0 ~ 0,
// three layers, base at the foundation, no cycles, no inversions.
func TestAnalyze_CoherentChain(t *testing.T) {
	g := mgraph.New()
	addFn(g, "base")
	addFn(g, "mid")
	addFn(g, "top")
	call(g, "top", "mid")
	call(g, "mid", "base")

	r := Analyze(g, Options{})

	if r.IncoherenceF0 > 0.01 {
		t.Errorf("F0 = %v, want ~0 for a clean chain", r.IncoherenceF0)
	}
	if r.LayerCount != 3 {
		t.Fatalf("layer_count = %d, want 3", r.LayerCount)
	}
	if got := r.Layers[0].Members; len(got) != 1 || got[0] != "base" {
		t.Errorf("layer 0 = %v, want [base] (foundation)", got)
	}
	if got := r.Layers[2].Members; len(got) != 1 || got[0] != "top" {
		t.Errorf("top layer = %v, want [top]", got)
	}
	if len(r.BackwardEdges) != 0 {
		t.Errorf("backward edges = %v, want none", r.BackwardEdges)
	}
	if len(r.Cycles) != 0 {
		t.Errorf("cycles = %v, want none", r.Cycles)
	}
}

// A 3-cycle is reported as a strongly-connected component.
func TestAnalyze_Cycle(t *testing.T) {
	g := mgraph.New()
	addFn(g, "a")
	addFn(g, "b")
	addFn(g, "c")
	call(g, "a", "b")
	call(g, "b", "c")
	call(g, "c", "a")

	r := Analyze(g, Options{})

	if len(r.Cycles) != 1 {
		t.Fatalf("cycles = %d, want 1", len(r.Cycles))
	}
	if r.Cycles[0].Size != 3 {
		t.Errorf("cycle size = %d, want 3", r.Cycles[0].Size)
	}
}

// A frustrated configuration: `from` depends on `to`, but the m_i nodes force
// `to` above `from` (to -> m_i -> from), so the from->to edge points UP the
// emergent hierarchy — a dependency inversion that must be flagged as backward.
// The least-squares solve places from=0, m_i=0.4, to=0.8, so span(from->to)=0.8.
// Such frustration is inherently cyclic in small graphs (from->to->m_i->from is
// an SCC); in large graphs inversions also arise from fractional pinning.
func TestAnalyze_BackwardEdge(t *testing.T) {
	g := mgraph.New()
	addFn(g, "from")
	addFn(g, "to")
	call(g, "from", "to")
	for _, m := range []string{"m1", "m2", "m3"} {
		addFn(g, m)
		call(g, "to", m)
		call(g, m, "from")
	}

	r := Analyze(g, Options{})

	if r.IncoherenceF0 <= 0 {
		t.Errorf("F0 = %v, want > 0 (graph has an inversion)", r.IncoherenceF0)
	}
	if r.BackwardEdgeCount == 0 {
		t.Fatalf("backward_edge_count = 0, want >= 1")
	}
	found := false
	for _, be := range r.BackwardEdges {
		if be.From == "from" && be.To == "to" {
			found = true
			if be.Span <= 0.5 {
				t.Errorf("span = %v, want > 0.5", be.Span)
			}
		}
	}
	if !found {
		t.Errorf("backward edges = %v, want one from 'from' -> 'to'", r.BackwardEdges)
	}
}

// EdgeKindsUsed is echoed and defaults to the directional flow set.
func TestAnalyze_EchoesDefaultEdgeKinds(t *testing.T) {
	g := mgraph.New()
	addFn(g, "a")
	addFn(g, "b")
	call(g, "a", "b")

	r := Analyze(g, Options{})
	if len(r.EdgeKindsUsed) != len(DefaultEdgeKinds) {
		t.Errorf("edge_kinds_used = %v, want default set %v", r.EdgeKindsUsed, DefaultEdgeKinds)
	}
}
