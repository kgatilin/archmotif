package metrics

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// MotifRedundancy counts repeated typed subgraphs of size 3 to
// MotifMaxSize (default 4) in the call/type substrate (Function,
// Method, Type nodes connected via Calls, CallsFrom, References,
// Implements, Embeds, Returns, and UsesType). Repeated isomorphic
// instances become a "motif group"; the metric reports one ScopeRegion
// record per group whose instance count is ≥ 2.
//
// Why those node and edge kinds? Stage 3 motifs are about extract-
// interface / extract-function opportunities (per docs/concepts.md
// §5). Loops, branches, channel ops, and packages don't participate
// in those rewrites; including them would inflate the search space
// without improving signal. ADR-013 records the choice and the gSpan
// deferral.
//
// Output:
//   - one ScopeGraph record carrying the group count (groups with
//     ≥ 2 instances), with details.budgetExhausted true when
//     enumeration hit MotifSampleLimit.
//   - one ScopeRegion record per group, value = instance count,
//     details.canonical = canonical form, details.size = k,
//     details.instances = list of instance member ID lists.
type MotifRedundancy struct {
	// MaxSize caps the upper end of the enumerated motif size. Lower
	// bound is fixed at 3 (single-edge motifs are too noisy). The
	// CLI exposes --motif-max-size; runtime default is 4 because k=5
	// enumeration is empirically slow on archmotif's ~760-node graph.
	MaxSize int
	// SampleLimit bounds the total number of candidate subgraphs
	// considered across all sizes. When hit, enumeration stops and
	// the run is marked budgetExhausted=true. Empirically, 100k is
	// fine for archmotif itself (sub-second).
	SampleLimit int
}

// DefaultMotifMaxSize is the default upper bound on motif size. ADR-013
// pins this at 4 — k=5 enumeration is configurable but off by default.
const DefaultMotifMaxSize = 4

// DefaultMotifSampleLimit caps enumeration work so the metric stays
// bounded on large graphs.
const DefaultMotifSampleLimit = 100_000

// Name returns the metric identifier.
func (m MotifRedundancy) Name() string { return "motif_redundancy" }

// Description returns the metric documentation string.
func (m MotifRedundancy) Description() string {
	return "groups of isomorphic 3- to N-node subgraphs (N defaults to 4) in the call/type substrate"
}

// Configurable returns user-tunable knobs.
func (m MotifRedundancy) Configurable() map[string]any {
	return map[string]any{
		"motif-max-size":     DefaultMotifMaxSize,
		"motif-sample-limit": DefaultMotifSampleLimit,
	}
}

// motifEdgeKinds defines the substrate explored for motif enumeration.
// Treated as undirected for adjacency walks; canonical labels keep
// direction.
var motifEdgeKinds = map[mgraph.EdgeKind]bool{
	mgraph.EdgeCalls:      true,
	mgraph.EdgeCallsFrom:  true,
	mgraph.EdgeReferences: true,
	mgraph.EdgeImplements: true,
	mgraph.EdgeEmbeds:     true,
	mgraph.EdgeReturns:    true,
	mgraph.EdgeUsesType:   true,
}

var motifNodeKinds = map[mgraph.NodeKind]bool{
	mgraph.NodeFunction: true,
	mgraph.NodeMethod:   true,
	mgraph.NodeType:     true,
}

