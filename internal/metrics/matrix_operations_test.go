package metrics

import (
	"testing"

	"gonum.org/v1/gonum/mat"
)

// Internal tests for the matrix operations — kept in-package so we can
// reach the unexported types directly without exporting them.

func TestHadamardOp_ElementWise(t *testing.T) {
	A := mat.NewDense(2, 2, []float64{1, 1, 1, 1})
	R := mat.NewDense(2, 2, []float64{0, 1, 1, 0})
	V, err := (HadamardOp{}).Apply(A, R)
	if err != nil {
		t.Fatal(err)
	}
	want := []float64{0, 1, 1, 0}
	for i, v := range want {
		if V.At(i/2, i%2) != v {
			t.Fatalf("Hadamard[%d/%d] = %v, want %v", i/2, i%2, V.At(i/2, i%2), v)
		}
	}
}

func TestPowerDiagOp_FourCycleDetectsLength4(t *testing.T) {
	// Adjacency of a directed 4-cycle 0→1→2→3→0.
	A := mat.NewDense(4, 4, []float64{
		0, 1, 0, 0,
		0, 0, 1, 0,
		0, 0, 0, 1,
		1, 0, 0, 0,
	})
	V, err := (PowerDiagOp{K: 5}).Apply(A, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if V.At(i, i) != 4 {
			t.Fatalf("diag[%d] = %v, want 4", i, V.At(i, i))
		}
	}
}

func TestPowerDiagOp_SelfLoopDetectsLength1(t *testing.T) {
	A := mat.NewDense(2, 2, []float64{
		1, 0, // self loop on node 0
		0, 0,
	})
	V, _ := (PowerDiagOp{K: 3}).Apply(A, nil)
	if V.At(0, 0) != 1 {
		t.Fatalf("self-loop diag = %v, want 1", V.At(0, 0))
	}
	if V.At(1, 1) != 0 {
		t.Fatalf("isolated diag = %v, want 0", V.At(1, 1))
	}
}

func TestLayerCollapseOp_FoldsIntoLayerGraph(t *testing.T) {
	// 4 nodes, 2 layers via incidence L: nodes 0,1 in layer 0; nodes 2,3 in layer 1.
	// Edges: 0→2, 1→3, 2→0 (so layer-graph should be: layer0→layer1 = 2, layer1→layer0 = 1).
	A := mat.NewDense(4, 4, []float64{
		0, 0, 1, 0,
		0, 0, 0, 1,
		1, 0, 0, 0,
		0, 0, 0, 0,
	})
	L := mat.NewDense(4, 2, []float64{
		1, 0,
		1, 0,
		0, 1,
		0, 1,
	})
	V, err := (LayerCollapseOp{}).Apply(A, L)
	if err != nil {
		t.Fatal(err)
	}
	r, c := V.Dims()
	if r != 2 || c != 2 {
		t.Fatalf("collapsed shape = %dx%d, want 2x2", r, c)
	}
	if V.At(0, 1) != 2 {
		t.Fatalf("layer0→layer1 = %v, want 2", V.At(0, 1))
	}
	if V.At(1, 0) != 1 {
		t.Fatalf("layer1→layer0 = %v, want 1", V.At(1, 0))
	}
	if V.At(0, 0) != 0 || V.At(1, 1) != 0 {
		t.Fatalf("diagonal nonzero: %v %v", V.At(0, 0), V.At(1, 1))
	}
}

func TestTransposeDiffOp_AsymmetricEdge(t *testing.T) {
	// A has 0→1 but not 1→0; result should be +1 at (0,1), -1 at (1,0).
	A := mat.NewDense(2, 2, []float64{
		0, 1,
		0, 0,
	})
	V, err := (TransposeDiffOp{}).Apply(A, nil)
	if err != nil {
		t.Fatal(err)
	}
	if V.At(0, 1) != 1 {
		t.Fatalf("V[0][1] = %v, want 1", V.At(0, 1))
	}
	if V.At(1, 0) != -1 {
		t.Fatalf("V[1][0] = %v, want -1", V.At(1, 0))
	}
}

func TestRowColSumOp_FanOutFanIn(t *testing.T) {
	// 3-node graph: 0→1, 0→2, 1→2.
	A := mat.NewDense(3, 3, []float64{
		0, 1, 1,
		0, 0, 1,
		0, 0, 0,
	})
	V, err := (RowColSumOp{}).Apply(A, nil)
	if err != nil {
		t.Fatal(err)
	}
	r, c := V.Dims()
	if r != 3 || c != 2 {
		t.Fatalf("shape = %dx%d, want 3x2", r, c)
	}
	want := [][2]float64{
		{2, 0}, // node 0: out=2, in=0
		{1, 1}, // node 1: out=1, in=1
		{0, 2}, // node 2: out=0, in=2
	}
	for i, w := range want {
		if V.At(i, 0) != w[0] || V.At(i, 1) != w[1] {
			t.Fatalf("node %d: (out,in)=(%v,%v), want (%v,%v)", i, V.At(i, 0), V.At(i, 1), w[0], w[1])
		}
	}
}
