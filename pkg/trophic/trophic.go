// Package trophic derives emergent architectural layers from the direction of
// dependency edges, with no declared policy. It computes a real-valued
// "trophic height" per node by solving the symmetrized graph Laplacian system
// Λh = (out-degree − in-degree) — the standard trophic-level construction from
// network science (MacKay, Johnson & Sansom, "How directed is a directed
// network?", 2020) — then:
//
//   - reports an incoherence number F0 ∈ [0,1] (0 = perfectly layered DAG,
//     ~1 = a tangle) summarising how layered the code is;
//   - cuts nodes into emergent layers by gaps in the height distribution;
//   - flags "backward" edges that point UP the hierarchy (a low layer depending
//     on a higher one — a dependency inversion);
//   - reports strongly-connected components, where layering is undefined.
//
// Foundation nodes (depended upon, depending on little) sink to height 0; entry
// points (depend on much, depended on by nothing) rise to the top.
//
// This is unsupervised: unlike a policy check, it needs no declared
// domain/adapter rules. It answers "are there layers, and where are the
// inversions?" for unfamiliar code.
package trophic

import (
	"math"
	"sort"

	"gonum.org/v1/gonum/graph/simple"
	"gonum.org/v1/gonum/graph/topo"
	"gonum.org/v1/gonum/mat"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
	archmotifimport "github.com/kgatilin/archmotif/pkg/archmotifimport"
)

// Graph is a type alias for archmotif's graph type.
type Graph = archmotifimport.Graph

// DefaultEdgeKinds is the directional "flow" edge set the analysis runs on:
// dependency edges that point from a depender to its dependee. Structural
// `contains` and the inverse `callsFrom` traversal edge are deliberately
// excluded — including them would wash out edge direction and destroy the
// layering signal. This set is fixed on purpose; changing it changes what
// "a layer" means.
var DefaultEdgeKinds = []string{
	string(mgraph.EdgeCalls),
	string(mgraph.EdgeUsesType),
	string(mgraph.EdgeReturns),
	string(mgraph.EdgeImplements),
}

const (
	// layerGap is the minimum jump in trophic height that separates two
	// layers. Heights are normalised so an ideal hierarchy spaces edges by
	// 1.0; a gap above half a level marks a real stratum boundary.
	layerGap = 0.5
	// backwardSpan is the minimum upward reach (h[to] − h[from]) for a
	// dependency edge to count as an inversion, filtering intra-layer jitter
	// from the continuous solve.
	backwardSpan = 0.5
)

// Options configures the analysis.
type Options struct {
	// NodeIDs restricts analysis to the induced subgraph on these nodes.
	// Empty means the whole graph.
	NodeIDs []string
	// EdgeKinds selects which edge kinds carry direction. Empty uses
	// DefaultEdgeKinds; callers should almost always leave it empty.
	EdgeKinds []string
}

// Layer is one emergent stratum, ordered by height (level 0 = foundation).
type Layer struct {
	Level   int      `json:"level"`
	Size    int      `json:"size"`
	Center  string   `json:"center"`
	Members []string `json:"members,omitempty"`
}

// Cycle is a strongly-connected component (size > 1) where height is undefined.
type Cycle struct {
	Size    int      `json:"size"`
	Center  string   `json:"center"`
	Members []string `json:"members,omitempty"`
}

// BackwardEdge is a dependency that points up the hierarchy (an inversion).
// Span is how many levels up it reaches; larger is worse.
type BackwardEdge struct {
	From string  `json:"from"`
	To   string  `json:"to"`
	Span float64 `json:"span"`
}

// Result is the full analysis output.
type Result struct {
	EdgeKindsUsed     []string       `json:"edge_kinds_used"`
	NodeCount         int            `json:"node_count"`
	EdgeCount         int            `json:"edge_count"`
	IncoherenceF0     float64        `json:"incoherence_f0"`
	LayerCount        int            `json:"layer_count"`
	Layers            []Layer        `json:"layers"`
	Cycles            []Cycle        `json:"cycles"`
	BackwardEdges     []BackwardEdge `json:"backward_edges"`
	BackwardEdgeCount int            `json:"backward_edge_count"`
}