// Compute enumerates connected subgraphs in the motif substrate and
// groups them by canonical form. Subgraphs whose entire node set sits
// inside a single Type's Contains region are skipped — they represent
// an already-extracted abstraction (see issue #4).
func (m MotifRedundancy) Compute(ctx context.Context, g *mgraph.Graph) ([]Record, error) {
	maxSize := m.MaxSize
	if maxSize <= 0 {
		maxSize = DefaultMotifMaxSize
	}
	if maxSize < 3 {
		maxSize = 3
	}
	limit := m.SampleLimit
	if limit <= 0 {
		limit = DefaultMotifSampleLimit
	}

	subset := buildMotifSubset(g)
	groups := map[string][][]string{} // canonical -> list of instance ID lists
	visited := map[string]bool{}      // dedup by sorted ID set

	// Enumerate per-anchor with k = 3..maxSize. ESU avoids redundant
	// enumeration by only extending to neighbours with id > anchor id.
	count := 0
	exhausted := false
	for _, anchorID := range subset.order {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for k := 3; k <= maxSize; k++ {
			esuExtend(subset, anchorID, []string{anchorID}, neighborSet(subset, anchorID, anchorID), k, &count, limit, &exhausted, visited, groups, g)
			if exhausted {
				break
			}
		}
		if exhausted {
			break
		}
	}

	// Build records.
	out := []Record{}
	repeatedGroups := 0
	type groupRow struct {
		canonical string
		instances [][]string
	}
	rows := make([]groupRow, 0, len(groups))
	for canon, insts := range groups {
		if len(insts) < 2 {
			continue
		}
		repeatedGroups++
		// Sort instances and members for determinism.
		copyInsts := make([][]string, len(insts))
		for i, inst := range insts {
			cp := append([]string(nil), inst...)
			sort.Strings(cp)
			copyInsts[i] = cp
		}
		sort.Slice(copyInsts, func(i, j int) bool {
			return strings.Join(copyInsts[i], ",") < strings.Join(copyInsts[j], ",")
		})
		rows = append(rows, groupRow{canonical: canon, instances: copyInsts})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if len(rows[i].instances) != len(rows[j].instances) {
			return len(rows[i].instances) > len(rows[j].instances)
		}
		return rows[i].canonical < rows[j].canonical
	})

	out = append(out, Record{
		Metric: "motif_redundancy",
		Scope:  ScopeGraph,
		Value:  float64(repeatedGroups),
		Details: map[string]any{
			"sampledSubgraphs": count,
			"budgetExhausted":  exhausted,
			"maxSize":          maxSize,
			"sampleLimit":      limit,
		},
	})
	for i, row := range rows {
		instances := make([]any, 0, len(row.instances))
		for _, inst := range row.instances {
			instances = append(instances, inst)
		}
		out = append(out, Record{
			Metric: "motif_redundancy",
			Scope:  ScopeRegion,
			Target: regionID("motif", i),
			Value:  float64(len(row.instances)),
			Details: map[string]any{
				"canonical": row.canonical,
				"size":      sizeFromCanonical(row.canonical),
				"instances": instances,
			},
		})
	}
	return out, nil
}

// motifSubset is the projection of the typed graph onto motif-relevant
// nodes/edges, with adjacency lookups precomputed.
type motifSubset struct {
	order []string                       // stable enumeration order (ascending)
	rank  map[string]int                 // rank in order; lower rank = "smaller"
	adj   map[string]map[string]struct{} // undirected adjacency (motif edges only)
	g     *mgraph.Graph
}

func buildMotifSubset(g *mgraph.Graph) motifSubset {
	in := map[string]bool{}
	for _, n := range g.Nodes() {
		if !motifNodeKinds[n.Kind] {
			continue
		}
		in[n.ID] = true
	}
	adj := map[string]map[string]struct{}{}
	for id := range in {
		adj[id] = map[string]struct{}{}
	}
	for _, e := range g.Edges() {
		if !motifEdgeKinds[e.Kind] {
			continue
		}
		if !in[e.From] || !in[e.To] || e.From == e.To {
			continue
		}
		adj[e.From][e.To] = struct{}{}
		adj[e.To][e.From] = struct{}{}
	}
	order := make([]string, 0, len(in))
	for id := range in {
		order = append(order, id)
	}
	sort.Strings(order)
	rank := make(map[string]int, len(order))
	for i, id := range order {
		rank[id] = i
	}
	return motifSubset{order: order, rank: rank, adj: adj, g: g}
}

