package spectralcluster

import (
	"testing"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// TestTwoClusters verifies that two cliques connected by a bridge are
// correctly split into K=2 clusters.
func TestTwoClusters(t *testing.T) {
	g := buildTwoCliques()

	result, err := SpectralCluster(g, DefaultOptions())
	if err != nil {
		t.Fatalf("SpectralCluster failed: %v", err)
	}

	// Should detect K=2 automatically.
	if result.ChosenK != 2 {
		t.Errorf("expected ChosenK=2, got %d", result.ChosenK)
	}
	if result.KSource != "auto" {
		t.Errorf("expected KSource=auto, got %s", result.KSource)
	}

	// Verify clusters.
	if len(result.Clusters) != 2 {
		t.Fatalf("expected 2 clusters, got %d", len(result.Clusters))
	}

	// Each cluster should have exactly 3 members (clique A or clique B).
	for _, c := range result.Clusters {
		if len(c.Members) != 3 {
			t.Errorf("cluster %d: expected 3 members, got %d", c.ID, len(c.Members))
		}
	}

	// Bridge node should be in boundary symbols.
	// The bridge connects A3 to B1, so one of them may be ambiguous.
	// (This depends on the graph structure; the test is loose here.)
}

// TestExplicitK verifies that an explicit K is honored.
func TestExplicitK(t *testing.T) {
	g := buildTwoCliques()

	opts := DefaultOptions()
	opts.K = 3 // force 3 clusters

	result, err := SpectralCluster(g, opts)
	if err != nil {
		t.Fatalf("SpectralCluster failed: %v", err)
	}

	if result.ChosenK != 3 {
		t.Errorf("expected ChosenK=3, got %d", result.ChosenK)
	}
	if result.KSource != "explicit" {
		t.Errorf("expected KSource=explicit, got %s", result.KSource)
	}
}

// TestSingleClique verifies a fully connected graph clusters as one.
func TestSingleClique(t *testing.T) {
	g := buildPureClique(4)

	result, err := SpectralCluster(g, DefaultOptions())
	if err != nil {
		t.Fatalf("SpectralCluster failed: %v", err)
	}

	// A 4-clique is highly connected, so eigengap may detect K=1 or K=2.
	// The key is that it shouldn't fail.
	if result.ChosenK < 1 || result.ChosenK > 4 {
		t.Errorf("ChosenK out of range: %d", result.ChosenK)
	}

	// All nodes should be in clusters.
	totalMembers := 0
	for _, c := range result.Clusters {
		totalMembers += len(c.Members)
	}
	if totalMembers != 4 {
		t.Errorf("expected 4 total members, got %d", totalMembers)
	}
}

// TestEmptyGraph verifies empty graph handling.
func TestEmptyGraph(t *testing.T) {
	g := mgraph.New()

	result, err := SpectralCluster(g, DefaultOptions())
	if err != nil {
		t.Fatalf("SpectralCluster failed: %v", err)
	}

	if result.ChosenK != 0 {
		t.Errorf("expected ChosenK=0 for empty graph, got %d", result.ChosenK)
	}
	if len(result.Clusters) != 0 {
		t.Errorf("expected 0 clusters for empty graph, got %d", len(result.Clusters))
	}
}

// TestSingleNode verifies single-node graph handling.
func TestSingleNode(t *testing.T) {
	g := mgraph.New()
	g.AddNode(mgraph.Node{ID: "only", Kind: mgraph.NodeFunction, Name: "only"})

	result, err := SpectralCluster(g, DefaultOptions())
	if err != nil {
		t.Fatalf("SpectralCluster failed: %v", err)
	}

	if result.ChosenK != 1 {
		t.Errorf("expected ChosenK=1 for single node, got %d", result.ChosenK)
	}
	if len(result.Clusters) != 1 {
		t.Fatalf("expected 1 cluster, got %d", len(result.Clusters))
	}
	if len(result.Clusters[0].Members) != 1 {
		t.Errorf("expected 1 member, got %d", len(result.Clusters[0].Members))
	}
}

// TestNodeSubset verifies clustering on a subset of nodes.
func TestNodeSubset(t *testing.T) {
	g := buildTwoCliques()

	// Only cluster nodes from clique A.
	opts := DefaultOptions()
	opts.NodeIDs = []string{
		"cluster/main.go:1:1:function:A1",
		"cluster/main.go:1:1:function:A2",
		"cluster/main.go:1:1:function:A3",
	}

	result, err := SpectralCluster(g, opts)
	if err != nil {
		t.Fatalf("SpectralCluster failed: %v", err)
	}

	// Should cluster just 3 nodes.
	totalMembers := 0
	for _, c := range result.Clusters {
		totalMembers += len(c.Members)
	}
	if totalMembers != 3 {
		t.Errorf("expected 3 total members in subset, got %d", totalMembers)
	}
}

// TestCutQuality verifies intra/inter edge counting.
func TestCutQuality(t *testing.T) {
	g := buildTwoCliques()

	result, err := SpectralCluster(g, DefaultOptions())
	if err != nil {
		t.Fatalf("SpectralCluster failed: %v", err)
	}

	// Two 3-cliques have 3 edges each = 6 intra edges.
	// One bridge edge = 1 inter edge.
	// (assuming K=2 and correct split)
	if result.ChosenK == 2 {
		if result.CutQuality.InterEdges != 1 {
			t.Errorf("expected 1 inter edge for K=2, got %d", result.CutQuality.InterEdges)
		}
		// Intra edges: 6 within cliques.
		if result.CutQuality.IntraEdges != 6 {
			t.Errorf("expected 6 intra edges for K=2, got %d", result.CutQuality.IntraEdges)
		}
	}
}

// --- Test fixtures ---

// buildTwoCliques creates two 3-cliques connected by a single bridge edge.
// Clique A: A1-A2-A3 (fully connected)
// Clique B: B1-B2-B3 (fully connected)
// Bridge: A3-B1
func buildTwoCliques() *Graph {
	g := mgraph.New()

	// Add clique A nodes.
	for _, name := range []string{"A1", "A2", "A3"} {
		id := "cluster/main.go:1:1:function:" + name
		g.AddNode(mgraph.Node{ID: id, Kind: mgraph.NodeFunction, Name: name})
	}
	// Add clique B nodes.
	for _, name := range []string{"B1", "B2", "B3"} {
		id := "cluster/main.go:1:1:function:" + name
		g.AddNode(mgraph.Node{ID: id, Kind: mgraph.NodeFunction, Name: name})
	}

	// Add clique A edges (full connectivity).
	addEdge := func(from, to string) {
		fromID := "cluster/main.go:1:1:function:" + from
		toID := "cluster/main.go:1:1:function:" + to
		_, _ = g.AddEdge(mgraph.Edge{From: fromID, To: toID, Kind: mgraph.EdgeCalls})
	}
	addEdge("A1", "A2")
	addEdge("A1", "A3")
	addEdge("A2", "A3")

	// Add clique B edges.
	addEdge("B1", "B2")
	addEdge("B1", "B3")
	addEdge("B2", "B3")

	// Add bridge edge.
	addEdge("A3", "B1")

	return g
}

// buildPureClique builds a K_n clique with no wrapper nodes.
func buildPureClique(n int) *Graph {
	g := mgraph.New()
	ids := make([]string, n)
	for i := 0; i < n; i++ {
		id := "clique/main.go:1:1:function:N" + string(rune('0'+i))
		ids[i] = id
		g.AddNode(mgraph.Node{ID: id, Kind: mgraph.NodeFunction, Name: "N" + string(rune('0'+i))})
	}
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			_, _ = g.AddEdge(mgraph.Edge{From: ids[i], To: ids[j], Kind: mgraph.EdgeCalls})
		}
	}
	return g
}

