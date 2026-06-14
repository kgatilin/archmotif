// Package matrixgraph is a public, graph-in matrix-algebra validation library.
// The caller hands over a directed graph — named nodes (with optional string
// attributes) and directed edges between node names — and matrixgraph builds
// the dense adjacency matrix internally and runs every validator as linear
// algebra over it.
//
// The caller never sees a matrix. There is no [][]bool and no *mat.Dense
// anywhere in the public surface: adjacency is a private detail. Callers think
// in node names; matrixgraph maps names to indices, runs the matrix algebra,
// and offers both index-based and name-based result accessors.
//
// Internally every operation is expressed as linear algebra over the dense
// adjacency matrix A (gonum *mat.Dense), never as ad-hoc map traversal.
// Booleans ride on *mat.Dense as 0/1 values with positivity thresholding,
// matching archmotif's internal matrix validators (Hadamard masks, matrix
// powers, row/column sums).
//
//	A[i][j] = 1  means a directed edge From=names[i] → To=names[j].
package matrixgraph

import (
	"fmt"

	"gonum.org/v1/gonum/mat"
)

// Node is a named graph node with optional string attributes. Name identifies
// the node; it must be unique within a graph. Attrs may be nil.
type Node struct {
	Name  string
	Attrs map[string]string
}

// Edge is a directed edge between two nodes, referenced by name. From and To
// must both name a node passed to New (self-edges, From==To, are allowed and
// significant for cycle/SCC detection).
type Edge struct {
	From string
	To   string
}

// Graph is an immutable directed graph. The caller builds it from nodes and
// edges; matrixgraph holds the dense 0/1 adjacency internally and never exposes
// it.
//
// Node identity is positional inside the graph: index i in [0,N) corresponds to
// the i-th node passed to New. Index-returning operations report these indices;
// callers map back to names via Name/Names, or use the name-based convenience
// methods.
type Graph struct {
	names []string
	index map[string]int // node name → index
	attrs []map[string]string
	a     *mat.Dense // N×N 0/1 adjacency; A[i][j]=1 means i→j.
}

// New builds a Graph from a set of named nodes and directed edges.
//
//   - nodes: the node set. Ordering determines node indices and N. Node names
//     must be unique; a duplicate name is an error.
//   - edges: directed edges referencing node names. Every From and To must name
//     a node in nodes, or New returns an error. Self-edges and duplicate edges
//     are accepted (a duplicate edge is idempotent in a 0/1 adjacency).
//
// New copies all input so the caller may reuse its slices and maps freely. The
// adjacency matrix is built internally and is never exposed.
func New(nodes []Node, edges []Edge) (*Graph, error) {
	n := len(nodes)
	names := make([]string, n)
	index := make(map[string]int, n)
	attrs := make([]map[string]string, n)
	for i, nd := range nodes {
		if nd.Name == "" {
			return nil, fmt.Errorf("matrixgraph: node %d has an empty name", i)
		}
		if _, dup := index[nd.Name]; dup {
			return nil, fmt.Errorf("matrixgraph: duplicate node name %q", nd.Name)
		}
		names[i] = nd.Name
		index[nd.Name] = i
		if nd.Attrs != nil {
			cp := make(map[string]string, len(nd.Attrs))
			for k, v := range nd.Attrs {
				cp[k] = v
			}
			attrs[i] = cp
		}
	}

	a := mat.NewDense(maxDim(n), maxDim(n), nil)
	for _, e := range edges {
		from, ok := index[e.From]
		if !ok {
			return nil, fmt.Errorf("matrixgraph: edge references unknown source node %q", e.From)
		}
		to, ok := index[e.To]
		if !ok {
			return nil, fmt.Errorf("matrixgraph: edge references unknown target node %q", e.To)
		}
		a.Set(from, to, 1)
	}

	return &Graph{names: names, index: index, attrs: attrs, a: a}, nil
}

// maxDim guards gonum's NewDense, which panics on zero dimensions. A 0-node
// graph is represented by a 1×1 dense that is never indexed (N==0 callers
// short-circuit via N()).
func maxDim(n int) int {
	if n == 0 {
		return 1
	}
	return n
}

// N returns the number of nodes.
func (g *Graph) N() int { return len(g.names) }

// Names returns a copy of the node names in index order.
func (g *Graph) Names() []string { return append([]string(nil), g.names...) }

// Name returns the name of node i, or "" if i is out of range.
func (g *Graph) Name(i int) string {
	if i < 0 || i >= len(g.names) {
		return ""
	}
	return g.names[i]
}

// Index returns the index of the node with the given name and whether it
// exists.
func (g *Graph) Index(name string) (int, bool) {
	i, ok := g.index[name]
	return i, ok
}

// indicesToNames maps a slice of node indices to their names, dropping any
// out-of-range index. Always returns a non-nil slice for a non-nil input.
func (g *Graph) indicesToNames(idx []int) []string {
	out := make([]string, 0, len(idx))
	for _, i := range idx {
		if i >= 0 && i < len(g.names) {
			out = append(out, g.names[i])
		}
	}
	return out
}

// Attrs returns the attribute map for node i, or an empty (non-nil) map when
// the node had none. The returned map is a copy; mutating it does not affect
// the graph.
func (g *Graph) Attrs(i int) map[string]string {
	if i < 0 || i >= len(g.attrs) || g.attrs[i] == nil {
		return map[string]string{}
	}
	cp := make(map[string]string, len(g.attrs[i]))
	for k, v := range g.attrs[i] {
		cp[k] = v
	}
	return cp
}
