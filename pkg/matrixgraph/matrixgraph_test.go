package matrixgraph

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// nodes builds a slice of attribute-less Node from names.
func nodes(names ...string) []Node {
	out := make([]Node, len(names))
	for i, n := range names {
		out[i] = Node{Name: n}
	}
	return out
}

// edges builds directed Edges from (from,to) name pairs.
func edges(pairs ...[2]string) []Edge {
	out := make([]Edge, len(pairs))
	for i, p := range pairs {
		out[i] = Edge{From: p[0], To: p[1]}
	}
	return out
}

func mustGraph(t *testing.T, nodes []Node, edges []Edge) *Graph {
	t.Helper()
	g, err := New(nodes, edges)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return g
}

func TestNewValidation(t *testing.T) {
	if _, err := New(nodes("a", "b"), edges([2]string{"a", "b"})); err != nil {
		t.Fatalf("valid graph rejected: %v", err)
	}
	// Edge referencing an unknown node.
	if _, err := New(nodes("a", "b"), edges([2]string{"a", "z"})); err == nil {
		t.Fatal("expected error on edge to unknown node")
	}
	if _, err := New(nodes("a", "b"), edges([2]string{"z", "a"})); err == nil {
		t.Fatal("expected error on edge from unknown node")
	}
	// Duplicate node name.
	if _, err := New(nodes("a", "a"), nil); err == nil {
		t.Fatal("expected error on duplicate node name")
	}
	// Empty node name.
	if _, err := New([]Node{{Name: ""}}, nil); err == nil {
		t.Fatal("expected error on empty node name")
	}
}

func TestClosureDAG(t *testing.T) {
	// 0 -> 1 -> 2, 0 -> 3. No cycles.
	g := mustGraph(t, nodes("0", "1", "2", "3"),
		edges([2]string{"0", "1"}, [2]string{"1", "2"}, [2]string{"0", "3"}))
	star := g.closure()
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
	g := mustGraph(t, nodes("0", "1", "2", "3"),
		edges([2]string{"0", "1"}, [2]string{"1", "2"}, [2]string{"2", "0"}, [2]string{"2", "3"}))
	star := g.closure()
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
	g := mustGraph(t, nodes("0", "1", "2", "3", "4"),
		edges([2]string{"0", "1"}, [2]string{"1", "2"}, [2]string{"3", "4"}))
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

	// Name-based companion: reflex drives this in node names.
	if names := g.ReachableFromNames([]string{"0"}); !reflect.DeepEqual(names, []string{"0", "1", "2"}) {
		t.Errorf("ReachableFromNames([0]) = %v want [0 1 2]", names)
	}
	if names := g.ReachableFromNames([]string{"0", "3", "nope"}); !reflect.DeepEqual(names, []string{"0", "1", "2", "3", "4"}) {
		t.Errorf("ReachableFromNames([0,3,nope]) = %v", names)
	}
}

func TestSCCs3CycleAndSelfLoop(t *testing.T) {
	// 0,1,2 form a 3-cycle. 3 has a self-loop. 4 is isolated.
	g := mustGraph(t, nodes("0", "1", "2", "3", "4"),
		edges([2]string{"0", "1"}, [2]string{"1", "2"}, [2]string{"2", "0"}, [2]string{"3", "3"}))
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
	// Name-based accessors.
	if got := g.SCCsAsNames(); !reflect.DeepEqual(got, [][]string{{"0", "1", "2"}, {"3"}, {"4"}}) {
		t.Fatalf("SCCsAsNames = %v", got)
	}
	if got := g.NonTrivialSCCsAsNames(); !reflect.DeepEqual(got, [][]string{{"0", "1", "2"}, {"3"}}) {
		t.Fatalf("NonTrivialSCCsAsNames = %v", got)
	}
}

func TestSCCsMissingAttr(t *testing.T) {
	// Two 3-cycles. First (0,1,2) has a node with guard=owner; second
	// (3,4,5) has none. SCCsMissingAttr should return only the second.
	withAttrs := []Node{
		{Name: "0", Attrs: map[string]string{"guard": "owner"}},
		{Name: "1"}, {Name: "2"},
		{Name: "3"}, {Name: "4"}, {Name: "5"},
	}
	cycleEdges := edges(
		[2]string{"0", "1"}, [2]string{"1", "2"}, [2]string{"2", "0"},
		[2]string{"3", "4"}, [2]string{"4", "5"}, [2]string{"5", "3"},
	)
	g := mustGraph(t, withAttrs, cycleEdges)
	pred := func(a map[string]string) bool { return a["guard"] == "owner" }
	got := g.SCCsMissingAttr(pred)
	want := [][]int{{3, 4, 5}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SCCsMissingAttr = %v want %v", got, want)
	}
	if names := g.SCCsMissingAttrAsNames(pred); !reflect.DeepEqual(names, [][]string{{"3", "4", "5"}}) {
		t.Fatalf("SCCsMissingAttrAsNames = %v", names)
	}

	// If every cycle has a qualifying node, none are returned.
	bothOwned := []Node{
		{Name: "0", Attrs: map[string]string{"guard": "owner"}},
		{Name: "1"}, {Name: "2"},
		{Name: "3"}, {Name: "4", Attrs: map[string]string{"guard": "owner"}}, {Name: "5"},
	}
	g2 := mustGraph(t, bothOwned, cycleEdges)
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
	g := mustGraph(t, nodes("0", "1", "2", "3"),
		edges([2]string{"0", "1"}, [2]string{"0", "2"}, [2]string{"1", "2"}))
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
	// Name-based bridge.
	if got, want := g.NamesOf(g.Sinks()), []string{"2", "3"}; !reflect.DeepEqual(got, want) {
		t.Errorf("NamesOf(Sinks) = %v want %v", got, want)
	}
}

