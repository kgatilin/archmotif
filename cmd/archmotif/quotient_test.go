package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// TestQuotientCommand_NoArg confirms missing-graph surfaces usage on
// stderr and exits 2.
func TestQuotientCommand_NoArg(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"quotient"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "Usage") {
		t.Errorf("stderr missing Usage:\n%s", stderr.String())
	}
}

// TestQuotientCommand_MissingCommunities requires --communities.
func TestQuotientCommand_MissingCommunities(t *testing.T) {
	graphPath := writeQuotientGraphFixture(t, threeCommunityQuotientGraph())
	var stdout, stderr bytes.Buffer
	code := run([]string{"quotient", graphPath}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "--communities is required") {
		t.Errorf("stderr missing --communities hint:\n%s", stderr.String())
	}
}

// TestQuotientCommand_BadFormat rejects unknown --format values.
func TestQuotientCommand_BadFormat(t *testing.T) {
	graphPath := writeQuotientGraphFixture(t, threeCommunityQuotientGraph())
	commPath := writeQuotientCommunitiesFixture(t, threeCommunityMembers())
	var stdout, stderr bytes.Buffer
	code := run([]string{"quotient", "--format", "xml", "--communities", commPath, graphPath}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
}

// TestQuotientCommand_MissingGraphFile surfaces a read error on the
// positional graph path.
func TestQuotientCommand_MissingGraphFile(t *testing.T) {
	commPath := writeQuotientCommunitiesFixture(t, threeCommunityMembers())
	var stdout, stderr bytes.Buffer
	code := run([]string{"quotient", "--communities", commPath, "/nonexistent/graph.json"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr=%s", code, stderr.String())
	}
}

// TestQuotientCommand_MissingCommunitiesFile surfaces a read error on
// the --communities path.
func TestQuotientCommand_MissingCommunitiesFile(t *testing.T) {
	graphPath := writeQuotientGraphFixture(t, threeCommunityQuotientGraph())
	var stdout, stderr bytes.Buffer
	code := run([]string{"quotient", "--communities", "/nonexistent/comm.json", graphPath}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr=%s", code, stderr.String())
	}
}

// TestQuotientCommand_ThreeCommunities is the headline acceptance test.
//
// Fixture: 3 K_4 cliques A, B, C with a single directed bridge edge
// A0->B0 and B0->C0 (so two inter-community super-edges, no cycle).
//
// Expectations:
//   - 3 super-nodes (community_0, _1, _2), each size 4
//   - 2 super-edges: community_0 -> community_1 weight 1; community_1 ->
//     community_2 weight 1
//   - Each super-edge's `underlying` lists the source-graph edge in
//     "src->dst" form
//   - Files written: <graph>.quotient.json and <graph>.quotient.graphml
//   - GraphML parses as XML
func TestQuotientCommand_ThreeCommunities(t *testing.T) {
	graphPath := writeQuotientGraphFixture(t, threeCommunityQuotientGraph())
	commPath := writeQuotientCommunitiesFixture(t, threeCommunityMembers())

	var stdout, stderr bytes.Buffer
	code := run([]string{"quotient", "--communities", commPath, graphPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", code, stderr.String())
	}
	var report quotientReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode stdout JSON: %v\nraw=\n%s", err, stdout.String())
	}
	if report.NSuperNodes != 3 {
		t.Errorf("n_super_nodes = %d, want 3", report.NSuperNodes)
	}
	if report.NSuperEdges != 2 {
		t.Errorf("n_super_edges = %d, want 2", report.NSuperEdges)
	}
	for _, n := range report.SuperNodes {
		if n.Size != 4 || len(n.Members) != 4 {
			t.Errorf("super-node %s: size=%d members=%v, want 4", n.ID, n.Size, n.Members)
		}
	}

	// Find the A->B and B->C super-edges.
	byPair := make(map[string]quotientSuperEdge)
	for _, e := range report.SuperEdges {
		byPair[e.Src+"->"+e.Dst] = e
	}
	for _, want := range []struct {
		src, dst, under string
	}{
		{"community_0", "community_1", "A0->B0"},
		{"community_1", "community_2", "B0->C0"},
	} {
		e, ok := byPair[want.src+"->"+want.dst]
		if !ok {
			t.Errorf("missing super-edge %s->%s", want.src, want.dst)
			continue
		}
		if e.Weight != 1 {
			t.Errorf("super-edge %s->%s weight = %d, want 1", want.src, want.dst, e.Weight)
		}
		if len(e.Underlying) != 1 || e.Underlying[0] != want.under {
			t.Errorf("super-edge %s->%s underlying = %v, want [%s]", want.src, want.dst, e.Underlying, want.under)
		}
	}

	// Total super-edge weight must equal the count of inter-community
	// edges in the source graph (2 here).
	total := 0
	for _, e := range report.SuperEdges {
		total += e.Weight
	}
	if total != 2 {
		t.Errorf("total super-edge weight = %d, want 2 (matches inter-community edge count)", total)
	}

	// On-disk artefacts exist next to the input graph.
	jsonOut := strings.TrimSuffix(graphPath, filepath.Ext(graphPath)) + ".quotient.json"
	graphmlOut := strings.TrimSuffix(graphPath, filepath.Ext(graphPath)) + ".quotient.graphml"
	for _, p := range []string{jsonOut, graphmlOut} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("artefact missing: %s: %v", p, err)
		}
	}
	// GraphML round-trips via encoding/xml — minimal sanity check that
	// the document is well-formed.
	raw, err := os.ReadFile(graphmlOut)
	if err != nil {
		t.Fatalf("read graphml: %v", err)
	}
	dec := xml.NewDecoder(bytes.NewReader(raw))
	for {
		_, err := dec.Token()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			// XML parse error: fail with the offending bytes.
			t.Fatalf("GraphML parse error: %v\n--- file ---\n%s", err, string(raw))
		}
	}
	// GraphML must reference all three communities by ID.
	for _, want := range []string{"community_0", "community_1", "community_2"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("GraphML missing %q", want)
		}
	}
	// And must use edgedefault="directed" (mirrors the source).
	if !strings.Contains(string(raw), `edgedefault="directed"`) {
		t.Errorf("GraphML missing edgedefault=directed:\n%s", string(raw))
	}
}

