package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// TestCurvatureCommand_NoArg confirms missing-graph surfaces usage on
// stderr and exits 2.
func TestCurvatureCommand_NoArg(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"curvature"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "Usage") {
		t.Errorf("stderr missing Usage:\n%s", stderr.String())
	}
}

// TestCurvatureCommand_BadK rejects K < 1.
func TestCurvatureCommand_BadK(t *testing.T) {
	path := writeCurvatureFixture(t, triangleStarGraph())
	var stdout, stderr bytes.Buffer
	code := run([]string{"curvature", "-k", "0", path}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2; stderr=%s", code, stderr.String())
	}
}

// TestCurvatureCommand_BadFormat rejects unknown --format values.
func TestCurvatureCommand_BadFormat(t *testing.T) {
	path := writeCurvatureFixture(t, triangleStarGraph())
	var stdout, stderr bytes.Buffer
	code := run([]string{"curvature", "--format", "xml", path}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
}

// TestCurvatureCommand_MissingFile surfaces a read error.
func TestCurvatureCommand_MissingFile(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"curvature", "/nonexistent/graph.json"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr=%s", code, stderr.String())
	}
}

// TestCurvatureCommand_TriangleVsStar is the headline acceptance test
// for the combinatorial Forman-Ricci formula.
//
// Topology: a triangle {T0,T1,T2} (all pairwise) plus a "star spoke"
// edge T0->L (L is a degree-1 leaf attached only to T0).
//
// Expected κ:
//   - triangle edges have deg(u)=deg(v)=3 (T0) or =2 (T1,T2) — for the
//     T1-T2 edge: 4 − 2 − 2 + 3·1 = 3
//   - T0-T1 edge:  4 − 3 − 2 + 3·1 = 2
//   - T0-T2 edge:  4 − 3 − 2 + 3·1 = 2
//   - T0-L spoke:  4 − 3 − 1 + 3·0 = 0
//
// So the spoke is the most-negative (well, least-positive) and the
// triangle edges are more positive. The spoke is the "bridge" — removing
// it disconnects the leaf.
func TestCurvatureCommand_TriangleVsStar(t *testing.T) {
	path := writeCurvatureFixture(t, triangleStarGraph())
	var stdout, stderr bytes.Buffer
	code := run([]string{"curvature", "-k", "2", path}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", code, stderr.String())
	}

	var report curvatureReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode JSON: %v\nraw=\n%s", err, stdout.String())
	}
	if report.NEdges != 4 {
		t.Errorf("n_edges = %d, want 4", report.NEdges)
	}
	if got := len(report.Edges); got != 4 {
		t.Errorf("edges has %d entries, want 4", got)
	}

	// Index by endpoint pair (unordered) for assertions.
	get := func(a, b string) (float64, bool) {
		for _, e := range report.Edges {
			if (e.Src == a && e.Dst == b) || (e.Src == b && e.Dst == a) {
				return e.KappaForman, true
			}
		}
		return 0, false
	}
	for _, c := range []struct {
		a, b string
		want float64
	}{
		{"T1", "T2", 3},
		{"T0", "T1", 2},
		{"T0", "T2", 2},
		{"T0", "L", 0},
	} {
		got, ok := get(c.a, c.b)
		if !ok {
			t.Errorf("edge %s-%s missing", c.a, c.b)
			continue
		}
		if got != c.want {
			t.Errorf("κ(%s-%s) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
	// most_negative_k[0] should be the spoke (most-negative κ).
	if len(report.MostNegativeK) == 0 {
		t.Fatalf("most_negative_k empty")
	}
	mn := report.MostNegativeK[0]
	if !(mn.Src == "T0" && mn.Dst == "L") {
		t.Errorf("most_negative_k[0] = %s->%s, want T0->L", mn.Src, mn.Dst)
	}
	// most_positive_k[0] should be T1-T2 with κ=3.
	if len(report.MostPositiveK) == 0 {
		t.Fatalf("most_positive_k empty")
	}
	mp := report.MostPositiveK[0]
	if mp.KappaForman != 3 {
		t.Errorf("most_positive_k[0] κ = %v, want 3", mp.KappaForman)
	}
	// -k=2 → both tail slices must have len 2.
	if len(report.MostNegativeK) != 2 {
		t.Errorf("most_negative_k len = %d, want 2", len(report.MostNegativeK))
	}
	if len(report.MostPositiveK) != 2 {
		t.Errorf("most_positive_k len = %d, want 2", len(report.MostPositiveK))
	}
}

// TestCurvatureCommand_TextFormat exercises the human-readable path.
func TestCurvatureCommand_TextFormat(t *testing.T) {
	path := writeCurvatureFixture(t, triangleStarGraph())
	var stdout, stderr bytes.Buffer
	code := run([]string{"curvature", "--format", "text", path}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"curvature report",
		"nodes:",
		"edges:",
		"most negative",
		"most positive",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("text output missing %q\n--- output ---\n%s", want, out)
		}
	}
}

// TestCurvatureCommand_KClamp confirms that -k larger than |edges|
// doesn't crash and returns all edges in each tail.
func TestCurvatureCommand_KClamp(t *testing.T) {
	path := writeCurvatureFixture(t, triangleStarGraph()) // 4 edges
	var stdout, stderr bytes.Buffer
	code := run([]string{"curvature", "-k", "100", path}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", code, stderr.String())
	}
	var report curvatureReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	if len(report.MostNegativeK) != report.NEdges {
		t.Errorf("most_negative_k len = %d, want %d", len(report.MostNegativeK), report.NEdges)
	}
	if len(report.MostPositiveK) != report.NEdges {
		t.Errorf("most_positive_k len = %d, want %d", len(report.MostPositiveK), report.NEdges)
	}
}

// TestCurvatureReport_EmptyGraph asserts the empty input emits zeros
// rather than panicking. Mirrors the convention in spectral/communities.
func TestCurvatureReport_EmptyGraph(t *testing.T) {
	doc := mgraph.JSON{Version: mgraph.CurrentJSONVersion}
	r := computeCurvatureReport(doc, 10)
	if r.NNodes != 0 || r.NEdges != 0 {
		t.Errorf("empty graph: nNodes=%d nEdges=%d", r.NNodes, r.NEdges)
	}
	if len(r.Edges) != 0 || len(r.MostNegativeK) != 0 || len(r.MostPositiveK) != 0 {
		t.Errorf("empty graph: non-empty slices")
	}
}

// TestCurvatureReport_SingleNode covers a graph with one node and no
// edges — Forman-Ricci is undefined on no edges, the report must be
// empty rather than failing.
func TestCurvatureReport_SingleNode(t *testing.T) {
	doc := mgraph.JSON{Version: mgraph.CurrentJSONVersion}
	doc.Nodes = append(doc.Nodes, mgraph.Node{ID: "solo", Kind: mgraph.NodeFunction})
	r := computeCurvatureReport(doc, 10)
	if r.NNodes != 1 {
		t.Errorf("nNodes = %d, want 1", r.NNodes)
	}
	if r.NEdges != 0 || len(r.Edges) != 0 {
		t.Errorf("single node has edges: %+v", r.Edges)
	}
}

// TestCurvatureReport_ParallelAndSelfEdgesCollapse confirms that
// duplicate / self / reverse edges all collapse to a single undirected
// projected edge for κ purposes.
func TestCurvatureReport_ParallelAndSelfEdgesCollapse(t *testing.T) {
	doc := mgraph.JSON{Version: mgraph.CurrentJSONVersion}
	doc.Nodes = append(doc.Nodes,
		mgraph.Node{ID: "a", Kind: mgraph.NodeFunction},
		mgraph.Node{ID: "b", Kind: mgraph.NodeFunction},
	)
	doc.Edges = append(doc.Edges,
		mgraph.Edge{From: "a", To: "b", Kind: mgraph.EdgeCalls},
		mgraph.Edge{From: "a", To: "b", Kind: mgraph.EdgeReferences}, // parallel (different kind)
		mgraph.Edge{From: "b", To: "a", Kind: mgraph.EdgeCalls},      // reverse
		mgraph.Edge{From: "a", To: "a", Kind: mgraph.EdgeCalls},      // self-edge
	)
	r := computeCurvatureReport(doc, 10)
	if r.NEdges != 1 {
		t.Errorf("nEdges = %d, want 1 (parallel/self collapse)", r.NEdges)
	}
	if len(r.Edges) != 1 {
		t.Fatalf("edges = %d, want 1", len(r.Edges))
	}
	// deg(a) = deg(b) = 1, triangles = 0 → κ = 4 − 1 − 1 + 0 = 2
	if r.Edges[0].KappaForman != 2 {
		t.Errorf("κ = %v, want 2", r.Edges[0].KappaForman)
	}
}

// triangleStarGraph builds a 4-node graph: a triangle T0-T1-T2 plus a
// leaf L attached to T0 via a single spoke edge T0->L. Used by both
// the headline acceptance test and the most-negative / most-positive
// assertions.
func triangleStarGraph() mgraph.JSON {
	doc := mgraph.JSON{Version: mgraph.CurrentJSONVersion}
	for _, id := range []string{"T0", "T1", "T2", "L"} {
		doc.Nodes = append(doc.Nodes, mgraph.Node{
			ID:   id,
			Kind: mgraph.NodeFunction,
			Name: id,
		})
	}
	doc.Edges = append(doc.Edges,
		mgraph.Edge{From: "T0", To: "T1", Kind: mgraph.EdgeCalls},
		mgraph.Edge{From: "T1", To: "T2", Kind: mgraph.EdgeCalls},
		mgraph.Edge{From: "T0", To: "T2", Kind: mgraph.EdgeCalls},
		mgraph.Edge{From: "T0", To: "L", Kind: mgraph.EdgeCalls},
	)
	return doc
}

// writeCurvatureFixture serialises doc to a tmp graph.json and returns
// the path. Shape matches `archmotif graph --format=json`.
func writeCurvatureFixture(t *testing.T, doc mgraph.JSON) string {
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

// pretty-print helper for debugging κ output. Kept around so failing
// tests can include a readable dump via -v.
func dumpEdges(r curvatureReport) string {
	var b strings.Builder
	for _, e := range r.Edges {
		fmt.Fprintf(&b, "  %s -> %s κ=%.2f\n", e.Src, e.Dst, e.KappaForman)
	}
	return b.String()
}

var _ = dumpEdges