// Analyze computes trophic layers for the (optionally node-restricted) graph.
func Analyze(g *Graph, opts Options) Result {
	kinds := opts.EdgeKinds
	if len(kinds) == 0 {
		kinds = DefaultEdgeKinds
	}
	kindSet := make(map[mgraph.EdgeKind]bool, len(kinds))
	used := make([]string, 0, len(kinds))
	for _, k := range kinds {
		ek := mgraph.EdgeKind(k)
		if kindSet[ek] {
			continue
		}
		kindSet[ek] = true
		used = append(used, k)
	}
	sort.Strings(used)

	result := Result{
		EdgeKindsUsed: used,
		Layers:        []Layer{},
		Cycles:        []Cycle{},
		BackwardEdges: []BackwardEdge{},
	}
	if g == nil {
		return result
	}

	// Stable node ordering → deterministic integer indices.
	nodeSet := nodeFilter(opts.NodeIDs)
	ids := make([]string, 0)
	kindOf := map[string]mgraph.NodeKind{}
	for _, n := range g.Nodes() {
		if nodeSet != nil && !nodeSet[n.ID] {
			continue
		}
		ids = append(ids, n.ID)
		kindOf[n.ID] = n.Kind
	}
	sort.Strings(ids)
	n := len(ids)
	result.NodeCount = n
	if n == 0 {
		return result
	}
	idx := make(map[string]int, n)
	for i, id := range ids {
		idx[id] = i
	}

	// Directed edges of the selected kinds, both endpoints in the node set.
	var edges []edgeIdx
	outDeg := make([]float64, n)
	inDeg := make([]float64, n)
	for _, e := range g.Edges() {
		if !kindSet[e.Kind] {
			continue
		}
		fi, ok1 := idx[e.From]
		ti, ok2 := idx[e.To]
		if !ok1 || !ok2 || fi == ti {
			continue
		}
		edges = append(edges, edgeIdx{fi, ti})
		outDeg[fi]++
		inDeg[ti]++
	}
	result.EdgeCount = len(edges)

	// Weakly-connected components over the selected edges; every node is
	// present, so edge-less nodes form their own singleton component.
	uv := simple.NewUndirectedGraph()
	for i := 0; i < n; i++ {
		uv.AddNode(simple.Node(int64(i)))
	}
	for _, e := range edges {
		if !uv.HasEdgeBetween(int64(e.from), int64(e.to)) {
			uv.SetEdge(simple.Edge{F: simple.Node(int64(e.from)), T: simple.Node(int64(e.to))})
		}
	}

	// Adjacency counts for the per-component Laplacian solve.
	adj := make([]map[int]float64, n) // symmetric weight a[i][j]+a[j][i]
	for i := range adj {
		adj[i] = map[int]float64{}
	}
	for _, e := range edges {
		adj[e.from][e.to]++
		adj[e.to][e.from]++
	}

	heights := make([]float64, n)
	for _, comp := range topo.ConnectedComponents(uv) {
		members := make([]int, 0, len(comp))
		for _, gn := range comp {
			members = append(members, int(gn.ID()))
		}
		sort.Ints(members)
		solveComponentHeights(members, adj, outDeg, inDeg, heights)
	}

	// F0 incoherence: mean over edges of (x − 1)^2 where x = h[from] − h[to]
	// is the trophic difference (ideal +1 per dependency hop).
	if len(edges) > 0 {
		var sum float64
		for _, e := range edges {
			x := heights[e.from] - heights[e.to]
			d := x - 1
			sum += d * d
		}
		result.IncoherenceF0 = sum / float64(len(edges))
	}

	// Total degree per node for picking layer/cycle centers.
	totalDeg := make([]float64, n)
	for i := 0; i < n; i++ {
		totalDeg[i] = outDeg[i] + inDeg[i]
	}

	result.Layers = cutLayers(ids, heights, totalDeg)
	result.LayerCount = len(result.Layers)

	// Backward edges: dependencies that reach up the hierarchy.
	for _, e := range edges {
		span := heights[e.to] - heights[e.from]
		if span > backwardSpan {
			result.BackwardEdges = append(result.BackwardEdges, BackwardEdge{
				From: ids[e.from],
				To:   ids[e.to],
				Span: round(span),
			})
		}
	}
	sort.Slice(result.BackwardEdges, func(i, j int) bool {
		a, b := result.BackwardEdges[i], result.BackwardEdges[j]
		if a.Span != b.Span {
			return a.Span > b.Span
		}
		if a.From != b.From {
			return a.From < b.From
		}
		return a.To < b.To
	})
	result.BackwardEdgeCount = len(result.BackwardEdges)

	result.Cycles = findCycles(ids, edges2dir(edges, n), totalDeg)

	return result
}

