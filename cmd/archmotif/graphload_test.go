package main

import (
	"testing"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// filterFixture: two internal packages (a→b dependsOn) + one foreign package +
// a couple of internal symbol nodes with a calls edge.
func filterFixture() mgraph.JSON {
	return mgraph.JSON{
		Version: 1,
		Nodes: []mgraph.Node{
			{ID: "pa", Kind: mgraph.NodePackage, QName: "mod/a", Name: "a", Attrs: map[string]any{"foreign": false}},
			{ID: "pb", Kind: mgraph.NodePackage, QName: "mod/b", Name: "b", Attrs: map[string]any{"foreign": false}},
			{ID: "pext", Kind: mgraph.NodePackage, QName: "fmt", Name: "fmt", Attrs: map[string]any{"foreign": true}},
			{ID: "fa", Kind: mgraph.NodeFunction, QName: "mod/a.Foo", Name: "Foo"},
			{ID: "fext", Kind: mgraph.NodeFunction, QName: "fmt.Println", Name: "Println", Attrs: map[string]any{"foreign": true}},
		},
		Edges: []mgraph.Edge{
			{From: "pa", To: "pb", Kind: mgraph.EdgeDependsOn},
			{From: "pa", To: "pext", Kind: mgraph.EdgeDependsOn},
			{From: "fa", To: "fext", Kind: mgraph.EdgeCalls},
		},
	}
}

func TestFilterDropsForeignBySymbolDefault(t *testing.T) {
	got := filterGraph(filterFixture(), graphFilter{Granularity: granularitySymbol})
	for _, n := range got.Nodes {
		if n.ID == "pext" || n.ID == "fext" {
			t.Errorf("foreign node %q survived default filter", n.ID)
		}
	}
	// The calls edge referenced a dropped foreign node and must be gone.
	for _, e := range got.Edges {
		if e.From == "fa" && e.To == "fext" {
			t.Errorf("edge to dropped foreign node survived")
		}
	}
	// Non-foreign nodes remain.
	if len(got.Nodes) != 3 { // pa, pb, fa
		t.Errorf("nodes after foreign drop = %d, want 3", len(got.Nodes))
	}
}

func TestFilterIncludeForeignKeepsAll(t *testing.T) {
	got := filterGraph(filterFixture(), graphFilter{Granularity: granularitySymbol, IncludeForeign: true})
	if len(got.Nodes) != 5 {
		t.Errorf("nodes with IncludeForeign = %d, want 5", len(got.Nodes))
	}
}

func TestFilterPackageGranularity(t *testing.T) {
	got := filterGraph(filterFixture(), graphFilter{Granularity: granularityPackage})

	// Only the two non-foreign package nodes, keyed by QName; no symbols, no fmt.
	if len(got.Nodes) != 2 {
		t.Fatalf("package nodes = %d, want 2 (mod/a, mod/b)", len(got.Nodes))
	}
	names := map[string]bool{}
	for _, n := range got.Nodes {
		names[n.ID] = true
		if n.Kind != mgraph.NodePackage {
			t.Errorf("node %q kind = %q, want package", n.ID, n.Kind)
		}
	}
	if !names["mod/a"] || !names["mod/b"] {
		t.Errorf("package node ids = %v, want mod/a + mod/b", names)
	}

	// Only the internal dependsOn edge remains, remapped to QNames; the
	// edge to foreign fmt is dropped.
	if len(got.Edges) != 1 {
		t.Fatalf("package edges = %d, want 1", len(got.Edges))
	}
	if e := got.Edges[0]; e.From != "mod/a" || e.To != "mod/b" {
		t.Errorf("edge = %s→%s, want mod/a→mod/b", e.From, e.To)
	}
}

func TestFilterPackageIncludeForeign(t *testing.T) {
	got := filterGraph(filterFixture(), graphFilter{Granularity: granularityPackage, IncludeForeign: true})
	if len(got.Nodes) != 3 { // mod/a, mod/b, fmt
		t.Errorf("package nodes with foreign = %d, want 3", len(got.Nodes))
	}
}
