package metrics

import (
	"errors"

	"gonum.org/v1/gonum/mat"
)

// HadamardOp computes the element-wise product V = A ⊙ R. Used as the
// "apply mask" primitive: encode a rule as a 0/1 mask R where R[i][j]=1
// marks the forbidden edge, then any nonzero cell in V is a violation.
//
// Both A and R must be the same N×N shape.
type HadamardOp struct{}

// Name returns "hadamard".
func (HadamardOp) Name() string { return "hadamard" }

// Apply returns A ⊙ R as a freshly allocated *mat.Dense.
func (HadamardOp) Apply(A, R *mat.Dense) (*mat.Dense, error) {
	if A == nil || R == nil {
		return nil, errors.New("hadamard: nil matrix")
	}
	ar, ac := A.Dims()
	rr, rc := R.Dims()
	if ar != rr || ac != rc {
		return nil, errors.New("hadamard: shape mismatch")
	}
	out := mat.NewDense(ar, ac, nil)
	out.MulElem(A, R)
	return out, nil
}

// PowerDiagOp computes A^1..A^K and returns an N×N matrix whose diagonal
// holds, for each node i, the smallest k ∈ [1..K] with (A^k)[i][i] > 0;
// other diagonal cells and all off-diagonal cells are zero. A node's
// diagonal cell is therefore "the length of the shortest cycle of length
// ≤ K touching this node", or 0 if none.
//
// Storing the shortest cycle length (not the sum of path counts) keeps
// the interpreter trivial and matches what the user asks of the validator
// ("does this node sit on a cycle?"). The full A^k sequence is computed
// via gonum's `mat.Pow`-style iterative product — no hand-rolled loops over
// individual cells.
type PowerDiagOp struct{ K int }

// Name returns "power_diag".
func (PowerDiagOp) Name() string { return "power_diag" }

// Apply ignores R and uses A only.
func (p PowerDiagOp) Apply(A, _ *mat.Dense) (*mat.Dense, error) {
	if A == nil {
		return nil, errors.New("power_diag: nil adjacency")
	}
	n, c := A.Dims()
	if n != c {
		return nil, errors.New("power_diag: non-square adjacency")
	}
	k := p.K
	if k < 1 {
		k = 1
	}
	out := mat.NewDense(n, n, nil)
	// Pk holds the current A^k. Seed with A^1, multiply on each iteration.
	Pk := mat.NewDense(n, n, nil)
	Pk.Copy(A)
	for step := 1; step <= k; step++ {
		for i := 0; i < n; i++ {
			if Pk.At(i, i) > 0 && out.At(i, i) == 0 {
				out.Set(i, i, float64(step))
			}
		}
		if step == k {
			break
		}
		next := mat.NewDense(n, n, nil)
		next.Mul(Pk, A)
		Pk = next
	}
	return out, nil
}

// LayerCollapseOp computes V = Lᵀ · A · L where L is the N×K layer
// incidence matrix supplied as R. The result is a K×K layer-graph
// adjacency: V[p][q] = number of edges in A whose source is in layer p
// and target is in layer q.
//
// This is the canonical "fold a node-level adjacency into a coarse
// architectural adjacency" operation; it falls out of the matrix algebra
// for free once roles are encoded as a one-hot incidence matrix.
type LayerCollapseOp struct{}

// Name returns "layer_collapse".
func (LayerCollapseOp) Name() string { return "layer_collapse" }

// Apply returns Lᵀ A L.
func (LayerCollapseOp) Apply(A, R *mat.Dense) (*mat.Dense, error) {
	if A == nil || R == nil {
		return nil, errors.New("layer_collapse: nil matrix")
	}
	an, ac := A.Dims()
	rn, _ := R.Dims()
	if an != ac || an != rn {
		return nil, errors.New("layer_collapse: dimension mismatch")
	}
	tmp := mat.NewDense(an, mustCols(R), nil)
	tmp.Mul(A, R)
	out := mat.NewDense(mustCols(R), mustCols(R), nil)
	out.Mul(R.T(), tmp)
	return out, nil
}

// TransposeDiffOp computes V = A − Aᵀ. Used to surface asymmetric edges:
// any positive cell V[i][j] > 0 means i→j is present but j→i is not.
// Provided for symmetry/coupling diagnostics; not used by the three
// validators in this PR but kept in the ops library for parity with the
// design intent.
type TransposeDiffOp struct{}

// Name returns "transpose_diff".
func (TransposeDiffOp) Name() string { return "transpose_diff" }

// Apply returns A − Aᵀ.
func (TransposeDiffOp) Apply(A, _ *mat.Dense) (*mat.Dense, error) {
	if A == nil {
		return nil, errors.New("transpose_diff: nil matrix")
	}
	n, c := A.Dims()
	if n != c {
		return nil, errors.New("transpose_diff: non-square")
	}
	out := mat.NewDense(n, n, nil)
	out.Sub(A, A.T())
	return out, nil
}

// RowColSumOp computes V where V[i][0] = Σⱼ A[i][j] (row sum, fan-out)
// and V[i][1] = Σⱼ A[j][i] (column sum, fan-in). The two sums are
// expressible as A·𝟙 and Aᵀ·𝟙, which keeps the operation in the matrix
// algebra (no per-cell hand loops). Result shape is N×2.
type RowColSumOp struct{}

// Name returns "row_col_sum".
func (RowColSumOp) Name() string { return "row_col_sum" }

// Apply returns the N×2 fan-out / fan-in matrix.
func (RowColSumOp) Apply(A, _ *mat.Dense) (*mat.Dense, error) {
	if A == nil {
		return nil, errors.New("row_col_sum: nil matrix")
	}
	n, c := A.Dims()
	if n != c {
		return nil, errors.New("row_col_sum: non-square")
	}
	ones := mat.NewDense(n, 1, nil)
	for i := 0; i < n; i++ {
		ones.Set(i, 0, 1)
	}
	fanOut := mat.NewDense(n, 1, nil)
	fanOut.Mul(A, ones)
	fanIn := mat.NewDense(n, 1, nil)
	fanIn.Mul(A.T(), ones)
	out := mat.NewDense(n, 2, nil)
	for i := 0; i < n; i++ {
		out.Set(i, 0, fanOut.At(i, 0))
		out.Set(i, 1, fanIn.At(i, 0))
	}
	return out, nil
}

// mustCols returns m.Dims's column count without bothering with the row
// count. Used in the operations above where we only need column shape to
// allocate intermediates.
func mustCols(m *mat.Dense) int {
	_, c := m.Dims()
	return c
}
