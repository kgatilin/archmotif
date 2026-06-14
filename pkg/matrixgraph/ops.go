package matrixgraph

import (
	"fmt"
	"sort"

	"gonum.org/v1/gonum/mat"
)

// closure returns the boolean transitive closure A* of the adjacency, as a
// 0/1 *mat.Dense. A*[i][j] = 1 iff there is a directed path of length ≥ 1
// from i to j (j is reachable from i). The reflexive case A*[i][i] is set
// only when i actually sits on a cycle (a path i→…→i of length ≥ 1); it is
// NOT forced to 1, so the closure distinguishes "reaches itself" from
// "trivially is itself".
//
// Computed by boolean matrix powers to a fixpoint: starting from R = A, we
// repeatedly fold in R·A (boolean product, i.e. positivity-thresholded
// gonum Mul) until no new reachable pair appears. The fixpoint is reached in
// at most N iterations, matching (I ∨ A)^(N-1) reachability without forcing
// the reflexive diagonal.
//
// It is unexported: the *mat.Dense closure is a private detail. ReachableFrom
// and SCCs expose its results in node-index / node-name terms instead.
func (g *Graph) closure() *mat.Dense {
	n := g.N()
	if n == 0 {
		// gonum's NewDense panics on zero dimensions; a zero-value Dense
		// reports 0×0 dims, the right empty representation.
		return &mat.Dense{}
	}
	a := g.a.Slice(0, n, 0, n)

	// reach holds the running boolean closure; seed it with A (paths of
	// length 1). frontier holds the newest power A^k; we keep multiplying
	// by A and OR-ing into reach until reach stops growing.
	reach := mat.NewDense(n, n, nil)
	reach.Copy(a)

	frontier := mat.NewDense(n, n, nil)
	frontier.Copy(a)

	for step := 1; step < n; step++ {
		next := mat.NewDense(n, n, nil)
		next.Mul(frontier, a) // A^(k) · A = A^(k+1)
		boolize(next)

		changed := orInto(reach, next)
		if !changed {
			break // fixpoint: no new reachable pairs.
		}
		frontier = next
	}
	return reach
}

// ReachableFrom returns a length-N boolean slice marking every node reachable
// from at least one of the given root indices. A root is considered reachable
// from itself (the root's own bit is always set), in addition to whatever its
// closure row marks. Out-of-range roots are ignored. The result is the union
// (boolean OR) of the closure rows over the root set.
func (g *Graph) ReachableFrom(roots []int) []bool {
	n := g.N()
	out := make([]bool, n)
	if n == 0 {
		return out
	}
	star := g.closure()
	for _, r := range roots {
		if r < 0 || r >= n {
			continue
		}
		out[r] = true // a root reaches itself.
		for j := 0; j < n; j++ {
			if star.At(r, j) > 0 {
				out[j] = true
			}
		}
	}
	return out
}

// ReachableFromNames is the name-based companion to ReachableFrom: given root
// node names, it returns the names of every reachable node (each root reaches
// itself), sorted ascending. Unknown root names are ignored. This is the
// accessor reflex uses, since reflex works in node names, not indices.
func (g *Graph) ReachableFromNames(roots []string) []string {
	idx := make([]int, 0, len(roots))
	for _, name := range roots {
		if i, ok := g.index[name]; ok {
			idx = append(idx, i)
		}
	}
	mask := g.ReachableFrom(idx)
	out := make([]string, 0)
	for i, ok := range mask {
		if ok {
			out = append(out, g.names[i])
		}
	}
	sort.Strings(out)
	return out
}

// SCCs returns the strongly-connected components of the graph as groups of
// node indices, derived purely from the transitive closure: i and j share an
// SCC iff A*[i][j] && A*[j][i] (mutual reachability). Singletons are included.
//
// Each returned component is sorted ascending; components are ordered by their
// smallest member. Use NonTrivialSCCs (or IsNonTrivial) to filter for the
// interesting ones — a component of size > 1, or a singleton that self-loops.
func (g *Graph) SCCs() [][]int {
	n := g.N()
	if n == 0 {
		return [][]int{}
	}
	star := g.closure()
	assigned := make([]bool, n)
	var comps [][]int
	for i := 0; i < n; i++ {
		if assigned[i] {
			continue
		}
		comp := []int{i}
		assigned[i] = true
		for j := i + 1; j < n; j++ {
			if assigned[j] {
				continue
			}
			if star.At(i, j) > 0 && star.At(j, i) > 0 {
				comp = append(comp, j)
				assigned[j] = true
			}
		}
		sort.Ints(comp)
		comps = append(comps, comp)
	}
	sort.Slice(comps, func(a, b int) bool { return comps[a][0] < comps[b][0] })
	return comps
}

// IsNonTrivial reports whether a component is a genuine cycle: more than one
// node, or a single node carrying a self-loop (A[i][i]=1). A lone node with no
// self-edge is trivial.
func (g *Graph) IsNonTrivial(comp []int) bool {
	if len(comp) > 1 {
		return true
	}
	if len(comp) == 1 {
		i := comp[0]
		if i >= 0 && i < g.N() && g.a.At(i, i) > 0 {
			return true
		}
	}
	return false
}

