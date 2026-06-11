// Package anomaliestest builds hand-crafted typed graphs used by
// anomaly detection tests. Stage 4 fixtures are constructed in code
// (not on disk) for the same reasons as the metricstest fixtures:
// the detectors operate on metrics.Record + *graph.Graph in memory,
// and asserting closed-form expectations against named builders
// reads more cleanly than parsing JSON in every test.
package anomaliestest

import (
	"fmt"
	"strconv"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// PlantedMotif builds a graph with N copies of the (constructor,
// concrete type, interface) triangle — the same shape as
// metricstest.TwoStores but parameterised by N. Used as the Stage 4
// "planted anomaly" verify case from issue #5: a synthetic graph
// with an obvious motif × N should be flagged with high score by
// the motif_redundancy detector.
//
// The graph contains:
//   - one Package (host)
//   - one shared interface Type "I"
//   - N concrete types T_i each implementing I
//   - N constructor functions NewT_i, each Returns I and Calls T_i
//
// The motif metric will see N instances of the canonical
// (constructor, type, interface) triangle (cross-package via the
// shared interface, so the abstraction filter retains them).
func PlantedMotif(n int) *mgraph.Graph {
	g := mgraph.New()
	pkg := "pkg:fixture/store"
	g.AddNode(mgraph.Node{ID: pkg, Kind: mgraph.NodePackage, Name: "store"})

	add := func(id, name string, kind mgraph.NodeKind, line int) string {
		g.AddNode(mgraph.Node{
			ID:    id,
			Kind:  kind,
			Name:  name,
			Pos:   mgraph.Position{File: "fixture/store/" + name + ".go", Line: line, Col: 1},
			QName: "fixture/store." + name,
		})
		_, _ = g.AddEdge(mgraph.Edge{From: pkg, To: id, Kind: mgraph.EdgeContains})
		return id
	}

	ifaceID := add("fixture/store/I.go:1:1:type:I", "I", mgraph.NodeType, 1)

	for i := 0; i < n; i++ {
		typeName := "T" + strconv.Itoa(i)
		ctorName := "NewT" + strconv.Itoa(i)
		typeID := add("fixture/store/"+typeName+".go:1:1:type:"+typeName, typeName, mgraph.NodeType, 1)
		ctorID := add("fixture/store/"+typeName+".go:5:1:function:"+ctorName, ctorName, mgraph.NodeFunction, 5)
		_, _ = g.AddEdge(mgraph.Edge{From: typeID, To: ifaceID, Kind: mgraph.EdgeImplements})
		_, _ = g.AddEdge(mgraph.Edge{From: ctorID, To: ifaceID, Kind: mgraph.EdgeReturns})
		_, _ = g.AddEdge(mgraph.Edge{From: ctorID, To: typeID, Kind: mgraph.EdgeCalls})
	}
	return g
}

// CycleGraph builds a graph with k disjoint directed cycles of
// length 3 plus a single acyclic path. Used to verify the cycle_rank
// detector flags every non-trivial SCC.
func CycleGraph(numCycles int) *mgraph.Graph {
	g := mgraph.New()
	for c := 0; c < numCycles; c++ {
		pkgID := "pkg:fixture/cycle" + strconv.Itoa(c)
		g.AddNode(mgraph.Node{ID: pkgID, Kind: mgraph.NodePackage, Name: "cycle" + strconv.Itoa(c)})
		ids := make([]string, 3)
		for j := 0; j < 3; j++ {
			id := fmt.Sprintf("fixture/cycle%d/main.go:%d:1:function:N%d", c, j+1, j)
			ids[j] = id
			g.AddNode(mgraph.Node{
				ID:   id,
				Kind: mgraph.NodeFunction,
				Name: fmt.Sprintf("c%d_N%d", c, j),
				Pos:  mgraph.Position{File: fmt.Sprintf("fixture/cycle%d/main.go", c), Line: j + 1, Col: 1},
			})
			_, _ = g.AddEdge(mgraph.Edge{From: pkgID, To: id, Kind: mgraph.EdgeContains})
		}
		for j := 0; j < 3; j++ {
			_, _ = g.AddEdge(mgraph.Edge{From: ids[j], To: ids[(j+1)%3], Kind: mgraph.EdgeCalls})
		}
	}
	// Add an acyclic path so we have non-cyclic baseline content too.
	pkgID := "pkg:fixture/path"
	g.AddNode(mgraph.Node{ID: pkgID, Kind: mgraph.NodePackage, Name: "path"})
	prev := ""
	for j := 0; j < 4; j++ {
		id := fmt.Sprintf("fixture/path/main.go:%d:1:function:P%d", j+1, j)
		g.AddNode(mgraph.Node{
			ID:   id,
			Kind: mgraph.NodeFunction,
			Name: "P" + strconv.Itoa(j),
			Pos:  mgraph.Position{File: "fixture/path/main.go", Line: j + 1, Col: 1},
		})
		_, _ = g.AddEdge(mgraph.Edge{From: pkgID, To: id, Kind: mgraph.EdgeContains})
		if prev != "" {
			_, _ = g.AddEdge(mgraph.Edge{From: prev, To: id, Kind: mgraph.EdgeCalls})
		}
		prev = id
	}
	return g
}

// OversizePackage builds a graph with two packages: "big" containing
// many functions, "small" containing few. Used to verify the
// modularity detector flags the oversize community.
func OversizePackage(bigSize, smallSize int) *mgraph.Graph {
	g := mgraph.New()
	mk := func(prefix string, count int) {
		pkgID := "pkg:fixture/" + prefix
		g.AddNode(mgraph.Node{ID: pkgID, Kind: mgraph.NodePackage, Name: prefix})
		for i := 0; i < count; i++ {
			id := fmt.Sprintf("fixture/%s/main.go:%d:1:function:F%d", prefix, i+1, i)
			g.AddNode(mgraph.Node{
				ID:   id,
				Kind: mgraph.NodeFunction,
				Name: "F" + strconv.Itoa(i),
				Pos:  mgraph.Position{File: fmt.Sprintf("fixture/%s/main.go", prefix), Line: i + 1, Col: 1},
			})
			_, _ = g.AddEdge(mgraph.Edge{From: pkgID, To: id, Kind: mgraph.EdgeContains})
		}
	}
	mk("big", bigSize)
	for i := 0; i < 5; i++ {
		mk("small"+strconv.Itoa(i), smallSize)
	}
	return g
}
