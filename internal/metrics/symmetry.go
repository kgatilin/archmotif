package metrics

import (
	"context"
	"sort"
	"strconv"
	"strings"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// LocalSymmetry scores, for each node, how many ≤2-hop neighbours
// play an interchangeable role. Two nodes are *interchangeable* iff
// they share:
//
//   - the same NodeKind, and
//   - the same multiset of outbound EdgeKind tags, and
//   - the same multiset of inbound EdgeKind tags.
//
// Score = number of distinct ≤2-hop neighbours (excluding the node
// itself) that pass the interchangeability test. Higher scores mark
// nodes embedded in a symmetric local structure.
//
// Output:
//   - one ScopeNode record per non-Package node (Package nodes inflate
//     the metric because every File they contain shares an "in:contains"
//     signature; we exclude them to keep the lens interpretable).
type LocalSymmetry struct{}

// Name returns the metric identifier.
func (LocalSymmetry) Name() string { return "local_symmetry" }

// Description returns the metric documentation string.
func (LocalSymmetry) Description() string {
	return "per-node count of ≤2-hop neighbours sharing kind + in/out edge multisets"
}

// Configurable returns user-tunable knobs (hop radius defaults to 2).
func (LocalSymmetry) Configurable() map[string]any {
	return map[string]any{"radius": 2}
}

// Compute walks every node, computes its role signature, and counts
// matching ≤2-hop neighbours.
func (LocalSymmetry) Compute(ctx context.Context, g *mgraph.Graph) ([]Record, error) {
	const radius = 2
	signatures := make(map[string]string, g.NodeCount())
	for _, n := range g.Nodes() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		signatures[n.ID] = roleSignature(g, n)
	}
	out := make([]Record, 0, g.NodeCount())
	for _, n := range g.Nodes() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if n.Kind == mgraph.NodePackage {
			continue
		}
		sig := signatures[n.ID]
		neigh := neighbourhood(g, n.ID, radius)
		matches := 0
		matchIDs := make([]string, 0)
		for id := range neigh {
			if id == n.ID {
				continue
			}
			if signatures[id] == sig {
				matches++
				matchIDs = append(matchIDs, id)
			}
		}
		sort.Strings(matchIDs)
		out = append(out, Record{
			Metric: "local_symmetry",
			Scope:  ScopeNode,
			Target: n.ID,
			Value:  float64(matches),
			Details: map[string]any{
				"signature": sig,
				"matchIDs":  matchIDs,
				"radius":    radius,
			},
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Target < out[j].Target })
	return out, nil
}

// roleSignature renders a stable string capturing kind + edge multisets.
// Format: "<kind>|out:<sorted,counted out kinds>|in:<sorted,counted in kinds>".
func roleSignature(g *mgraph.Graph, n mgraph.Node) string {
	out := edgeKindCounts(g.IncidentEdges(n.ID, mgraph.DirectionOut, ""))
	in := edgeKindCounts(g.IncidentEdges(n.ID, mgraph.DirectionIn, ""))
	var b strings.Builder
	b.WriteString(string(n.Kind))
	b.WriteString("|out:")
	b.WriteString(formatCounts(out))
	b.WriteString("|in:")
	b.WriteString(formatCounts(in))
	return b.String()
}

func edgeKindCounts(edges []mgraph.Edge) map[mgraph.EdgeKind]int {
	out := map[mgraph.EdgeKind]int{}
	for _, e := range edges {
		out[e.Kind]++
	}
	return out
}

func formatCounts(c map[mgraph.EdgeKind]int) string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, string(k))
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+":"+strconv.Itoa(c[mgraph.EdgeKind(k)]))
	}
	return strings.Join(parts, ",")
}

// neighbourhood returns the set of node IDs reachable from id within
// `depth` hops along edges of any kind, treating direction as
// undirected. Excludes id itself.
func neighbourhood(g *mgraph.Graph, id string, depth int) map[string]struct{} {
	seen := map[string]struct{}{id: {}}
	frontier := []string{id}
	for d := 0; d < depth; d++ {
		next := []string{}
		for _, cur := range frontier {
			for _, n := range g.Neighbors(cur, mgraph.DirectionBoth, "") {
				if _, ok := seen[n.ID]; ok {
					continue
				}
				seen[n.ID] = struct{}{}
				next = append(next, n.ID)
			}
		}
		frontier = next
		if len(frontier) == 0 {
			break
		}
	}
	delete(seen, id)
	return seen
}

func init() { Register(LocalSymmetry{}) }
