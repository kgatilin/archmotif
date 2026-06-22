package metrics

import (
	"sort"

	"gonum.org/v1/gonum/graph/spectral"
	"gonum.org/v1/gonum/mat"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// laplacianSpectrum returns the sorted eigenvalues of the symmetric Laplacian
// L = D − A of g's undirected projection (per ADR-012 symmetrisation). Tiny
// numerical negatives are clamped to 0 (the Laplacian is PSD analytically).
// Returns nil when the graph has fewer than 2 nodes or the eigendecomposition
// fails. Shared by the radius (λ_max) and eigenvalues (full spectrum) metrics.
func laplacianSpectrum(g *mgraph.Graph) []float64 {
	uv := toUndirected(g, nil)

	n := uv.G.Nodes().Len()
	if n < 2 {
		return nil
	}

	lap := spectral.NewLaplacian(uv.G)

	sd := mat.NewSymDense(n, nil)
	for i := 0; i < n; i++ {
		for j := i; j < n; j++ {
			sd.SetSym(i, j, lap.At(i, j))
		}
	}

	var es mat.EigenSym
	if ok := es.Factorize(sd, false); !ok {
		return nil
	}

	values := es.Values(nil)
	sort.Float64s(values)

	for i, v := range values {
		if v < 0 {
			values[i] = 0
		}
	}

	return values
}
