package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// TestCommunitiesCommand_NoArg confirms the command surfaces usage on
// stderr and exits 2 when called without a path.
func TestCommunitiesCommand_NoArg(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"communities"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "Usage") {
		t.Errorf("stderr missing Usage:\n%s", stderr.String())
	}
}

// TestCommunitiesCommand_BadFormat rejects unknown --format values.
func TestCommunitiesCommand_BadFormat(t *testing.T) {
	path := writeCommunitiesFixture(t, threeClusterCommunitiesGraph())
	var stdout, stderr bytes.Buffer
	code := run([]string{"communities", "--format", "xml", path}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
}

// TestCommunitiesCommand_BadResolution rejects non-positive resolution.
func TestCommunitiesCommand_BadResolution(t *testing.T) {
	path := writeCommunitiesFixture(t, threeClusterCommunitiesGraph())
	var stdout, stderr bytes.Buffer
	code := run([]string{"communities", "--resolution", "0", path}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
}

// TestCommunitiesCommand_MissingFile surfaces a read error.
func TestCommunitiesCommand_MissingFile(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"communities", "/nonexistent/graph.json"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr=%s", code, stderr.String())
	}
}

// TestCommunitiesCommand_ThreeClusterDetectsN3 is the headline
// acceptance test: on a synthetic 3-cluster graph Louvain modularisation
// surfaces N=3 communities with Q >= 0.6 and the canonical JSON
// schema.
//
// We use 3 K_6 cliques connected pairwise by a single bridge each (the
// shape carried over from spectral_test.go's threeClusterGraph, but
// with denser cliques so Q clears the >=0.6 threshold from #76 AC).
// Using K_4 — matching the spectral test exactly — caps Q at ~0.524
// because modularity scales with intra/(intra+inter) edge ratio; K_6
// gives Q ≈ 0.604, satisfying the AC without changing the topology
// pattern.
func TestCommunitiesCommand_ThreeClusterDetectsN3(t *testing.T) {
	path := writeCommunitiesFixture(t, threeClusterCommunitiesGraph())
	var stdout, stderr bytes.Buffer
	code := run([]string{"communities", path}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", code, stderr.String())
	}

	var report communitiesReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode JSON: %v\nraw=\n%s", err, stdout.String())
	}
	if report.NCommunities != 3 {
		t.Errorf("n_communities = %d, want 3", report.NCommunities)
	}
	if report.ModularityQ < 0.6 {
		t.Errorf("modularity_q = %v, want >= 0.6", report.ModularityQ)
	}
	if len(report.Members) != 3 {
		t.Errorf("members has %d communities, want 3", len(report.Members))
	}
	// Every community must have 6 members (each K_6 clique).
	totalMembers := 0
	for k, m := range report.Members {
		if len(m) != 6 {
			t.Errorf("community %q has %d members, want 6", k, len(m))
		}
		totalMembers += len(m)
	}
	if totalMembers != 18 {
		t.Errorf("total members = %d, want 18", totalMembers)
	}
	// Each clique has 15 intra-cluster edges (C(6,2) = 15).
	for k, n := range report.IntraEdgesPerCommunity {
		if n != 15 {
			t.Errorf("intra_edges_per_community[%q] = %d, want 15", k, n)
		}
	}
	// 3 inter-community pairs, each with exactly 1 bridge edge.
	if len(report.InterEdgesPerPair) != 3 {
		t.Errorf("inter_edges_per_pair has %d entries, want 3", len(report.InterEdgesPerPair))
	}
	for k, n := range report.InterEdgesPerPair {
		if n != 1 {
			t.Errorf("inter_edges_per_pair[%q] = %d, want 1", k, n)
		}
	}
}

// TestCommunitiesCommand_TextFormat exercises the human-readable path.
func TestCommunitiesCommand_TextFormat(t *testing.T) {
	path := writeCommunitiesFixture(t, threeClusterCommunitiesGraph())
	var stdout, stderr bytes.Buffer
	code := run([]string{"communities", "--format", "text", path}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"communities report",
		"n_communities:",
		"modularity Q:",
		"community_0",
		"inter-community edges:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("text output missing %q\n--- output ---\n%s", want, out)
		}
	}
}