// solveComponentHeights fills heights for one weakly-connected component by
// solving the grounded symmetrized Laplacian Λh = (out − in), then shifting the
// component so its minimum height is 0. Singletons get height 0.
func solveComponentHeights(members []int, adj []map[int]float64, outDeg, inDeg, heights []float64) {
	m := len(members)
	if m == 1 {
		heights[members[0]] = 0
		return
	}
	local := make(map[int]int, m)
	for i, g := range members {
		local[g] = i
	}
	// degree d[i] = sum of symmetric weights; b[i] = out − in.
	d := make([]float64, m)
	b := make([]float64, m)
	for i, g := range members {
		var deg float64
		for _, w := range adj[g] {
			deg += w
		}
		d[i] = deg
		b[i] = outDeg[g] - inDeg[g]
	}
	// Ground node 0 (height 0); solve the reduced (m-1) PD system.
	r := m - 1
	lap := mat.NewSymDense(r, nil)
	for i := 1; i < m; i++ {
		lap.SetSym(i-1, i-1, d[i])
	}
	for i, g := range members {
		if i == 0 {
			continue
		}
		for gj, w := range adj[g] {
			j, ok := local[gj]
			if !ok || j == 0 || j <= i {
				continue
			}
			lap.SetSym(i-1, j-1, -w)
		}
	}
	rhs := mat.NewVecDense(r, nil)
	for i := 1; i < m; i++ {
		rhs.SetVec(i-1, b[i])
	}

	var chol mat.Cholesky
	if ok := chol.Factorize(lap); !ok {
		// Singular/ill-conditioned: leave the component flat at 0.
		for _, g := range members {
			heights[g] = 0
		}
		return
	}
	var sol mat.VecDense
	if err := chol.SolveVecTo(&sol, rhs); err != nil {
		for _, g := range members {
			heights[g] = 0
		}
		return
	}

	h := make([]float64, m)
	min := 0.0
	for i := 1; i < m; i++ {
		h[i] = sol.AtVec(i - 1)
		if h[i] < min {
			min = h[i]
		}
	}
	for i, g := range members {
		heights[g] = h[i] - min
	}
}

// cutLayers groups nodes into layers by gaps in the sorted height
// distribution. Level 0 is the lowest (foundation).
func cutLayers(ids []string, heights, totalDeg []float64) []Layer {
	n := len(ids)
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(i, j int) bool {
		if heights[order[i]] != heights[order[j]] {
			return heights[order[i]] < heights[order[j]]
		}
		return ids[order[i]] < ids[order[j]]
	})

	var layers []Layer
	var cur []int
	flush := func(level int) {
		if len(cur) == 0 {
			return
		}
		members := make([]string, len(cur))
		for i, g := range cur {
			members[i] = ids[g]
		}
		sort.Strings(members)
		layers = append(layers, Layer{
			Level:   level,
			Size:    len(members),
			Center:  centerOf(cur, ids, totalDeg),
			Members: members,
		})
		cur = nil
	}

	level := 0
	for i, g := range order {
		if i > 0 && heights[g]-heights[order[i-1]] > layerGap {
			flush(level)
			level++
		}
		cur = append(cur, g)
	}
	flush(level)
	return layers
}

// findCycles returns strongly-connected components of size > 1.
func findCycles(ids []string, dg *simple.DirectedGraph, totalDeg []float64) []Cycle {
	var cycles []Cycle
	for _, comp := range topo.TarjanSCC(dg) {
		if len(comp) <= 1 {
			continue
		}
		local := make([]int, 0, len(comp))
		members := make([]string, 0, len(comp))
		for _, gn := range comp {
			i := int(gn.ID())
			local = append(local, i)
			members = append(members, ids[i])
		}
		sort.Strings(members)
		cycles = append(cycles, Cycle{
			Size:    len(comp),
			Center:  centerOf(local, ids, totalDeg),
			Members: members,
		})
	}
	sort.Slice(cycles, func(i, j int) bool {
		if cycles[i].Size != cycles[j].Size {
			return cycles[i].Size > cycles[j].Size
		}
		return cycles[i].Center < cycles[j].Center
	})
	return cycles
}

// centerOf returns the highest-total-degree node in the set as its
// representative, tie-broken by ID for determinism.
func centerOf(members []int, ids []string, totalDeg []float64) string {
	best := -1
	for _, g := range members {
		if best < 0 || totalDeg[g] > totalDeg[best] ||
			(totalDeg[g] == totalDeg[best] && ids[g] < ids[best]) {
			best = g
		}
	}
	if best < 0 {
		return ""
	}
	return ids[best]
}

// edgeIdx is a directed edge between integer node indices.
type edgeIdx struct{ from, to int }

// edges2dir builds a gonum directed graph from the edge list for SCC analysis.
func edges2dir(edges []edgeIdx, n int) *simple.DirectedGraph {
	dg := simple.NewDirectedGraph()
	for i := 0; i < n; i++ {
		dg.AddNode(simple.Node(int64(i)))
	}
	for _, e := range edges {
		if e.from == e.to || dg.HasEdgeFromTo(int64(e.from), int64(e.to)) {
			continue
		}
		dg.SetEdge(simple.Edge{F: simple.Node(int64(e.from)), T: simple.Node(int64(e.to))})
	}
	return dg
}

func nodeFilter(ids []string) map[string]bool {
	if len(ids) == 0 {
		return nil
	}
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return set
}

func round(x float64) float64 { return math.Round(x*100) / 100 }
