package metrics

import (
	"context"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// Radius reports the spectral radius — the largest eigenvalue λ_max of the
// symmetric Laplacian of g's undirected projection. A magnitude descriptor;
// a signal, never a gate.
type Radius struct{}

// Name returns the metric identifier.
func (Radius) Name() string { return "radius" }

// Description returns the metric documentation string.
func (Radius) Description() string {
	return "spectral radius (largest Laplacian eigenvalue λ_max)"
}

// Configurable returns user-tunable knobs (none).
func (Radius) Configurable() map[string]any { return map[string]any{} }

// Compute returns one graph-scope record with λ_max.
func (Radius) Compute(ctx context.Context, g *mgraph.Graph) ([]Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	sp := laplacianSpectrum(g)
	if len(sp) == 0 {
		return []Record{{
			Metric:  "radius",
			Scope:   ScopeGraph,
			Value:   0,
			Details: map[string]any{"note": "fewer than 2 nodes or eigendecomposition failed"},
		}}, nil
	}

	return []Record{{Metric: "radius", Scope: ScopeGraph, Value: sp[len(sp)-1]}}, nil
}

func init() { Register(Radius{}) }
