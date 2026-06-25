// Package localpartition extracts the local cluster around a set of seed
// nodes using the Andersen–Chung–Lang (ACL) algorithm: approximate
// Personalized PageRank via the "push" operation, followed by a sweep cut
// that picks the prefix of minimum conductance.
//
// Unlike spectralcluster (which partitions the *whole* graph into K
// communities), localpartition answers a local question: "given these
// seeds, which tightly-connected region do they belong to?" — without
// touching the rest of the graph. The cost is bounded by the size of that
// region, not the graph, which is exactly what diff-scoped analysis needs:
// seed = changed symbols, region = the part of the architecture that change
// actually pulls on.
//
// The graph is symmetrized (per archmotif ADR-012, like spectralcluster):
// all edges are treated as undirected for the random walk and the
// conductance computation. EdgeKinds restricts which edge kinds participate
// (e.g. structural dependency edges only, excluding contains/field/file).
//
// # Output
//
// Result.Weights is the continuous primitive: the degree-normalized PPR mass
// p[u]/deg[u] for every node the walk reached. Result.Region is the binary
// special case: the min-conductance sweep set over those weights. A caller
// that wants a hard region uses Region; a caller that wants to weight a
// downstream analysis uses Weights.
package localpartition

import (
	"fmt"
	"sort"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// Graph is a type alias for archmotif's internal graph type, matching
// spectralcluster so callers pass the same *Graph to either.
type Graph = mgraph.Graph

// Options configures the local partition.
type Options struct {
	// Alpha is the ACL teleport constant (restart probability of the lazy
	// random walk). Larger alpha keeps mass nearer the seeds (tighter, more
	// local region); smaller alpha lets it diffuse further. ACL's standard
	// value is 0.15 (PageRank damping 0.85).
	Alpha float64
	// Epsilon is the push approximation tolerance. A node u is pushed while
	// its residual r[u] >= Epsilon * deg(u). Smaller epsilon = more accurate
	// PPR but more work. The support of the resulting vector is bounded by
	// 1/((1-Alpha)*Epsilon), so epsilon also caps region size.
	Epsilon float64
	// EdgeKinds restricts which edge kinds the walk and conductance use.
	// Empty means all edge kinds participate.
	EdgeKinds []string
}

// DefaultOptions returns the standard ACL parameters.
func DefaultOptions() Options {
	return Options{Alpha: 0.15, Epsilon: 1e-5}
}

// Result holds the local partition around the seeds.
type Result struct {
	// Region is the min-conductance sweep set: the local cluster the seeds
	// belong to, sorted by node id.
	Region []string `json:"region"`
	// Weights is the degree-normalized PPR mass (p[u]/deg(u)) for every node
	// the walk reached, including those outside Region. This is the
	// continuous selection primitive; Region is its thresholded form.
	Weights map[string]float64 `json:"weights"`
	// Conductance is the conductance of the chosen sweep cut (0 = a perfectly
	// isolated region, 1 = no better than the whole graph). Lower is a
	// crisper local cluster.
	Conductance float64 `json:"conductance"`
	// SeedCount is how many of the requested seeds were present in the graph
	// (after edge-kind filtering, isolated seeds still count — they seed mass
	// even with degree 0).
	SeedCount int `json:"seed_count"`
}

// LocalPartition runs ACL approximate-PPR + sweep cut from seeds over g.
//
// Seeds not present in g are ignored. If no seeds are present, an empty
// Result (no region, empty weights) is returned with a nil error — an empty
// diff has no region, which is a valid answer, not a failure.
func LocalPartition(g *Graph, seeds []string, opts Options) (Result, error) {
	if g == nil {
		return Result{}, fmt.Errorf("localpartition: nil graph")
	}
	if opts.Alpha <= 0 || opts.Alpha >= 1 {
		return Result{}, fmt.Errorf("localpartition: alpha must be in (0,1), got %v", opts.Alpha)
	}
	if opts.Epsilon <= 0 {
		return Result{}, fmt.Errorf("localpartition: epsilon must be > 0, got %v", opts.Epsilon)
	}

	adj := buildAdjacency(g, opts.EdgeKinds)

	// Restrict seeds to nodes that exist in the (filtered) graph. A seed
	// with no incident edges of the requested kinds still exists as a node
	// and seeds mass; it simply cannot diffuse.
	present := make([]string, 0, len(seeds))
	seen := map[string]bool{}
	for _, s := range seeds {
		if _, ok := adj[s]; ok && !seen[s] {
			present = append(present, s)
			seen[s] = true
		}
	}
	if len(present) == 0 {
		return Result{Weights: map[string]float64{}}, nil
	}
	sort.Strings(present)

	p := approxPPR(adj, present, opts.Alpha, opts.Epsilon)

	// Degree-normalized weights are what the sweep ranks on and what callers
	// use to weight downstream analysis.
	weights := make(map[string]float64, len(p))
	for id, mass := range p {
		deg := float64(len(adj[id]))
		if deg == 0 {
			weights[id] = mass
			continue
		}
		weights[id] = mass / deg
	}

	region, cond := sweepCut(adj, weights)
	sort.Strings(region)

	return Result{
		Region:      region,
		Weights:     weights,
		Conductance: cond,
		SeedCount:   len(present),
	}, nil
}

// buildAdjacency constructs a symmetric adjacency map from g, keeping only
// the requested edge kinds (all kinds if edgeKinds is empty). Every node in
// g appears as a key (possibly with an empty neighbour set) so isolated
// seeds are still recognized as present. Parallel edges between the same
// pair collapse to a single undirected neighbour link (simple-graph degree).
func buildAdjacency(g *Graph, edgeKinds []string) map[string]map[string]bool {
	kindSet := map[mgraph.EdgeKind]bool{}
	for _, k := range edgeKinds {
		kindSet[mgraph.EdgeKind(k)] = true
	}

	adj := make(map[string]map[string]bool, g.NodeCount())
	for _, n := range g.Nodes() {
		adj[n.ID] = map[string]bool{}
	}
	for _, e := range g.Edges() {
		if len(kindSet) > 0 && !kindSet[e.Kind] {
			continue
		}
		if e.From == e.To {
			continue // self-loops do not contribute to conductance
		}
		// Defensive: only link nodes that exist as graph nodes.
		if _, ok := adj[e.From]; !ok {
			continue
		}
		if _, ok := adj[e.To]; !ok {
			continue
		}
		adj[e.From][e.To] = true
		adj[e.To][e.From] = true
	}
	return adj
}

// approxPPR computes the ACL approximate Personalized PageRank vector p for a
// uniform distribution over seeds, using the push operation on the lazy
// random walk. The active worklist is processed FIFO; the fixed point is
// order-independent up to the epsilon tolerance, and the sweep applies a
// deterministic tie-break, so the Result is deterministic.
func approxPPR(adj map[string]map[string]bool, seeds []string, alpha, eps float64) map[string]float64 {
	p := map[string]float64{}
	r := map[string]float64{}

	seedMass := 1.0 / float64(len(seeds))
	for _, s := range seeds {
		r[s] += seedMass
	}

	// active holds nodes whose residual currently exceeds eps*deg. queued
	// guards against enqueuing the same node twice.
	queue := make([]string, 0, len(seeds))
	queued := map[string]bool{}
	enqueue := func(u string) {
		if queued[u] {
			return
		}
		deg := float64(len(adj[u]))
		threshold := eps * deg
		if deg == 0 {
			// An isolated node can never spread; push it once so its alpha
			// mass lands in p, but only while it still holds residual.
			threshold = 0
		}
		if r[u] > threshold {
			queue = append(queue, u)
			queued[u] = true
		}
	}
	for _, s := range seeds {
		enqueue(s)
	}

	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]
		queued[u] = false

		ru := r[u]
		deg := float64(len(adj[u]))
		if deg == 0 {
			// Drain isolated residual into p once; nothing to spread.
			p[u] += alpha * ru
			r[u] = 0
			continue
		}
		if ru <= eps*deg {
			continue
		}

		p[u] += alpha * ru
		// Lazy-walk push: half the non-teleport mass stays, half spreads.
		r[u] = (1 - alpha) * ru / 2
		spread := (1 - alpha) * ru / (2 * deg)
		for v := range adj[u] {
			r[v] += spread
			enqueue(v)
		}
		// u may still be over threshold after keeping half its mass.
		enqueue(u)
	}
	return p
}

