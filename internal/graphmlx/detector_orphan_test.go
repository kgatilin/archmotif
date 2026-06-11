package graphmlx

import "testing"

const orphanFixture = `<?xml version="1.0"?>
<graphml xmlns="http://graphml.graphdrawing.org/xmlns">
  <key id="n_kind" for="node" attr.name="kind" attr.type="string"/>
  <key id="n_id" for="node" attr.name="archmotif_id" attr.type="string"/>
  <key id="e_kind" for="edge" attr.name="kind" attr.type="string"/>
  <graph id="G" edgedefault="directed">
    <node id="n0"><data key="n_id">a</data><data key="n_kind">type</data></node>
    <node id="n1"><data key="n_id">b</data><data key="n_kind">type</data></node>
    <node id="n2"><data key="n_id">c</data><data key="n_kind">type</data></node>
    <node id="n3"><data key="n_id">d</data><data key="n_kind">stub</data></node>
    <node id="n4"><data key="n_id">e</data><data key="n_kind">connected</data></node>
    <node id="n5"><data key="n_id">f</data><data key="n_kind">connected</data></node>
    <edge id="e0" source="n4" target="n5"><data key="e_kind">depends_on</data></edge>
  </graph>
</graphml>`

func TestOrphanBucketDetector_GroupsByKind(t *testing.T) {
	g := graphFromInline(t, orphanFixture)
	d := OrphanBucketDetector{}
	out, err := d.Detect(g)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 buckets (type, stub), got %d: %+v", len(out), out)
	}
	// Buckets sorted by key alphabetically: "stub", "type"
	if out[0].Reason.Code != "orphan_bucket" {
		t.Errorf("code: %s", out[0].Reason.Code)
	}
	// Find the type bucket and check size = 3.
	var typeBucket Finding
	for _, f := range out {
		if f.Reason.Details["bucket"] == "type" {
			typeBucket = f
		}
	}
	if typeBucket.Score != 3 {
		t.Errorf("type bucket score: got %v want 3", typeBucket.Score)
	}
	if got := typeBucket.Members; len(got) != 3 || got[0] != "a" {
		t.Errorf("type bucket members: %v", got)
	}
}

func TestOrphanBucketDetector_NoOrphansEmits(t *testing.T) {
	doc := `<?xml version="1.0"?>
<graphml xmlns="http://graphml.graphdrawing.org/xmlns">
  <key id="n_id" for="node" attr.name="archmotif_id" attr.type="string"/>
  <graph id="G" edgedefault="directed">
    <node id="n0"><data key="n_id">a</data></node>
    <node id="n1"><data key="n_id">b</data></node>
    <edge id="e0" source="n0" target="n1"/>
  </graph>
</graphml>`
	g := graphFromInline(t, doc)
	out, err := OrphanBucketDetector{}.Detect(g)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("expected zero findings, got %d", len(out))
	}
}
