package trophic

import (
	"math"
	"math/rand"
	"testing"

	"gonum.org/v1/gonum/mat"
)

// randomComponent builds a connected directed graph on n nodes: a spanning
// chain plus extra random edges, some of them pointing back up.
func randomComponent(rng *rand.Rand, n, extra int) (adj []map[int]float64, outDeg, inDeg []float64) {
	adj = make([]map[int]float64, n)
	for i := range adj {
		adj[i] = map[int]float64{}
	}
	outDeg = make([]float64, n)
	inDeg = make([]float64, n)
	add := func(f, t int) {
		adj[f][t]++
		adj[t][f]++
		outDeg[f]++
		inDeg[t]++
	}
	for i := 1; i < n; i++ {
		add(i, rng.Intn(i))
	}
	for k := 0; k < extra; k++ {
		f, t := rng.Intn(n), rng.Intn(n)
		if f != t {
			add(f, t)
		}
	}
	return adj, outDeg, inDeg
}

// denseHeights is the grounded-Laplacian solve by dense Cholesky, the
// reference the sparse solver must reproduce.
func denseHeights(t *testing.T, adj []map[int]float64, outDeg, inDeg []float64) []float64 {
	n := len(adj)
	lap := mat.NewSymDense(n-1, nil)
	rhs := mat.NewVecDense(n-1, nil)
	for i := 1; i < n; i++ {
		var deg float64
		for j, w := range adj[i] {
			deg += w
			if j > i {
				lap.SetSym(i-1, j-1, -w)
			}
		}
		lap.SetSym(i-1, i-1, deg)
		rhs.SetVec(i-1, outDeg[i]-inDeg[i])
	}
	var chol mat.Cholesky
	if !chol.Factorize(lap) {
		t.Fatal("reference factorization failed")
	}
	var sol mat.VecDense
	if err := chol.SolveVecTo(&sol, rhs); err != nil {
		t.Fatal(err)
	}
	h := make([]float64, n)
	min := 0.0
	for i := 1; i < n; i++ {
		h[i] = sol.AtVec(i - 1)
		min = math.Min(min, h[i])
	}
	for i := range h {
		h[i] -= min
	}
	return h
}

func TestSolveComponentHeightsMatchesDenseSolve(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for _, tc := range []struct{ n, extra int }{{2, 0}, {30, 10}, {400, 900}, {1500, 200}} {
		adj, outDeg, inDeg := randomComponent(rng, tc.n, tc.extra)
		members := make([]int, tc.n)
		for i := range members {
			members[i] = i
		}
		got := make([]float64, tc.n)
		solveComponentHeights(members, adj, outDeg, inDeg, got)
		want := denseHeights(t, adj, outDeg, inDeg)
		for i := range want {
			if math.Abs(got[i]-want[i]) > 1e-7 {
				t.Fatalf("n=%d node %d: height %v, dense solve %v", tc.n, i, got[i], want[i])
			}
		}
	}
}

func BenchmarkSolveComponentHeights(b *testing.B) {
	const n = 12000
	adj, outDeg, inDeg := randomComponent(rand.New(rand.NewSource(1)), n, 3*n)
	members := make([]int, n)
	for i := range members {
		members[i] = i
	}
	heights := make([]float64, n)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		solveComponentHeights(members, adj, outDeg, inDeg, heights)
	}
}
