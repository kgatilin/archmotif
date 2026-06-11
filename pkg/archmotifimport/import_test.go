package archmotifimport

import (
	"strings"
	"testing"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// TestBuilder_RoundTrip constructs a small graph through the public
// Builder and asserts the node/edge counts and kinds match what direct
// internal/graph construction would yield.
func TestBuilder_RoundTrip(t *testing.T) {
	b := NewBuilder()

	must(t, b.AddPackage("pkg:a", "domain", "core"))
	must(t, b.AddPackage("pkg:b", "application", ""))
	must(t, b.AddPackage("pkg:c", "outbound_adapter", ""))

	must(t, b.AddType("pkg:a.T", "pkg:a", false, "domain_entity"))
	must(t, b.AddType("pkg:a.I", "pkg:a", true, "port"))
	must(t, b.AddType("pkg:b.S", "pkg:b", false, ""))

	must(t, b.AddFunction("pkg:b.F", "pkg:b"))
	must(t, b.AddMethod("pkg:a.T.M", "pkg:a.T"))
	must(t, b.AddField("pkg:a.T.f", "pkg:a.T", "string"))

	must(t, b.AddImplements("pkg:b.S", "pkg:a.I"))
	must(t, b.AddDependency("pkg:b", "pkg:a", DependencyDependsOn))
	must(t, b.AddDependency("pkg:c", "pkg:a", DependencyDependsOn))
	must(t, b.AddDependency("pkg:b.F", "pkg:a.T.M", DependencyCalls))

	g, err := b.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Build the equivalent graph by hand.
	want := mgraph.New()
	want.AddNode(mgraph.Node{ID: "pkg:a", Kind: mgraph.NodePackage, Name: "pkg:a"})
	want.AddNode(mgraph.Node{ID: "pkg:b", Kind: mgraph.NodePackage, Name: "pkg:b"})
	want.AddNode(mgraph.Node{ID: "pkg:c", Kind: mgraph.NodePackage, Name: "pkg:c"})
	want.AddNode(mgraph.Node{ID: "pkg:a.T", Kind: mgraph.NodeType, Name: "pkg:a.T"})
	want.AddNode(mgraph.Node{ID: "pkg:a.I", Kind: mgraph.NodeType, Name: "pkg:a.I"})
	want.AddNode(mgraph.Node{ID: "pkg:b.S", Kind: mgraph.NodeType, Name: "pkg:b.S"})
	want.AddNode(mgraph.Node{ID: "pkg:b.F", Kind: mgraph.NodeFunction, Name: "pkg:b.F"})
	want.AddNode(mgraph.Node{ID: "pkg:a.T.M", Kind: mgraph.NodeMethod, Name: "pkg:a.T.M"})
	want.AddNode(mgraph.Node{ID: "pkg:a.T.f", Kind: mgraph.NodeField, Name: "pkg:a.T.f"})

	addEdge(t, want, mgraph.Edge{From: "pkg:a", To: "pkg:a.T", Kind: mgraph.EdgeContains})
	addEdge(t, want, mgraph.Edge{From: "pkg:a", To: "pkg:a.I", Kind: mgraph.EdgeContains})
	addEdge(t, want, mgraph.Edge{From: "pkg:b", To: "pkg:b.S", Kind: mgraph.EdgeContains})
	addEdge(t, want, mgraph.Edge{From: "pkg:b", To: "pkg:b.F", Kind: mgraph.EdgeContains})
	addEdge(t, want, mgraph.Edge{From: "pkg:a.T", To: "pkg:a.T.M", Kind: mgraph.EdgeContains})
	addEdge(t, want, mgraph.Edge{From: "pkg:a.T", To: "pkg:a.T.f", Kind: mgraph.EdgeContains})
	addEdge(t, want, mgraph.Edge{From: "pkg:b.S", To: "pkg:a.I", Kind: mgraph.EdgeImplements})
	addEdge(t, want, mgraph.Edge{From: "pkg:b", To: "pkg:a", Kind: mgraph.EdgeDependsOn})
	addEdge(t, want, mgraph.Edge{From: "pkg:c", To: "pkg:a", Kind: mgraph.EdgeDependsOn})
	addEdge(t, want, mgraph.Edge{From: "pkg:b.F", To: "pkg:a.T.M", Kind: mgraph.EdgeCalls})

	if g.NodeCount() != want.NodeCount() {
		t.Errorf("node count: got %d, want %d", g.NodeCount(), want.NodeCount())
	}
	if g.EdgeCount() != want.EdgeCount() {
		t.Errorf("edge count: got %d, want %d", g.EdgeCount(), want.EdgeCount())
	}

	gotNodes := nodeKindCounts(g)
	wantNodes := nodeKindCounts(want)
	for k, w := range wantNodes {
		if gotNodes[k] != w {
			t.Errorf("node kind %s: got %d, want %d", k, gotNodes[k], w)
		}
	}

	gotEdges := edgeKindCounts(g)
	wantEdges := edgeKindCounts(want)
	for k, w := range wantEdges {
		if gotEdges[k] != w {
			t.Errorf("edge kind %s: got %d, want %d", k, gotEdges[k], w)
		}
	}
}

func TestBuilder_AddPackage_Errors(t *testing.T) {
	b := NewBuilder()
	if err := b.AddPackage("", "domain", ""); err == nil {
		t.Fatal("empty id: expected error")
	}
	if err := b.AddPackage("p", "", ""); err != nil {
		t.Fatalf("first add: %v", err)
	}
	if err := b.AddPackage("p", "", ""); err == nil {
		t.Fatal("duplicate id: expected error")
	}
}

func TestBuilder_AddType_Errors(t *testing.T) {
	b := NewBuilder()
	must(t, b.AddPackage("p", "", ""))
	if err := b.AddType("", "p", false, ""); err == nil {
		t.Fatal("empty id: expected error")
	}
	if err := b.AddType("t", "missing", false, ""); err == nil {
		t.Fatal("missing package: expected error")
	}
	must(t, b.AddType("t", "p", false, ""))
	// parent of wrong kind
	if err := b.AddType("t2", "t", false, ""); err == nil {
		t.Fatal("wrong-kind parent: expected error")
	}
}

func TestBuilder_AddFunction_Errors(t *testing.T) {
	b := NewBuilder()
	must(t, b.AddPackage("p", "", ""))
	if err := b.AddFunction("", "p"); err == nil {
		t.Fatal("empty id: expected error")
	}
	if err := b.AddFunction("f", "missing"); err == nil {
		t.Fatal("missing package: expected error")
	}
}

func TestBuilder_AddMethod_Errors(t *testing.T) {
	b := NewBuilder()
	must(t, b.AddPackage("p", "", ""))
	must(t, b.AddType("t", "p", false, ""))
	if err := b.AddMethod("", "t"); err == nil {
		t.Fatal("empty id: expected error")
	}
	if err := b.AddMethod("m", "missing"); err == nil {
		t.Fatal("missing parent: expected error")
	}
	// parent is package, not type
	if err := b.AddMethod("m2", "p"); err == nil {
		t.Fatal("wrong-kind parent: expected error")
	}
}

func TestBuilder_AddField_Errors(t *testing.T) {
	b := NewBuilder()
	must(t, b.AddPackage("p", "", ""))
	must(t, b.AddType("t", "p", false, ""))
	if err := b.AddField("", "t", "string"); err == nil {
		t.Fatal("empty id: expected error")
	}
	if err := b.AddField("f", "missing", "string"); err == nil {
		t.Fatal("missing parent: expected error")
	}
}

func TestBuilder_AddDependency_Errors(t *testing.T) {
	b := NewBuilder()
	must(t, b.AddPackage("a", "", ""))
	must(t, b.AddPackage("b", "", ""))
	if err := b.AddDependency("", "b", DependencyDependsOn); err == nil {
		t.Fatal("empty from: expected error")
	}
	if err := b.AddDependency("a", "", DependencyDependsOn); err == nil {
		t.Fatal("empty to: expected error")
	}
	if err := b.AddDependency("a", "b", DependencyKind("nope")); err == nil {
		t.Fatal("unknown kind: expected error")
	}
	if err := b.AddDependency("missing", "b", DependencyDependsOn); err == nil {
		t.Fatal("missing from-node: expected error")
	}
	if err := b.AddDependency("a", "missing", DependencyDependsOn); err == nil {
		t.Fatal("missing to-node: expected error")
	}
}

func TestBuilder_AddImplements_Errors(t *testing.T) {
	b := NewBuilder()
	must(t, b.AddPackage("p", "", ""))
	must(t, b.AddType("s", "p", false, ""))
	if err := b.AddImplements("", "s"); err == nil {
		t.Fatal("empty struct: expected error")
	}
	if err := b.AddImplements("s", ""); err == nil {
		t.Fatal("empty interface: expected error")
	}
	if err := b.AddImplements("missing", "s"); err == nil {
		t.Fatal("missing struct: expected error")
	}
	if err := b.AddImplements("s", "missing"); err == nil {
		t.Fatal("missing interface: expected error")
	}
}

func TestBuilder_AddContains_Errors(t *testing.T) {
	b := NewBuilder()
	must(t, b.AddPackage("p", "", ""))
	if err := b.AddContains("", "p"); err == nil {
		t.Fatal("empty parent: expected error")
	}
	if err := b.AddContains("p", ""); err == nil {
		t.Fatal("empty child: expected error")
	}
	if err := b.AddContains("missing", "p"); err == nil {
		t.Fatal("missing parent: expected error")
	}
	if err := b.AddContains("p", "missing"); err == nil {
		t.Fatal("missing child: expected error")
	}
}

func TestBuilder_AllDependencyKinds(t *testing.T) {
	b := NewBuilder()
	must(t, b.AddPackage("a", "", ""))
	must(t, b.AddPackage("b", "", ""))
	kinds := []DependencyKind{
		DependencyDependsOn,
		DependencyCalls,
		DependencyCallsFrom,
		DependencyReferences,
		DependencyEmbeds,
		DependencyReturns,
		DependencyUsesType,
	}
	for _, k := range kinds {
		if err := b.AddDependency("a", "b", k); err != nil {
			t.Fatalf("AddDependency(%s): %v", k, err)
		}
	}
	g, _ := b.Build()
	if g.EdgeCount() != len(kinds) {
		t.Fatalf("edge count: got %d, want %d", g.EdgeCount(), len(kinds))
	}
}

func TestBuilder_PackageAttrs(t *testing.T) {
	b := NewBuilder()
	must(t, b.AddPackage("p", "domain", "core"))
	g, _ := b.Build()
	n, ok := g.Node("p")
	if !ok {
		t.Fatal("package node not found")
	}
	if n.Attrs["layer"] != "domain" {
		t.Errorf("layer: got %v, want domain", n.Attrs["layer"])
	}
	if n.Attrs["aggregate"] != "core" {
		t.Errorf("aggregate: got %v, want core", n.Attrs["aggregate"])
	}
}

func TestBuilder_TypeAttrs(t *testing.T) {
	b := NewBuilder()
	must(t, b.AddPackage("p", "", ""))
	must(t, b.AddType("t", "p", true, "port"))
	g, _ := b.Build()
	n, _ := g.Node("t")
	if n.Attrs["isInterface"] != true {
		t.Errorf("isInterface: got %v, want true", n.Attrs["isInterface"])
	}
	if n.Attrs["role"] != "port" {
		t.Errorf("role: got %v, want port", n.Attrs["role"])
	}
}

func TestBuilder_FieldAttrs(t *testing.T) {
	b := NewBuilder()
	must(t, b.AddPackage("p", "", ""))
	must(t, b.AddType("t", "p", false, ""))
	must(t, b.AddField("f", "t", "string"))
	g, _ := b.Build()
	n, _ := g.Node("f")
	if n.Attrs["typeRef"] != "string" {
		t.Errorf("typeRef: got %v, want string", n.Attrs["typeRef"])
	}
}

func TestBuilder_ErrorMessages(t *testing.T) {
	b := NewBuilder()
	err := b.AddType("", "p", false, "")
	if err == nil || !strings.Contains(err.Error(), "archmotifimport") {
		t.Fatalf("error should be namespaced: %v", err)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func addEdge(t *testing.T, g *mgraph.Graph, e mgraph.Edge) {
	t.Helper()
	if _, err := g.AddEdge(e); err != nil {
		t.Fatalf("unexpected edge error: %v", err)
	}
}

func nodeKindCounts(g *mgraph.Graph) map[mgraph.NodeKind]int {
	out := map[mgraph.NodeKind]int{}
	for _, n := range g.Nodes() {
		out[n.Kind]++
	}
	return out
}

func edgeKindCounts(g *mgraph.Graph) map[mgraph.EdgeKind]int {
	out := map[mgraph.EdgeKind]int{}
	for _, e := range g.Edges() {
		out[e.Kind]++
	}
	return out
}