// NonTrivialSCCs returns only the SCCs that IsNonTrivial accepts.
func (g *Graph) NonTrivialSCCs() [][]int {
	var out [][]int
	for _, c := range g.SCCs() {
		if g.IsNonTrivial(c) {
			out = append(out, c)
		}
	}
	return out
}

// SCCsMissingAttr returns the non-trivial SCCs that contain NO node satisfying
// pred. This is the generic attribute-driven guard: the caller supplies a
// predicate over a node's attribute map (e.g. "is this node a designated
// boundary/owner?"), and the result is every real cycle that lacks such a
// node — a cycle with nothing in it to break or own the loop.
//
// It mirrors the spirit of the internal layer_mask Hadamard guard, but is
// fully caller-driven: the predicate is the mask. A nil predicate is treated
// as "no node ever satisfies it", so every non-trivial SCC is returned.
func (g *Graph) SCCsMissingAttr(pred func(attrs map[string]string) bool) [][]int {
	var out [][]int
	for _, comp := range g.NonTrivialSCCs() {
		satisfied := false
		if pred != nil {
			for _, i := range comp {
				if pred(g.attrAt(i)) {
					satisfied = true
					break
				}
			}
		}
		if !satisfied {
			out = append(out, comp)
		}
	}
	return out
}

// NamesOf maps a group of node indices (e.g. one SCC, or Sinks/Sources/
// CycleNodes output) to their node names, preserving order and dropping any
// out-of-range index. It is the bridge for callers like reflex that reason in
// names: g.NamesOf(g.CycleNodes(3)), g.NamesOf(g.Sinks()), etc.
func (g *Graph) NamesOf(idx []int) []string { return g.indicesToNames(idx) }

// SCCsAsNames returns the same components as SCCs but with node names instead
// of indices. Each component preserves SCCs' ascending-by-index order.
func (g *Graph) SCCsAsNames() [][]string { return g.componentsAsNames(g.SCCs()) }

// NonTrivialSCCsAsNames is the name-based companion to NonTrivialSCCs.
func (g *Graph) NonTrivialSCCsAsNames() [][]string {
	return g.componentsAsNames(g.NonTrivialSCCs())
}

// SCCsMissingAttrAsNames is the name-based companion to SCCsMissingAttr.
func (g *Graph) SCCsMissingAttrAsNames(pred func(attrs map[string]string) bool) [][]string {
	return g.componentsAsNames(g.SCCsMissingAttr(pred))
}

// componentsAsNames maps each component of indices to a component of names.
func (g *Graph) componentsAsNames(comps [][]int) [][]string {
	out := make([][]string, 0, len(comps))
	for _, c := range comps {
		out = append(out, g.indicesToNames(c))
	}
	return out
}

// attrAt returns the (possibly empty) attribute map for node i without
// copying — internal use only, never handed to callers.
func (g *Graph) attrAt(i int) map[string]string {
	if g.attrs == nil || i < 0 || i >= len(g.attrs) || g.attrs[i] == nil {
		return map[string]string{}
	}
	return g.attrs[i]
}

// FanOut returns, for each node i, its out-degree Σⱼ A[i][j]. Computed as the
// matrix-vector product A·𝟙, porting the internal RowColSumOp's fan-out leg.
func (g *Graph) FanOut() []int {
	n := g.N()
	out := make([]int, n)
	if n == 0 {
		return out
	}
	ones := onesVec(n)
	res := mat.NewDense(n, 1, nil)
	res.Mul(g.a.Slice(0, n, 0, n), ones)
	for i := 0; i < n; i++ {
		out[i] = int(res.At(i, 0))
	}
	return out
}

// FanIn returns, for each node i, its in-degree Σⱼ A[j][i]. Computed as the
// matrix-vector product Aᵀ·𝟙, porting the internal RowColSumOp's fan-in leg.
func (g *Graph) FanIn() []int {
	n := g.N()
	out := make([]int, n)
	if n == 0 {
		return out
	}
	ones := onesVec(n)
	res := mat.NewDense(n, 1, nil)
	res.Mul(g.a.Slice(0, n, 0, n).T(), ones)
	for i := 0; i < n; i++ {
		out[i] = int(res.At(i, 0))
	}
	return out
}

// Sinks returns the indices of nodes with zero fan-out (no outgoing edges).
func (g *Graph) Sinks() []int {
	var out []int
	for i, d := range g.FanOut() {
		if d == 0 {
			out = append(out, i)
		}
	}
	return out
}

// Sources returns the indices of nodes with zero fan-in (no incoming edges).
func (g *Graph) Sources() []int {
	var out []int
	for i, d := range g.FanIn() {
		if d == 0 {
			out = append(out, i)
		}
	}
	return out
}

