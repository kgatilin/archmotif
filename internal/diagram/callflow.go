package diagram

import (
	"fmt"
	"strings"

	"github.com/kgatilin/archmotif/internal/graph"
)

// DefaultCallFlowDepth is the BFS hop limit when Options.Depth <= 0.
// Three hops surfaces a useful slice for most entrypoints without
// fanning out into transitive closure noise.
const DefaultCallFlowDepth = 3

// buildCallFlow projects a forward call-graph slice rooted at the
// configured seeds. The walk follows EdgeCalls and EdgeCallsFrom
// (control-flow primitives that issue a call) so loop / branch /
// goroutine context is preserved when present.
//
// Filter rules (ADR-035):
//   - resolve seeds to nodes (exact ID or QName suffix match);
//   - if no seeds resolve, auto-pick Function nodes named "main",
//     "Run", or "Serve" (and methods of the same name) — covers the
//     common Go entrypoints;
//   - BFS forward along EdgeCalls / EdgeCallsFrom up to opts.Depth
//     hops (default DefaultCallFlowDepth);
//   - drop foreign nodes unless opts.IncludeForeign — keeps the
//     diagram focused on owned code by default.
func buildCallFlow(g *graph.Graph, opts Options) *Diagram {
	depth := opts.Depth
	if depth <= 0 {
		depth = DefaultCallFlowDepth
	}

	d := &Diagram{
		Kind:  KindCallFlow,
		Title: fmt.Sprintf("Call flow (depth=%d)", depth),
	}

	seeds, droppedSeeds := ResolveSeeds(g, opts.Seeds)
	for _, ds := range droppedSeeds {
		d.Notes = append(d.Notes, fmt.Sprintf("seed not found: %q", ds))
	}
	if len(seeds) == 0 {
		seeds = autoSeeds(g)
		if len(seeds) > 0 {
			names := make([]string, 0, len(seeds))
			for _, s := range seeds {
				if n, ok := g.Node(s); ok {
					names = append(names, displayName(n))
				}
			}
			d.Notes = append(d.Notes,
				fmt.Sprintf("auto-picked %d seed(s): %s", len(seeds), strings.Join(names, ", ")))
		}
	}
	if len(seeds) == 0 {
		d.Notes = append(d.Notes, "no entrypoints found — pass --seed=<qname> to select one")
		return d
	}

	keep := make(map[string]struct{})
	for _, s := range seeds {
		keep[s] = struct{}{}
	}

	// BFS forward along EdgeCalls / EdgeCallsFrom.
	frontier := append([]string(nil), seeds...)
	for hop := 0; hop < depth; hop++ {
		next := []string{}
		for _, id := range frontier {
			for _, kind := range []graph.EdgeKind{graph.EdgeCalls, graph.EdgeCallsFrom} {
				for _, neigh := range g.Neighbors(id, graph.DirectionOut, kind) {
					if !opts.IncludeForeign && nodeForeign(neigh) {
						continue
					}
					if _, ok := keep[neigh.ID]; ok {
						continue
					}
					keep[neigh.ID] = struct{}{}
					next = append(next, neigh.ID)
				}
			}
		}
		frontier = next
		if len(frontier) == 0 {
			break
		}
	}

	containerOf := buildContainerIndex(g)
	for _, n := range g.Nodes() {
		if _, ok := keep[n.ID]; !ok {
			continue
		}
		cluster := packageClusterFor(g, containerOf, n)
		isSeed := false
		for _, s := range seeds {
			if s == n.ID {
				isSeed = true
				break
			}
		}
		label := displayName(n)
		if isSeed {
			label = "[seed] " + label
		}
		d.Nodes = append(d.Nodes, DiagNode{
			ID:          n.ID,
			Label:       label,
			Kind:        n.Kind,
			Role:        n.Role(),
			Cluster:     cluster,
			EvidenceIDs: []string{n.ID},
		})
	}

	for _, e := range g.Edges() {
		if e.Kind != graph.EdgeCalls && e.Kind != graph.EdgeCallsFrom {
			continue
		}
		if _, ok := keep[e.From]; !ok {
			continue
		}
		if _, ok := keep[e.To]; !ok {
			continue
		}
		d.Edges = append(d.Edges, DiagEdge{
			From:        e.From,
			To:          e.To,
			Kind:        e.Kind,
			Count:       1,
			Label:       string(e.Kind),
			EvidenceIDs: []string{edgeEvidenceID(e.From, e.To, e.Kind)},
		})
	}

	sortNodesByLabel(d.Nodes)
	sortEdges(d.Edges)
	return d
}

// ResolveSeeds matches the user-supplied seed strings against graph
// node IDs and QNames. Each input matches a node when:
//   - it equals a node's stable ID, OR
//   - it equals a node's QName, OR
//   - it is a suffix of a node's QName (so callers can pass
//     "pkg.Func" without the full module prefix).
//
// Inputs that match no node are returned in `dropped`. Resolution is
// deterministic: when a suffix matches multiple nodes, the
// lexicographically smallest QName wins so snapshots stay stable.
func ResolveSeeds(g *graph.Graph, inputs []string) (resolved []string, dropped []string) {
	if len(inputs) == 0 {
		return nil, nil
	}
	for _, raw := range inputs {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		// Exact ID first.
		if g.HasNode(s) {
			resolved = append(resolved, s)
			continue
		}
		// QName / suffix match. Walk in stable insertion order so
		// the smallest-ID tie-break is implicit.
		hit := ""
		for _, n := range g.Nodes() {
			if n.QName == "" {
				continue
			}
			if n.QName == s || strings.HasSuffix(n.QName, "."+s) || strings.HasSuffix(n.QName, "/"+s) {
				if hit == "" || n.QName < hit {
					hit = n.QName
					// Track ID via a sentinel below.
				}
			}
		}
		if hit != "" {
			// Find the node whose QName equals hit — first match
			// wins.
			for _, n := range g.Nodes() {
				if n.QName == hit {
					resolved = append(resolved, n.ID)
					break
				}
			}
			continue
		}
		dropped = append(dropped, raw)
	}
	return resolved, dropped
}

// autoSeeds picks Function / Method nodes named "main", "Run", or
// "Serve" as default entrypoints. Methods are included so server
// types like (*Server).Run surface naturally.
func autoSeeds(g *graph.Graph) []string {
	wanted := map[string]struct{}{
		"main":  {},
		"Run":   {},
		"Serve": {},
	}
	out := []string{}
	for _, kind := range []graph.NodeKind{graph.NodeFunction, graph.NodeMethod} {
		for _, n := range g.NodesByKind(kind) {
			if nodeForeign(n) {
				continue
			}
			if _, ok := wanted[n.Name]; !ok {
				continue
			}
			out = append(out, n.ID)
		}
	}
	return out
}

// displayName picks the most human-friendly label for a graph node.
// QName when present (gives the package path), Name otherwise, ID as
// last resort. Same ordering as coupling.displayName.
func displayName(n graph.Node) string {
	if strings.TrimSpace(n.QName) != "" {
		return n.QName
	}
	if n.Name != "" {
		return n.Name
	}
	return n.ID
}
