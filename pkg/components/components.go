// Package components provides connected-component analysis for archmotif graphs.
// It computes undirected connected components and identifies the "center" node
// in each component via eigenvector centrality — the dominant eigenvector of the
// component's adjacency matrix.
//
// This is the public API; internal/metrics/components.go provides just a count,
// whereas this package returns full component membership and centrality data
// suitable for UI/tooling that needs to highlight isolated subgraphs.
package components

import (
	"sort"

	"gonum.org/v1/gonum/graph/simple"
	"gonum.org/v1/gonum/graph/topo"
	"gonum.org/v1/gonum/mat"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
	archmotifimport "github.com/kgatilin/archmotif/pkg/archmotifimport"
)

// Graph is a type alias for archmotif's graph type.
type Graph = archmotifimport.Graph

// Component describes a single connected component.
type Component struct {
	// Size is the number of nodes in this component.
	Size int `json:"size"`
	// CenterNodeID is the node with highest eigenvector centrality in this component.
	CenterNodeID string `json:"center"`
	// Centrality is the eigenvector centrality score of the center node.
	Centrality float64 `json:"centrality"`
	// Members lists all node IDs in this component (sorted alphabetically).
	Members []string `json:"members,omitempty"`
}

// Result holds the full analysis output.
type Result struct {
	// NodeCount is the total number of nodes in the analyzed subgraph.
	NodeCount int `json:"node_count"`
	// EdgeCount is the total number of edges in the analyzed subgraph.
	EdgeCount int `json:"edge_count"`
	// ComponentCount is the number of connected components.
	ComponentCount int `json:"component_count"`
	// SizeHistogram maps component size to count of components with that size.
	SizeHistogram map[int]int `json:"size_histogram"`
	// Components lists each component, sorted by size descending.
	Components []Component `json:"components"`
}

// Analyze computes connected components of the graph's undirected projection.
// For each component, it identifies the center node (highest eigenvector centrality).
// Components are sorted by size descending.
//
// If nodeIDs is non-empty, only the induced subgraph on those nodes is analyzed.
// This enables package-scoped analysis within a larger codebase graph.
func Analyze(g *Graph, nodeIDs []string) Result {
	if g == nil {
		return Result{SizeHistogram: map[int]int{}}
	}

	// Induce subgraph if nodeIDs specified.
	subg := induceSubgraph(g, nodeIDs)

	nodes := subg.Nodes()
	nodeCount := len(nodes)
	edgeCount := subg.EdgeCount()

	if nodeCount == 0 {
		return Result{SizeHistogram: map[int]int{}}
	}

	// Build undirected view for component detection.
	uv := toUndirected(subg)

	// Compute connected components.
	cc := topo.ConnectedComponents(uv.G)

	// Build histogram and component details.
	histogram := make(map[int]int)
	components := make([]Component, 0, len(cc))

	for _, comp := range cc {
		size := len(comp)
		histogram[size]++

		// Extract member IDs.
		members := make([]string, 0, size)
		for _, gnode := range comp {
			if id, ok := uv.IDs[gnode.ID()]; ok {
				members = append(members, id)
			}
		}
		sort.Strings(members)

		// Compute eigenvector centrality for this component.
		center, centrality := computeComponentCenter(uv, members)

		components = append(components, Component{
			Size:         size,
			CenterNodeID: center,
			Centrality:   centrality,
			Members:      members,
		})
	}

	// Sort components by size descending.
	sort.Slice(components, func(i, j int) bool {
		return components[i].Size > components[j].Size
	})

	return Result{
		NodeCount:      nodeCount,
		EdgeCount:      edgeCount,
		ComponentCount: len(cc),
		SizeHistogram:  histogram,
		Components:     components,
	}
}

// undirectedView projects a typed graph onto a gonum simple.UndirectedGraph.
type undirectedView struct {
	G    *simple.UndirectedGraph
	IDs  map[int64]string // gonum int -> stable ID
	Idx  map[string]int64 // stable ID -> gonum int
	Sort []string         // stable IDs in deterministic order
}

