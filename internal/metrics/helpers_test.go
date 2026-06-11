package metrics_test

import (
	"strconv"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
	"github.com/kgatilin/archmotif/internal/metrics"
)

// findGraph returns the first graph-scope record matching name, or nil.
func findGraph(recs []metrics.Record, name string) *metrics.Record {
	for i := range recs {
		if recs[i].Metric == name && recs[i].Scope == metrics.ScopeGraph {
			return &recs[i]
		}
	}
	return nil
}

// byScope filters records by scope.
func byScope(recs []metrics.Record, scope metrics.Scope) []metrics.Record {
	out := []metrics.Record{}
	for _, r := range recs {
		if r.Scope == scope {
			out = append(out, r)
		}
	}
	return out
}

// buildPurePath builds a path graph with n Function nodes and no
// Package wrapper, no Contains edges — the symmetrised Laplacian is
// the standard P_n adjacency. Used for closed-form spectral assertions.
func buildPurePath(n int) *mgraph.Graph {
	g := mgraph.New()
	ids := make([]string, n)
	for i := 0; i < n; i++ {
		id := "purepath/main.go:1:1:function:N" + strconv.Itoa(i)
		ids[i] = id
		g.AddNode(mgraph.Node{ID: id, Kind: mgraph.NodeFunction, Name: "N" + strconv.Itoa(i)})
	}
	for i := 0; i < n-1; i++ {
		_, _ = g.AddEdge(mgraph.Edge{From: ids[i], To: ids[i+1], Kind: mgraph.EdgeCalls})
	}
	return g
}

// buildPureClique builds K_n with no wrapper nodes. Used for closed-form
// spectral assertion (λ_2 = n).
func buildPureClique(n int) *mgraph.Graph {
	g := mgraph.New()
	ids := make([]string, n)
	for i := 0; i < n; i++ {
		id := "pureclique/main.go:1:1:function:N" + strconv.Itoa(i)
		ids[i] = id
		g.AddNode(mgraph.Node{ID: id, Kind: mgraph.NodeFunction, Name: "N" + strconv.Itoa(i)})
	}
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			_, _ = g.AddEdge(mgraph.Edge{From: ids[i], To: ids[j], Kind: mgraph.EdgeCalls})
		}
	}
	return g
}
