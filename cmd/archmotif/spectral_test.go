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

// TestSpectralCommand_NoArg confirms the command surfaces usage on
// stderr and exits 2 when called without a path.
func TestSpectralCommand_NoArg(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"spectral"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "Usage") {
		t.Errorf("stderr missing Usage:\n%s", stderr.String())
	}
}

// TestSpectralCommand_BadFormat rejects unknown --format values.
func TestSpectralCommand_BadFormat(t *testing.T) {
	path := writeSpectralFixture(t, threeClusterGraph())
	var stdout, stderr bytes.Buffer
	code := run([]string{"spectral", "--format", "xml", path}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
}

// TestSpectralCommand_MissingFile surfaces a read error.
func TestSpectralCommand_MissingFile(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"spectral", "/nonexistent/graph.json"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr=%s", code, stderr.String())
	}
}

// TestSpectralCommand_ThreeClusterDetectsK3 is the headline acceptance
// criterion: on a synthetic 3-cluster graph the eigengap analysis
// surfaces k=3, modularity Q for the resulting partition is positive,
// and the JSON schema is honoured.
func TestSpectralCommand_ThreeClusterDetectsK3(t *testing.T) {
	path := writeSpectralFixture(t, threeClusterGraph())
	var stdout, stderr bytes.Buffer
	code := run([]string{"spectral", path}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", code, stderr.String())
	}

	var report spectralReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode JSON: %v\nraw=\n%s", err, stdout.String())
	}
	if report.NNodes != 12 {
		t.Errorf("n_nodes = %d, want 12", report.NNodes)
	}
	if len(report.LaplacianEigenvalues) != 12 {
		t.Errorf("laplacian_eigenvalues len = %d, want 12", len(report.LaplacianEigenvalues))
	}
	// Eigenvalues must be sorted ascending.
	for i := 1; i < len(report.LaplacianEigenvalues); i++ {
		if report.LaplacianEigenvalues[i] < report.LaplacianEigenvalues[i-1]-1e-9 {
			t.Errorf("laplacian_eigenvalues not sorted ascending at i=%d: %v",
				i, report.LaplacianEigenvalues)
			break
		}
	}
	// Three connected clusters joined by a single bridge edge each
	// give a Laplacian with one zero eigenvalue and a small second
	// eigenvalue: 3 → expected detection.
	if report.EigengapK != 3 {
		t.Errorf("eigengap_k = %d, want 3 (synthetic 3-cluster graph); eigenvalues=%v",
			report.EigengapK, report.LaplacianEigenvalues)
	}
	if report.EigengapRatio <= 1.0 {
		t.Errorf("eigengap_ratio = %v, expected > 1.0", report.EigengapRatio)
	}
	if report.AlgebraicConnectivity <= 0 {
		t.Errorf("algebraic_connectivity = %v, want > 0 (graph is connected)",
			report.AlgebraicConnectivity)
	}
	// Modularity Q for the detected partition must be positive on a
	// well-separated 3-cluster topology (random partition would give
	// Q ≈ 0).
	if report.ModularityQAtK <= 0 {
		t.Errorf("modularity_q_at_k = %v, want > 0", report.ModularityQAtK)
	}
	// SVD singular values descending, knee surfaces.
	for i := 1; i < len(report.SVDSingularValues); i++ {
		if report.SVDSingularValues[i] > report.SVDSingularValues[i-1]+1e-9 {
			t.Errorf("svd_singular_values not descending at i=%d: %v",
				i, report.SVDSingularValues)
			break
		}
	}
	if report.SVDKnee < 1 {
		t.Errorf("svd_knee = %d, want >= 1", report.SVDKnee)
	}
}

// TestSpectralCommand_TextFormat exercises the human-readable path.
func TestSpectralCommand_TextFormat(t *testing.T) {
	path := writeSpectralFixture(t, threeClusterGraph())
	var stdout, stderr bytes.Buffer
	code := run([]string{"spectral", "--format", "text", path}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"spectral report",
		"algebraic connectivity",
		"eigengap",
		"svd knee",
		"modularity Q",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("text output missing %q\n--- output ---\n%s", want, out)
		}
	}
}