// sweepCut orders nodes by descending weight (degree-normalized PPR) and
// returns the prefix S of minimum conductance, along with that conductance.
// Conductance(S) = cut(S) / min(vol(S), vol(V\S)), where vol is the sum of
// degrees and cut is the number of edges leaving S.
func sweepCut(adj map[string]map[string]bool, weights map[string]float64) ([]string, float64) {
	// Candidate nodes are those the walk reached (weight > 0), ordered by
	// weight desc, id asc for deterministic ties.
	ordered := make([]string, 0, len(weights))
	for id, w := range weights {
		if w > 0 {
			ordered = append(ordered, id)
		}
	}
	if len(ordered) == 0 {
		return nil, 0
	}
	sort.Slice(ordered, func(i, j int) bool {
		wi, wj := weights[ordered[i]], weights[ordered[j]]
		if wi != wj {
			return wi > wj
		}
		return ordered[i] < ordered[j]
	})

	// Total volume of the whole (filtered) graph.
	var totalVol float64
	for id := range adj {
		totalVol += float64(len(adj[id]))
	}

	inSet := map[string]bool{}
	var vol, cut float64
	bestCond := 0.0
	bestK := 0
	haveBest := false

	for k, id := range ordered {
		deg := float64(len(adj[id]))
		// Adding id: its degree joins the volume; edges to nodes already in
		// the set stop being cut edges, edges to outside nodes become cut.
		vol += deg
		for v := range adj[id] {
			if inSet[v] {
				cut-- // previously a cut edge (v in, id out) — now internal
			} else {
				cut++ // id in, v out — a new cut edge
			}
		}
		inSet[id] = true

		complementVol := totalVol - vol
		denom := vol
		if complementVol < denom {
			denom = complementVol
		}
		if denom <= 0 {
			// S is the whole graph (or its complement has no volume): a cut
			// here is meaningless, stop — no better sweep beyond this point.
			break
		}
		cond := cut / denom
		if !haveBest || cond < bestCond {
			bestCond = cond
			bestK = k + 1
			haveBest = true
		}
	}

	if !haveBest {
		// No interior cut found (e.g. a single isolated seed): the region is
		// just the reached set.
		region := append([]string(nil), ordered...)
		return region, 0
	}
	region := append([]string(nil), ordered[:bestK]...)
	return region, bestCond
}
