package metrics

import (
	"context"
	"sort"

	"gonum.org/v1/gonum/graph/spectral"
	"gonum.org/v1/gonum/mat"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// SpectralGap computes the algebraic connectivity of g — the
// second-smallest eigenvalue of the graph Laplacian L = D − A. The
// graph is symmetrised first (per ADR-012): all directed archmotif
// edges become undirected gonum edges. A disconnected graph has
// connectivity 0; an empty graph reports 0.
//
// Output:
//   - one ScopeGraph record carrying the algebraic connectivity
//     (i.e. λ₂ of the Laplacian).
//   - details.eigenvalues holds the first up-to-five smallest
//     eigenvalues for context (Stage 4 may use the full spectrum).
type SpectralGap struct{}

// Name returns the metric identifier.
func (SpectralGap) Name() string { return "spectral_gap" }

// Description returns the metric documentation string.
func (SpectralGap) Description() string {
	return "algebraic connectivity (second-smallest eigenvalue of the symmetric Laplacian)"
}

// Configurable returns user-tunable knobs (none for spectral gap;
// directed→undirected symmetrisation is documented, not flagged).
func (SpectralGap) Configurable() map[string]any { return map[string]any{} }

// Compute builds the symmetric Laplacian and runs EigenSym. Empty and
// single-node graphs return 0 with details.note explaining why.
func (SpectralGap) Compute(ctx context.Context, g *mgraph.Graph) ([]Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	uv := toUndirected(g, nil)
	n := uv.G.Nodes().Len()
	if n < 2 {
		return []Record{{
			Metric: "spectral_gap",
			Scope:  ScopeGraph,
			Value:  0,
			Details: map[string]any{
				"note": "graph has fewer than 2 nodes",
			},
		}}, nil
	}
	lap := spectral.NewLaplacian(uv.G)
	// The Laplacian is symmetric by construction. EigenSym needs a
	// mat.Symmetric — convert via SymDense copy. The Laplacian's
	// inner Matrix is already a *mat.SymDense (gonum implementation
	// detail; we copy defensively for forward-compat).
	sd := mat.NewSymDense(n, nil)
	for i := 0; i < n; i++ {
		for j := i; j < n; j++ {
			sd.SetSym(i, j, lap.At(i, j))
		}
	}
	var es mat.EigenSym
	if ok := es.Factorize(sd, false); !ok {
		// Numerical breakdown — surface as zero with a note rather
		// than aborting all metrics.
		return []Record{{
			Metric: "spectral_gap",
			Scope:  ScopeGraph,
			Value:  0,
			Details: map[string]any{
				"note": "EigenSym.Factorize failed",
			},
		}}, nil
	}
	values := es.Values(nil)
	sort.Float64s(values)
	// Numerical noise: clamp tiny negatives to zero. The Laplacian is
	// PSD analytically; small negative values come from FP error in
	// the eigendecomposition.
	for i, v := range values {
		if v < 0 && v > -1e-10 {
			values[i] = 0
		}
	}
	algebraicConn := 0.0
	if len(values) >= 2 {
		algebraicConn = values[1]
	}
	limit := 5
	if limit > len(values) {
		limit = len(values)
	}
	return []Record{{
		Metric: "spectral_gap",
		Scope:  ScopeGraph,
		Value:  algebraicConn,
		Details: map[string]any{
			"eigenvalues":    values[:limit],
			"nodes":          n,
			"undirectedView": true,
		},
	}}, nil
}

func init() { Register(SpectralGap{}) }