// CycleNodes returns the indices of nodes that sit on a directed cycle of
// length ≤ k, computed by inspecting the diagonal of A^1..A^k. A node i is
// included iff (A^m)[i][i] > 0 for some m ∈ [1..k] — i.e. there is a closed
// walk of length ≤ k through i. Ports the internal PowerDiagOp.
//
// k < 1 is clamped to 1.
func (g *Graph) CycleNodes(k int) []int {
	n := g.N()
	if n == 0 {
		return nil
	}
	if k < 1 {
		k = 1
	}
	a := g.a.Slice(0, n, 0, n)
	pk := mat.NewDense(n, n, nil)
	pk.Copy(a) // A^1
	onCycle := make([]bool, n)
	for step := 1; step <= k; step++ {
		for i := 0; i < n; i++ {
			if pk.At(i, i) > 0 {
				onCycle[i] = true
			}
		}
		if step == k {
			break
		}
		next := mat.NewDense(n, n, nil)
		next.Mul(pk, a)
		pk = next
	}
	var out []int
	for i, c := range onCycle {
		if c {
			out = append(out, i)
		}
	}
	return out
}

// DeadItems is the graph-in form of the "dead-column" / column-sum-zero
// detector. Given a bipartite graph — a set of producer node names, a set of
// item node names, and producer→item edges — it returns the names of items that
// no producer touches (zero incoming edges), sorted ascending.
//
// The caller speaks only in names: matrixgraph builds the rectangular 0/1
// incidence matrix internally and computes the column-sum vector 𝟙ᵀ·M,
// thresholded at zero. Item names must be unique; an empty item set returns no
// items. An edge whose From is not a known producer or whose To is not a known
// item is rejected.
func DeadItems(producers, items []string, edges []Edge) ([]string, error) {
	prodIdx := make(map[string]int, len(producers))
	for i, p := range producers {
		if p == "" {
			return nil, fmt.Errorf("matrixgraph: producer %d has an empty name", i)
		}
		if _, dup := prodIdx[p]; dup {
			return nil, fmt.Errorf("matrixgraph: duplicate producer name %q", p)
		}
		prodIdx[p] = i
	}
	itemIdx := make(map[string]int, len(items))
	for j, it := range items {
		if it == "" {
			return nil, fmt.Errorf("matrixgraph: item %d has an empty name", j)
		}
		if _, dup := itemIdx[it]; dup {
			return nil, fmt.Errorf("matrixgraph: duplicate item name %q", it)
		}
		itemIdx[it] = j
	}
	rows := len(producers)
	cols := len(items)
	if cols == 0 {
		return nil, nil
	}

	// Incidence M[p][i] = 1 iff producer p touches item i.
	M := mat.NewDense(maxDim(rows), maxDim(cols), nil)
	for _, e := range edges {
		p, ok := prodIdx[e.From]
		if !ok {
			return nil, fmt.Errorf("matrixgraph: edge references unknown producer %q", e.From)
		}
		it, ok := itemIdx[e.To]
		if !ok {
			return nil, fmt.Errorf("matrixgraph: edge references unknown item %q", e.To)
		}
		M.Set(p, it, 1)
	}
	if rows == 0 {
		// No producers: every item is dead.
		out := append([]string(nil), items...)
		sort.Strings(out)
		return out, nil
	}

	// colSum = 𝟙ᵀ · M  (1×cols row vector of per-column totals).
	onesRow := mat.NewDense(1, rows, nil)
	for i := 0; i < rows; i++ {
		onesRow.Set(0, i, 1)
	}
	colSum := mat.NewDense(1, cols, nil)
	colSum.Mul(onesRow, M.Slice(0, rows, 0, cols))
	var out []string
	for j := 0; j < cols; j++ {
		if colSum.At(0, j) == 0 {
			out = append(out, items[j])
		}
	}
	sort.Strings(out)
	return out, nil
}

// onesVec returns an n×1 column of ones.
func onesVec(n int) *mat.Dense {
	v := mat.NewDense(n, 1, nil)
	for i := 0; i < n; i++ {
		v.Set(i, 0, 1)
	}
	return v
}

// boolize thresholds every cell of m to 0/1 (positive → 1). Keeps integer
// path counts from blowing up across successive boolean powers.
func boolize(m *mat.Dense) {
	r, c := m.Dims()
	for i := 0; i < r; i++ {
		for j := 0; j < c; j++ {
			if m.At(i, j) > 0 {
				m.Set(i, j, 1)
			} else {
				m.Set(i, j, 0)
			}
		}
	}
}

// orInto folds src into dst as a boolean OR and reports whether dst gained any
// new 1. Both must share dimensions.
func orInto(dst, src *mat.Dense) bool {
	r, c := dst.Dims()
	changed := false
	for i := 0; i < r; i++ {
		for j := 0; j < c; j++ {
			if src.At(i, j) > 0 && dst.At(i, j) == 0 {
				dst.Set(i, j, 1)
				changed = true
			}
		}
	}
	return changed
}
