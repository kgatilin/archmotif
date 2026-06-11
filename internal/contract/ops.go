package contract

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/kgatilin/archmotif/internal/graphmlx"
)

// Mutable is an editable graph for applying refactoring operations. Operations
// are pure: clone, mutate, then ToGraph() to get a fresh *graphmlx.Graph. The
// output of every operation is a new graph (the "target" / proposal) — applying
// it to actual code is downstream (agent/human), not this package's job.
type Mutable struct {
	Directed bool
	Nodes    []graphmlx.Node
	Edges    []graphmlx.Edge
}

// Clone copies a graphmlx.Graph into a Mutable.
func Clone(g *graphmlx.Graph) *Mutable {
	m := &Mutable{Directed: g.Directed}
	for _, n := range g.Nodes {
		attrs := map[string]string{}
		for k, v := range n.Attrs {
			attrs[k] = v
		}
		m.Nodes = append(m.Nodes, graphmlx.Node{XMLID: n.XMLID, ID: n.ID, Label: n.Label, Kind: n.Kind, Attrs: attrs})
	}
	for _, e := range g.Edges {
		attrs := map[string]string{}
		for k, v := range e.Attrs {
			attrs[k] = v
		}
		m.Edges = append(m.Edges, graphmlx.Edge{XMLID: e.XMLID, From: e.From, To: e.To, Kind: e.Kind, Attrs: attrs})
	}
	return m
}

func (m *Mutable) has(id string) bool {
	for _, n := range m.Nodes {
		if n.ID == id {
			return true
		}
	}
	return false
}

// --- primitives ----------------------------------------------------------

// Move reassigns a node to a different group (changes its `group` attribute).
func (m *Mutable) Move(id, group string) error {
	for i := range m.Nodes {
		if m.Nodes[i].ID == id {
			if m.Nodes[i].Attrs == nil {
				m.Nodes[i].Attrs = map[string]string{}
			}
			m.Nodes[i].Attrs["group"] = group
			return nil
		}
	}
	return fmt.Errorf("Move: node %q not found", id)
}

// Redirect changes an edge's endpoints.
func (m *Mutable) Redirect(from, to, newFrom, newTo string) error {
	for i := range m.Edges {
		if m.Edges[i].From == from && m.Edges[i].To == to {
			m.Edges[i].From, m.Edges[i].To = newFrom, newTo
			return nil
		}
	}
	return fmt.Errorf("Redirect: edge %s->%s not found", from, to)
}

// Reverse flips an edge direction (special case of Redirect) — dependency
// inversion at the graph level.
func (m *Mutable) Reverse(from, to string) error { return m.Redirect(from, to, to, from) }

// RemoveEdge deletes an edge (special case of Redirect to nothing).
func (m *Mutable) RemoveEdge(from, to string) error {
	for i := range m.Edges {
		if m.Edges[i].From == from && m.Edges[i].To == to {
			m.Edges = append(m.Edges[:i], m.Edges[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("RemoveEdge: edge %s->%s not found", from, to)
}

// Introduce inserts a new node between callers and a target: every edge
// X->target is rerouted to X->newID, and one edge newID->target is added. This
// is the "introduce interface / facade" primitive.
func (m *Mutable) Introduce(newID, group, target string) error {
	if m.has(newID) {
		return fmt.Errorf("Introduce: node %q already exists", newID)
	}
	if !m.has(target) {
		return fmt.Errorf("Introduce: target %q not found", target)
	}
	m.Nodes = append(m.Nodes, graphmlx.Node{ID: newID, Label: newID, Attrs: map[string]string{"group": group}})
	for i := range m.Edges {
		if m.Edges[i].To == target && m.Edges[i].From != newID {
			m.Edges[i].To = newID
		}
	}
	m.Edges = append(m.Edges, graphmlx.Edge{From: newID, To: target, Attrs: map[string]string{}})
	return nil
}

// Merge folds src into dst: src's edges are re-pointed to dst, then src is
// removed (the "inline" primitive).
func (m *Mutable) Merge(src, dst string) error {
	if !m.has(src) || !m.has(dst) {
		return fmt.Errorf("Merge: need both %q and %q", src, dst)
	}
	for i := range m.Edges {
		if m.Edges[i].From == src {
			m.Edges[i].From = dst
		}
		if m.Edges[i].To == src {
			m.Edges[i].To = dst
		}
	}
	m.dropNode(src)
	m.dedupSelfLoops()
	return nil
}

func (m *Mutable) dropNode(id string) {
	var ns []graphmlx.Node
	for _, n := range m.Nodes {
		if n.ID != id {
			ns = append(ns, n)
		}
	}
	m.Nodes = ns
}

func (m *Mutable) dedupSelfLoops() {
	var es []graphmlx.Edge
	for _, e := range m.Edges {
		if e.From != e.To {
			es = append(es, e)
		}
	}
	m.Edges = es
}

// --- architectural recipe (composition) ----------------------------------

// InvertDependency turns an upward dependency from->to into to->from, the
// graph-level effect of moving the shared abstraction into the foundational
// package. Returns the proposed target graph.
func (m *Mutable) InvertDependency(from, to string) error { return m.Reverse(from, to) }

// ToGraph serializes the Mutable to GraphML and reads it back as a canonical
// *graphmlx.Graph (rebuilding the internal index).
func (m *Mutable) ToGraph() (*graphmlx.Graph, error) {
	var buf bytes.Buffer
	tmp := &graphmlx.Graph{Directed: m.Directed, Nodes: m.Nodes, Edges: m.Edges}
	sort.Slice(tmp.Nodes, func(i, j int) bool { return tmp.Nodes[i].ID < tmp.Nodes[j].ID })
	if err := WriteGraphML(&buf, tmp); err != nil {
		return nil, err
	}
	return graphmlx.Read(&buf)
}
