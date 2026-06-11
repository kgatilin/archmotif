package contracts

import (
	"sort"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// ProducerKind names the relation that makes a node a producer of a
// contract. See ADR-010 for the catalogue.
type ProducerKind string

const (
	// ProducerReturns marks a node as a function/method whose result list
	// includes the contract type.
	ProducerReturns ProducerKind = "returns"
	// ProducerImplements marks a node as a concrete type that satisfies
	// the contract interface.
	ProducerImplements ProducerKind = "implements"
)

// Producer is a single producer record — one node, one relation.
type Producer struct {
	Node mgraph.Node
	Kind ProducerKind
}

// Producers returns the one-hop direct producers of the contract node
// with the given ID. The list is deterministic: sorted by (kind,
// node ID) and de-duplicated against (nodeID, kind).
//
// For an interface contract, producers are:
//   - in-neighbours along Returns (functions/methods returning the iface)
//   - in-neighbours along Implements (concrete types satisfying the iface)
//
// For a non-interface contract, producers are in-neighbours along
// Returns only.
func Producers(g *mgraph.Graph, contractID string) []Producer {
	contract, ok := g.Node(contractID)
	if !ok {
		return nil
	}

	seen := make(map[string]map[ProducerKind]bool)
	add := func(n mgraph.Node, k ProducerKind) {
		if seen[n.ID] == nil {
			seen[n.ID] = make(map[ProducerKind]bool)
		}
		seen[n.ID][k] = true
	}

	for _, n := range g.Neighbors(contractID, mgraph.DirectionIn, mgraph.EdgeReturns) {
		add(n, ProducerReturns)
	}
	if contract.ContractKind() == "interface" || kindOf(contract) == "interface" {
		for _, n := range g.Neighbors(contractID, mgraph.DirectionIn, mgraph.EdgeImplements) {
			add(n, ProducerImplements)
		}
	}

	out := make([]Producer, 0)
	for id, kinds := range seen {
		node, ok := g.Node(id)
		if !ok {
			continue
		}
		for k := range kinds {
			out = append(out, Producer{Node: node, Kind: k})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Node.ID < out[j].Node.ID
	})
	return out
}

// AllContracts returns every node currently marked as a contract, in
// graph insertion order. Convenience wrapper around g.Nodes() +
// IsContract().
func AllContracts(g *mgraph.Graph) []mgraph.Node {
	out := make([]mgraph.Node, 0)
	for _, n := range g.Nodes() {
		if n.IsContract() {
			out = append(out, n)
		}
	}
	return out
}
