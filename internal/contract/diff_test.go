package contract

import (
	"strings"
	"testing"

	"github.com/kgatilin/archmotif/internal/graphmlx"
)

const diffBefore = `<?xml version="1.0"?>
<graphml xmlns="http://graphml.graphdrawing.org/xmlns">
  <key id="qname" for="node" attr.name="qname" attr.type="string"/>
  <graph edgedefault="directed">
    <node id="n0"><data key="qname">pkg.A</data></node>
    <node id="n1"><data key="qname">pkg.B</data></node>
    <edge source="n0" target="n1"/>
  </graph>
</graphml>`

// diffAfter keeps A and B (with B's element id shifted, simulating position
// churn) and adds C plus an A->C edge.
const diffAfter = `<?xml version="1.0"?>
<graphml xmlns="http://graphml.graphdrawing.org/xmlns">
  <key id="qname" for="node" attr.name="qname" attr.type="string"/>
  <graph edgedefault="directed">
    <node id="x9"><data key="qname">pkg.A</data></node>
    <node id="x7"><data key="qname">pkg.B</data></node>
    <node id="x8"><data key="qname">pkg.C</data></node>
    <edge source="x9" target="x7"/>
    <edge source="x9" target="x8"/>
  </graph>
</graphml>`

func mustRead(t *testing.T, s string) *graphmlx.Graph {
	t.Helper()
	g, err := graphmlx.Read(strings.NewReader(s))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return g
}

func TestDiffAddedByQName(t *testing.T) {
	before := mustRead(t, diffBefore)
	after := mustRead(t, diffAfter)

	focus, sum := Diff(before, after, "qname", 1, true)

	if sum.AddedN != 1 || sum.Added[0] != "pkg.C" {
		t.Fatalf("added: want [pkg.C], got %v", sum.Added)
	}
	if sum.RemovedN != 0 {
		t.Fatalf("removed: want 0, got %v", sum.Removed)
	}
	// Focus = added C + 1-hop context A (source of A->C). B is unrelated to the
	// addition and must not appear.
	gotDiff := map[string]string{}
	for _, n := range focus.Nodes {
		gotDiff[n.Attrs["qname"]] = n.Attrs["diff"]
	}
	if gotDiff["pkg.C"] != "added" {
		t.Errorf("pkg.C diff = %q, want added", gotDiff["pkg.C"])
	}
	if gotDiff["pkg.A"] != "context" {
		t.Errorf("pkg.A diff = %q, want context", gotDiff["pkg.A"])
	}
	if _, ok := gotDiff["pkg.B"]; ok {
		t.Errorf("pkg.B should not be in the 1-hop focus, got %v", gotDiff)
	}

	// The A->C edge is new; it must be marked added.
	var foundAdded bool
	for _, e := range focus.Edges {
		if e.Attrs["diff"] == "added" {
			foundAdded = true
		}
	}
	if !foundAdded {
		t.Errorf("expected an edge marked diff=added")
	}
}

func TestDiffIgnoresPositionChurnByQName(t *testing.T) {
	// before == after semantically (same qnames) but every element id differs.
	before := mustRead(t, diffBefore)
	churned := strings.NewReplacer("n0", "z0", "n1", "z1").Replace(diffBefore)
	after := mustRead(t, churned)

	_, sum := Diff(before, after, "qname", 1, true)
	if sum.AddedN != 0 || sum.RemovedN != 0 {
		t.Fatalf("position churn should produce no diff, got +%d -%d", sum.AddedN, sum.RemovedN)
	}
}
