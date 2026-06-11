package graphmlx

import "testing"

const communityFixture = `<?xml version="1.0"?>
<graphml xmlns="http://graphml.graphdrawing.org/xmlns">
  <key id="n_id" for="node" attr.name="archmotif_id" attr.type="string"/>
  <key id="n_community" for="node" attr.name="community" attr.type="string"/>
  <key id="e_kind" for="edge" attr.name="kind" attr.type="string"/>
  <graph id="G" edgedefault="directed">
    <node id="np"><data key="n_id">parent_x</data></node>
    <node id="nq"><data key="n_id">parent_y</data></node>
    <node id="n1"><data key="n_id">a</data><data key="n_community">cluster1</data></node>
    <node id="n2"><data key="n_id">b</data><data key="n_community">cluster1</data></node>
    <node id="n3"><data key="n_id">c</data><data key="n_community">cluster1</data></node>
    <node id="n4"><data key="n_id">outlier</data><data key="n_community">cluster1</data></node>
    <!-- a, b, c parented by parent_x; outlier parented by parent_y -->
    <edge id="e0" source="np" target="n1"><data key="e_kind">contains</data></edge>
    <edge id="e1" source="np" target="n2"><data key="e_kind">contains</data></edge>
    <edge id="e2" source="np" target="n3"><data key="e_kind">contains</data></edge>
    <edge id="e3" source="nq" target="n4"><data key="e_kind">contains</data></edge>
  </graph>
</graphml>`

func TestCommunityParentMismatch_FlagsOutlier(t *testing.T) {
	g := graphFromInline(t, communityFixture)
	out, err := CommunityParentMismatchDetector{}.Detect(g)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(out), out)
	}
	f := out[0]
	if f.PrimaryID != "parent_x" {
		t.Errorf("majority parent: got %s want parent_x", f.PrimaryID)
	}
	outliers, ok := f.Reason.Details["outliers"].([]string)
	if !ok || len(outliers) != 1 || outliers[0] != "outlier" {
		t.Errorf("outliers: %v", f.Reason.Details["outliers"])
	}
}

func TestCommunityParentMismatch_NoCommunityNoFindings(t *testing.T) {
	doc := `<?xml version="1.0"?>
<graphml xmlns="http://graphml.graphdrawing.org/xmlns">
  <key id="n_id" for="node" attr.name="archmotif_id" attr.type="string"/>
  <graph id="G" edgedefault="directed">
    <node id="n0"><data key="n_id">a</data></node>
    <node id="n1"><data key="n_id">b</data></node>
  </graph>
</graphml>`
	g := graphFromInline(t, doc)
	out, _ := CommunityParentMismatchDetector{}.Detect(g)
	if len(out) != 0 {
		t.Errorf("expected 0 findings, got %d", len(out))
	}
}
