package metrics

import (
	"context"
	"sort"
	"strconv"

	"gonum.org/v1/gonum/graph/topo"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// CycleRank counts strongly-connected components of size >1 (or with a
// self-loop) in the dependency subgraph (Calls + DependsOn edges).
// It also emits one Region record per non-trivial SCC.
//
// Output:
//   - one ScopeGraph record with the count
//   - one ScopeRegion record per non-trivial SCC, value = component size,
//     details.members = node IDs
type CycleRank struct{}

// Name returns the metric identifier.
func (CycleRank) Name() string { return "cycle_rank" }

// Description returns the metric documentation string.
func (CycleRank) Description() string {
	return "count of non-trivial SCCs in the dependency subgraph (Calls + DependsOn)"
}

// Configurable returns the user-tunable knobs (none for cycle rank).
func (CycleRank) Configurable() map[string]any { return map[string]any{} }

// Compute runs Tarjan SCC and emits one graph-scope count plus one
// region record per non-trivial component.
func (CycleRank) Compute(ctx context.Context, g *mgraph.Graph) ([]Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dv := toDirected(g, map[mgraph.EdgeKind]bool{
		mgraph.EdgeCalls:     true,
		mgraph.EdgeDependsOn: true,
	})
	sccs := topo.TarjanSCC(dv.G)
	count := 0
	out := []Record{}
	regionIdx := 0
	for _, comp := range sccs {
		if len(comp) <= 1 {
			// Single node: only counts as a cycle if it has a self
			// loop. Our gonum view drops self edges, so single-node
			// SCCs are always trivial here. Skip.
			continue
		}
		count++
		members := make([]string, 0, len(comp))
		for _, n := range comp {
			members = append(members, dv.IDs[n.ID()])
		}
		sort.Strings(members)
		out = append(out, Record{
			Metric: "cycle_rank",
			Scope:  ScopeRegion,
			Target: regionID("scc", regionIdx),
			Value:  float64(len(comp)),
			Details: map[string]any{
				"members": members,
			},
		})
		regionIdx++
	}
	out = append([]Record{{
		Metric: "cycle_rank",
		Scope:  ScopeGraph,
		Value:  float64(count),
		Details: map[string]any{
			"edgeKinds": []string{string(mgraph.EdgeCalls), string(mgraph.EdgeDependsOn)},
		},
	}}, out...)
	return out, nil
}

// regionID renders a deterministic region identifier.
func regionID(prefix string, idx int) string {
	return prefix + "-" + strconv.Itoa(idx)
}

func init() { Register(CycleRank{}) }