// TestSpectralReport_PureClique4 checks numerics on a closed-form
// case: K_4 has Laplacian eigenvalues {0, 4, 4, 4}.
func TestSpectralReport_PureClique4(t *testing.T) {
	doc := mgraph.JSON{Version: mgraph.CurrentJSONVersion}
	for i := 0; i < 4; i++ {
		doc.Nodes = append(doc.Nodes, mgraph.Node{
			ID:   fmt.Sprintf("n%d", i),
			Kind: mgraph.NodeFunction,
		})
	}
	for i := 0; i < 4; i++ {
		for j := i + 1; j < 4; j++ {
			doc.Edges = append(doc.Edges, mgraph.Edge{
				From: fmt.Sprintf("n%d", i),
				To:   fmt.Sprintf("n%d", j),
				Kind: mgraph.EdgeCalls,
			})
		}
	}
	report, err := computeSpectralReport(doc, 12)
	if err != nil {
		t.Fatalf("computeSpectralReport: %v", err)
	}
	if math.Abs(report.AlgebraicConnectivity-4.0) > 1e-9 {
		t.Fatalf("λ_2 = %v, want 4.0 for K_4", report.AlgebraicConnectivity)
	}
	for i, v := range report.LaplacianEigenvalues[1:] {
		if math.Abs(v-4.0) > 1e-9 {
			t.Errorf("eigenvalue %d = %v, want 4.0 for K_4", i+1, v)
		}
	}
}

// TestSpectralReport_EmptyGraph returns an empty report rather than
// failing.
func TestSpectralReport_EmptyGraph(t *testing.T) {
	doc := mgraph.JSON{Version: mgraph.CurrentJSONVersion}
	report, err := computeSpectralReport(doc, 12)
	if err != nil {
		t.Fatalf("computeSpectralReport empty: %v", err)
	}
	if report.NNodes != 0 || report.NEdges != 0 {
		t.Errorf("empty graph: NNodes=%d NEdges=%d", report.NNodes, report.NEdges)
	}
	if len(report.LaplacianEigenvalues) != 0 {
		t.Errorf("empty graph eigenvalues: %v", report.LaplacianEigenvalues)
	}
}

// threeClusterGraph builds a synthetic 3-cluster graph: three K_4
// cliques connected pairwise by a single bridge edge each. This is
// the canonical spectral-clustering test case — the top of the
// Laplacian spectrum has 3 small eigenvalues (one zero, two near-zero
// for the well-separated clusters) followed by a large gap.
//
// Layout:
//
//	cluster A (4 nodes, K_4): A0 A1 A2 A3
//	cluster B (4 nodes, K_4): B0 B1 B2 B3
//	cluster C (4 nodes, K_4): C0 C1 C2 C3
//	bridges: A0-B0, B0-C0, C0-A0
func threeClusterGraph() mgraph.JSON {
	doc := mgraph.JSON{Version: mgraph.CurrentJSONVersion}
	addClique := func(prefix string) {
		for i := 0; i < 4; i++ {
			doc.Nodes = append(doc.Nodes, mgraph.Node{
				ID:   fmt.Sprintf("%s%d", prefix, i),
				Kind: mgraph.NodeFunction,
				Name: fmt.Sprintf("%s%d", prefix, i),
			})
		}
		for i := 0; i < 4; i++ {
			for j := i + 1; j < 4; j++ {
				doc.Edges = append(doc.Edges, mgraph.Edge{
					From: fmt.Sprintf("%s%d", prefix, i),
					To:   fmt.Sprintf("%s%d", prefix, j),
					Kind: mgraph.EdgeCalls,
				})
			}
		}
	}
	addClique("A")
	addClique("B")
	addClique("C")
	// Bridge edges (one per inter-cluster pair).
	doc.Edges = append(doc.Edges,
		mgraph.Edge{From: "A0", To: "B0", Kind: mgraph.EdgeCalls},
		mgraph.Edge{From: "B0", To: "C0", Kind: mgraph.EdgeCalls},
		mgraph.Edge{From: "C0", To: "A0", Kind: mgraph.EdgeCalls},
	)
	return doc
}

// writeSpectralFixture serialises doc to a tmp graph.json and returns
// the path. The on-disk shape matches what `archmotif graph
// --format=json` emits.
func writeSpectralFixture(t *testing.T, doc mgraph.JSON) string {
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
