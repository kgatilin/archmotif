package metrics

import (
	"sort"

	"gonum.org/v1/gonum/graph/simple"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// undirectedView projects g onto a gonum simple.UndirectedGraph.
// Self-edges and duplicate edges (same endpoints, different kinds) are
// merged into a single undirected edge between the endpoints.
//
// Returns the gonum graph plus an index from gonum int64 IDs back to
// our stable string node IDs (and vice versa) so downstream metrics
// can map results back to typed nodes.
//
// The "treat directed edges as undirected" choice is documented in
// ADR-012 (spectral) and ADR-014 (modularity): for Stage 3 lenses
// we prioritise a clean Laplacian/community view over preserving
// edge direction. Directed-only metrics (cycle rank) build their
// own gonum view from the dependency subgraph.
type undirectedView struct {
	G    *simple.UndirectedGraph
	IDs  map[int64]string // gonum int -> stable ID
	Idx  map[string]int64 // stable ID -> gonum int
	Sort []string         // stable IDs in deterministic order
}

func toUndirected(g *mgraph.Graph, kinds map[mgraph.EdgeKind]bool) undirectedView {
	out := undirectedView{
		G:   simple.NewUndirectedGraph(),
		IDs: map[int64]string{},
		Idx: map[string]int64{},
	}
	nodes := g.Nodes()
	out.Sort = make([]string, 0, len(nodes))
	// Sort by stable ID for deterministic gonum integer IDs.
	stable := make([]string, 0, len(nodes))
	for _, n := range nodes {
		stable = append(stable, n.ID)
	}
	sort.Strings(stable)
	for i, id := range stable {
		gi := int64(i)
		out.G.AddNode(simple.Node(gi))
		out.IDs[gi] = id
		out.Idx[id] = gi
		out.Sort = append(out.Sort, id)
	}
	for _, e := range g.Edges() {
		if kinds != nil && !kinds[e.Kind] {
			continue
		}
		fi, ok1 := out.Idx[e.From]
		ti, ok2 := out.Idx[e.To]
		if !ok1 || !ok2 || fi == ti {
			continue
		}
		// Skip if edge already present.
		if out.G.HasEdgeBetween(fi, ti) {
			continue
		}
		out.G.SetEdge(simple.Edge{F: simple.Node(fi), T: simple.Node(ti)})
	}
	return out
}

// directedView projects g onto a gonum simple.DirectedGraph,
// keeping only the edge kinds present in `kinds` (nil == all). Used
// by metrics that need direction (cycle rank). Self-edges are dropped
// because gonum simple graphs reject them.
type directedView struct {
	G   *simple.DirectedGraph
	IDs map[int64]string
	Idx map[string]int64
}

func toDirected(g *mgraph.Graph, kinds map[mgraph.EdgeKind]bool) directedView {
	out := directedView{
		G:   simple.NewDirectedGraph(),
		IDs: map[int64]string{},
		Idx: map[string]int64{},
	}
	nodes := g.Nodes()
	stable := make([]string, 0, len(nodes))
	for _, n := range nodes {
		stable = append(stable, n.ID)
	}
	sort.Strings(stable)
	for i, id := range stable {
		gi := int64(i)
		out.G.AddNode(simple.Node(gi))
		out.IDs[gi] = id
		out.Idx[id] = gi
	}
	for _, e := range g.Edges() {
		if kinds != nil && !kinds[e.Kind] {
			continue
		}
		fi, ok1 := out.Idx[e.From]
		ti, ok2 := out.Idx[e.To]
		if !ok1 || !ok2 || fi == ti {
			continue
		}
		if out.G.HasEdgeFromTo(fi, ti) {
			continue
		}
		out.G.SetEdge(simple.Edge{F: simple.Node(fi), T: simple.Node(ti)})
	}
	return out
}
