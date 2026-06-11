package wlkernel

import (
	"testing"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// TestSmoke is the prototype's only test. It exercises three things:
//   - Two structurally-isomorphic role-typed graphs (same shape: a
//     domain entity, a port interface, an adapter implementing the
//     port) produce the same WL multiset.
//   - The same domain shape paired with a different adapter wiring
//     (port not implemented; adapter calls domain directly) produces
//     a strictly-lower cosine similarity.
//   - Comparing embeddings computed at different iteration depths
//     returns 0 — the label spaces are not commensurable.
//
// The numbers in the asserts are intentionally generous: the test is
// here to keep the prototype building and to catch refactor breakage,
// not to pin a specific cosine value.
func TestSmoke(t *testing.T) {
	a := buildHexGraph(t, "A")
	b := buildHexGraph(t, "B")
	c := buildBrokenHexGraph(t, "C")

	// Iteration 0: only (kind, role) histogram. Both graphs have the
	// same role multiset so even the broken wiring scores 1.0 — this
	// is an explicit observation captured in ADR-036: a depth-0 WL
	// embedding cannot distinguish wiring from role mix.
	emb0A := Compute(a, 0)
	emb0B := Compute(b, 0)
	emb0C := Compute(c, 0)
	if got := Cosine(emb0A, emb0B); got < 0.99 {
		t.Fatalf("iter-0 isomorphic: got %.3f, want ≥ 0.99", got)
	}
	if got := Cosine(emb0A, emb0C); got < 0.99 {
		t.Fatalf("iter-0 same-roles different-wiring: got %.3f, want ≥ 0.99 (ADR-036 observation)", got)
	}

	// Iteration 1: neighbour signatures fold in. Isomorphic graphs
	// still match perfectly; the broken-hex now shares no refined
	// labels because every neighbour signature differs. This is the
	// "WL is too brittle on small graphs" finding documented in
	// ADR-036.
	embA := Compute(a, 1)
	embB := Compute(b, 1)
	embC := Compute(c, 1)
	simAB := Cosine(embA, embB)
	simAC := Cosine(embA, embC)
	if simAB < 0.99 {
		t.Fatalf("iter-1 isomorphic: simAB = %.3f, want ≥ 0.99", simAB)
	}
	if simAC >= simAB {
		t.Fatalf("iter-1 broken < isomorphic expected: simAC=%.3f simAB=%.3f", simAC, simAB)
	}

	// Iteration mismatch returns 0 by construction.
	if got := Cosine(embA, Compute(b, 0)); got != 0 {
		t.Fatalf("iteration-mismatch cosine: got %.3f, want 0", got)
	}
}

// buildHexGraph constructs a 3-node ports-and-adapters fixture:
//   - <prefix>.Entity (domain_entity)
//   - <prefix>.Port (port)
//   - <prefix>.Adapter (outbound_adapter)
//
// Edges: Adapter --implements--> Port; Adapter --calls--> Entity.
func buildHexGraph(t *testing.T, prefix string) *mgraph.Graph {
	t.Helper()
	g := mgraph.New()
	addNode(t, g, prefix+".Entity", mgraph.NodeType, "domain_entity")
	addNode(t, g, prefix+".Port", mgraph.NodeType, "port")
	addNode(t, g, prefix+".Adapter", mgraph.NodeType, "outbound_adapter")
	addEdge(t, g, prefix+".Adapter", prefix+".Port", mgraph.EdgeImplements)
	addEdge(t, g, prefix+".Adapter", prefix+".Entity", mgraph.EdgeCalls)
	return g
}

// buildBrokenHexGraph keeps the same node roles as buildHexGraph but
// drops the Implements edge and re-routes the Calls edge: the adapter
// calls the port directly without implementing it. This is the kind
// of "looks like the right shape but isn't" case the deterministic
// patterns can already catch via forbidden_role_edges.
func buildBrokenHexGraph(t *testing.T, prefix string) *mgraph.Graph {
	t.Helper()
	g := mgraph.New()
	addNode(t, g, prefix+".Entity", mgraph.NodeType, "domain_entity")
	addNode(t, g, prefix+".Port", mgraph.NodeType, "port")
	addNode(t, g, prefix+".Adapter", mgraph.NodeType, "outbound_adapter")
	addEdge(t, g, prefix+".Adapter", prefix+".Port", mgraph.EdgeCalls)
	addEdge(t, g, prefix+".Port", prefix+".Entity", mgraph.EdgeCalls)
	return g
}

func addNode(t *testing.T, g *mgraph.Graph, id string, kind mgraph.NodeKind, role string) {
	t.Helper()
	_, _ = g.AddNode(mgraph.Node{
		ID:    id,
		Kind:  kind,
		Name:  id,
		Attrs: map[string]any{"role": role},
	})
}

func addEdge(t *testing.T, g *mgraph.Graph, from, to string, kind mgraph.EdgeKind) {
	t.Helper()
	if _, err := g.AddEdge(mgraph.Edge{From: from, To: to, Kind: kind}); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}
}
