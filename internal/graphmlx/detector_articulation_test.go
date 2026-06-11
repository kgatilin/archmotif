package graphmlx

import "testing"

const articulationFixture = `<?xml version="1.0"?>
<graphml xmlns="http://graphml.graphdrawing.org/xmlns">
  <key id="n_id" for="node" attr.name="archmotif_id" attr.type="string"/>
  <key id="e_kind" for="edge" attr.name="kind" attr.type="string"/>
  <graph id="G" edgedefault="directed">
    <node id="n0"><data key="n_id">a</data></node>
    <node id="n1"><data key="n_id">b</data></node>
    <node id="n2"><data key="n_id">c</data></node>
    <node id="n3"><data key="n_id">d</data></node>
    <node id="n4"><data key="n_id">e</data></node>
    <node id="n5"><data key="n_id">f</data></node>
    <node id="n6"><data key="n_id">g</data></node>
    <!-- two clusters {a,b,c} and {e,f,g} bridged by d -->
    <edge id="e0" source="n0" target="n1"/>
    <edge id="e1" source="n1" target="n2"/>
    <edge id="e2" source="n2" target="n0"/>
    <edge id="e3" source="n2" target="n3"/>
    <edge id="e4" source="n3" target="n4"/>
    <edge id="e5" source="n4" target="n5"/>
    <edge id="e6" source="n5" target="n6"/>
    <edge id="e7" source="n6" target="n4"/>
  </graph>
</graphml>`

func TestArticulation_FindsBridgeNode(t *testing.T) {
	g := graphFromInline(t, articulationFixture)
	out, err := ArticulationDetector{}.Detect(g)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	// Both 'c' (joining triangle to bridge) and 'd' (bridge itself)
	// are articulation points. 'd' guards a 3-node cluster.
	if len(out) == 0 {
		t.Fatalf("expected articulation findings, got 0")
	}
	hasD := false
	for _, f := range out {
		if f.PrimaryID == "d" {
			hasD = true
		}
	}
	if !hasD {
		t.Errorf("expected articulation finding for node d, got %+v", out)
	}
}

func TestArticulation_NoCutsInClique(t *testing.T) {
	doc := `<?xml version="1.0"?>
<graphml xmlns="http://graphml.graphdrawing.org/xmlns">
  <key id="n_id" for="node" attr.name="archmotif_id" attr.type="string"/>
  <graph id="G" edgedefault="directed">
    <node id="n0"><data key="n_id">a</data></node>
    <node id="n1"><data key="n_id">b</data></node>
    <node id="n2"><data key="n_id">c</data></node>
    <edge id="e0" source="n0" target="n1"/>
    <edge id="e1" source="n1" target="n2"/>
    <edge id="e2" source="n2" target="n0"/>
  </graph>
</graphml>`
	g := graphFromInline(t, doc)
	out, _ := ArticulationDetector{}.Detect(g)
	if len(out) != 0 {
		t.Errorf("expected 0 cuts in a triangle, got %d: %+v", len(out), out)
	}
}
