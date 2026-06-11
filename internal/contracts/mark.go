package contracts

import (
	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// Mark applies the contract attributes to the graph for every Resolved
// entry, then propagates the marker through interface embedding: any
// interface that embeds a contract interface (transitively) is also
// marked, with `contractSource: "embedded"` and a back-pointer to the
// origin contract.
//
// Returns the list of node IDs that ended up marked (in graph
// insertion order — stable across runs).
func Mark(g *mgraph.Graph, resolved []Resolved) []string {
	// Phase 1 — explicit config markers.
	originBy := make(map[string]string, len(resolved)) // marked nodeID -> origin contract nodeID
	for _, r := range resolved {
		kind := "type"
		if r.IsIface {
			kind = "interface"
		}
		// `r.Entry.Kind()` is what the user *declared*; we record the
		// actual underlying kind to avoid lying when it disagrees.
		_ = g.MarkContract(r.NodeID, kind, "config", nil)
		originBy[r.NodeID] = r.NodeID
	}

	// Phase 2 — interface embedding propagation. For every interface
	// already marked, walk *inbound* Embeds edges (anything that
	// embeds it) and propagate. Iterate to a fixed point so chains
	// like C embeds B embeds A propagate through.
	for {
		added := 0
		for nodeID, origin := range originBy {
			node, ok := g.Node(nodeID)
			if !ok {
				continue
			}
			if node.ContractKind() != "interface" {
				continue
			}
			// Embedders — `(B) --Embeds--> (A)`, so an interface that
			// embeds the contract is an in-neighbour along Embeds.
			for _, embedder := range g.Neighbors(nodeID, mgraph.DirectionIn, mgraph.EdgeEmbeds) {
				if embedder.Kind != mgraph.NodeType {
					continue
				}
				if embedder.IsContract() {
					continue
				}
				// Only propagate to interfaces — the issue says
				// "if interface B embeds A and A is a contract,
				// B's contract-typed members are transitively
				// contract-relevant." Struct embedding of an
				// interface contract is a different relationship
				// (impl-by-promotion) and is captured by Implements.
				if kindOf(embedder) != "interface" {
					continue
				}
				if g.MarkContract(embedder.ID, "interface", "embedded", map[string]any{
					mgraph.AttrContractEmbeds: origin,
				}) {
					originBy[embedder.ID] = origin
					added++
				}
			}
		}
		if added == 0 {
			break
		}
	}

	// Stable order: walk g.Nodes() (insertion order) and pick out the
	// marked ones.
	out := make([]string, 0, len(originBy))
	for _, n := range g.Nodes() {
		if _, ok := originBy[n.ID]; ok {
			out = append(out, n.ID)
		}
	}
	return out
}

// kindOf returns the underlying typeKind ("interface", "struct", …)
// recorded on a Type node by the parser.
func kindOf(n mgraph.Node) string {
	if n.Attrs == nil {
		return ""
	}
	s, _ := n.Attrs["typeKind"].(string)
	return s
}
