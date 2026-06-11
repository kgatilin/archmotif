package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFixtureGraphML(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

const fixtureGraph = `<?xml version="1.0" encoding="UTF-8"?>
<graphml xmlns="http://graphml.graphdrawing.org/xmlns">
  <key id="n_kind" for="node" attr.name="kind" attr.type="string"/>
  <key id="n_name" for="node" attr.name="name" attr.type="string"/>
  <key id="n_package" for="node" attr.name="package" attr.type="string"/>
  <key id="n_tags" for="node" attr.name="tags" attr.type="string"/>
  <key id="n_id" for="node" attr.name="archmotif_id" attr.type="string"/>
  <key id="e_kind" for="edge" attr.name="kind" attr.type="string"/>
  <graph id="G" edgedefault="directed">
    <node id="n0">
      <data key="n_id">pkg:foo</data>
      <data key="n_kind">package</data>
      <data key="n_name">foo</data>
      <data key="n_package">github.com/example/foo</data>
      <data key="n_tags">root,structure</data>
    </node>
    <node id="n1">
      <data key="n_id">pkg:foo:bar</data>
      <data key="n_kind">function</data>
      <data key="n_name">Bar</data>
      <data key="n_package">github.com/example/foo</data>
      <data key="n_tags">api</data>
    </node>
    <node id="n2">
      <data key="n_id">pkg:foo:baz</data>
      <data key="n_kind">function</data>
      <data key="n_name">Baz</data>
      <data key="n_package">github.com/example/foo</data>
    </node>
    <node id="n3">
      <data key="n_id">pkg:other</data>
      <data key="n_kind">package</data>
      <data key="n_name">other</data>
      <data key="n_package">github.com/example/other</data>
    </node>
    <edge id="e0" source="n0" target="n1">
      <data key="e_kind">contains</data>
    </edge>
    <edge id="e1" source="n0" target="n2">
      <data key="e_kind">contains</data>
    </edge>
    <edge id="e2" source="n1" target="n2">
      <data key="e_kind">calls</data>
    </edge>
    <edge id="e3" source="n2" target="n3">
      <data key="e_kind">dependsOn</data>
    </edge>
  </graph>
</graphml>
`

// installFixture sets up <tmpDir>/graphs/<slug>/actual.graphml with the
// canonical fixture and returns the workspace root.
func installFixture(t *testing.T, slug string) string {
	t.Helper()
	root := t.TempDir()
	writeFixtureGraphML(t, filepath.Join(root, "graphs", slug), "actual.graphml", fixtureGraph)
	return root
}

func TestLoadRoundtripPreservesAttrs(t *testing.T) {
	root := installFixture(t, "demo")
	g, err := Load(filepath.Join(root, "graphs", "demo", "actual.graphml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := len(g.Nodes), 4; got != want {
		t.Fatalf("nodes: got %d, want %d", got, want)
	}
	if got, want := len(g.Edges), 4; got != want {
		t.Fatalf("edges: got %d, want %d", got, want)
	}

	out := filepath.Join(root, "graphs", "demo", "out.graphml")
	if err := g.Save(out); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// Re-load and confirm node/edge sets match.
	g2, err := Load(out)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(g2.Nodes) != len(g.Nodes) || len(g2.Edges) != len(g.Edges) {
		t.Fatalf("roundtrip mismatch: %d/%d → %d/%d", len(g.Nodes), len(g.Edges), len(g2.Nodes), len(g2.Edges))
	}
	// Spot-check a custom attribute survived.
	n, ok := g2.Node("pkg:foo:bar")
	if !ok {
		t.Fatalf("missing pkg:foo:bar after roundtrip")
	}
	if n.Attrs["tags"] != "api" {
		t.Fatalf("tags attr lost: got %q", n.Attrs["tags"])
	}
}

func TestAddNodeRejectsDuplicate(t *testing.T) {
	g := NewGraph()
	if err := g.AddNode(Node{ID: "a", Kind: "x"}); err != nil {
		t.Fatalf("first AddNode: %v", err)
	}
	if err := g.AddNode(Node{ID: "a", Kind: "x"}); err == nil {
		t.Fatalf("expected duplicate error, got nil")
	}
}

func TestAddEdgeRejectsUnknownEndpoints(t *testing.T) {
	g := NewGraph()
	if err := g.AddNode(Node{ID: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := g.AddEdge(Edge{From: "a", To: "missing"}); err == nil {
		t.Fatal("expected error for unknown to-node")
	}
	if err := g.AddEdge(Edge{From: "missing", To: "a"}); err == nil {
		t.Fatal("expected error for unknown from-node")
	}
}

func TestSaveIsAtomic(t *testing.T) {
	g := NewGraph()
	if err := g.AddNode(Node{ID: "n1", Kind: "k", Name: "n1"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "deep", "nested", "out.graphml")
	if err := g.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `<graph id="G"`) {
		t.Fatalf("output not GraphML: %s", string(data))
	}
}

func TestSlug(t *testing.T) {
	cases := []struct{ in, want string }{
		{"foo", "foo"},
		{"github.com/k/v", "github.com_k_v"},
		{"weird name!", "weird_name_"},
		{"", "graph"},
	}
	for _, tc := range cases {
		if got := Slug(tc.in); got != tc.want {
			t.Errorf("Slug(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