// TestQuotientCommand_TextFormat exercises the human-readable summary.
func TestQuotientCommand_TextFormat(t *testing.T) {
	graphPath := writeQuotientGraphFixture(t, threeCommunityQuotientGraph())
	commPath := writeQuotientCommunitiesFixture(t, threeCommunityMembers())
	var stdout, stderr bytes.Buffer
	code := run([]string{"quotient", "--format", "text", "--communities", commPath, graphPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"quotient report",
		"super_nodes:",
		"super_edges:",
		"community_0",
		"inter-community edges:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("text output missing %q\n--- output ---\n%s", want, out)
		}
	}
}

// TestQuotientCommand_OutPrefix confirms --out overrides the default
// output path derivation.
func TestQuotientCommand_OutPrefix(t *testing.T) {
	graphPath := writeQuotientGraphFixture(t, threeCommunityQuotientGraph())
	commPath := writeQuotientCommunitiesFixture(t, threeCommunityMembers())
	dir := t.TempDir()
	prefix := filepath.Join(dir, "custom_quotient")

	var stdout, stderr bytes.Buffer
	code := run([]string{"quotient", "--out", prefix, "--communities", commPath, graphPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", code, stderr.String())
	}
	for _, p := range []string{prefix + ".json", prefix + ".graphml"} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("artefact missing at custom prefix: %s: %v", p, err)
		}
	}
}

// TestQuotientReport_EmptyGraph verifies empty inputs do not panic and
// emit an empty report — mirrors spectral/communities convention.
func TestQuotientReport_EmptyGraph(t *testing.T) {
	doc := mgraph.JSON{Version: mgraph.CurrentJSONVersion}
	r, err := computeQuotientReport(doc, map[string][]string{})
	if err != nil {
		t.Fatalf("empty graph: %v", err)
	}
	if r.NSuperNodes != 0 || r.NSuperEdges != 0 {
		t.Errorf("empty graph: nodes=%d edges=%d", r.NSuperNodes, r.NSuperEdges)
	}
}

