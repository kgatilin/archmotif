package metrics

import (
	"context"

	"gonum.org/v1/gonum/graph/topo"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// Components reports the number of connected components of g's undirected
// projection (a disconnected architecture graph = isolated islands). A
// structural descriptor; a signal, never a gate.
type Components struct{}

// Name returns the metric identifier.
func (Components) Name() string { return "components" }

// Description returns the metric documentation string.
func (Components) Description() string {
	return "number of connected components (undirected projection)"
}

// Configurable returns user-tunable knobs (none).
func (Components) Configurable() map[string]any { return map[string]any{} }

// Compute returns one graph-scope record with the component count.
func (Components) Compute(ctx context.Context, g *mgraph.Graph) ([]Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	uv := toUndirected(g, nil)
	cc := topo.ConnectedComponents(uv.G)

	return []Record{{Metric: "components", Scope: ScopeGraph, Value: float64(len(cc))}}, nil
}

func init() { Register(Components{}) }
