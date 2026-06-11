package graphmlx

import (
	"fmt"
	"sort"
)

// ArticulationDetector flags nodes whose removal would disconnect
// the (undirected projection of the) graph — a.k.a. cut vertices /
// articulation points. These are the "bottleneck" nodes the issue
// scope mentions: traversal becomes fragile when one such node
// goes down.
//
// We use the standard DFS-based algorithm (Hopcroft-Tarjan) on the
// undirected projection of the graph. Score is the number of
// reachable nodes "behind" the articulation point — i.e. the size
// of the largest component the node attaches via this edge.
type ArticulationDetector struct {
	// MinComponentSize is the smallest component-behind-cut size that
	// is worth flagging. Defaults to 2; a leaf-like cut that only
	// guards 1 node is usually noise.
	MinComponentSize int
}

// Name returns the detector identifier.
func (ArticulationDetector) Name() string { return "articulation" }

// Description returns the detector documentation string.
func (ArticulationDetector) Description() string {
	return "flags articulation points (cut vertices) — traversal-fragile bottlenecks"
}

// Detect emits one finding per articulation point. Score is the size
// of the largest sub-tree it guards in the DFS spanning tree.
func (d ArticulationDetector) Detect(g *Graph) ([]Finding, error) {
	if g == nil {
		return nil, nil
	}
	min := d.MinComponentSize
	if min <= 0 {
		min = 2
	}

	adj := undirectedAdjacency(g)
	nodes := make([]string, 0, len(adj))
	for n := range adj {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)

	// Standard articulation-point DFS. We track:
	//   disc[v]  - discovery time
	//   low[v]   - earliest discoverable from subtree
	//   parent[v]
	//   subtreeSize[v]
	// A non-root v is an articulation point if it has a child u with
	// low[u] >= disc[v]. Root v is an articulation point if it has
	// >= 2 children in the DFS tree.
	disc := map[string]int{}
	low := map[string]int{}
	parent := map[string]string{}
	subtreeSize := map[string]int{}
	isArt := map[string]int{} // nodeID -> max guarded-subtree size
	timer := 0

	var dfs func(u string)
	dfs = func(u string) {
		timer++
		disc[u] = timer
		low[u] = timer
		subtreeSize[u] = 1
		children := 0
		neighbors := append([]string(nil), adj[u]...)
		sort.Strings(neighbors)
		for _, v := range neighbors {
			if _, seen := disc[v]; !seen {
				parent[v] = u
				children++
				dfs(v)
				subtreeSize[u] += subtreeSize[v]
				if low[v] < low[u] {
					low[u] = low[v]
				}
				p, hasP := parent[u]
				_ = p
				if !hasP && children > 1 {
					if subtreeSize[v] > isArt[u] {
						isArt[u] = subtreeSize[v]
					}
				}
				if hasP && low[v] >= disc[u] {
					if subtreeSize[v] > isArt[u] {
						isArt[u] = subtreeSize[v]
					}
				}
			} else if v != parent[u] {
				if disc[v] < low[u] {
					low[u] = disc[v]
				}
			}
		}
	}
	for _, n := range nodes {
		if _, seen := disc[n]; !seen {
			dfs(n)
		}
	}

	cuts := make([]string, 0, len(isArt))
	for n := range isArt {
		cuts = append(cuts, n)
	}
	sort.Strings(cuts)

	out := make([]Finding, 0, len(cuts))
	for _, n := range cuts {
		size := isArt[n]
		if size < min {
			continue
		}
		out = append(out, Finding{
			Detector:  d.Name(),
			Score:     float64(size),
			Severity:  articulationSeverity(size),
			PrimaryID: n,
			Members:   []string{n},
			Reason: Reason{
				Code:    "articulation_point",
				Message: fmt.Sprintf("articulation point %s guards a component of %d node(s)", n, size),
				Details: map[string]any{
					"componentSize": size,
				},
			},
			Evidence: map[string]any{
				"node":          n,
				"componentSize": size,
				"totalNodes":    len(g.Nodes),
			},
		})
	}
	return out, nil
}

// undirectedAdjacency returns a map of each node ID to its unique
// neighbours under an undirected projection of g (collapsing edge
// direction and deduplicating multi-edges).
func undirectedAdjacency(g *Graph) map[string][]string {
	tmp := map[string]map[string]struct{}{}
	for _, n := range g.Nodes {
		tmp[n.ID] = map[string]struct{}{}
	}
	for _, e := range g.Edges {
		if e.From == e.To {
			continue
		}
		if _, ok := tmp[e.From]; !ok {
			tmp[e.From] = map[string]struct{}{}
		}
		if _, ok := tmp[e.To]; !ok {
			tmp[e.To] = map[string]struct{}{}
		}
		tmp[e.From][e.To] = struct{}{}
		tmp[e.To][e.From] = struct{}{}
	}
	out := make(map[string][]string, len(tmp))
	for n, set := range tmp {
		nbs := make([]string, 0, len(set))
		for v := range set {
			nbs = append(nbs, v)
		}
		sort.Strings(nbs)
		out[n] = nbs
	}
	return out
}

func articulationSeverity(size int) Severity {
	switch {
	case size >= 20:
		return SeverityHigh
	case size >= 5:
		return SeverityMedium
	default:
		return SeverityLow
	}
}

func init() { Register(ArticulationDetector{}) }
