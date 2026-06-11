package shape

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadGraphMLUsesStableIDsAndPredicate(t *testing.T) {
	g := loadFixture(t)

	if len(g.Nodes) != 14 {
		t.Fatalf("nodes = %d, want 14", len(g.Nodes))
	}
	root, ok := g.Nodes["root"]
	if !ok {
		t.Fatal("stable node id root not found")
	}
	if root.XMLID != "n0" {
		t.Fatalf("root XMLID = %q, want n0", root.XMLID)
	}
	if root.Label != "Root subsystem" {
		t.Fatalf("root label = %q", root.Label)
	}
	if got := g.Edges[0].Predicate; got != "part-of" {
		t.Fatalf("edge predicate = %q, want part-of", got)
	}
	if got := g.Edges[0].Layer; got != "SEMANTIC" {
		t.Fatalf("edge layer = %q, want SEMANTIC", got)
	}
}

func loadFixture(t *testing.T) *Graph {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "shape", "flat-star.graphml")
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	g, err := ReadGraphML(f)
	if err != nil {
		t.Fatal(err)
	}
	return g
}
