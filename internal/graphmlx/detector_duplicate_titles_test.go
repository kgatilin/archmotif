package graphmlx

import "testing"

const duplicateTitlesFixture = `<?xml version="1.0"?>
<graphml xmlns="http://graphml.graphdrawing.org/xmlns">
  <key id="n_title" for="node" attr.name="title" attr.type="string"/>
  <key id="n_id" for="node" attr.name="archmotif_id" attr.type="string"/>
  <graph id="G" edgedefault="directed">
    <node id="n0"><data key="n_id">a</data><data key="n_title">Build the typed graph</data></node>
    <node id="n1"><data key="n_id">b</data><data key="n_title">build the typed graph!</data></node>
    <node id="n2"><data key="n_id">c</data><data key="n_title">  Build  the  typed  graph  </data></node>
    <node id="n3"><data key="n_id">d</data><data key="n_title">Different title entirely</data></node>
    <node id="n4"><data key="n_id">e</data><data key="n_title">Single occurrence</data></node>
  </graph>
</graphml>`

func TestDuplicateTitles_GroupsCanonical(t *testing.T) {
	g := graphFromInline(t, duplicateTitlesFixture)
	out, err := DuplicateTitlesDetector{}.Detect(g)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 duplicate bucket, got %d: %+v", len(out), out)
	}
	f := out[0]
	if f.Score != 3 {
		t.Errorf("score: got %v want 3", f.Score)
	}
	if got := f.Members; len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("members: %v", got)
	}
	if f.Reason.Details["canonical"] != "build the typed graph" {
		t.Errorf("canonical: got %v", f.Reason.Details["canonical"])
	}
}

func TestCanonicalTitle_Idempotent(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Hello World", "hello world"},
		{"Hello, World!!!", "hello world"},
		{"   spaces   between   ", "spaces between"},
		{"已經", "已經"},
	}
	for _, c := range cases {
		if got := canonicalTitle(c.in); got != c.want {
			t.Errorf("canonicalTitle(%q): got %q want %q", c.in, got, c.want)
		}
	}
}
