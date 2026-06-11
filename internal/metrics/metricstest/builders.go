// Package metricstest builds hand-crafted typed graphs used by metric
// tests.
//
// Stage 3 fixtures are programmatically constructed *graph.Graph
// instances rather than on-disk JSON files: the metrics operate on the
// in-memory graph type, and asserting closed-form values against a
// well-named builder reads more cleanly than parsing JSON in every
// test. ADR-015 records the choice.
//
// This package lives outside testdata/ on purpose — the Go toolchain
// excludes testdata/ from compilation, so test helpers go here and
// stay importable.
package metricstest

import (
	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// FourCycle returns a directed 4-cycle in the dependency subgraph
// (Calls edges A→B→C→D→A) embedded in a single package. Cycle rank = 1.
func FourCycle() *mgraph.Graph {
	g := mgraph.New()
	pkgID := "pkg:fixture/cycle"
	g.AddNode(mgraph.Node{ID: pkgID, Kind: mgraph.NodePackage, Name: "cycle", QName: "fixture/cycle"})
	ids := []string{"A", "B", "C", "D"}
	for _, n := range ids {
		id := "fixture/cycle/main.go:1:1:function:" + n
		g.AddNode(mgraph.Node{ID: id, Kind: mgraph.NodeFunction, Name: n})
		_, _ = g.AddEdge(mgraph.Edge{From: pkgID, To: id, Kind: mgraph.EdgeContains})
	}
	addCalls := func(from, to string) {
		_, _ = g.AddEdge(mgraph.Edge{
			From: "fixture/cycle/main.go:1:1:function:" + from,
			To:   "fixture/cycle/main.go:1:1:function:" + to,
			Kind: mgraph.EdgeCalls,
		})
	}
	addCalls("A", "B")
	addCalls("B", "C")
	addCalls("C", "D")
	addCalls("D", "A")
	return g
}

// TwoTriangles returns a graph with two disjoint cycles (two triangles
// in different "packages"). Cycle rank = 2.
func TwoTriangles() *mgraph.Graph {
	g := mgraph.New()
	mk := func(prefix string, names ...string) {
		pkgID := "pkg:fixture/" + prefix
		g.AddNode(mgraph.Node{ID: pkgID, Kind: mgraph.NodePackage, Name: prefix, QName: "fixture/" + prefix})
		for _, n := range names {
			id := "fixture/" + prefix + "/main.go:1:1:function:" + n
			g.AddNode(mgraph.Node{ID: id, Kind: mgraph.NodeFunction, Name: n})
			_, _ = g.AddEdge(mgraph.Edge{From: pkgID, To: id, Kind: mgraph.EdgeContains})
		}
	}
	mk("a", "A1", "A2", "A3")
	mk("b", "B1", "B2", "B3")
	link := func(prefix, from, to string) {
		_, _ = g.AddEdge(mgraph.Edge{
			From: "fixture/" + prefix + "/main.go:1:1:function:" + from,
			To:   "fixture/" + prefix + "/main.go:1:1:function:" + to,
			Kind: mgraph.EdgeCalls,
		})
	}
	link("a", "A1", "A2")
	link("a", "A2", "A3")
	link("a", "A3", "A1")
	link("b", "B1", "B2")
	link("b", "B2", "B3")
	link("b", "B3", "B1")
	return g
}

// FourClique returns a 4-clique (K4): four Function nodes with each
// undirected pair connected via a Calls edge in both directions. Used
// for spectral gap (closed-form algebraic connectivity = n = 4).
func FourClique() *mgraph.Graph {
	g := mgraph.New()
	pkgID := "pkg:fixture/clique"
	g.AddNode(mgraph.Node{ID: pkgID, Kind: mgraph.NodePackage, Name: "clique"})
	names := []string{"A", "B", "C", "D"}
	for _, n := range names {
		id := "fixture/clique/main.go:1:1:function:" + n
		g.AddNode(mgraph.Node{ID: id, Kind: mgraph.NodeFunction, Name: n})
		_, _ = g.AddEdge(mgraph.Edge{From: pkgID, To: id, Kind: mgraph.EdgeContains})
	}
	for i, a := range names {
		for j, b := range names {
			if i >= j {
				continue
			}
			_, _ = g.AddEdge(mgraph.Edge{
				From: "fixture/clique/main.go:1:1:function:" + a,
				To:   "fixture/clique/main.go:1:1:function:" + b,
				Kind: mgraph.EdgeCalls,
			})
		}
	}
	return g
}

// PathFour returns an undirected 4-path A-B-C-D (modeled as Calls
// A→B, B→C, C→D). Algebraic connectivity for an n=4 path is
// 2(1−cos(π/4)) = 2−√2 ≈ 0.5858.
func PathFour() *mgraph.Graph {
	g := mgraph.New()
	pkgID := "pkg:fixture/path"
	g.AddNode(mgraph.Node{ID: pkgID, Kind: mgraph.NodePackage, Name: "path"})
	names := []string{"A", "B", "C", "D"}
	for _, n := range names {
		id := "fixture/path/main.go:1:1:function:" + n
		g.AddNode(mgraph.Node{ID: id, Kind: mgraph.NodeFunction, Name: n})
		_, _ = g.AddEdge(mgraph.Edge{From: pkgID, To: id, Kind: mgraph.EdgeContains})
	}
	link := func(from, to string) {
		_, _ = g.AddEdge(mgraph.Edge{
			From: "fixture/path/main.go:1:1:function:" + from,
			To:   "fixture/path/main.go:1:1:function:" + to,
			Kind: mgraph.EdgeCalls,
		})
	}
	link("A", "B")
	link("B", "C")
	link("C", "D")
	return g
}

// TwoIslands returns two disconnected components (one triangle each).
// Algebraic connectivity = 0 (graph is disconnected).
func TwoIslands() *mgraph.Graph {
	g := mgraph.New()
	mk := func(prefix string, names []string) {
		pkgID := "pkg:fixture/" + prefix
		g.AddNode(mgraph.Node{ID: pkgID, Kind: mgraph.NodePackage, Name: prefix})
		for _, n := range names {
			id := "fixture/" + prefix + "/main.go:1:1:function:" + n
			g.AddNode(mgraph.Node{ID: id, Kind: mgraph.NodeFunction, Name: n})
			_, _ = g.AddEdge(mgraph.Edge{From: pkgID, To: id, Kind: mgraph.EdgeContains})
		}
		for i := 0; i < len(names); i++ {
			a := names[i]
			b := names[(i+1)%len(names)]
			_, _ = g.AddEdge(mgraph.Edge{
				From: "fixture/" + prefix + "/main.go:1:1:function:" + a,
				To:   "fixture/" + prefix + "/main.go:1:1:function:" + b,
				Kind: mgraph.EdgeCalls,
			})
		}
	}
	mk("left", []string{"L1", "L2", "L3"})
	mk("right", []string{"R1", "R2", "R3"})
	return g
}

// TwoStores returns the canonical "two repeated motifs" fixture: two
// concrete types each implementing the same interface and each having
// a constructor that returns the interface. Pre-extraction would yield
// a single "store-like" motif × 2 instances.
//
// Interface I has two implementations T1, T2; each Tn has a method M
// (Function->Method via Contains, but Method nodes only matter inside
// their type). Top-level constructors Cn return I, so we get:
//
//	Cn --Returns--> I
//	Cn --Calls--> Tn  (constructing the impl)
//
// This produces multiple isomorphic 3-node motifs (Cn, Tn, I) once
// the abstraction filter retains them (they cross packages so
// enclosingTypeID is empty).
func TwoStores() *mgraph.Graph {
	g := mgraph.New()
	pkg := "pkg:fixture/store"
	g.AddNode(mgraph.Node{ID: pkg, Kind: mgraph.NodePackage, Name: "store"})

	add := func(id, name string, kind mgraph.NodeKind) string {
		g.AddNode(mgraph.Node{ID: id, Kind: kind, Name: name})
		_, _ = g.AddEdge(mgraph.Edge{From: pkg, To: id, Kind: mgraph.EdgeContains})
		return id
	}
	add("fixture/store/iface.go:1:1:type:I", "I", mgraph.NodeType)
	add("fixture/store/t1.go:1:1:type:T1", "T1", mgraph.NodeType)
	add("fixture/store/t2.go:1:1:type:T2", "T2", mgraph.NodeType)
	add("fixture/store/t1.go:5:1:function:NewT1", "NewT1", mgraph.NodeFunction)
	add("fixture/store/t2.go:5:1:function:NewT2", "NewT2", mgraph.NodeFunction)

	// T1, T2 implement I.
	_, _ = g.AddEdge(mgraph.Edge{
		From: "fixture/store/t1.go:1:1:type:T1",
		To:   "fixture/store/iface.go:1:1:type:I",
		Kind: mgraph.EdgeImplements,
	})
	_, _ = g.AddEdge(mgraph.Edge{
		From: "fixture/store/t2.go:1:1:type:T2",
		To:   "fixture/store/iface.go:1:1:type:I",
		Kind: mgraph.EdgeImplements,
	})
	// Constructors return I.
	_, _ = g.AddEdge(mgraph.Edge{
		From: "fixture/store/t1.go:5:1:function:NewT1",
		To:   "fixture/store/iface.go:1:1:type:I",
		Kind: mgraph.EdgeReturns,
	})
	_, _ = g.AddEdge(mgraph.Edge{
		From: "fixture/store/t2.go:5:1:function:NewT2",
		To:   "fixture/store/iface.go:1:1:type:I",
		Kind: mgraph.EdgeReturns,
	})
	// Constructors call their concrete type (use Calls to model "this
	// constructor uses T"; gives us a 3-node motif {Cn, Tn, I}).
	_, _ = g.AddEdge(mgraph.Edge{
		From: "fixture/store/t1.go:5:1:function:NewT1",
		To:   "fixture/store/t1.go:1:1:type:T1",
		Kind: mgraph.EdgeCalls,
	})
	_, _ = g.AddEdge(mgraph.Edge{
		From: "fixture/store/t2.go:5:1:function:NewT2",
		To:   "fixture/store/t2.go:1:1:type:T2",
		Kind: mgraph.EdgeCalls,
	})
	return g
}

// PackageWithChildren produces three Package nodes each containing a
// File and a Function. Used as a small modularity fixture: communities
// are well-separated, Q should be substantially > 0.
func PackageWithChildren() *mgraph.Graph {
	g := mgraph.New()
	for _, p := range []string{"a", "b", "c"} {
		pkgID := "pkg:fixture/" + p
		g.AddNode(mgraph.Node{ID: pkgID, Kind: mgraph.NodePackage, Name: p})
		fileID := "fixture/" + p + "/main.go:1:1:file:main.go"
		g.AddNode(mgraph.Node{ID: fileID, Kind: mgraph.NodeFile, Name: "main.go"})
		_, _ = g.AddEdge(mgraph.Edge{From: pkgID, To: fileID, Kind: mgraph.EdgeContains})
		fnID := "fixture/" + p + "/main.go:1:1:function:F"
		g.AddNode(mgraph.Node{ID: fnID, Kind: mgraph.NodeFunction, Name: "F"})
		_, _ = g.AddEdge(mgraph.Edge{From: fileID, To: fnID, Kind: mgraph.EdgeContains})
	}
	return g
}

// SymmetricFanout returns a Type T with three Method children (M1,M2,M3),
// none of which call any other node. The three methods share an
// identical role signature: kind=method, out:{}, in:{contains:1}.
// Each method has 2 interchangeable ≤2-hop neighbours (the other two
// methods, plus T's other methods are reachable via T). Used to assert
// LocalSymmetry returns 2 for each method.
func SymmetricFanout() *mgraph.Graph {
	g := mgraph.New()
	pkg := "pkg:fixture/sym"
	g.AddNode(mgraph.Node{ID: pkg, Kind: mgraph.NodePackage, Name: "sym"})
	tID := "fixture/sym/t.go:1:1:type:T"
	g.AddNode(mgraph.Node{ID: tID, Kind: mgraph.NodeType, Name: "T"})
	_, _ = g.AddEdge(mgraph.Edge{From: pkg, To: tID, Kind: mgraph.EdgeContains})
	for _, m := range []string{"M1", "M2", "M3"} {
		mID := "fixture/sym/t.go:5:1:method:" + m
		g.AddNode(mgraph.Node{ID: mID, Kind: mgraph.NodeMethod, Name: m})
		_, _ = g.AddEdge(mgraph.Edge{From: tID, To: mID, Kind: mgraph.EdgeContains})
	}
	return g
}
