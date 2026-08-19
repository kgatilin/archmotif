package localpartition

import (
	"fmt"
	"math"
	"sort"
)

// WeightedEdge is a directed edge between two node names, carrying the strength
// the caller assigns to it. The names are opaque: they are whatever identifies a
// node in the caller's own representation, and localpartition never interprets
// them.
//
// Weight rides all the way through — symmetrization, the transition
// probabilities of the random walk, and the conductance of the sweep cut — so a
// caller that considers a call edge stronger evidence than a type reference can
// say so once, here, instead of pre-filtering the graph.
type WeightedEdge struct {
	From string
	To   string
	// Weight is the edge strength. A non-positive Weight is treated as 1,
	// matching graphval.Edge, so a caller with an unweighted graph leaves it at
	// zero and gets exactly the plain 0/1 behaviour.
	Weight float64
}

// Seed is one personalization seed: a node name plus how much the caller
// believes in it.
//
// The classic ACL formulation seeds a uniform distribution over the seed set,
// which throws away everything the caller already knows about relative
// relevance. When the seeds come from a scored source — calibrated text-search
// hits, changed-line counts, a confidence per symbol — the score belongs in the
// personalization vector, not in a post-hoc reranking of the result.
type Seed struct {
	Name string
	// Weight is the seed's relevance. Seed weights are normalized to a
	// probability distribution over the seed set, so only their ratios matter. A
	// non-positive Weight is treated as 1, which makes uniform seeding the
	// zero-value default.
	Weight float64
}

// LocalPartitionWeighted runs the same ACL approximate-PPR and sweep cut as
// LocalPartition, but over a plain list of weighted edges rather than
// archmotif's typed graph.
//
// This is the representation-agnostic entrypoint. A caller that already holds
// its own graph — a symbol graph, a call graph, the neighbourhood index behind a
// hybrid search — passes edges and seeds directly instead of materializing an
// intermediate typed graph just to be read back out. Two things the typed
// entrypoint cannot express are available here: per-seed personalization weights
// and per-edge weights (see Seed and WeightedEdge), plus Options.HubDamping.
//
// Node set, symmetrization and seeding:
//
//   - The node set is every name appearing in edges plus every seed name. A seed
//     with no incident edge is a valid node: it holds its mass and cannot
//     diffuse, exactly as an isolated seed behaves in the typed entrypoint.
//   - Per ADR-012 the graph is undirected for both the walk and the conductance.
//     The undirected weight w(u,v) is the sum of every directed edge weight
//     between u and v in either direction, so parallel and opposing edges
//     reinforce each other instead of one overwriting the rest.
//   - Self-loops are dropped: they never cross a cut, so they cannot change
//     conductance, and keeping them would only inflate degrees.
//   - Duplicate seed names sum their weights. Two independent sources voting for
//     the same node make it a stronger start, not two competing entries.
//
// With no seeds at all, an empty Result (no region, empty non-nil weights) comes
// back with a nil error: nothing to localize around is a valid answer, not a
// failure.
func LocalPartitionWeighted(edges []WeightedEdge, seeds []Seed, opts Options) (Result, error) {
	if err := opts.validate(); err != nil {
		return Result{}, err
	}

	// Collapse duplicate seed names first, keeping a sorted name list so the
	// whole computation below is reproducible run to run.
	mass := make(map[string]float64, len(seeds))
	names := make([]string, 0, len(seeds))
	var totalMass float64
	for _, s := range seeds {
		w := s.Weight
		if w <= 0 {
			w = 1
		}
		if _, seen := mass[s.Name]; !seen {
			names = append(names, s.Name)
		}
		mass[s.Name] += w
		totalMass += w
	}
	if len(names) == 0 {
		return Result{Weights: map[string]float64{}}, nil
	}
	sort.Strings(names)

	g := buildWeightedGraph(edges, names, opts.HubDamping)

	// The personalization vector is the seed weights normalized to a probability
	// distribution: only the ratios between seeds matter, and the total residual
	// stays 1 so alpha keeps its usual meaning.
	residual := make(map[string]float64, len(names))
	for _, n := range names {
		residual[n] = mass[n] / totalMass
	}

	p := approxPPR(g, names, residual, opts.Alpha, opts.Epsilon)

	// Degree-normalized weights are what the sweep ranks on and what callers use
	// to weight downstream analysis.
	weights := make(map[string]float64, len(p))
	for id, m := range p {
		deg := g.deg[id]
		if deg == 0 {
			weights[id] = m
			continue
		}
		weights[id] = m / deg
	}

	region, cond := sweepCut(g, weights)
	sort.Strings(region)

	return Result{
		Region:      region,
		Weights:     weights,
		Conductance: cond,
		SeedCount:   len(names),
	}, nil
}

