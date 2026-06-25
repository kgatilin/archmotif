package localpartition

import (
	"sort"
	"testing"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// buildTwoCliques builds two triangles (A1A2A3, B1B2B3) joined by a single
// bridge edge A3–B1. Seeding inside one clique should recover that clique as
// the min-conductance region and leave the other clique out.
func buildTwoCliques() *Graph {
	g := mgraph.New()
	for _, name := range []string{"A1", "A2", "A3", "B1", "B2", "B3"} {
		g.AddNode(mgraph.Node{ID: name, Kind: mgraph.NodeFunction, Name: name})
	}
	add := func(from, to string) { _, _ = g.AddEdge(mgraph.Edge{From: from, To: to, Kind: mgraph.EdgeCalls}) }
	add("A1", "A2")
	add("A1", "A3")
	add("A2", "A3")
	add("B1", "B2")
	add("B1", "B3")
	add("B2", "B3")
	add("A3", "B1") // bridge
	return g
}

func TestLocalPartition_RecoversSeedClique(t *testing.T) {
	g := buildTwoCliques()

	res, err := LocalPartition(g, []string{"A1"}, DefaultOptions())
	if err != nil {
		t.Fatalf("LocalPartition: %v", err)
	}
	if res.SeedCount != 1 {
		t.Errorf("SeedCount = %d, want 1", res.SeedCount)
	}

	got := append([]string(nil), res.Region...)
	sort.Strings(got)
	want := []string{"A1", "A2", "A3"}
	if len(got) != len(want) {
		t.Fatalf("region = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("region = %v, want %v", got, want)
		}
	}

	// Bridge crosses 1 edge; vol(A)=7, so conductance = 1/7 ≈ 0.143.
	if res.Conductance <= 0 || res.Conductance > 0.3 {
		t.Errorf("conductance = %v, want a small positive cut (~0.14)", res.Conductance)
	}

	// Seed must carry the most mass of any node.
	for id, w := range res.Weights {
		if id != "A1" && w > res.Weights["A1"] {
			t.Errorf("node %s weight %v exceeds seed A1 weight %v", id, w, res.Weights["A1"])
		}
	}
}

func TestLocalPartition_NoSeedsPresent(t *testing.T) {
	g := buildTwoCliques()
	res, err := LocalPartition(g, []string{"does-not-exist"}, DefaultOptions())
	if err != nil {
		t.Fatalf("LocalPartition: %v", err)
	}
	if len(res.Region) != 0 {
		t.Errorf("region = %v, want empty", res.Region)
	}
	if res.Weights == nil {
		t.Errorf("Weights should be non-nil even when empty")
	}
}

func TestLocalPartition_IsolatedSeedUnderEdgeKindFilter(t *testing.T) {
	g := buildTwoCliques()
	// Filter to an edge kind no edge has: every node becomes isolated, so the
	// seed's region is just itself.
	opts := DefaultOptions()
	opts.EdgeKinds = []string{string(mgraph.EdgeDependsOn)}

	res, err := LocalPartition(g, []string{"A1"}, opts)
	if err != nil {
		t.Fatalf("LocalPartition: %v", err)
	}
	if len(res.Region) != 1 || res.Region[0] != "A1" {
		t.Errorf("region = %v, want [A1]", res.Region)
	}
	if res.Conductance != 0 {
		t.Errorf("conductance = %v, want 0 for an isolated seed", res.Conductance)
	}
}

func TestLocalPartition_RejectsBadParams(t *testing.T) {
	g := buildTwoCliques()
	if _, err := LocalPartition(g, []string{"A1"}, Options{Alpha: 0, Epsilon: 1e-5}); err == nil {
		t.Error("expected error for alpha=0")
	}
	if _, err := LocalPartition(g, []string{"A1"}, Options{Alpha: 0.15, Epsilon: 0}); err == nil {
		t.Error("expected error for epsilon=0")
	}
	if _, err := LocalPartition(nil, []string{"A1"}, DefaultOptions()); err == nil {
		t.Error("expected error for nil graph")
	}
}
