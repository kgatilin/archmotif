package metrics

import (
	"context"
	"sort"

	gn "gonum.org/v1/gonum/graph"
	gncommunity "gonum.org/v1/gonum/graph/community"
	"gonum.org/v1/gonum/graph/simple"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// Modularity computes Newman modularity Q over package boundaries
// (per ADR-014). Communities are defined by package membership: each
// loaded Package node plus everything transitively reachable along
// outbound Contains edges from it.
//
// We use community.Q on an undirected projection of the typed graph
// (the directed-vs-undirected lens choice is documented in ADR-014).
// Nodes that don't belong to any package community (orphans, foreign
// placeholders not contained by their foreign package) are placed
// each in a singleton community so Q is well-defined for the full
// node set.
//
// Output:
//   - one ScopeGraph record with Q
//   - one ScopeRegion record per community used to compute Q
//     (value = community size, details.members = stable IDs).
type Modularity struct{}

// Name returns the metric identifier.
func (Modularity) Name() string { return "modularity" }

// Description returns the metric documentation string.
func (Modularity) Description() string {
	return "Newman modularity Q over package boundaries (undirected projection)"
}

// Configurable returns user-tunable knobs (resolution = 1.0 by default).
func (Modularity) Configurable() map[string]any { return map[string]any{"resolution": 1.0} }

// Compute builds package communities and calls community.Q.
func (Modularity) Compute(ctx context.Context, g *mgraph.Graph) ([]Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	uv := toUndirected(g, nil)
	if uv.G.Nodes().Len() == 0 {
		return []Record{{
			Metric:  "modularity",
			Scope:   ScopeGraph,
			Value:   0,
			Details: map[string]any{"note": "empty graph"},
		}}, nil
	}

	// Map each typed node ID to a community index.
	commByNode := map[string]int{}
	communityIDs := []string{} // index -> package node ID, "" for orphans
	communityNodes := [][]string{}
	pkgIndex := map[string]int{}

	// Identify package communities: walk outbound Contains from each Package.
	for _, p := range g.NodesByKind(mgraph.NodePackage) {
		idx := len(communityIDs)
		pkgIndex[p.ID] = idx
		communityIDs = append(communityIDs, p.ID)
		communityNodes = append(communityNodes, []string{p.ID})
		commByNode[p.ID] = idx
	}
	// BFS along Contains from each package.
	for _, p := range g.NodesByKind(mgraph.NodePackage) {
		idx := pkgIndex[p.ID]
		frontier := []string{p.ID}
		for len(frontier) > 0 {
			next := []string{}
			for _, id := range frontier {
				for _, n := range g.Neighbors(id, mgraph.DirectionOut, mgraph.EdgeContains) {
					if _, claimed := commByNode[n.ID]; claimed {
						continue
					}
					commByNode[n.ID] = idx
					communityNodes[idx] = append(communityNodes[idx], n.ID)
					next = append(next, n.ID)
				}
			}
			frontier = next
		}
	}

	// Orphans (nodes not contained by any package): each in a singleton.
	for _, n := range g.Nodes() {
		if _, ok := commByNode[n.ID]; ok {
			continue
		}
		idx := len(communityIDs)
		communityIDs = append(communityIDs, "")
		communityNodes = append(communityNodes, []string{n.ID})
		commByNode[n.ID] = idx
	}

	// Build [][]graph.Node communities for community.Q. Use the same
	// undirected gonum view so node IDs line up.
	gonumComms := make([][]gn.Node, len(communityIDs))
	for stableID, idx := range commByNode {
		gi, ok := uv.Idx[stableID]
		if !ok {
			continue
		}
		gonumComms[idx] = append(gonumComms[idx], simple.Node(gi))
	}
	// Drop empty communities (shouldn't happen but defensive).
	pruned := make([][]gn.Node, 0, len(gonumComms))
	prunedIDs := make([]string, 0, len(gonumComms))
	prunedNodes := make([][]string, 0, len(gonumComms))
	for i, c := range gonumComms {
		if len(c) == 0 {
			continue
		}
		pruned = append(pruned, c)
		prunedIDs = append(prunedIDs, communityIDs[i])
		prunedNodes = append(prunedNodes, communityNodes[i])
	}

	q := gncommunity.Q(uv.G, pruned, 1.0)

	// Emit per-community region records (sorted by package ID).
	type commRow struct {
		id      string
		members []string
	}
	comms := make([]commRow, 0, len(prunedIDs))
	for i, id := range prunedIDs {
		members := append([]string(nil), prunedNodes[i]...)
		sort.Strings(members)
		comms = append(comms, commRow{id: id, members: members})
	}
	sort.SliceStable(comms, func(i, j int) bool { return comms[i].id < comms[j].id })

	out := []Record{{
		Metric: "modularity",
		Scope:  ScopeGraph,
		Value:  q,
		Details: map[string]any{
			"communities":    len(pruned),
			"resolution":     1.0,
			"undirectedView": true,
		},
	}}
	for i, c := range comms {
		target := c.id
		if target == "" {
			target = regionID("orphan", i)
		}
		out = append(out, Record{
			Metric: "modularity",
			Scope:  ScopeRegion,
			Target: target,
			Value:  float64(len(c.members)),
			Details: map[string]any{
				"members": c.members,
			},
		})
	}
	return out, nil
}

func init() { Register(Modularity{}) }
