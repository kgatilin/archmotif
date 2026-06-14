package graphval

import (
	"io"

	"github.com/kgatilin/archmotif/internal/shape"
)

// NewFromGraphML builds a Graph from a standard GraphML stream, so "a graph"
// can be the standard interchange format as well as in-process nodes+edges. It
// reuses archmotif's GraphML reader (the same one Gephi snapshots flow through)
// and maps every GraphML node to a Node (using the stable domain id as the
// name and the node's data attributes as Attrs) and every GraphML edge to an
// Edge (source→target by stable id). The adjacency is then built internally
// exactly as in New; the caller still never sees a matrix.
//
// Edges in well-formed GraphML always reference known nodes (the reader rejects
// dangling references), so this is effectively New over the parsed graph.
func NewFromGraphML(r io.Reader) (*Graph, error) {
	sg, err := shape.ReadGraphML(r)
	if err != nil {
		return nil, err
	}
	nodes := make([]Node, 0, len(sg.Nodes))
	for id, n := range sg.Nodes {
		nodes = append(nodes, Node{Name: id, Attrs: n.Attrs})
	}
	edges := make([]Edge, 0, len(sg.Edges))
	for _, e := range sg.Edges {
		edges = append(edges, Edge{From: e.Source, To: e.Target})
	}
	return New(nodes, edges)
}