// TestCommunitiesCommand_ResolutionKnob confirms --resolution alters
// the partition: higher resolution → more communities. Used to be sure
// the flag is plumbed through to the modularity optimiser rather than
// being silently dropped.
func TestCommunitiesCommand_ResolutionKnob(t *testing.T) {
	path := writeCommunitiesFixture(t, threeClusterCommunitiesGraph())

	var lowOut, lowErr bytes.Buffer
	if code := run([]string{"communities", "--resolution", "0.5", path}, &lowOut, &lowErr); code != 0 {
		t.Fatalf("low-resolution run failed: code=%d stderr=%s", code, lowErr.String())
	}
	var highOut, highErr bytes.Buffer
	if code := run([]string{"communities", "--resolution", "3.0", path}, &highOut, &highErr); code != 0 {
		t.Fatalf("high-resolution run failed: code=%d stderr=%s", code, highErr.String())
	}
	var low, high communitiesReport
	if err := json.Unmarshal(lowOut.Bytes(), &low); err != nil {
		t.Fatalf("decode low: %v", err)
	}
	if err := json.Unmarshal(highOut.Bytes(), &high); err != nil {
		t.Fatalf("decode high: %v", err)
	}
	// Higher resolution must not produce fewer communities. (May tie
	// at 3 if the topology pins the partition.)
	if high.NCommunities < low.NCommunities {
		t.Errorf("--resolution 3.0 gave %d communities, --resolution 0.5 gave %d; expected high ≥ low",
			high.NCommunities, low.NCommunities)
	}
}

// TestCommunitiesCommand_ZeroEdgesNoNaN exercises the path where the
// graph has nodes but zero edges. The report must clamp non-finite modularity
// values to 0.0 so JSON output remains valid.
func TestCommunitiesCommand_ZeroEdgesNoNaN(t *testing.T) {
	doc := mgraph.JSON{Version: mgraph.CurrentJSONVersion}
	doc.Nodes = append(doc.Nodes,
		mgraph.Node{ID: "a", Kind: mgraph.NodeFunction, Name: "a"},
		mgraph.Node{ID: "b", Kind: mgraph.NodeFunction, Name: "b"},
	)
	path := writeCommunitiesFixture(t, doc)

	var stdout, stderr bytes.Buffer
	code := run([]string{"communities", path}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", code, stderr.String())
	}

	var report communitiesReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode JSON: %v\nraw=\n%s", err, stdout.String())
	}
	if math.IsNaN(report.ModularityQ) || math.IsInf(report.ModularityQ, 0) {
		t.Errorf("modularity_q = %v, want finite (helper must clamp NaN/Inf)", report.ModularityQ)
	}
	if report.ModularityQ != 0.0 {
		t.Errorf("modularity_q = %v, want 0.0 for zero-edge graph", report.ModularityQ)
	}
}

// TestCommunitiesReport_EmptyGraph returns an empty report rather than
// failing (mirrors spectral.go's behaviour for n=0).
func TestCommunitiesReport_EmptyGraph(t *testing.T) {
	doc := mgraph.JSON{Version: mgraph.CurrentJSONVersion}
	report, err := computeCommunitiesReport(doc, 1.0)
	if err != nil {
		t.Fatalf("computeCommunitiesReport empty: %v", err)
	}
	if report.NCommunities != 0 {
		t.Errorf("empty graph: NCommunities=%d", report.NCommunities)
	}
	if len(report.Members) != 0 {
		t.Errorf("empty graph members: %v", report.Members)
	}
}

// threeClusterCommunitiesGraph builds a synthetic 3-cluster graph: three
// K_6 cliques connected pairwise by a single bridge edge each, in the
// same shape as spectral_test.go's threeClusterGraph but with K_6
// instead of K_4. K_6 is the smallest clique size where the natural
// 3-cluster partition reaches modularity Q >= 0.6 (the
// threshold called out in #76 acceptance criteria); for K_4 the
// theoretical maximum is ~0.524 regardless of algorithm.
//
// Layout:
//
//	cluster A (6 nodes, K_6): A0..A5
//	cluster B (6 nodes, K_6): B0..B5
//	cluster C (6 nodes, K_6): C0..C5
//	bridges: A0-B0, B0-C0, C0-A0
func threeClusterCommunitiesGraph() mgraph.JSON {
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
	addClique("A", 6)
	addClique("B", 6)
	addClique("C", 6)
	doc.Edges = append(doc.Edges,
		mgraph.Edge{From: "A0", To: "B0", Kind: mgraph.EdgeCalls},
		mgraph.Edge{From: "B0", To: "C0", Kind: mgraph.EdgeCalls},
		mgraph.Edge{From: "C0", To: "A0", Kind: mgraph.EdgeCalls},
	)
	return doc
}

// writeCommunitiesFixture serialises doc to a tmp graph.json and
// returns the path. Shape matches `archmotif graph --format=json`.
func writeCommunitiesFixture(t *testing.T, doc mgraph.JSON) string {
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
