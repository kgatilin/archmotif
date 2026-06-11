package graph

import (
	"fmt"
	"sort"

	"gonum.org/v1/gonum/graph/simple"
)

// Node is the typed payload for a graph node. The underlying gonum node ID
// (int64) is an internal detail; callers reference nodes by the stable ID
// (per ADR-005) returned in Node.ID.
type Node struct {
	ID   string   `json:"id"`
	Kind NodeKind `json:"kind"`
	Name string   `json:"name,omitempty"`
	// QName is the fully-qualified Go identifier path when known
	// (e.g. github.com/foo/bar/pkg.Type.Method). Optional — supplements
	// the position-based ID for symbol lookups.
	QName string `json:"qname,omitempty"`
	// Pos is the source position; empty for Package and foreign nodes.
	Pos Position `json:"pos,omitempty"`
	// Attrs holds kind-specific metadata (e.g. struct vs interface,
	// goroutine receiver, channel direction). Stable JSON keys.
	Attrs map[string]any `json:"attrs,omitempty"`
}

// Edge is the typed payload for a directed graph edge.
type Edge struct {
	From  string         `json:"from"`
	To    string         `json:"to"`
	Kind  EdgeKind       `json:"kind"`
	Attrs map[string]any `json:"attrs,omitempty"`
}

// Graph is archmotif's typed graph wrapper around gonum's directed graph.
//
// The wrapper:
//   - assigns gonum integer IDs internally,
//   - keeps a parallel map from stable string ID to Node,
//   - records edge kind alongside each gonum edge so the same (from, to)
//     pair can carry multiple kinds (e.g. Calls + Returns),
//   - exposes typed query helpers (NodesByKind, Neighbors, Subgraph).
type Graph struct {
	g       *simple.DirectedGraph
	nodes   map[string]*nodeEntry // by stable string ID
	byInt   map[int64]*nodeEntry  // by gonum int ID
	edges   []Edge                // in insertion order
	edgeIdx map[edgeKey]int       // index into edges for de-dup
	next    int64
}

type nodeEntry struct {
	node Node
	gid  int64
}

type edgeKey struct {
	from string
	to   string
	kind EdgeKind
}

// New returns an empty graph.
func New() *Graph {
	return &Graph{
		g:       simple.NewDirectedGraph(),
		nodes:   make(map[string]*nodeEntry),
		byInt:   make(map[int64]*nodeEntry),
		edgeIdx: make(map[edgeKey]int),
	}
}

// AddNode inserts n into the graph. If a node with the same ID already
// exists the existing entry is returned and the input is ignored. The
// boolean reports whether n was actually inserted (true) or merged (false).
func (g *Graph) AddNode(n Node) (Node, bool) {
	if existing, ok := g.nodes[n.ID]; ok {
		return existing.node, false
	}
	gid := g.next
	g.next++
	g.g.AddNode(simple.Node(gid))
	entry := &nodeEntry{node: n, gid: gid}
	g.nodes[n.ID] = entry
	g.byInt[gid] = entry
	return n, true
}

// HasNode reports whether a node with the given stable ID exists.
func (g *Graph) HasNode(id string) bool {
	_, ok := g.nodes[id]
	return ok
}

// Node returns the node with the given stable ID and a boolean indicating
// presence.
func (g *Graph) Node(id string) (Node, bool) {
	if e, ok := g.nodes[id]; ok {
		return e.node, true
	}
	return Node{}, false
}

// AddEdge inserts a typed edge. Both endpoints must already exist. The
// boolean reports whether the edge was newly added (true) or was a
// duplicate of an identical (from, to, kind) edge already present.
func (g *Graph) AddEdge(e Edge) (bool, error) {
	from, ok := g.nodes[e.From]
	if !ok {
		return false, fmt.Errorf("graph.AddEdge: unknown from-node %q", e.From)
	}
	to, ok := g.nodes[e.To]
	if !ok {
		return false, fmt.Errorf("graph.AddEdge: unknown to-node %q", e.To)
	}
	key := edgeKey{from: e.From, to: e.To, kind: e.Kind}
	if _, dup := g.edgeIdx[key]; dup {
		return false, nil
	}
	// gonum simple.DirectedGraph rejects parallel edges *and* self
	// edges. We only call SetEdge for non-self edges that don't yet
	// exist between the endpoints; self edges (recursive calls,
	// self-Implements) are tracked only in our edge slice and remain
	// queryable via Neighbors / IncidentEdges.
	if from.gid != to.gid && g.g.Edge(from.gid, to.gid) == nil {
		g.g.SetEdge(simple.Edge{F: simple.Node(from.gid), T: simple.Node(to.gid)})
	}
	g.edges = append(g.edges, e)
	g.edgeIdx[key] = len(g.edges) - 1
	return true, nil
}

