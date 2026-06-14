package matrixgraph

import (
	"reflect"
	"testing"
)

// edges builds an N×N boolean adjacency from a list of (from,to) index pairs.
func edges(n int, pairs ...[2]int) [][]bool {
	a := make([][]bool, n)
	for i := range a {
		a[i] = make([]bool, n)
	}
	for _, p := range pairs {
		a[p[0]][p[1]] = true
	}
	return a
}

func mustGraph(t *testing.T, names []string, adj [][]bool, attrs []map[string]string) *Graph {
	t.Helper()
	g, err := New(names, adj, attrs)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return g
}

func TestNewValidation(t *testing.T) {
	if _, err := New([]string{"a", "b"}, edges(2), nil); err != nil {
		t.Fatalf("valid graph rejected: %v", err)
	}
	// ragged row
	bad := [][]bool{{false, false}, {false}}
	if _, err := New([]string{"a", "b"}, bad, nil); err == nil {
		t.Fatal("expected error on ragged adjacency")
	}
	// row count mismatch
	if _, err := New([]string{"a", "b"}, edges(3), nil); err == nil {
		t.Fatal("expected error on row count mismatch")
	}
	// attrs length mismatch
	if _, err := New([]string{"a", "b"}, edges(2), []map[string]string{{}}); err == nil {
		t.Fatal("expected error on attrs length mismatch")
	}
}

func TestClosureDAG(t *testing.T) {
	// 0 -> 1 -> 2, 0 -> 3. No cycles.
	g := mustGraph(t, []string{"0", "1", "2", "3"}, edges(4, [2]int{0, 1}, [2]int{1, 2}, [2]int{0, 3}), nil)
	star := g.Closure()
	want := map[[2]int]bool{
		{0, 1}: true, {0, 2}: true, {0, 3}: true,
		{1, 2}: true,
	}
	for i := 0; i < 4; i++ {
		for j := 0; j < 4; j++ {
			got := star.At(i, j) > 0
			if got != want[[2]int{i, j}] {
				t.Errorf("closure[%d][%d]=%v want %v", i, j, got, want[[2]int{i, j}])
			}
		}
	}
	// No reflexive bits on an acyclic graph.
	for i := 0; i < 4; i++ {
		if star.At(i, i) > 0 {
			t.Errorf("acyclic node %d marked reflexive in closure", i)
		}
	}
}

func TestClosureCyclic(t *testing.T) {
	// 0 -> 1 -> 2 -> 0 (3-cycle) ; 2 -> 3 dangling.
	g := mustGraph(t, []string{"0", "1", "2", "3"},
		edges(4, [2]int{0, 1}, [2]int{1, 2}, [2]int{2, 0}, [2]int{2, 3}), nil)
	star := g.Closure()
	// Every cycle member reaches every other and itself.
	for _, i := range []int{0, 1, 2} {
		for _, j := range []int{0, 1, 2} {
			if star.At(i, j) == 0 {
				t.Errorf("expected %d reaches %d in cycle", i, j)
			}
		}
		if star.At(i, 3) == 0 {
			t.Errorf("expected %d reaches dangling 3", i)
		}
	}
	// 3 is a sink: reaches nothing.
	for j := 0; j < 4; j++ {
		if star.At(3, j) > 0 {
			t.Errorf("sink 3 should reach nothing, got reach to %d", j)
		}
	}
}

func TestReachableFrom(t *testing.T) {
	// 0 -> 1 -> 2 ; 3 -> 4 (separate component).
	g := mustGraph(t, []string{"0", "1", "2", "3", "4"},
		edges(5, [2]int{0, 1}, [2]int{1, 2}, [2]int{3, 4}), nil)
	got := g.ReachableFrom([]int{0})
	want := []bool{true, true, true, false, false}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ReachableFrom([0]) = %v want %v", got, want)
	}
	// Union of two roots, including out-of-range ignored.
	got = g.ReachableFrom([]int{0, 3, 99})
	want = []bool{true, true, true, true, true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ReachableFrom([0,3]) = %v want %v", got, want)
	}
}

func TestSCCs3CycleAndSelfLoop(t *testing.T) {
	// 0,1,2 form a 3-cycle. 3 has a self-loop. 4 is isolated.
	g := mustGraph(t, []string{"0", "1", "2", "3", "4"},
		edges(5, [2]int{0, 1}, [2]int{1, 2}, [2]int{2, 0}, [2]int{3, 3}), nil)
	sccs := g.SCCs()
	want := [][]int{{0, 1, 2}, {3}, {4}}
	if !reflect.DeepEqual(sccs, want) {
		t.Fatalf("SCCs = %v want %v", sccs, want)
	}
	nt := g.NonTrivialSCCs()
	wantNT := [][]int{{0, 1, 2}, {3}} // 3 self-loops; 4 is trivial.
	if !reflect.DeepEqual(nt, wantNT) {
		t.Fatalf("NonTrivialSCCs = %v want %v", nt, wantNT)
	}
}

