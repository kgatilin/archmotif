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
// # Two entrypoints
//
// LocalPartition takes archmotif's typed graph, treats every kept edge as
// weight 1 and every seed as equally relevant — the classic ACL setup.
//
// LocalPartitionWeighted takes plain WeightedEdge and Seed values, so a caller
// with its own graph representation never has to build a typed graph just to be
// read back out, and can additionally express what the typed form cannot: how
// much each seed is trusted, how much each edge counts, and how hard to discount
// the graph's hubs (Options.HubDamping). The typed entrypoint is a thin adapter
// over the weighted one; there is a single implementation of the algorithm.
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
	// Empty means all edge kinds participate. It applies to LocalPartition
	// only — LocalPartitionWeighted is handed the edges it should use.
	EdgeKinds []string
	// HubDamping (tau) discounts edges by how popular their endpoints are,
	// replacing every undirected weight w(u,v) with w(u,v)/(d_u*d_v)^tau, where
	// d is the weighted degree of the undamped graph. Zero (the default) leaves
	// the graph exactly as given.
	//
	// Dependency graphs have god-nodes: a shared helper, a logging call, a
	// central dispatcher that half the codebase touches. A random walk finds
	// those from every seed, so the same handful of hubs turns up in every local
	// region and crowds out whatever was specific to the query — and worse, they
	// are a bridge, so mass crosses through them into neighbourhoods that have
	// nothing to do with the seeds. Dividing by the endpoint degrees makes a link
	// through a hub cost more than a link between two ordinarily-connected nodes,
	// so the walk favours paths that are distinctive over paths that are merely
	// popular.
	//
	// tau = 0.5 is the usual starting point: it is the symmetric normalization
	// D^-1/2 W D^-1/2 that spectral methods apply for the same reason. tau = 1
	// is aggressive and can strand genuinely central code. Damping changes the
	// degrees the sweep sees as well as the walk, so it moves the region, not
	// just the ranking.
	HubDamping float64
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
// Every kept edge counts for 1 and every seed is equally relevant; the work is
// done by LocalPartitionWeighted, which this adapts g onto. Callers that need
// per-edge or per-seed weights go there directly.
//
// Seeds not present in g are ignored. If no seeds are present, an empty
// Result (no region, empty weights) is returned with a nil error — an empty
// diff has no region, which is a valid answer, not a failure.
func LocalPartition(g *Graph, seeds []string, opts Options) (Result, error) {
	if g == nil {
		return Result{}, fmt.Errorf("localpartition: nil graph")
	}
	if err := opts.validate(); err != nil {
		return Result{}, err
	}

	nodes, edges := graphEdges(g, opts.EdgeKinds)

	// Restrict seeds to nodes that exist in g. A seed with no incident edge of
	// the requested kinds still exists as a node and seeds mass; it simply
	// cannot diffuse. Nodes of g that are isolated and unseeded are left out of
	// the weighted graph entirely: they contribute zero degree, so they change
	// neither the volume nor any conductance.
	present := make([]Seed, 0, len(seeds))
	seen := map[string]bool{}
	for _, s := range seeds {
		if nodes[s] && !seen[s] {
			present = append(present, Seed{Name: s})
			seen[s] = true
		}
	}
	if len(present) == 0 {
		return Result{Weights: map[string]float64{}}, nil
	}

	return LocalPartitionWeighted(edges, present, opts)
}

// graphEdges extracts g's node set and its undirected edge list, keeping only
// the requested edge kinds (all kinds if edgeKinds is empty).
//
// Each unordered node pair yields exactly one weight-1 edge no matter how many
// edges of g connect it. That collapse is the historical contract of the typed
// entrypoint — degree there means "number of distinct neighbours", the
// simple-graph degree — and it has to survive the delegation: the weighted path
// sums parallel and opposing edges, so emitting them one by one would silently
// double the degree of every reciprocated pair and move the sweep cut.
func graphEdges(g *Graph, edgeKinds []string) (map[string]bool, []WeightedEdge) {
	kindSet := map[mgraph.EdgeKind]bool{}
	for _, k := range edgeKinds {
		kindSet[mgraph.EdgeKind(k)] = true
	}

	nodes := make(map[string]bool, g.NodeCount())
	for _, n := range g.Nodes() {
		nodes[n.ID] = true
	}

	edges := make([]WeightedEdge, 0, g.EdgeCount())
	emitted := map[[2]string]bool{}
	for _, e := range g.Edges() {
		if len(kindSet) > 0 && !kindSet[e.Kind] {
			continue
		}
		if e.From == e.To {
			continue // self-loops do not contribute to conductance
		}
		// Defensive: only link nodes that exist as graph nodes.
		if !nodes[e.From] || !nodes[e.To] {
			continue
		}
		pair := [2]string{e.From, e.To}
		if pair[0] > pair[1] {
			pair[0], pair[1] = pair[1], pair[0]
		}
		if emitted[pair] {
			continue
		}
		emitted[pair] = true
		edges = append(edges, WeightedEdge{From: pair[0], To: pair[1], Weight: 1})
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		return edges[i].To < edges[j].To
	})
	return nodes, edges
}