// TestQuotientReport_SingleCommunityZeroSuperEdges checks the
// degenerate case where all nodes belong to one community: there are no
// inter-community edges by definition.
func TestQuotientReport_SingleCommunityZeroSuperEdges(t *testing.T) {
	doc := mgraph.JSON{Version: mgraph.CurrentJSONVersion}
	doc.Nodes = []mgraph.Node{
		{ID: "a", Kind: mgraph.NodeFunction},
		{ID: "b", Kind: mgraph.NodeFunction},
	}
	doc.Edges = []mgraph.Edge{
		{From: "a", To: "b", Kind: mgraph.EdgeCalls},
	}
	members := map[string][]string{
		"community_0": {"a", "b"},
	}
	r, err := computeQuotientReport(doc, members)
	if err != nil {
		t.Fatalf("single community: %v", err)
	}
	if r.NSuperNodes != 1 {
		t.Errorf("super_nodes = %d, want 1", r.NSuperNodes)
	}
	if r.NSuperEdges != 0 {
		t.Errorf("super_edges = %d, want 0 (intra-community only)", r.NSuperEdges)
	}
}

// TestQuotientReport_DisjointPartitionRequired asserts that a node
// appearing in two communities is reported as an error rather than
// silently mis-classified.
func TestQuotientReport_DisjointPartitionRequired(t *testing.T) {
	doc := mgraph.JSON{Version: mgraph.CurrentJSONVersion}
	doc.Nodes = []mgraph.Node{{ID: "x", Kind: mgraph.NodeFunction}}
	members := map[string][]string{
		"community_0": {"x"},
		"community_1": {"x"},
	}
	if _, err := computeQuotientReport(doc, members); err == nil {
		t.Errorf("expected error for overlapping partition, got nil")
	}
}

// TestQuotientReport_UnknownNodesDropped confirms that members
// referencing nodes not in the source graph are silently dropped.
func TestQuotientReport_UnknownNodesDropped(t *testing.T) {
	doc := mgraph.JSON{Version: mgraph.CurrentJSONVersion}
	doc.Nodes = []mgraph.Node{
		{ID: "a", Kind: mgraph.NodeFunction},
		{ID: "b", Kind: mgraph.NodeFunction},
	}
	members := map[string][]string{
		"community_0": {"a", "ghost_a"},
		"community_1": {"b"},
	}
	r, err := computeQuotientReport(doc, members)
	if err != nil {
		t.Fatalf("unknown nodes: %v", err)
	}
	// ghost_a must not appear in the super-node members.
	for _, n := range r.SuperNodes {
		if n.ID == "community_0" {
			if n.Size != 1 || len(n.Members) != 1 || n.Members[0] != "a" {
				t.Errorf("community_0 members = %v, want [a]", n.Members)
			}
		}
	}
}

// TestQuotientReport_MultiEdgesDeduped collapses parallel underlying
// edges (same src/dst, different kind) to a single underlying entry
// so super-edge weight reflects topology, not multiplicity.
func TestQuotientReport_MultiEdgesDeduped(t *testing.T) {
	doc := mgraph.JSON{Version: mgraph.CurrentJSONVersion}
	doc.Nodes = []mgraph.Node{
		{ID: "a", Kind: mgraph.NodeFunction},
		{ID: "b", Kind: mgraph.NodeFunction},
	}
	doc.Edges = []mgraph.Edge{
		{From: "a", To: "b", Kind: mgraph.EdgeCalls},
		{From: "a", To: "b", Kind: mgraph.EdgeReferences},
	}
	members := map[string][]string{
		"community_0": {"a"},
		"community_1": {"b"},
	}
	r, err := computeQuotientReport(doc, members)
	if err != nil {
		t.Fatalf("multi-edge: %v", err)
	}
	if r.NSuperEdges != 1 {
		t.Fatalf("super_edges = %d, want 1", r.NSuperEdges)
	}
	if r.SuperEdges[0].Weight != 1 {
		t.Errorf("weight = %d, want 1 (parallel edges dedup)", r.SuperEdges[0].Weight)
	}
}