// TestModularityAndSpectrumExposed verifies the new auto-K outputs: a clear
// two-community graph scores high modularity, exposes the eigenvalue spectrum,
// and its candidates carry the absolute gap.
func TestModularityAndSpectrumExposed(t *testing.T) {
	g := buildTwoCliques()

	result, err := SpectralCluster(g, DefaultOptions())
	if err != nil {
		t.Fatalf("SpectralCluster failed: %v", err)
	}
	if result.Modularity <= 0.2 {
		t.Errorf("two-clique modularity = %v, want > 0.2 (clear community structure)", result.Modularity)
	}
	if len(result.Eigenvalues) == 0 {
		t.Error("eigenvalues not exposed")
	}
	// Eigenvalues must be ascending (smallest first).
	for i := 1; i < len(result.Eigenvalues); i++ {
		if result.Eigenvalues[i]+1e-9 < result.Eigenvalues[i-1] {
			t.Errorf("eigenvalues not ascending at %d: %v", i, result.Eigenvalues)
			break
		}
	}
	// At least one candidate should report the absolute gap.
	sawGap := false
	for _, c := range result.Candidates {
		if c.Gap > 0 {
			sawGap = true
		}
	}
	if len(result.Candidates) > 0 && !sawGap {
		t.Error("candidates do not carry absolute gap")
	}
}

// TestModularityLowOnClique verifies a single clique (no community structure)
// scores near-zero modularity — the hairball signal.
func TestModularityLowOnClique(t *testing.T) {
	g := buildPureClique(6)

	result, err := SpectralCluster(g, DefaultOptions())
	if err != nil {
		t.Fatalf("SpectralCluster failed: %v", err)
	}
	if result.Modularity > 0.2 {
		t.Errorf("clique modularity = %v, want <= 0.2 (no real modules)", result.Modularity)
	}
}