func toUndirected(g *mgraph.Graph) undirectedView {
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
		fi, ok1 := out.Idx[e.From]
		ti, ok2 := out.Idx[e.To]
		if !ok1 || !ok2 || fi == ti {
			continue
		}
		if out.G.HasEdgeBetween(fi, ti) {
			continue
		}
		out.G.SetEdge(simple.Edge{F: simple.Node(fi), T: simple.Node(ti)})
	}
	return out
}

// induceSubgraph returns a new graph containing only the specified nodes
// and edges between them. If nodeIDs is empty, returns g unchanged.
func induceSubgraph(g *mgraph.Graph, nodeIDs []string) *mgraph.Graph {
	if len(nodeIDs) == 0 {
		return g
	}

	nodeSet := make(map[string]bool, len(nodeIDs))
	for _, id := range nodeIDs {
		nodeSet[id] = true
	}

	sub := mgraph.New()
	for _, n := range g.Nodes() {
		if nodeSet[n.ID] {
			sub.AddNode(n)
		}
	}
	for _, e := range g.Edges() {
		if nodeSet[e.From] && nodeSet[e.To] {
			_, _ = sub.AddEdge(e)
		}
	}
	return sub
}

// computeComponentCenter finds the node with highest eigenvector centrality
// in a connected component. For size-1 components, returns the single node
// with centrality 1.0.
func computeComponentCenter(uv undirectedView, members []string) (string, float64) {
	n := len(members)
	if n == 0 {
		return "", 0
	}
	if n == 1 {
		return members[0], 1.0
	}

	// Build local adjacency matrix for this component.
	localIdx := make(map[string]int, n)
	for i, id := range members {
		localIdx[id] = i
	}

	adj := mat.NewDense(n, n, nil)
	for _, id := range members {
		gi := uv.Idx[id]
		it := uv.G.From(gi)
		for it.Next() {
			neighbor := it.Node()
			neighborID := uv.IDs[neighbor.ID()]
			if li, ok := localIdx[neighborID]; ok {
				adj.Set(localIdx[id], li, 1)
				adj.Set(li, localIdx[id], 1)
			}
		}
	}

	// Power iteration to find dominant eigenvector.
	centralities := powerIteration(adj, n, 100)

	// Find node with maximum centrality.
	maxIdx := 0
	maxVal := centralities[0]
	for i := 1; i < n; i++ {
		if centralities[i] > maxVal {
			maxVal = centralities[i]
			maxIdx = i
		}
	}

	return members[maxIdx], maxVal
}

// powerIteration computes the dominant eigenvector of a symmetric matrix
// using power iteration. Returns the eigenvector normalized to sum to 1.
func powerIteration(A *mat.Dense, n, maxIter int) []float64 {
	// Start with uniform vector.
	x := make([]float64, n)
	for i := range x {
		x[i] = 1.0 / float64(n)
	}

	xVec := mat.NewVecDense(n, x)
	result := mat.NewVecDense(n, nil)

	for iter := 0; iter < maxIter; iter++ {
		// y = A * x
		result.MulVec(A, xVec)

		// Normalize.
		norm := 0.0
		for i := 0; i < n; i++ {
			v := result.AtVec(i)
			norm += v * v
		}
		if norm < 1e-15 {
			// All zeros — return uniform.
			for i := range x {
				x[i] = 1.0 / float64(n)
			}
			return x
		}
		norm = sqrt(norm)
		for i := 0; i < n; i++ {
			xVec.SetVec(i, result.AtVec(i)/norm)
		}
	}

	// Extract final centrality scores.
	out := make([]float64, n)
	for i := 0; i < n; i++ {
		out[i] = abs(xVec.AtVec(i))
	}
	return out
}

func sqrt(x float64) float64 {
	if x <= 0 {
		return 0
	}
	// Newton-Raphson.
	guess := x
	for i := 0; i < 20; i++ {
		guess = 0.5 * (guess + x/guess)
	}
	return guess
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
