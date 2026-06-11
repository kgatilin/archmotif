// Package shape detects structural graph-shape violations and emits
// deterministic rewrite contracts. It operates on generic GraphML rather
// than archmotif's Go-specific typed graph so the same target-shape ideas can
// be tried on memory graphs, code graphs exported through GraphML, and Gephi
// snapshots.
package shape

// Graph is a small generic directed graph loaded from GraphML.
type Graph struct {
	Nodes map[string]Node
	Edges []Edge
}

// Node is a generic graph node with string attributes.
type Node struct {
	ID     string            `json:"id"`
	XMLID  string            `json:"xmlId,omitempty"`
	Label  string            `json:"label,omitempty"`
	Attrs  map[string]string `json:"attrs,omitempty"`
	Degree int               `json:"degree,omitempty"`
}

// Edge is a generic directed graph edge with string attributes.
type Edge struct {
	ID        string            `json:"id,omitempty"`
	Source    string            `json:"source"`
	Target    string            `json:"target"`
	Predicate string            `json:"predicate,omitempty"`
	Layer     string            `json:"layer,omitempty"`
	Attrs     map[string]string `json:"attrs,omitempty"`
}

// NodeRef is the compact node reference used in candidate contracts.
type NodeRef struct {
	ID     string            `json:"id"`
	Label  string            `json:"label,omitempty"`
	Attrs  map[string]string `json:"attrs,omitempty"`
	Degree int               `json:"degree,omitempty"`
}

// EdgeRef is the compact edge reference used in candidate contracts.
type EdgeRef struct {
	ID        string `json:"id,omitempty"`
	Source    string `json:"source"`
	Target    string `json:"target"`
	Predicate string `json:"predicate,omitempty"`
	Layer     string `json:"layer,omitempty"`
}

func (g *Graph) nodeRef(id string) NodeRef {
	n := g.Nodes[id]
	attrs := selectedNodeAttrs(n.Attrs)
	return NodeRef{
		ID:     n.ID,
		Label:  n.Label,
		Attrs:  attrs,
		Degree: n.Degree,
	}
}

// NodeRefFor returns the compact public reference for a graph node.
func (g *Graph) NodeRefFor(id string) NodeRef {
	return g.nodeRef(id)
}

func edgeRef(e Edge) EdgeRef {
	return EdgeRef{
		ID:        e.ID,
		Source:    e.Source,
		Target:    e.Target,
		Predicate: e.Predicate,
		Layer:     e.Layer,
	}
}

func selectedNodeAttrs(attrs map[string]string) map[string]string {
	if len(attrs) == 0 {
		return nil
	}
	keep := []string{
		"memory_title",
		"memory_type",
		"entity_type",
		"labels",
		"kind",
		"type",
		"layer",
	}
	out := map[string]string{}
	for _, k := range keep {
		if v := attrs[k]; v != "" {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
