package mcpserver

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// TestListGraphsEnumeratesVariants checks that ListGraphs walks the graphs/
// directory and returns one entry per (slug, variant) GraphML file.
func TestListGraphsEnumeratesVariants(t *testing.T) {
	root := installFixture(t, "demo")
	writeFixtureGraphML(t, filepath.Join(root, "graphs", "demo"), "target.graphml", fixtureGraph)
	writeFixtureGraphML(t, filepath.Join(root, "graphs", "other"), "actual.graphml", fixtureGraph)

	svc := NewService(root)
	refs, err := svc.ListGraphs()
	if err != nil {
		t.Fatalf("ListGraphs: %v", err)
	}
	ids := make([]string, 0, len(refs))
	for _, r := range refs {
		ids = append(ids, r.ID)
	}
	want := []string{"demo:actual", "demo:target", "other:actual"}
	if len(ids) != len(want) {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("ids[%d] = %s, want %s", i, ids[i], want[i])
		}
	}
	for _, r := range refs {
		if r.Nodes == 0 || r.Edges == 0 {
			t.Errorf("expected nodes/edges for %s, got %d/%d", r.ID, r.Nodes, r.Edges)
		}
	}
}

// TestListGraphsEmpty confirms missing roots return [] (not an error).
func TestListGraphsEmpty(t *testing.T) {
	svc := NewService(t.TempDir())
	refs, err := svc.ListGraphs()
	if err != nil {
		t.Fatalf("ListGraphs: %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("expected empty list, got %d", len(refs))
	}
}

// TestCheckoutGraph verifies happy + missing graph paths.
func TestCheckoutGraph(t *testing.T) {
	svc, _ := mustService(t, "demo")
	ref, err := svc.CheckoutGraph("demo")
	if err != nil {
		t.Fatalf("Checkout: %v", err)
	}
	if ref.ID != "demo:actual" {
		t.Fatalf("ref id = %q", ref.ID)
	}
	if ref.Nodes != 4 {
		t.Fatalf("nodes = %d", ref.Nodes)
	}
	if _, err := svc.CheckoutGraph("missing"); err == nil {
		t.Fatalf("expected error for missing graph")
	}
}

// TestForkGraphCopies confirms a successful fork creates a new GraphML file
// with the same content. Existing destinations are protected unless force=true.
func TestForkGraphCopies(t *testing.T) {
	svc, root := mustService(t, "demo")
	ref, err := svc.ForkGraph("demo", "demo:experiment", false)
	if err != nil {
		t.Fatalf("Fork: %v", err)
	}
	if ref.Path != filepath.Join(root, "graphs", "demo", "experiment.graphml") {
		t.Fatalf("unexpected path: %s", ref.Path)
	}
	// Forking onto an existing path without force should fail.
	if _, err := svc.ForkGraph("demo", "demo:experiment", false); err == nil {
		t.Fatalf("expected error without force")
	}
	// With force, the copy succeeds.
	if _, err := svc.ForkGraph("demo", "demo:experiment", true); err != nil {
		t.Fatalf("force fork: %v", err)
	}
}

// TestMergeGraphsUnion confirms union strategy adds B's nodes/edges into A.
func TestMergeGraphsUnion(t *testing.T) {
	svc, _ := mustService(t, "demo")
	// Build a second graph by forking and adding a node.
	if _, err := svc.ForkGraph("demo", "demo:extra", false); err != nil {
		t.Fatalf("fork: %v", err)
	}
	if _, err := svc.AddNode("demo:extra", "function", map[string]string{"id": "pkg:foo:new", "name": "New"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := svc.AddEdge("demo:extra", "pkg:foo", "pkg:foo:new", "contains", nil); err != nil {
		t.Fatalf("add edge: %v", err)
	}
	res, err := svc.MergeGraphs("demo", "demo:extra", "demo:merged", "union")
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if res.NodesAdded != 1 || res.EdgesAdded != 1 {
		t.Fatalf("expected 1+1, got %d/%d", res.NodesAdded, res.EdgesAdded)
	}
	// Verify the merged graph has the new node.
	mg, err := svc.LoadGraph("demo:merged")
	if err != nil {
		t.Fatalf("load merged: %v", err)
	}
	if !mg.HasNode("pkg:foo:new") {
		t.Fatalf("merged graph missing pkg:foo:new")
	}
}

// TestMergeGraphsStrictDetectsConflicts confirms strict mode surfaces ID
// collisions without writing.
func TestMergeGraphsStrictDetectsConflicts(t *testing.T) {
	svc, _ := mustService(t, "demo")
	if _, err := svc.ForkGraph("demo", "demo:dup", false); err != nil {
		t.Fatalf("fork: %v", err)
	}
	res, err := svc.MergeGraphs("demo", "demo:dup", "demo:out", "strict")
	if err == nil {
		t.Fatalf("expected conflict error")
	}
	if len(res.NodeConflicts) == 0 {
		t.Fatalf("expected node conflicts list")
	}
}

// TestDiffGraphsStructuralDelta confirms graph_diff classifies adds/removes/changes.
func TestDiffGraphsStructuralDelta(t *testing.T) {
	svc, _ := mustService(t, "demo")
	if _, err := svc.ForkGraph("demo", "demo:work", false); err != nil {
		t.Fatalf("fork: %v", err)
	}
	if _, err := svc.AddNode("demo:work", "function", map[string]string{"id": "pkg:foo:fresh", "name": "Fresh"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := svc.AddEdge("demo:work", "pkg:foo", "pkg:foo:fresh", "contains", nil); err != nil {
		t.Fatalf("add edge: %v", err)
	}

	d, err := svc.DiffGraphs("demo", "demo:work")
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if d.Summary.NodesAdded != 1 {
		t.Fatalf("nodes_added = %d", d.Summary.NodesAdded)
	}
	if d.Summary.EdgesAdded != 1 {
		t.Fatalf("edges_added = %d", d.Summary.EdgesAdded)
	}
	if len(d.Nodes.Added) != 1 || d.Nodes.Added[0].ID != "pkg:foo:fresh" {
		t.Fatalf("unexpected nodes.added: %+v", d.Nodes.Added)
	}
	// Reverse direction should report removed instead.
	d2, err := svc.DiffGraphs("demo:work", "demo")
	if err != nil {
		t.Fatalf("diff reverse: %v", err)
	}
	if d2.Summary.NodesRemoved != 1 {
		t.Fatalf("nodes_removed = %d", d2.Summary.NodesRemoved)
	}
}

// TestDiffGraphsDetectsAttrChange confirms a same-id node with new attrs lands
// in the `changed` set.
func TestDiffGraphsDetectsAttrChange(t *testing.T) {
	svc, _ := mustService(t, "demo")
	if _, err := svc.ForkGraph("demo", "demo:tweaked", false); err != nil {
		t.Fatalf("fork: %v", err)
	}
	// Modify an existing node's tags.
	g, err := svc.LoadGraph("demo:tweaked")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := g.UpdateNodeAttr("pkg:foo:bar", "tags", "api,important"); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := svc.SaveGraph("demo:tweaked", g); err != nil {
		t.Fatalf("save: %v", err)
	}

	d, err := svc.DiffGraphs("demo", "demo:tweaked")
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if d.Summary.NodesChanged != 1 {
		t.Fatalf("expected 1 changed node, got %d", d.Summary.NodesChanged)
	}
	if d.Nodes.Changed[0].ID != "pkg:foo:bar" {
		t.Fatalf("unexpected changed node: %s", d.Nodes.Changed[0].ID)
	}
	if _, ok := d.Nodes.Changed[0].AttrsDiff["tags"]; !ok {
		t.Fatalf("expected attrs_diff[tags], got %+v", d.Nodes.Changed[0].AttrsDiff)
	}
}

// TestGraphHashIsStable ensures the same file produces the same hash on
// repeated calls, and different content produces a different hash.
func TestGraphHashIsStable(t *testing.T) {
	svc, _ := mustService(t, "demo")
	h1, err := svc.graphHash("demo")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	h2, err := svc.graphHash("demo")
	if err != nil {
		t.Fatalf("hash2: %v", err)
	}
	if h1 != h2 {
		t.Fatalf("hash unstable: %s != %s", h1, h2)
	}
	if _, err := svc.AddNode("demo", "function", map[string]string{"id": "pkg:foo:hashprobe", "name": "Probe"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	h3, err := svc.graphHash("demo")
	if err != nil {
		t.Fatalf("hash3: %v", err)
	}
	if h1 == h3 {
		t.Fatalf("hash should change after mutation")
	}
}

// TestSplitGraphIDDefaults confirms slug:variant parsing.
func TestSplitGraphIDDefaults(t *testing.T) {
	cases := []struct {
		in      string
		slug    string
		variant string
	}{
		{"foo", "foo", "actual"},
		{"foo:target", "foo", "target"},
		{"feat/cost-cap:actual", "feat_cost-cap", "actual"},
	}
	for _, c := range cases {
		s, v, err := splitGraphID(c.in)
		if err != nil {
			t.Errorf("split(%q): unexpected error %v", c.in, err)
			continue
		}
		if s != c.slug || v != c.variant {
			t.Errorf("split(%q) = (%q,%q), want (%q,%q)", c.in, s, v, c.slug, c.variant)
		}
	}
}

// TestSplitGraphIDRejectsTraversal verifies that path-traversal segments are
// rejected with ErrInvalidGraphID, regardless of which separator is used.
func TestSplitGraphIDRejectsTraversal(t *testing.T) {
	cases := []string{
		"..",
		"../etc/passwd",
		"foo/../bar",
		"foo:..",
		"..:actual",
		`foo\..\bar`,
	}
	for _, in := range cases {
		_, _, err := splitGraphID(in)
		if err == nil {
			t.Errorf("splitGraphID(%q): expected error, got nil", in)
			continue
		}
		if !errors.Is(err, ErrInvalidGraphID) {
			t.Errorf("splitGraphID(%q): error %v not ErrInvalidGraphID", in, err)
		}
	}
}

// TestValidateGraphIDRejectsEmpty verifies that empty graph_ids fail
// validation with ErrInvalidGraphID.
func TestValidateGraphIDRejectsEmpty(t *testing.T) {
	if err := validateGraphID(""); !errors.Is(err, ErrInvalidGraphID) {
		t.Errorf("validateGraphID(\"\"): want ErrInvalidGraphID, got %v", err)
	}
}

// TestDiffGraphsEmptyOnIdentical confirms a graph diffed against itself
// produces zero delta.
func TestDiffGraphsEmptyOnIdentical(t *testing.T) {
	svc, _ := mustService(t, "demo")
	d, err := svc.DiffGraphs("demo", "demo")
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if d.Summary.NodesAdded+d.Summary.NodesRemoved+d.Summary.NodesChanged != 0 {
		t.Fatalf("expected zero node delta: %+v", d.Summary)
	}
	if d.Summary.EdgesAdded+d.Summary.EdgesRemoved != 0 {
		t.Fatalf("expected zero edge delta: %+v", d.Summary)
	}
}

// TestForkInvalidSameIDFails covers the corner case of source == destination.
func TestForkInvalidSameIDFails(t *testing.T) {
	svc, _ := mustService(t, "demo")
	if _, err := svc.ForkGraph("demo", "demo:actual", true); err == nil {
		t.Fatalf("expected error when src==dst")
	} else if !strings.Contains(err.Error(), "same path") {
		t.Fatalf("unexpected error: %v", err)
	}
}