// neighborSet returns the open neighbourhood of anchor in subset
// excluding the anchor itself.
func neighborSet(s motifSubset, anchor, lowest string) map[string]struct{} {
	out := map[string]struct{}{}
	for v := range s.adj[anchor] {
		if s.rank[v] > s.rank[lowest] {
			out[v] = struct{}{}
		}
	}
	return out
}

// esuExtend implements the recursive step of the ESU enumeration. It
// only extends the subgraph using nodes whose rank is greater than the
// anchor's, which guarantees each connected subgraph is enumerated
// exactly once.
//
// When the subgraph reaches size k, the canonical form is computed and
// the instance is recorded into groups (subject to dedup against
// visited and to the abstraction filter).
func esuExtend(
	s motifSubset,
	anchor string,
	current []string,
	extension map[string]struct{},
	k int,
	count *int,
	limit int,
	exhausted *bool,
	visited map[string]bool,
	groups map[string][][]string,
	full *mgraph.Graph,
) {
	if *exhausted {
		return
	}
	if len(current) == k {
		*count++
		if *count > limit {
			*exhausted = true
			return
		}
		key := dedupKey(current)
		if visited[key] {
			return
		}
		visited[key] = true
		if !subgraphEligible(full, current) {
			return
		}
		canon := canonicalForm(full, current)
		groups[canon] = append(groups[canon], append([]string(nil), current...))
		return
	}
	if len(extension) == 0 {
		return
	}
	// Pop one neighbour from the extension set, recurse with it added.
	for w := range extension {
		newExt := make(map[string]struct{}, len(extension))
		for v := range extension {
			if v != w {
				newExt[v] = struct{}{}
			}
		}
		// Add w's neighbours that are >anchor and not already in current
		// or already-considered (extension didn't include them at this
		// level).
		for v := range s.adj[w] {
			if s.rank[v] <= s.rank[anchor] {
				continue
			}
			if containsString(current, v) {
				continue
			}
			// Avoid re-adding nodes that were already in the parent
			// extension by also filtering against current's existing
			// neighbours that came from earlier expansions.
			alreadyAdjacent := false
			for _, c := range current {
				if c == w {
					continue
				}
				if _, adj := s.adj[c][v]; adj {
					alreadyAdjacent = true
					break
				}
			}
			if alreadyAdjacent {
				continue
			}
			newExt[v] = struct{}{}
		}
		next := append(append([]string(nil), current...), w)
		esuExtend(s, anchor, next, newExt, k, count, limit, exhausted, visited, groups, full)
		if *exhausted {
			return
		}
	}
}

// dedupKey returns a stable string for a node-set identity (sorted IDs).
func dedupKey(ids []string) string {
	cp := append([]string(nil), ids...)
	sort.Strings(cp)
	return strings.Join(cp, "\x00")
}

