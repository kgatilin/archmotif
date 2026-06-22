package metrics

import (
	"context"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// Eigenvalues reports the FULL sorted spectrum of the symmetric Laplacian of
// g's undirected projection (details.eigenvalues). The graph-scope value is the
// spectrum length. Consumers (e.g. archlint's SpectralDistance) use the full
// list as a structural fingerprint. A descriptor; never a gate.
type Eigenvalues struct{}

// Name returns the metric identifier.
func (Eigenvalues) Name() string { return "eigenvalues" }

// Description returns the metric documentation string.
func (Eigenvalues) Description() string {
	return "full sorted spectrum of the symmetric Laplacian (details.eigenvalues)"
}

// Configurable returns user-tunable knobs (none).
func (Eigenvalues) Configurable() map[string]any { return map[string]any{} }

// Compute returns one graph-scope record carrying the full spectrum.
func (Eigenvalues) Compute(ctx context.Context, g *mgraph.Graph) ([]Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	sp := laplacianSpectrum(g)
	if len(sp) == 0 {
		return []Record{{
			Metric:  "eigenvalues",
			Scope:   ScopeGraph,
			Value:   0,
			Details: map[string]any{"note": "fewer than 2 nodes or eigendecomposition failed", "eigenvalues": []float64{}},
		}}, nil
	}

	return []Record{{
		Metric:  "eigenvalues",
		Scope:   ScopeGraph,
		Value:   float64(len(sp)),
		Details: map[string]any{"eigenvalues": sp},
	}}, nil
}

func init() { Register(Eigenvalues{}) }