// link is one endpoint of an undirected, symmetrized (and possibly damped) edge.
type link struct {
	to string
	w  float64
}

// weightedGraph is the symmetrized graph both the walk and the sweep run on.
//
// Adjacency lists are sorted by neighbour id and degrees are summed in that same
// order, so every floating-point accumulation happens in a fixed sequence. Go's
// map iteration order is randomized per run; summing weights straight out of a
// map would make the result differ in the last bits between runs, which is a
// miserable property for something a caller ranks on.
type weightedGraph struct {
	nodes []string          // sorted node ids
	adj   map[string][]link // neighbours, sorted by id
	deg   map[string]float64
	vol   float64 // total volume: sum of all degrees
}

// buildWeightedGraph symmetrizes edges into an undirected weighted graph over
// the node set (every edge endpoint plus every seed name), optionally applying
// hub damping.
//
// Damping is applied after symmetrization and against the *original* weighted
// degrees, so that every edge is discounted by the same measure of how popular
// its endpoints were before anything was rescaled. Degrees are then recomputed
// from the damped weights, because the walk and the conductance must both see
// the graph they are actually running on.
func buildWeightedGraph(edges []WeightedEdge, seedNames []string, tau float64) *weightedGraph {
	und := make(map[string]map[string]float64)
	touch := func(id string) {
		if _, ok := und[id]; !ok {
			und[id] = map[string]float64{}
		}
	}
	for _, e := range edges {
		// Endpoints become nodes even for a dropped self-loop: the node exists,
		// it just has no usable incident edge.
		touch(e.From)
		touch(e.To)
		if e.From == e.To {
			continue // self-loops never cross a cut
		}
		w := e.Weight
		if w <= 0 {
			w = 1
		}
		und[e.From][e.To] += w
		und[e.To][e.From] += w
	}
	for _, s := range seedNames {
		touch(s)
	}

	g := &weightedGraph{
		nodes: make([]string, 0, len(und)),
		adj:   make(map[string][]link, len(und)),
		deg:   make(map[string]float64, len(und)),
	}
	for id := range und {
		g.nodes = append(g.nodes, id)
	}
	sort.Strings(g.nodes)
	for _, u := range g.nodes {
		nbrs := und[u]
		ls := make([]link, 0, len(nbrs))
		for v, w := range nbrs {
			ls = append(ls, link{to: v, w: w})
		}
		sort.Slice(ls, func(i, j int) bool { return ls[i].to < ls[j].to })
		g.adj[u] = ls
	}
	g.computeDegrees()

	if tau > 0 {
		// Freeze the undamped degrees: every edge must be discounted against how
		// popular its endpoints were before any rescaling, or the discount would
		// depend on the order edges happen to be visited in. g.deg gets a fresh
		// map so the recompute below cannot write back into orig.
		//
		// Both directions of a pair see the same product d_u*d_v, and float
		// multiplication is commutative, so the damped matrix stays symmetric.
		orig := g.deg
		g.deg = make(map[string]float64, len(g.nodes))
		for _, u := range g.nodes {
			ls := g.adj[u]
			for i := range ls {
				ls[i].w /= math.Pow(orig[u]*orig[ls[i].to], tau)
			}
		}
		g.computeDegrees()
	}
	return g
}

// computeDegrees (re)derives weighted degrees and the total volume from the
// current adjacency, summing in sorted order for reproducibility.
func (g *weightedGraph) computeDegrees() {
	g.vol = 0
	for _, u := range g.nodes {
		var d float64
		for _, l := range g.adj[u] {
			d += l.w
		}
		g.deg[u] = d
		g.vol += d
	}
}

