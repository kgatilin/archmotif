package graphmlx

import "testing"

const entropyFixture = `<?xml version="1.0"?>
<graphml xmlns="http://graphml.graphdrawing.org/xmlns">
  <key id="n_kind" for="node" attr.name="kind" attr.type="string"/>
  <key id="n_id" for="node" attr.name="archmotif_id" attr.type="string"/>
  <key id="e_kind" for="edge" attr.name="kind" attr.type="string"/>
  <graph id="G" edgedefault="directed">
    <node id="n0"><data key="n_id">parent</data><data key="n_kind">package</data></node>
    <node id="n1"><data key="n_id">c1</data><data key="n_kind">type</data></node>
    <node id="n2"><data key="n_id">c2</data><data key="n_kind">function</data></node>
    <node id="n3"><data key="n_id">c3</data><data key="n_kind">stub</data></node>
    <node id="n4"><data key="n_id">c4</data><data key="n_kind">memory</data></node>
    <node id="n5"><data key="n_id">c5</data><data key="n_kind">other</data></node>
    <node id="n6"><data key="n_id">c6</data><data key="n_kind">other</data></node>
    <edge id="e0" source="n0" target="n1"><data key="e_kind">contains</data></edge>
    <edge id="e1" source="n0" target="n2"><data key="e_kind">contains</data></edge>
    <edge id="e2" source="n0" target="n3"><data key="e_kind">contains</data></edge>
    <edge id="e3" source="n0" target="n4"><data key="e_kind">contains</data></edge>
    <edge id="e4" source="n0" target="n5"><data key="e_kind">contains</data></edge>
    <edge id="e5" source="n0" target="n6"><data key="e_kind">contains</data></edge>
  </graph>
</graphml>`

func TestLabelEntropyHub_FlagsHighEntropy(t *testing.T) {
	g := graphFromInline(t, entropyFixture)
	out, err := LabelEntropyHubDetector{}.Detect(g)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(out), out)
	}
	f := out[0]
	if f.PrimaryID != "parent" {
		t.Errorf("primary: got %s want parent", f.PrimaryID)
	}
	if f.Reason.Details["fanout"] != 6 {
		t.Errorf("fanout: got %v want 6", f.Reason.Details["fanout"])
	}
	// 5 distinct kinds across 6 children: type, function, stub, memory, other(x2)
	kinds, ok := f.Reason.Details["kinds"].([]string)
	if !ok {
		t.Fatalf("kinds wrong type: %T", f.Reason.Details["kinds"])
	}
	if len(kinds) != 5 {
		t.Errorf("kinds: got %v want 5 distinct", kinds)
	}
}

func TestLabelEntropyHub_SkipsHomogeneousFanout(t *testing.T) {
	doc := `<?xml version="1.0"?>
<graphml xmlns="http://graphml.graphdrawing.org/xmlns">
  <key id="n_kind" for="node" attr.name="kind" attr.type="string"/>
  <key id="n_id" for="node" attr.name="archmotif_id" attr.type="string"/>
  <key id="e_kind" for="edge" attr.name="kind" attr.type="string"/>
  <graph id="G" edgedefault="directed">
    <node id="n0"><data key="n_id">p</data><data key="n_kind">package</data></node>
    <node id="n1"><data key="n_id">c1</data><data key="n_kind">type</data></node>
    <node id="n2"><data key="n_id">c2</data><data key="n_kind">type</data></node>
    <node id="n3"><data key="n_id">c3</data><data key="n_kind">type</data></node>
    <node id="n4"><data key="n_id">c4</data><data key="n_kind">type</data></node>
    <node id="n5"><data key="n_id">c5</data><data key="n_kind">type</data></node>
    <edge id="e0" source="n0" target="n1"><data key="e_kind">contains</data></edge>
    <edge id="e1" source="n0" target="n2"><data key="e_kind">contains</data></edge>
    <edge id="e2" source="n0" target="n3"><data key="e_kind">contains</data></edge>
    <edge id="e3" source="n0" target="n4"><data key="e_kind">contains</data></edge>
    <edge id="e4" source="n0" target="n5"><data key="e_kind">contains</data></edge>
  </graph>
</graphml>`
	g := graphFromInline(t, doc)
	out, _ := LabelEntropyHubDetector{}.Detect(g)
	if len(out) != 0 {
		t.Errorf("expected 0 findings (homogeneous), got %d", len(out))
	}
}
