package metrics

import (
	"context"

	"gonum.org/v1/gonum/graph/topo"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// SCC reports the TOTAL number of strongly-connected components of g's directed
// projection (every node is in exactly one SCC, so this counts condensation
// nodes, including singletons). Distinct from cycle_rank, which counts only the
// non-trivial SCCs (the actual dependency cycles). A descriptor.
type SCC struct{}

// Name returns the metric identifier.
func (SCC) Name() string { return "scc" }

// Description returns the metric documentation string.
func (SCC) Description() string {
	return "total number of strongly-connected components (condensation size)"
}

// Configurable returns user-tunable knobs (none).
func (SCC) Configurable() map[string]any { return map[string]any{} }

// Compute returns one graph-scope record with the total SCC count.
func (SCC) Compute(ctx context.Context, g *mgraph.Graph) ([]Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	dv := toDirected(g, nil)
	sccs := topo.TarjanSCC(dv.G)

	return []Record{{Metric: "scc", Scope: ScopeGraph, Value: float64(len(sccs))}}, nil
}

func init() { Register(SCC{}) }
