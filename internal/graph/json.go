package graph

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
)

// JSON is the on-disk / on-stdout serialisation form of the typed graph.
// Versioned so we can evolve the shape later without silent breakage.
type JSON struct {
	Version int    `json:"version"`
	Nodes   []Node `json:"nodes"`
	Edges   []Edge `json:"edges"`
}

// CurrentJSONVersion is the version emitted by ToJSON. Bump on
// breaking changes.
const CurrentJSONVersion = 1

// ToJSON returns the graph in its serialisable form.
func (g *Graph) ToJSON() JSON {
	return JSON{
		Version: CurrentJSONVersion,
		Nodes:   g.Nodes(),
		Edges:   g.Edges(),
	}
}

// WriteJSON encodes the graph as JSON to w with two-space indentation.
func (g *Graph) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(g.ToJSON())
}

// PrettyPrint renders the graph as human-readable text, grouping nodes
// by kind and listing edges as adjacency lines. Useful for the Stage 1
// verify step ("show a struct, its methods, who calls them"). The
// output is intentionally simple; not a stable format.
func PrettyPrint(g *Graph, w io.Writer) error {
	nodes := g.Nodes()
	byKind := make(map[NodeKind][]Node, len(AllNodeKinds()))
	for _, n := range nodes {
		byKind[n.Kind] = append(byKind[n.Kind], n)
	}
	if _, err := fmt.Fprintf(w, "graph: %d nodes, %d edges\n", g.NodeCount(), g.EdgeCount()); err != nil {
		return err
	}
	for _, kind := range AllNodeKinds() {
		group := byKind[kind]
		if len(group) == 0 {
			continue
		}
		if _, err := fmt.Fprintf(w, "\n[%s] %d\n", kind, len(group)); err != nil {
			return err
		}
		sort.SliceStable(group, func(i, j int) bool { return group[i].ID < group[j].ID })
		for _, n := range group {
			label := n.Name
			if label == "" {
				label = "(anon)"
			}
			if _, err := fmt.Fprintf(w, "  %s  %s\n", label, n.ID); err != nil {
				return err
			}
		}
	}
	if _, err := fmt.Fprintf(w, "\n[edges]\n"); err != nil {
		return err
	}
	for _, e := range g.Edges() {
		if _, err := fmt.Fprintf(w, "  %s --%s--> %s\n", e.From, e.Kind, e.To); err != nil {
			return err
		}
	}
	return nil
}
