package graphmlx

import "testing"

const hierarchyCycleFixture = `<?xml version="1.0"?>
<graphml xmlns="http://graphml.graphdrawing.org/xmlns">
  <key id="n_id" for="node" attr.name="archmotif_id" attr.type="string"/>
  <key id="e_kind" for="edge" attr.name="kind" attr.type="string"/>
  <graph id="G" edgedefault="directed">
    <node id="n0"><data key="n_id">a</data></node>
    <node id="n1"><data key="n_id">b</data></node>
    <node id="n2"><data key="n_id">c</data></node>
    <node id="n3"><data key="n_id">d</data></node>
    <edge id="e0" source="n0" target="n1"><data key="e_kind">contains</data></edge>
    <edge id="e1" source="n1" target="n2"><data key="e_kind">contains</data></edge>
    <edge id="e2" source="n2" target="n0"><data key="e_kind">contains</data></edge>
    <edge id="e3" source="n0" target="n3"><data key="e_kind">contains</data></edge>
  </graph>
</graphml>`

func TestHierarchyCycle_FindsCycle(t *testing.T) {
	g := graphFromInline(t, hierarchyCycleFixture)
	out, err := HierarchyCycleDetector{}.Detect(g)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 cycle, got %d: %+v", len(out), out)
	}
	f := out[0]
	if f.Score != 3 {
		t.Errorf("score: got %v want 3", f.Score)
	}
	if got := f.Members; len(got) != 3 || got[0] != "a" {
		t.Errorf("members: %v", got)
	}
	if f.Severity != SeverityHigh {
		t.Errorf("severity: got %s want high", f.Severity)
	}
}

func TestHierarchyCycle_NoCyclesNoFindings(t *testing.T) {
	doc := `<?xml version="1.0"?>
<graphml xmlns="http://graphml.graphdrawing.org/xmlns">
  <key id="n_id" for="node" attr.name="archmotif_id" attr.type="string"/>
  <key id="e_kind" for="edge" attr.name="kind" attr.type="string"/>
  <graph id="G" edgedefault="directed">
    <node id="n0"><data key="n_id">a</data></node>
    <node id="n1"><data key="n_id">b</data></node>
    <node id="n2"><data key="n_id">c</data></node>
    <edge id="e0" source="n0" target="n1"><data key="e_kind">contains</data></edge>
    <edge id="e1" source="n1" target="n2"><data key="e_kind">contains</data></edge>
  </graph>
</graphml>`
	g := graphFromInline(t, doc)
	out, _ := HierarchyCycleDetector{}.Detect(g)
	if len(out) != 0 {
		t.Errorf("expected 0 findings, got %d", len(out))
	}
}

func TestHierarchyCycle_SelfLoop(t *testing.T) {
	doc := `<?xml version="1.0"?>
<graphml xmlns="http://graphml.graphdrawing.org/xmlns">
  <key id="n_id" for="node" attr.name="archmotif_id" attr.type="string"/>
  <key id="e_kind" for="edge" attr.name="kind" attr.type="string"/>
  <graph id="G" edgedefault="directed">
    <node id="n0"><data key="n_id">a</data></node>
    <edge id="e0" source="n0" target="n0"><data key="e_kind">contains</data></edge>
  </graph>
</graphml>`
	g := graphFromInline(t, doc)
	out, _ := HierarchyCycleDetector{}.Detect(g)
	if len(out) != 1 {
		t.Errorf("expected 1 self-loop finding, got %d", len(out))
	}
}