// NodeCount returns the total number of nodes.
func (g *Graph) NodeCount() int { return len(g.nodes) }

// EdgeCount returns the total number of typed edges (multiple kinds
// between the same endpoints count separately).
func (g *Graph) EdgeCount() int { return len(g.edges) }

// Nodes returns all nodes in insertion order.
func (g *Graph) Nodes() []Node {
	out := make([]Node, 0, len(g.nodes))
	// iterate in insertion order via byInt with sorted gonum IDs
	ids := make([]int64, 0, len(g.byInt))
	for id := range g.byInt {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		out = append(out, g.byInt[id].node)
	}
	return out
}

// Edges returns all edges in insertion order.
func (g *Graph) Edges() []Edge {
	out := make([]Edge, len(g.edges))
	copy(out, g.edges)
	return out
}

// NodesByKind returns nodes of the given kind in insertion order.
func (g *Graph) NodesByKind(kind NodeKind) []Node {
	all := g.Nodes()
	out := make([]Node, 0)
	for _, n := range all {
		if n.Kind == kind {
			out = append(out, n)
		}
	}
	return out
}

// Neighbors returns nodes reachable from id along edges of the given kind
// in the requested direction. Pass an empty EdgeKind to match any edge.
func (g *Graph) Neighbors(id string, dir Direction, kind EdgeKind) []Node {
	if _, ok := g.nodes[id]; !ok {
		return nil
	}
	seen := make(map[string]struct{})
	out := make([]Node, 0)
	add := func(otherID string) {
		if _, dup := seen[otherID]; dup {
			return
		}
		seen[otherID] = struct{}{}
		if e, ok := g.nodes[otherID]; ok {
			out = append(out, e.node)
		}
	}
	for _, e := range g.edges {
		matchKind := kind == "" || e.Kind == kind
		if !matchKind {
			continue
		}
		switch dir {
		case DirectionOut:
			if e.From == id {
				add(e.To)
			}
		case DirectionIn:
			if e.To == id {
				add(e.From)
			}
		case DirectionBoth:
			if e.From == id {
				add(e.To)
			} else if e.To == id {
				add(e.From)
			}
		}
	}
	return out
}

// IncidentEdges returns the typed edges incident to id in the given
// direction. Pass an empty EdgeKind to match any edge.
func (g *Graph) IncidentEdges(id string, dir Direction, kind EdgeKind) []Edge {
	out := make([]Edge, 0)
	for _, e := range g.edges {
		if kind != "" && e.Kind != kind {
			continue
		}
		switch dir {
		case DirectionOut:
			if e.From == id {
				out = append(out, e)
			}
		case DirectionIn:
			if e.To == id {
				out = append(out, e)
			}
		case DirectionBoth:
			if e.From == id || e.To == id {
				out = append(out, e)
			}
		}
	}
	return out
}

// Subgraph returns a new graph containing the seed nodes plus everything
// reachable from any seed within `depth` hops along edges of any kind
// (treating them as undirected). depth=0 returns just the seeds.
func (g *Graph) Subgraph(seeds []string, depth int) *Graph {
	keep := make(map[string]struct{})
	frontier := make([]string, 0, len(seeds))
	for _, s := range seeds {
		if _, ok := g.nodes[s]; ok {
			keep[s] = struct{}{}
			frontier = append(frontier, s)
		}
	}
	for d := 0; d < depth; d++ {
		next := make([]string, 0)
		for _, id := range frontier {
			for _, n := range g.Neighbors(id, DirectionBoth, "") {
				if _, ok := keep[n.ID]; ok {
					continue
				}
				keep[n.ID] = struct{}{}
				next = append(next, n.ID)
			}
		}
		frontier = next
		if len(frontier) == 0 {
			break
		}
	}

	out := New()
	// Preserve original insertion order for determinism.
	for _, n := range g.Nodes() {
		if _, ok := keep[n.ID]; ok {
			out.AddNode(n)
		}
	}
	for _, e := range g.edges {
		_, fk := keep[e.From]
		_, tk := keep[e.To]
		if fk && tk {
			_, _ = out.AddEdge(e)
		}
	}
	return out
}
