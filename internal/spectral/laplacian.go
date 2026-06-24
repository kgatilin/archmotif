// Package spectral provides spectral analysis helpers for graphs,
// factored from the metrics package to enable reuse by both SpectralGap
// (the metric) and SpectralCluster (the public clustering API).
//
// The package follows ADR-012: all computations project directed graphs
// onto an undirected symmetrized view. The Laplacian is computed on this
// undirected projection.
package spectral

import (
	"sort"

	"gonum.org/v1/gonum/graph/simple"
	"gonum.org/v1/gonum/graph/spectral"
	"gonum.org/v1/gonum/mat"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// UndirectedView projects a typed graph onto a gonum simple.UndirectedGraph.
// Self-edges and duplicate edges (same endpoints, different kinds) are
// merged into a single undirected edge between the endpoints.
//
// The kinds filter restricts which edge kinds to include; nil means all.
type UndirectedView struct {
	G    *simple.UndirectedGraph
	IDs  map[int64]string // gonum int -> stable ID
	Idx  map[string]int64 // stable ID -> gonum int
	Sort []string         // stable IDs in deterministic order
}

// ToUndirected builds an undirected view of g, optionally filtering by edge kinds.
func ToUndirected(g *mgraph.Graph, kinds map[mgraph.EdgeKind]bool) UndirectedView {
	out := UndirectedView{
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

// EigenResult holds the eigenvalues and eigenvectors from decomposition.
type EigenResult struct {
	// Values are eigenvalues sorted in ascending order.
	Values []float64
	// Vectors is the n×n matrix of eigenvectors as columns, ordered
	// to match Values (column i is the eigenvector for Values[i]).
	Vectors *mat.Dense
	// N is the number of nodes.
	N int
}

// ComputeEigen computes the eigendecomposition of a graph's Laplacian.
// If normalized is true, uses the normalized Laplacian L_sym = D^(-1/2) L D^(-1/2).
// Returns nil if the graph has fewer than 2 nodes or if EigenSym fails.
func ComputeEigen(uv UndirectedView, normalized bool) *EigenResult {
	n := uv.G.Nodes().Len()
	if n < 2 {
		return nil
	}

	var lap *mat.SymDense
	if normalized {
		lap = buildNormalizedLaplacian(uv, n)
	} else {
		lap = buildLaplacian(uv, n)
	}

	var es mat.EigenSym
	if ok := es.Factorize(lap, true); !ok {
		return nil
	}

	values := es.Values(nil)
	// Get eigenvectors as a dense matrix.
	var vectors mat.Dense
	es.VectorsTo(&vectors)

	// Sort eigenvalues ascending and reorder vectors to match.
	indices := make([]int, len(values))
	for i := range indices {
		indices[i] = i
	}
	sort.Slice(indices, func(i, j int) bool {
		return values[indices[i]] < values[indices[j]]
	})

	sortedValues := make([]float64, len(values))
	sortedVectors := mat.NewDense(n, n, nil)
	for newIdx, oldIdx := range indices {
		sortedValues[newIdx] = values[oldIdx]
		// Copy column oldIdx to column newIdx.
		for row := 0; row < n; row++ {
			sortedVectors.Set(row, newIdx, vectors.At(row, oldIdx))
		}
	}

	// Clamp tiny negatives to zero (FP noise in Laplacian eigendecomp).
	for i, v := range sortedValues {
		if v < 0 && v > -1e-10 {
			sortedValues[i] = 0
		}
	}

	return &EigenResult{
		Values:  sortedValues,
		Vectors: sortedVectors,
		N:       n,
	}
}

// buildLaplacian constructs the standard graph Laplacian L = D - A.
func buildLaplacian(uv UndirectedView, n int) *mat.SymDense {
	lap := spectral.NewLaplacian(uv.G)
	sd := mat.NewSymDense(n, nil)
	for i := 0; i < n; i++ {
		for j := i; j < n; j++ {
			sd.SetSym(i, j, lap.At(i, j))
		}
	}
	return sd
}

// buildNormalizedLaplacian constructs L_sym = D^(-1/2) L D^(-1/2).
// For nodes with degree 0, we treat D^(-1/2) as 0 to avoid division by zero.
func buildNormalizedLaplacian(uv UndirectedView, n int) *mat.SymDense {
	// Compute degrees.
	degrees := make([]float64, n)
	for i := int64(0); i < int64(n); i++ {
		deg := 0
		it := uv.G.From(i)
		for it.Next() {
			deg++
		}
		degrees[i] = float64(deg)
	}

	// Compute D^(-1/2).
	dInvSqrt := make([]float64, n)
	for i, d := range degrees {
		if d > 0 {
			dInvSqrt[i] = 1.0 / sqrtFloat(d)
		}
	}

	// Build normalized Laplacian.
	sd := mat.NewSymDense(n, nil)
	for i := 0; i < n; i++ {
		for j := i; j < n; j++ {
			if i == j {
				// Diagonal: 1 if degree > 0, else 0.
				if degrees[i] > 0 {
					sd.SetSym(i, j, 1.0)
				}
			} else {
				// Off-diagonal: -1/(sqrt(d_i)*sqrt(d_j)) if edge exists.
				if uv.G.HasEdgeBetween(int64(i), int64(j)) && degrees[i] > 0 && degrees[j] > 0 {
					sd.SetSym(i, j, -dInvSqrt[i]*dInvSqrt[j])
				}
			}
		}
	}
	return sd
}

func sqrtFloat(x float64) float64 {
	// Simple sqrt using math package would require import.
	// Use gonum's internal or manual iteration. For simplicity:
	if x <= 0 {
		return 0
	}
	// Newton-Raphson iteration.
	guess := x
	for i := 0; i < 20; i++ {
		guess = 0.5 * (guess + x/guess)
	}
	return guess
}