func containsString(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// subgraphEligible returns false when the candidate subgraph already
// represents an extracted abstraction. Heuristic: every member node is
// either the same Type, or contained by the same Type via Contains.
// Members that share a single enclosing Type (a struct + its methods,
// already factored) are exactly what motif redundancy should *not*
// flag.
func subgraphEligible(g *mgraph.Graph, members []string) bool {
	enclosingType := ""
	for _, id := range members {
		t := enclosingTypeID(g, id)
		if t == "" {
			return true // at least one member sits outside any single Type
		}
		if enclosingType == "" {
			enclosingType = t
			continue
		}
		if enclosingType != t {
			return true
		}
	}
	// All members share an enclosing Type — abstraction already
	// extracted; skip.
	return false
}

// enclosingTypeID returns the ID of the Type node that Contains the
// given node (one hop), or the node ID itself if it is a Type. Empty
// when neither applies.
func enclosingTypeID(g *mgraph.Graph, id string) string {
	n, ok := g.Node(id)
	if !ok {
		return ""
	}
	if n.Kind == mgraph.NodeType {
		return n.ID
	}
	for _, p := range g.Neighbors(id, mgraph.DirectionIn, mgraph.EdgeContains) {
		if p.Kind == mgraph.NodeType {
			return p.ID
		}
	}
	return ""
}

// canonicalForm returns a permutation-invariant signature of the
// induced subgraph on members. We try all permutations (k≤4 → 24)
// and pick the lexicographically smallest rendering.
//
// The rendering covers:
//   - sorted list of node kinds (with their permuted index slot)
//   - sorted list of (fromIdx, toIdx, edgeKind) tuples for every
//     directed edge whose endpoints are both in members
func canonicalForm(g *mgraph.Graph, members []string) string {
	k := len(members)
	if k == 0 {
		return ""
	}
	// Pre-collect per-pair edge kinds (multi-edge between same endpoints
	// of different kinds is preserved as a sorted set).
	idxOf := make(map[string]int, k)
	kinds := make([]string, k)
	nodes := make([]mgraph.Node, k)
	for i, id := range members {
		idxOf[id] = i
		n, _ := g.Node(id)
		nodes[i] = n
		kinds[i] = string(n.Kind)
	}
	type edgeRec struct {
		from, to int
		kind     string
	}
	edges := make([]edgeRec, 0)
	for _, e := range g.Edges() {
		if !motifEdgeKinds[e.Kind] {
			continue
		}
		fi, ok1 := idxOf[e.From]
		ti, ok2 := idxOf[e.To]
		if !ok1 || !ok2 || fi == ti {
			continue
		}
		edges = append(edges, edgeRec{from: fi, to: ti, kind: string(e.Kind)})
	}
	// Try all permutations of [0..k-1] mapping subgraph slot -> canonical slot.
	indices := make([]int, k)
	for i := range indices {
		indices[i] = i
	}
	best := ""
	first := true
	permute(indices, 0, func(perm []int) {
		// perm[i] = canonical slot for original member i.
		permKinds := make([]string, k)
		for i := 0; i < k; i++ {
			permKinds[perm[i]] = kinds[i]
		}
		permEdges := make([]edgeRec, len(edges))
		for i, e := range edges {
			permEdges[i] = edgeRec{from: perm[e.from], to: perm[e.to], kind: e.kind}
		}
		sort.Slice(permEdges, func(i, j int) bool {
			if permEdges[i].from != permEdges[j].from {
				return permEdges[i].from < permEdges[j].from
			}
			if permEdges[i].to != permEdges[j].to {
				return permEdges[i].to < permEdges[j].to
			}
			return permEdges[i].kind < permEdges[j].kind
		})
		var b strings.Builder
		b.WriteString("k=")
		b.WriteString(strconv.Itoa(k))
		b.WriteByte(';')
		b.WriteString("nodes=")
		b.WriteString(strings.Join(permKinds, ","))
		b.WriteByte(';')
		b.WriteString("edges=")
		for _, pe := range permEdges {
			fmt.Fprintf(&b, "%d-%s->%d|", pe.from, pe.kind, pe.to)
		}
		s := b.String()
		if first || s < best {
			best = s
			first = false
		}
	})
	return best
}

// permute enumerates all permutations of arr, calling fn with a fresh
// copy each time. Recursive; fine for k ≤ 5 (120 permutations).
func permute(arr []int, start int, fn func([]int)) {
	if start == len(arr) {
		cp := append([]int(nil), arr...)
		fn(cp)
		return
	}
	for i := start; i < len(arr); i++ {
		arr[start], arr[i] = arr[i], arr[start]
		permute(arr, start+1, fn)
		arr[start], arr[i] = arr[i], arr[start]
	}
}

// sizeFromCanonical extracts k from the canonical form prefix
// "k=<int>;...".
func sizeFromCanonical(s string) int {
	if !strings.HasPrefix(s, "k=") {
		return 0
	}
	rest := s[2:]
	semi := strings.IndexByte(rest, ';')
	if semi < 0 {
		return 0
	}
	v, err := strconv.Atoi(rest[:semi])
	if err != nil {
		return 0
	}
	return v
}

func init() { Register(MotifRedundancy{}) }