// TestWriteQuotientGraphML_RoundTripSchema asserts the GraphML emission
// contains the expected key declarations and graph structure for a
// minimal report. Decouples the emission tests from filesystem state.
func TestWriteQuotientGraphML_RoundTripSchema(t *testing.T) {
	r := quotientReport{
		NSuperNodes: 2,
		NSuperEdges: 1,
		SuperNodes: []quotientSuperNode{
			{ID: "community_0", Members: []string{"a"}, Size: 1},
			{ID: "community_1", Members: []string{"b"}, Size: 1},
		},
		SuperEdges: []quotientSuperEdge{
			{Src: "community_0", Dst: "community_1", Weight: 3, Underlying: []string{"a->b"}},
		},
	}
	var buf bytes.Buffer
	if err := writeQuotientGraphML(&buf, r); err != nil {
		t.Fatalf("writeQuotientGraphML: %v", err)
	}
	got := buf.String()
	for _, want := range []string{
		`<?xml version="1.0" encoding="UTF-8"?>`,
		`<graphml xmlns="http://graphml.graphdrawing.org/xmlns"`,
		`attr.name="size"`,
		`attr.name="members"`,
		`attr.name="weight"`,
		`edgedefault="directed"`,
		`community_0`,
		`community_1`,
		`<data key="e_weight">3</data>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("GraphML missing %q\n--- got ---\n%s", want, got)
		}
	}
	// Validate well-formed XML via encoding/xml.
	dec := xml.NewDecoder(strings.NewReader(got))
	for {
		_, err := dec.Token()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			t.Fatalf("GraphML parse: %v\n%s", err, got)
		}
	}
}

// threeCommunityQuotientGraph builds a synthetic 3-community graph:
// three K_4 cliques A, B, C plus two directed bridge edges A0->B0 and
// B0->C0. Used to assert quotient collapses each clique to a super-node
// with two inter-community super-edges.
func threeCommunityQuotientGraph() mgraph.JSON {
	doc := mgraph.JSON{Version: mgraph.CurrentJSONVersion}
	addClique := func(prefix string, k int) {
		for i := 0; i < k; i++ {
			doc.Nodes = append(doc.Nodes, mgraph.Node{
				ID:   fmt.Sprintf("%s%d", prefix, i),
				Kind: mgraph.NodeFunction,
				Name: fmt.Sprintf("%s%d", prefix, i),
			})
		}
		for i := 0; i < k; i++ {
			for j := i + 1; j < k; j++ {
				doc.Edges = append(doc.Edges, mgraph.Edge{
					From: fmt.Sprintf("%s%d", prefix, i),
					To:   fmt.Sprintf("%s%d", prefix, j),
					Kind: mgraph.EdgeCalls,
				})
			}
		}
	}
	addClique("A", 4)
	addClique("B", 4)
	addClique("C", 4)
	doc.Edges = append(doc.Edges,
		mgraph.Edge{From: "A0", To: "B0", Kind: mgraph.EdgeCalls},
		mgraph.Edge{From: "B0", To: "C0", Kind: mgraph.EdgeCalls},
	)
	return doc
}

// threeCommunityMembers is the ground-truth partition for
// threeCommunityQuotientGraph. Real-world quotient input comes from
// `archmotif communities`, but the inputs are decoupled so tests can
// pin behaviour without invoking the Python helper.
func threeCommunityMembers() map[string][]string {
	return map[string][]string{
		"community_0": {"A0", "A1", "A2", "A3"},
		"community_1": {"B0", "B1", "B2", "B3"},
		"community_2": {"C0", "C1", "C2", "C3"},
	}
}

// writeQuotientGraphFixture serialises doc to a tmp graph.json and
// returns the path. Same shape as `archmotif graph --format=json`.
func writeQuotientGraphFixture(t *testing.T, doc mgraph.JSON) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "graph.json")
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

// writeQuotientCommunitiesFixture serialises members to a tmp
// communities.json mimicking the schema produced by `archmotif
// communities` (only the "members" field is required by quotient).
func writeQuotientCommunitiesFixture(t *testing.T, members map[string][]string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "communities.json")
	payload := map[string]any{
		"n_communities":             len(members),
		"modularity_q":              0.6,
		"members":                   members,
		"intra_edges_per_community": map[string]int{},
		"inter_edges_per_pair":      map[string]int{},
	}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}
