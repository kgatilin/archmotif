package metrics

import (
	"sort"

	"gonum.org/v1/gonum/mat"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// adjacencyDense materialises g into a dense N×N adjacency matrix A with
// stable string-ID ordering. Cell A[i][j] = 1 when there exists a directed
// edge from ids[i] to ids[j] whose kind is in `kinds`; otherwise 0. Self
// edges are dropped (consistent with toDirected, which our other directed
// metrics reuse). Duplicate edge kinds between the same pair collapse to a
// single 1 — A is a 0/1 matrix.
//
// Pass nil/empty kinds to include every edge kind in the typed graph. The
// returned ids slice is sorted; callers are expected to use it for both
// row/column lookup and for mapping back to mgraph.Node values.
//
// This helper is the single place where graph traversal happens for the
// matrix validators. Everything downstream operates on *mat.Dense.
func adjacencyDense(g *mgraph.Graph, kinds map[mgraph.EdgeKind]bool) (*mat.Dense, []string, error) {
	nodes := g.Nodes()
	if len(nodes) == 0 {
		return mat.NewDense(0, 0, nil), nil, nil
	}
	ids := make([]string, 0, len(nodes))
	for _, n := range nodes {
		ids = append(ids, n.ID)
	}
	sort.Strings(ids)
	idx := make(map[string]int, len(ids))
	for i, id := range ids {
		idx[id] = i
	}
	n := len(ids)
	A := mat.NewDense(n, n, nil)
	for _, e := range g.Edges() {
		if len(kinds) > 0 && !kinds[e.Kind] {
			continue
		}
		i, ok1 := idx[e.From]
		j, ok2 := idx[e.To]
		if !ok1 || !ok2 || i == j {
			continue
		}
		A.Set(i, j, 1)
	}
	return A, ids, nil
}

// defaultDependencyEdgeKinds is the conventional "this edge represents an
// architectural dependency" set used by every matrix validator unless
// noted otherwise. Mirrors cycle_rank's selection (Calls + DependsOn) but
// adds UsesType / Returns / References so type-level coupling between
// adapter and domain shows up in the layer mask.
func defaultDependencyEdgeKinds() map[mgraph.EdgeKind]bool {
	return map[mgraph.EdgeKind]bool{
		mgraph.EdgeCalls:      true,
		mgraph.EdgeCallsFrom:  true,
		mgraph.EdgeDependsOn:  true,
		mgraph.EdgeUsesType:   true,
		mgraph.EdgeReturns:    true,
		mgraph.EdgeReferences: true,
	}
}