func TestCycleNodes(t *testing.T) {
	// 3-cycle 0->1->2->0 plus self-loop on 4, dangling 3.
	g := mustGraph(t, nodes("0", "1", "2", "3", "4"),
		edges([2]string{"0", "1"}, [2]string{"1", "2"}, [2]string{"2", "0"}, [2]string{"2", "3"}, [2]string{"4", "4"}))
	// k=1 finds only the self-loop.
	if got, want := g.CycleNodes(1), []int{4}; !reflect.DeepEqual(got, want) {
		t.Errorf("CycleNodes(1) = %v want %v", got, want)
	}
	// k=3 finds the 3-cycle plus the self-loop; 3 is dangling, never on a cycle.
	if got, want := g.CycleNodes(3), []int{0, 1, 2, 4}; !reflect.DeepEqual(got, want) {
		t.Errorf("CycleNodes(3) = %v want %v", got, want)
	}
	if got, want := g.NamesOf(g.CycleNodes(3)), []string{"0", "1", "2", "4"}; !reflect.DeepEqual(got, want) {
		t.Errorf("NamesOf(CycleNodes(3)) = %v want %v", got, want)
	}
}

func TestDeadItems(t *testing.T) {
	// 3 producers, 4 items. Item "i2" is consumed by nobody.
	producers := []string{"p0", "p1", "p2"}
	items := []string{"i0", "i1", "i2", "i3"}
	es := edges(
		[2]string{"p0", "i0"}, [2]string{"p0", "i3"},
		[2]string{"p1", "i1"},
		[2]string{"p2", "i0"}, [2]string{"p2", "i1"}, [2]string{"p2", "i3"},
	)
	got, err := DeadItems(producers, items, es)
	if err != nil {
		t.Fatalf("DeadItems: %v", err)
	}
	if want := []string{"i2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("DeadItems = %v want %v", got, want)
	}

	// Edge to unknown item errors.
	if _, err := DeadItems(producers, items, edges([2]string{"p0", "zzz"})); err == nil {
		t.Fatal("expected error on edge to unknown item")
	}
	// Edge from unknown producer errors.
	if _, err := DeadItems(producers, items, edges([2]string{"zzz", "i0"})); err == nil {
		t.Fatal("expected error on edge from unknown producer")
	}
	// Duplicate item name errors.
	if _, err := DeadItems(producers, []string{"i0", "i0"}, nil); err == nil {
		t.Fatal("expected error on duplicate item name")
	}

	// No items => nothing dead.
	if got, _ := DeadItems(producers, nil, nil); got != nil {
		t.Errorf("DeadItems(no items) = %v want nil", got)
	}
	// No producers => every item is dead.
	if got, _ := DeadItems(nil, items, nil); !reflect.DeepEqual(got, []string{"i0", "i1", "i2", "i3"}) {
		t.Errorf("DeadItems(no producers) = %v want all items", got)
	}
}

func TestEmptyGraph(t *testing.T) {
	g := mustGraph(t, nil, nil)
	if g.N() != 0 {
		t.Fatalf("N = %d want 0", g.N())
	}
	if r, c := g.closure().Dims(); r != 0 || c != 0 {
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

func TestAttrsCopyAndIndex(t *testing.T) {
	ns := []Node{{Name: "a", Attrs: map[string]string{"k": "v"}}, {Name: "b"}}
	g := mustGraph(t, ns, edges([2]string{"a", "b"}))
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
	// Index / Name round-trip.
	if i, ok := g.Index("b"); !ok || i != 1 {
		t.Errorf("Index(b) = %d,%v want 1,true", i, ok)
	}
	if _, ok := g.Index("nope"); ok {
		t.Error("Index(nope) should report missing")
	}
	if g.Name(0) != "a" {
		t.Errorf("Name(0) = %q want a", g.Name(0))
	}
}

func TestNewFromGraphML(t *testing.T) {
	// A 3-cycle plus an isolated node, in GraphML, exercises the standard-
	// format constructor end to end.
	doc := `<graphml>
	  <graph edgedefault="directed">
	    <node id="a"/><node id="b"/><node id="c"/><node id="d"/>
	    <edge source="a" target="b"/>
	    <edge source="b" target="c"/>
	    <edge source="c" target="a"/>
	  </graph>
	</graphml>`
	g, err := NewFromGraphML(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("NewFromGraphML: %v", err)
	}
	if g.N() != 4 {
		t.Fatalf("N = %d want 4", g.N())
	}
	nt := g.NonTrivialSCCsAsNames()
	if len(nt) != 1 {
		t.Fatalf("NonTrivialSCCsAsNames = %v want one 3-cycle", nt)
	}
	got := append([]string(nil), nt[0]...)
	sort.Strings(got)
	if !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("cycle members = %v want [a b c]", got)
	}
}
