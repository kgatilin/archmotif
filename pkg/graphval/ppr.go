package graphval

import (
	"sort"

	"gonum.org/v1/gonum/mat"
)

// Score pairs a node name with a computed real value. It is the result shape
// for ranking operations (e.g. PersonalizedPageRank) that assign every node a
// score rather than partition the node set.
type Score struct {
	Name  string  `json:"name"`
	Score float64 `json:"score"`
}

// DefaultRestart is the teleport probability used when a caller passes a
// non-positive restart to PersonalizedPageRank (damping = 1 − 0.15 = 0.85, the
// classic PageRank value).
const DefaultRestart = 0.15

const (
	pprTol     = 1e-10
	pprMaxIter = 1000
)

// PersonalizedPageRank computes restart-biased ("personalized") PageRank over
// the directed adjacency A by power iteration — the standard random-surfer
// diffusion. At each step the surfer either follows a uniformly-chosen outgoing
// edge (probability 1−restart) or teleports back to the seed set (probability
// restart). The returned length-N vector is a probability distribution over the
// nodes (entries sum to 1); each entry is that node's structural proximity to
// the seeds. Higher = closer to / more reachable from the seeds.
//
// Parameters:
//   - seeds: node indices the surfer teleports to. The personalization vector s
//     is uniform over the in-range, de-duplicated seeds. When seeds is empty (or
//     all out of range), s is uniform over every node and the result is ordinary
//     global PageRank.
//   - restart: teleport probability α ∈ (0,1] (damping = 1−α). α ≤ 0 is replaced
//     by DefaultRestart; α > 1 is clamped to 1.
//
// It is the fixpoint of r ← (1−α)·Pᵀr + ((1−α)·d + α)·s, where P is the
// row-stochastic transition matrix (A with each row divided by its out-degree),
// and d is the probability mass sitting on dangling (out-degree-0) nodes,
// redistributed through s so r stays a distribution. Iterates to L1 convergence
// (tol 1e-10) or pprMaxIter steps. Deterministic.
func (g *Graph) PersonalizedPageRank(seeds []int, restart float64) []float64 {
	n := g.N()
	if n == 0 {
		return nil
	}

	alpha := restart
	if alpha <= 0 {
		alpha = DefaultRestart
	}
	if alpha > 1 {
		alpha = 1
	}

	// Personalization vector s: uniform over unique, in-range seeds; uniform
	// over all nodes when no valid seed is supplied (→ global PageRank).
	s := make([]float64, n)
	seen := make(map[int]bool, len(seeds))
	for _, idx := range seeds {
		if idx < 0 || idx >= n || seen[idx] {
			continue
		}
		seen[idx] = true
	}
	if len(seen) == 0 {
		w := 1.0 / float64(n)
		for i := range s {
			s[i] = w
		}
	} else {
		w := 1.0 / float64(len(seen))
		for idx := range seen {
			s[idx] = w
		}
	}

	// Row-stochastic transition P[i][j] = A[i][j]/outdeg(i). Dangling rows
	// (out-degree 0) stay zero; their mass is teleported via the d term.
	a := g.a.Slice(0, n, 0, n)
	P := mat.NewDense(n, n, nil)
	dangling := make([]bool, n)
	for i := 0; i < n; i++ {
		deg := 0.0
		for j := 0; j < n; j++ {
			if a.At(i, j) > 0 {
				deg++
			}
		}
		if deg == 0 {
			dangling[i] = true
			continue
		}
		inv := 1.0 / deg
		for j := 0; j < n; j++ {
			if a.At(i, j) > 0 {
				P.Set(i, j, inv)
			}
		}
	}
	Pt := P.T()

	sVec := mat.NewVecDense(n, s)
	r := mat.NewVecDense(n, nil)
	r.CopyVec(sVec) // start from the personalization vector.
	tmp := mat.NewVecDense(n, nil)
	next := mat.NewVecDense(n, nil)

	for iter := 0; iter < pprMaxIter; iter++ {
		// Dangling mass d = Σ_{i dangling} r(i).
		d := 0.0
		for i := 0; i < n; i++ {
			if dangling[i] {
				d += r.AtVec(i)
			}
		}
		tmp.MulVec(Pt, r) // Pᵀ r — the follow-an-edge term.

		// next = (1−α)·tmp + ((1−α)·d + α)·s.
		next.ScaleVec(1-alpha, tmp)
		next.AddScaledVec(next, (1-alpha)*d+alpha, sVec)

		// L1 convergence check.
		diff := 0.0
		for i := 0; i < n; i++ {
			diff += abs(next.AtVec(i) - r.AtVec(i))
		}
		r.CopyVec(next)
		if diff < pprTol {
			break
		}
	}

	out := make([]float64, n)
	for i := 0; i < n; i++ {
		out[i] = r.AtVec(i)
	}
	return out
}

// PersonalizedPageRankByNames is the name-based companion to
// PersonalizedPageRank: it resolves the seed names to indices (unknown names
// are ignored; if none are known the result is global PageRank), then returns
// every node as a Score sorted by score descending, ties broken by name
// ascending for determinism. This is the accessor callers like archai use to
// rank search neighbourhoods by structural proximity to query hits.
func (g *Graph) PersonalizedPageRankByNames(seeds []string, restart float64) []Score {
	idx := make([]int, 0, len(seeds))
	for _, name := range seeds {
		if i, ok := g.index[name]; ok {
			idx = append(idx, i)
		}
	}
	scores := g.PersonalizedPageRank(idx, restart)
	out := make([]Score, len(scores))
	for i, sc := range scores {
		out[i] = Score{Name: g.names[i], Score: sc}
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Score != out[b].Score {
			return out[a].Score > out[b].Score
		}
		return out[a].Name < out[b].Name
	})
	return out
}

// abs is a tiny float64 absolute value helper (avoids importing math for one op).
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
