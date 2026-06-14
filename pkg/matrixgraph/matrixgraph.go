// Package matrixgraph is a public, graph-agnostic matrix-algebra validation
// library. It operates purely on a caller-built adjacency matrix plus
// optional per-node string attributes and knows nothing about Go source,
// events, kinds, or any particular ingestion pipeline.
//
// Every operation is expressed as linear algebra over the dense adjacency
// matrix A (gonum *mat.Dense), never as ad-hoc map traversal. Booleans ride
// on *mat.Dense as 0/1 values with positivity thresholding, matching the
// conventions of archmotif's internal matrix validators (Hadamard masks,
// matrix powers, row/column sums).
//
// Construction is the caller's job: build names, a 0/1 adjacency, and (if
// desired) attributes, then call New. The library does not parse anything.
//
//	A[i][j] = 1  means a directed edge i → j.
package matrixgraph

import (
	"errors"

	"gonum.org/v1/gonum/mat"
)

// Graph is an immutable directed graph backed by a dense adjacency matrix.
//
// Node identity is positional: index i in [0,N) corresponds to Names[i] and
// Attrs[i]. All operations return node indices; callers map back to names or
// attributes via the exported slices.
type Graph struct {
	names []string
	attrs []map[string]string
	a     *mat.Dense // N×N 0/1 adjacency; A[i][j]=1 means i→j.
}

// New builds a Graph from caller-supplied data.
//
//   - names: node labels, length N. Determines node ordering and N.
//   - adj:   N×N boolean adjacency; adj[i][j]==true means a directed edge
//     i→j. Self-edges (adj[i][i]) are preserved — they matter for SCC and
//     cycle detection.
//   - attrs: optional per-node attribute maps. If non-nil it must have
//     length N. Pass nil to omit attributes; SCCsMissingAttr then treats
//     every node as having an empty attribute map.
//
// New copies all input so the caller may reuse its slices freely. It returns
// an error on a ragged adjacency or an attrs/names length mismatch.
func New(names []string, adj [][]bool, attrs []map[string]string) (*Graph, error) {
	n := len(names)
	if len(adj) != n {
		return nil, errors.New("matrixgraph: adjacency row count must equal len(names)")
	}
	if attrs != nil && len(attrs) != n {
		return nil, errors.New("matrixgraph: attrs length must equal len(names) or be nil")
	}
	a := mat.NewDense(maxDim(n), maxDim(n), nil)
	for i := 0; i < n; i++ {
		if len(adj[i]) != n {
			return nil, errors.New("matrixgraph: adjacency must be square (N×N)")
		}
		for j := 0; j < n; j++ {
			if adj[i][j] {
				a.Set(i, j, 1)
			}
		}
	}
	g := &Graph{
		names: append([]string(nil), names...),
		a:     a,
	}
	if attrs != nil {
		g.attrs = make([]map[string]string, n)
		for i, m := range attrs {
			cp := make(map[string]string, len(m))
			for k, v := range m {
				cp[k] = v
			}
			g.attrs[i] = cp
		}
	}
	return g, nil
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

// Names returns a copy of the node labels in index order.
func (g *Graph) Names() []string { return append([]string(nil), g.names...) }

// Attrs returns the attribute map for node i, or an empty (non-nil) map when
// the graph was built without attributes or the node had none. The returned
// map is a copy; mutating it does not affect the graph.
func (g *Graph) Attrs(i int) map[string]string {
	if g.attrs == nil || i < 0 || i >= len(g.attrs) || g.attrs[i] == nil {
		return map[string]string{}
	}
	cp := make(map[string]string, len(g.attrs[i]))
	for k, v := range g.attrs[i] {
		cp[k] = v
	}
	return cp
}

// Adjacency returns a fresh copy of the N×N 0/1 adjacency matrix. The copy is
// owned by the caller. For a zero-node graph the result is a 0×0 dense.
func (g *Graph) Adjacency() *mat.Dense {
	n := g.N()
	if n == 0 {
		// gonum's NewDense panics on zero dimensions; a zero-value Dense
		// reports 0×0 dims, which is the right empty representation.
		return &mat.Dense{}
	}
	out := mat.NewDense(n, n, nil)
	out.Copy(g.a.Slice(0, n, 0, n))
	return out
}