// approxPPR computes the ACL approximate Personalized PageRank vector p for the
// given personalization residual, using the push operation on the lazy random
// walk.
//
// Transitions are weighted: a push at u sends each neighbour v a share
// w(u,v)/deg(u) of the spreading mass, where deg is the weighted degree. With
// every weight at 1 this collapses to the uniform 1/deg of the unweighted
// formulation, bit for bit.
//
// seedOrder fixes the order the seeds enter the worklist; the worklist itself is
// FIFO and adjacency is sorted, so the whole push sequence is deterministic.
func approxPPR(g *weightedGraph, seedOrder []string, residual map[string]float64, alpha, eps float64) map[string]float64 {
	p := map[string]float64{}
	r := make(map[string]float64, len(residual))
	for id, m := range residual {
		r[id] = m
	}

	// queue holds nodes whose residual currently exceeds eps*deg. queued guards
	// against enqueuing the same node twice.
	queue := make([]string, 0, len(seedOrder))
	queued := map[string]bool{}
	enqueue := func(u string) {
		if queued[u] {
			return
		}
		deg := g.deg[u]
		threshold := eps * deg
		if deg == 0 {
			// An isolated node can never spread; push it once so its alpha mass
			// lands in p, but only while it still holds residual.
			threshold = 0
		}
		if r[u] > threshold {
			queue = append(queue, u)
			queued[u] = true
		}
	}
	for _, s := range seedOrder {
		enqueue(s)
	}

	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]
		queued[u] = false

		ru := r[u]
		deg := g.deg[u]
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
		// Lazy-walk push: half the non-teleport mass stays, half spreads across
		// the neighbours in proportion to their edge weights.
		spread := (1 - alpha) * ru / 2
		r[u] = spread
		for _, l := range g.adj[u] {
			r[l.to] += spread * l.w / deg
			enqueue(l.to)
		}
		// u may still be over threshold after keeping half its mass.
		enqueue(u)
	}
	return p
}

// sweepCut orders nodes by descending weight (degree-normalized PPR) and returns
// the prefix S of minimum conductance, along with that conductance.
//
// Conductance(S) = cut(S) / min(vol(S), vol(V\S)), where vol(S) is the sum of
// the weighted degrees in S and cut(S) is the total weight of the edges leaving
// S. In an unweighted graph both reduce to the familiar counts.
func sweepCut(g *weightedGraph, weights map[string]float64) ([]string, float64) {
	// Candidate nodes are those the walk reached (weight > 0), ordered by weight
	// desc, id asc for deterministic ties.
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

	inSet := map[string]bool{}
	var vol, cut float64
	bestCond := 0.0
	bestK := 0
	haveBest := false

	for k, id := range ordered {
		deg := g.deg[id]
		// Adding id: its degree joins the volume; edges to nodes already in the
		// set stop being cut edges, edges to outside nodes become cut.
		vol += deg
		for _, l := range g.adj[id] {
			if inSet[l.to] {
				cut -= l.w // previously a cut edge (l.to in, id out) — now internal
			} else {
				cut += l.w // id in, l.to out — a new cut edge
			}
		}
		inSet[id] = true

		complementVol := g.vol - vol
		denom := vol
		if complementVol < denom {
			denom = complementVol
		}
		// Guard with a tolerance relative to the total volume, not against zero.
		// Weighted degrees do not cancel exactly, so the complement of the whole
		// graph lands a few ulps either side of zero; dividing by that noise
		// would manufacture a spurious minimum.
		if denom <= g.vol*volEpsilon {
			// S is the whole graph (or its complement has no volume): a cut here
			// is meaningless, stop — no better sweep beyond this point.
			break
		}
		c := cut
		if c < 0 {
			c = 0 // same cancellation, on the cut side
		}
		cond := c / denom
		if !haveBest || cond < bestCond {
			bestCond = cond
			bestK = k + 1
			haveBest = true
		}
	}

	if !haveBest {
		// No interior cut found (e.g. a single isolated seed): the region is just
		// the reached set.
		region := append([]string(nil), ordered...)
		return region, 0
	}
	region := append([]string(nil), ordered[:bestK]...)
	return region, bestCond
}

// volEpsilon is the relative tolerance below which a remaining volume counts as
// zero. Small enough that any genuine unweighted degree (>= 1) clears it for
// graphs far larger than anything localpartition is asked to walk.
const volEpsilon = 1e-12

// validate checks the ACL parameters. Kept separate from the entrypoints so both
// report identical diagnostics.
func (o Options) validate() error {
	if o.Alpha <= 0 || o.Alpha >= 1 {
		return fmt.Errorf("localpartition: alpha must be in (0,1), got %v", o.Alpha)
	}
	if o.Epsilon <= 0 {
		return fmt.Errorf("localpartition: epsilon must be > 0, got %v", o.Epsilon)
	}
	if math.IsNaN(o.HubDamping) || o.HubDamping < 0 {
		return fmt.Errorf("localpartition: hub damping must be >= 0, got %v", o.HubDamping)
	}
	return nil
}