func TestSCCsMissingAttr(t *testing.T) {
	// Two 3-cycles. First (0,1,2) has a node with guard=owner; second
	// (3,4,5) has none. SCCsMissingAttr should return only the second.
	attrs := []map[string]string{
		{"guard": "owner"}, {}, {},
		{}, {}, {},
	}
	g := mustGraph(t, []string{"0", "1", "2", "3", "4", "5"},
		edges(6,
			[2]int{0, 1}, [2]int{1, 2}, [2]int{2, 0},
			[2]int{3, 4}, [2]int{4, 5}, [2]int{5, 3},
		), attrs)
	pred := func(a map[string]string) bool { return a["guard"] == "owner" }
	got := g.SCCsMissingAttr(pred)
	want := [][]int{{3, 4, 5}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SCCsMissingAttr = %v want %v", got, want)
	}

	// If every cycle has a qualifying node, none are returned.
	attrs2 := []map[string]string{
		{"guard": "owner"}, {}, {},
		{}, {"guard": "owner"}, {},
	}
	g2 := mustGraph(t, g.Names(),
		edges(6,
			[2]int{0, 1}, [2]int{1, 2}, [2]int{2, 0},
			[2]int{3, 4}, [2]int{4, 5}, [2]int{5, 3},
		), attrs2)
	if got := g2.SCCsMissingAttr(pred); len(got) != 0 {
		t.Fatalf("expected no missing-attr SCCs, got %v", got)
	}

	// Nil predicate => every non-trivial SCC is "missing".
	if got := g.SCCsMissingAttr(nil); len(got) != 2 {
		t.Fatalf("nil pred should return all non-trivial SCCs, got %v", got)
	}
}

func TestFanInOutSinksSources(t *testing.T) {
	// 0 -> 1, 0 -> 2, 1 -> 2. 3 isolated.
	g := mustGraph(t, []string{"0", "1", "2", "3"},
		edges(4, [2]int{0, 1}, [2]int{0, 2}, [2]int{1, 2}), nil)
	if got, want := g.FanOut(), []int{2, 1, 0, 0}; !reflect.DeepEqual(got, want) {
		t.Errorf("FanOut = %v want %v", got, want)
	}
	if got, want := g.FanIn(), []int{0, 1, 2, 0}; !reflect.DeepEqual(got, want) {
		t.Errorf("FanIn = %v want %v", got, want)
	}
	if got, want := g.Sinks(), []int{2, 3}; !reflect.DeepEqual(got, want) {
		t.Errorf("Sinks = %v want %v", got, want)
	}
	if got, want := g.Sources(), []int{0, 3}; !reflect.DeepEqual(got, want) {
		t.Errorf("Sources = %v want %v", got, want)
	}
}

func TestCycleNodes(t *testing.T) {
	// 3-cycle 0->1->2->0 plus self-loop on 4, dangling 3.
	g := mustGraph(t, []string{"0", "1", "2", "3", "4"},
		edges(5, [2]int{0, 1}, [2]int{1, 2}, [2]int{2, 0}, [2]int{2, 3}, [2]int{4, 4}), nil)
	// k=1 finds only the self-loop.
	if got, want := g.CycleNodes(1), []int{4}; !reflect.DeepEqual(got, want) {
		t.Errorf("CycleNodes(1) = %v want %v", got, want)
	}
	// k=3 finds the 3-cycle plus the self-loop; 3 is dangling, never on a cycle.
	if got, want := g.CycleNodes(3), []int{0, 1, 2, 4}; !reflect.DeepEqual(got, want) {
		t.Errorf("CycleNodes(3) = %v want %v", got, want)
	}
}

func TestDeadColumns(t *testing.T) {
	// 3 producers, 4 items. Column 2 is consumed by nobody.
	m := [][]bool{
		{true, false, false, true},
		{false, true, false, false},
		{true, true, false, true},
	}
	got, err := DeadColumns(m)
	if err != nil {
		t.Fatalf("DeadColumns: %v", err)
	}
	if want := []int{2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("DeadColumns = %v want %v", got, want)
	}

	// Ragged matrix errors.
	if _, err := DeadColumns([][]bool{{true, false}, {true}}); err == nil {
		t.Fatal("expected error on ragged incidence")
	}

	// Empty cases.
	if got, _ := DeadColumns(nil); got != nil {
		t.Errorf("DeadColumns(nil) = %v want nil", got)
	}
}

func TestEmptyGraph(t *testing.T) {
	g := mustGraph(t, nil, [][]bool{}, nil)
	if g.N() != 0 {
		t.Fatalf("N = %d want 0", g.N())
	}
	if r, c := g.Closure().Dims(); r != 0 || c != 0 {
		t.Errorf("empty closure dims = %dx%d want 0x0", r, c)
	}
	if got := g.SCCs(); len(got) != 0 {
		t.Errorf("empty SCCs = %v", got)
	}
	if got := g.ReachableFrom([]int{0}); len(got) != 0 {
		t.Errorf("empty ReachableFrom = %v", got)
	}
	if got := g.CycleNodes(3); got != nil {
		t.Errorf("empty CycleNodes = %v", got)
	}
}

func TestAttrsAndAdjacencyCopy(t *testing.T) {
	attrs := []map[string]string{{"k": "v"}, nil}
	g := mustGraph(t, []string{"a", "b"}, edges(2, [2]int{0, 1}), attrs)
	got := g.Attrs(0)
	if got["k"] != "v" {
		t.Errorf("Attrs(0)[k] = %q want v", got["k"])
	}
	got["k"] = "mutated"
	if g.Attrs(0)["k"] != "v" {
		t.Error("Attrs returned a shared map; mutation leaked")
	}
	if g.Attrs(1)["x"] != "" || g.Attrs(1) == nil {
		t.Error("Attrs(1) should be empty non-nil map")
	}
	A := g.Adjacency()
	A.Set(0, 0, 9)
	if g.Adjacency().At(0, 0) != 0 {
		t.Error("Adjacency returned a shared backing; mutation leaked")
	}
}
