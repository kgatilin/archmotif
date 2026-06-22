package metrics

import (
	"context"
	"sort"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// Curvature computes the combinatorial Forman-Ricci curvature κ over the
// undirected projection of g (Sreejith et al. 2016, unweighted form):
//
//	κ(e=(u,v)) = 4 − deg(u) − deg(v) + 3·|triangles through e|
//
// Bridge edges between hubs land deeply negative; triangle (redundant) edges
// land at κ ≥ 1. Emits one ScopeEdge record per edge plus one ScopeGraph record
// with the mean κ. A magnitude descriptor; a signal, never a gate.
type Curvature struct{}

// Name returns the metric identifier.
func (Curvature) Name() string { return "curvature" }

// Description returns the metric documentation string.
func (Curvature) Description() string {
	return "combinatorial Forman-Ricci curvature per edge (undirected projection)"
}

// Configurable returns user-tunable knobs (none).
func (Curvature) Configurable() map[string]any { return map[string]any{} }

// Compute returns per-edge κ records plus a graph-scope mean κ.
func (Curvature) Compute(ctx context.Context, g *mgraph.Graph) ([]Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	uv := toUndirected(g, nil)

	type edgeKappa struct {
		target string
		kappa  float64
	}

	var edges []edgeKappa

	var sum float64

	it := uv.G.Edges()
	for it.Next() {
		e := it.Edge()
		uID := e.From().ID()
		vID := e.To().ID()

		du := uv.G.From(uID).Len()
		dv := uv.G.From(vID).Len()
		tri := triangleCount(uv, uID, vID)

		kappa := float64(4 - du - dv + 3*tri)
		sum += kappa

		a, b := uv.IDs[uID], uv.IDs[vID]
		if b < a {
			a, b = b, a
		}

		edges = append(edges, edgeKappa{target: a + "--" + b, kappa: kappa})
	}

	sort.Slice(edges, func(i, j int) bool { return edges[i].target < edges[j].target })

	out := make([]Record, 0, len(edges)+1)

	mean := 0.0
	if len(edges) > 0 {
		mean = sum / float64(len(edges))
	}

	out = append(out, Record{Metric: "curvature", Scope: ScopeGraph, Value: mean})

	for _, ek := range edges {
		out = append(out, Record{Metric: "curvature", Scope: ScopeEdge, Target: ek.target, Value: ek.kappa})
	}

	return out, nil
}

// triangleCount counts common neighbours of u and v (triangles through edge uv).
func triangleCount(uv undirectedView, uID, vID int64) int {
	tri := 0

	nb := uv.G.From(uID)
	for nb.Next() {
		w := nb.Node().ID()
		if w == vID {
			continue
		}

		if uv.G.HasEdgeBetween(w, vID) {
			tri++
		}
	}

	return tri
}

func init() { Register(Curvature{}) }
