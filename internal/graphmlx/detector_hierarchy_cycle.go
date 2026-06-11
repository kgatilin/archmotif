package graphmlx

import (
	"fmt"
	"sort"
)

// HierarchyCycleDetector finds cycles in the structural part-of /
// contains hierarchy. Hierarchical edges should always form a DAG;
// any SCC with more than one member (or a self-loop) is a hard error.
//
// Implementation: Tarjan's SCC over the subgraph of hierarchical
// edges only. One Finding per non-trivial SCC.
type HierarchyCycleDetector struct{}

// Name returns the detector identifier.
func (HierarchyCycleDetector) Name() string { return "hierarchy_cycle" }

// Description returns the detector documentation string.
func (HierarchyCycleDetector) Description() string {
	return "flags cycles in the structural part-of/contains hierarchy"
}

// Detect emits one finding per non-trivial SCC in the hierarchy
// subgraph (or per self-loop).
func (d HierarchyCycleDetector) Detect(g *Graph) ([]Finding, error) {
	if g == nil {
		return nil, nil
	}
	adj, nodes := hierarchyAdjacency(g)
	sccs := tarjanSCC(adj, nodes)
	out := make([]Finding, 0)
	for _, scc := range sccs {
		// Trivial SCC: single node with no self-loop in the hierarchy
		// subgraph. Skip.
		if len(scc) == 1 {
			node := scc[0]
			if !hasSelfLoop(adj, node) {
				continue
			}
		}
		members := append([]string(nil), scc...)
		sort.Strings(members)
		out = append(out, Finding{
			Detector:  d.Name(),
			Score:     float64(len(members)),
			Severity:  hierarchyCycleSeverity(len(members)),
			PrimaryID: members[0],
			Members:   members,
			Reason: Reason{
				Code:    "hierarchy_cycle",
				Message: fmt.Sprintf("hierarchy cycle of %d node(s) detected (parent-of/contains)", len(members)),
				Details: map[string]any{
					"size":    len(members),
					"members": members,
				},
			},
			Evidence: map[string]any{
				"size":    len(members),
				"members": members,
			},
		})
	}
	return out, nil
}

// hierarchyAdjacency returns adjacency keyed by node ID, restricted
// to hierarchy edges. The second return value is the set of node IDs
// that participate in any hierarchy edge — Tarjan only needs to scan
// these, not the whole graph.
func hierarchyAdjacency(g *Graph) (map[string][]string, []string) {
	adj := map[string][]string{}
	seen := map[string]struct{}{}
	for _, e := range g.Edges {
		if !isHierarchyKind(e.Kind) {
			continue
		}
		adj[e.From] = append(adj[e.From], e.To)
		seen[e.From] = struct{}{}
		seen[e.To] = struct{}{}
	}
	nodes := make([]string, 0, len(seen))
	for n := range seen {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)
	for k, v := range adj {
		adj[k] = dedupSorted(v)
	}
	return adj, nodes
}

func hasSelfLoop(adj map[string][]string, n string) bool {
	for _, t := range adj[n] {
		if t == n {
			return true
		}
	}
	return false
}

// tarjanSCC computes strongly-connected components of the directed
// graph (adj, nodes) and returns them. Each SCC's node order is
// stable (we sort members below at finding-emit time).
func tarjanSCC(adj map[string][]string, nodes []string) [][]string {
	index := 0
	indices := map[string]int{}
	lowlink := map[string]int{}
	onStack := map[string]bool{}
	stack := []string{}
	var out [][]string

	var strongconnect func(v string)
	strongconnect = func(v string) {
		indices[v] = index
		lowlink[v] = index
		index++
		stack = append(stack, v)
		onStack[v] = true
		for _, w := range adj[v] {
			if _, ok := indices[w]; !ok {
				strongconnect(w)
				if lowlink[w] < lowlink[v] {
					lowlink[v] = lowlink[w]
				}
			} else if onStack[w] {
				if indices[w] < lowlink[v] {
					lowlink[v] = indices[w]
				}
			}
		}
		if lowlink[v] == indices[v] {
			var scc []string
			for {
				if len(stack) == 0 {
					break
				}
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				scc = append(scc, w)
				if w == v {
					break
				}
			}
			out = append(out, scc)
		}
	}
	for _, n := range nodes {
		if _, ok := indices[n]; !ok {
			strongconnect(n)
		}
	}
	return out
}

func hierarchyCycleSeverity(size int) Severity {
	switch {
	case size >= 5:
		return SeverityCritical
	case size >= 2:
		return SeverityHigh
	default:
		// self-loop on a single node
		return SeverityMedium
	}
}

func init() { Register(HierarchyCycleDetector{}) }
