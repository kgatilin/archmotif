// Package spectralcluster provides spectral graph clustering for archmotif
// graphs. It implements spectral clustering with automatic K selection via
// the eigengap heuristic.
//
// The package symmetrizes directed graphs per ADR-012: all edges become
// undirected for spectral analysis.
package spectralcluster

import (
	"fmt"
	"math"
	"sort"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
	"github.com/kgatilin/archmotif/internal/spectral"
)

// Graph is a type alias for archmotif's internal graph type.
type Graph = mgraph.Graph

// ClusterOptions configures the spectral clustering operation.
type ClusterOptions struct {
	// K is the number of clusters. 0 means auto-detect via eigengap.
	K int
	// NodeIDs restricts clustering to a subset of nodes. Empty means all.
	NodeIDs []string
	// EdgeKinds restricts which edge kinds to consider. Empty means all.
	EdgeKinds []string
	// Normalized uses the normalized Laplacian (L_sym = D^-1/2 L D^-1/2).
	// Default true for better clustering on graphs with varying degrees.
	Normalized bool
}

// DefaultOptions returns sensible defaults for spectral clustering.
func DefaultOptions() ClusterOptions {
	return ClusterOptions{
		K:          0,    // auto
		Normalized: true, // use normalized Laplacian
	}
}

// ClusterResult holds the output of spectral clustering.
type ClusterResult struct {
	// ChosenK is the number of clusters used.
	ChosenK int `json:"chosen_k"`
	// KSource indicates how K was determined: "auto" or "explicit".
	KSource string `json:"k_source"`
	// Candidates lists K candidates with their gap ratios (for auto-K).
	Candidates []KCandidate `json:"candidates"`
	// Clusters contains the cluster assignments.
	Clusters []Cluster `json:"clusters"`
	// BoundarySymbols are nodes near the Fiedler cut boundary (ambiguous).
	BoundarySymbols []string `json:"boundary_symbols"`
	// CutQuality measures the clustering quality.
	CutQuality CutQuality `json:"cut_quality"`
}

// KCandidate describes a potential K value with its eigengap strength.
type KCandidate struct {
	K          int     `json:"k"`
	GapRatio   float64 `json:"gap_ratio"`
	Confidence string  `json:"confidence"` // "strong", "moderate", "weak"
}

// Cluster holds the members of one cluster.
type Cluster struct {
	ID      int      `json:"id"`
	Members []string `json:"members"`
}

// CutQuality measures how clean the cluster boundaries are.
type CutQuality struct {
	IntraEdges int `json:"intra_edges"` // edges within clusters
	InterEdges int `json:"inter_edges"` // edges between clusters
}

// SpectralCluster performs spectral clustering on the graph.
// Returns an error if the graph is too small or clustering fails.
func SpectralCluster(g *Graph, opts ClusterOptions) (ClusterResult, error) {
	if g == nil {
		return ClusterResult{}, fmt.Errorf("spectralcluster: nil graph")
	}

	// Step 1: Induce subgraph if node subset specified.
	subgraph := induceSubgraph(g, opts.NodeIDs, opts.EdgeKinds)
	nodeCount := subgraph.NodeCount()

	if nodeCount == 0 {
		return ClusterResult{
			ChosenK:         0,
			KSource:         "auto",
			Candidates:      []KCandidate{},
			Clusters:        []Cluster{},
			BoundarySymbols: []string{},
			CutQuality:      CutQuality{},
		}, nil
	}

	if nodeCount == 1 {
		nodes := subgraph.Nodes()
		return ClusterResult{
			ChosenK:         1,
			KSource:         opts.kSource(),
			Candidates:      []KCandidate{},
			Clusters:        []Cluster{{ID: 0, Members: []string{nodes[0].ID}}},
			BoundarySymbols: []string{},
			CutQuality:      CutQuality{},
		}, nil
	}

	// Step 2: Build undirected view with edge kind filter.
	edgeKinds := buildEdgeKindFilter(opts.EdgeKinds)
	uv := spectral.ToUndirected(subgraph, edgeKinds)

	// Step 3: Compute eigendecomposition.
	eigen := spectral.ComputeEigen(uv, opts.Normalized)
	if eigen == nil {
		return ClusterResult{}, fmt.Errorf("spectralcluster: eigendecomposition failed")
	}

	// Step 4: Determine K.
	k := opts.K
	candidates := computeKCandidates(eigen.Values)
	kSource := "explicit"
	if k <= 0 {
		// Auto-K via eigengap.
		k = autoSelectK(eigen.Values, candidates)
		kSource = "auto"
	}

	// Validate K.
	if k > nodeCount {
		k = nodeCount
	}
	if k < 1 {
		k = 1
	}

	// Check for disconnected components (multiple near-zero eigenvalues).
	numComponents := countComponents(eigen.Values)
	if k < numComponents {
		k = numComponents
	}

	// Step 5: Extract spectral embedding (first K eigenvectors).
	embedding := extractEmbedding(eigen, k)

	// Step 6: Run k-means on the embedding.
	labels := spectral.KMeans(embedding, k, 100)

	// Step 7: Map labels back to node IDs.
	clusters := buildClusters(labels, uv.Sort)

	// Step 8: Identify boundary symbols (Fiedler vector near zero).
	boundary := findBoundarySymbols(eigen, uv.Sort)

	// Step 9: Compute cut quality.
	cutQuality := computeCutQuality(subgraph, labels, uv)

	return ClusterResult{
		ChosenK:         k,
		KSource:         kSource,
		Candidates:      candidates,
		Clusters:        clusters,
		BoundarySymbols: boundary,
		CutQuality:      cutQuality,
	}, nil
}

func (o ClusterOptions) kSource() string {
	if o.K <= 0 {
		return "auto"
	}
	return "explicit"
}

// induceSubgraph creates a subgraph with only the specified nodes and edges.
func induceSubgraph(g *Graph, nodeIDs []string, edgeKinds []string) *Graph {
	if len(nodeIDs) == 0 && len(edgeKinds) == 0 {
		return g
	}

	// Collect target node set.
	nodeSet := map[string]bool{}
	if len(nodeIDs) > 0 {
		for _, id := range nodeIDs {
			nodeSet[id] = true
		}
	} else {
		for _, n := range g.Nodes() {
			nodeSet[n.ID] = true
		}
	}

	// Collect edge kind filter.
	edgeKindSet := map[mgraph.EdgeKind]bool{}
	for _, k := range edgeKinds {
		edgeKindSet[mgraph.EdgeKind(k)] = true
	}

	// Build induced subgraph.
	sub := mgraph.New()
	for _, n := range g.Nodes() {
		if nodeSet[n.ID] {
			sub.AddNode(n)
		}
	}
	for _, e := range g.Edges() {
		if !nodeSet[e.From] || !nodeSet[e.To] {
			continue
		}
		if len(edgeKindSet) > 0 && !edgeKindSet[e.Kind] {
			continue
		}
		_, _ = sub.AddEdge(e)
	}

	return sub
}

func buildEdgeKindFilter(kinds []string) map[mgraph.EdgeKind]bool {
	if len(kinds) == 0 {
		return nil
	}
	m := make(map[mgraph.EdgeKind]bool, len(kinds))
	for _, k := range kinds {
		m[mgraph.EdgeKind(k)] = true
	}
	return m
}

// computeKCandidates finds potential K values based on eigengap ratios.
func computeKCandidates(eigenvalues []float64) []KCandidate {
	if len(eigenvalues) < 2 {
		return nil
	}

	// Skip near-zero eigenvalues (components).
	const eps = 1e-9
	startIdx := 0
	for i, v := range eigenvalues {
		if v > eps {
			startIdx = i
			break
		}
	}
	if startIdx >= len(eigenvalues)-1 {
		return nil
	}

	// Compute gap ratios: λ_{i+1} / λ_i for small eigenvalues.
	// K = index where the largest gap occurs.
	type gap struct {
		k     int
		ratio float64
	}
	gaps := []gap{}

	// Look at the first few eigenvalues (up to 10 or half the spectrum).
	maxCheck := len(eigenvalues) / 2
	if maxCheck > 10 {
		maxCheck = 10
	}
	if maxCheck < 2 {
		maxCheck = 2
	}

	for i := startIdx; i < startIdx+maxCheck && i < len(eigenvalues)-1; i++ {
		prev := eigenvalues[i]
		next := eigenvalues[i+1]
		if prev <= eps {
			continue
		}
		ratio := next / prev
		gaps = append(gaps, gap{k: i + 1, ratio: ratio})
	}

	// Sort by ratio descending.
	sort.Slice(gaps, func(i, j int) bool {
		return gaps[i].ratio > gaps[j].ratio
	})

	// Convert to candidates with confidence.
	candidates := make([]KCandidate, 0, len(gaps))
	for _, g := range gaps {
		conf := "weak"
		if g.ratio > 10.0 {
			conf = "strong"
		} else if g.ratio > 3.0 {
			conf = "moderate"
		}
		candidates = append(candidates, KCandidate{
			K:          g.k,
			GapRatio:   roundFloat(g.ratio, 4),
			Confidence: conf,
		})
	}

	// Limit to top 5.
	if len(candidates) > 5 {
		candidates = candidates[:5]
	}

	return candidates
}

// autoSelectK picks K from the candidates or defaults to 2.
func autoSelectK(eigenvalues []float64, candidates []KCandidate) int {
	// If there's a strong candidate, use it.
	for _, c := range candidates {
		if c.Confidence == "strong" || c.Confidence == "moderate" {
			return c.K
		}
	}
	// If any candidate exists, use the top one.
	if len(candidates) > 0 {
		return candidates[0].K
	}
	// Default to 2 if we have enough eigenvalues.
	if len(eigenvalues) >= 2 {
		return 2
	}
	return 1
}

// countComponents counts connected components (eigenvalues near zero).
func countComponents(eigenvalues []float64) int {
	const eps = 1e-9
	count := 0
	for _, v := range eigenvalues {
		if math.Abs(v) < eps {
			count++
		}
	}
	if count == 0 {
		count = 1
	}
	return count
}

// extractEmbedding extracts the first k eigenvectors as row embeddings.
func extractEmbedding(eigen *spectral.EigenResult, k int) [][]float64 {
	n := eigen.N
	embedding := make([][]float64, n)
	for i := 0; i < n; i++ {
		row := make([]float64, k)
		for j := 0; j < k; j++ {
			row[j] = eigen.Vectors.At(i, j)
		}
		// Normalize the row for better k-means performance.
		norm := 0.0
		for _, v := range row {
			norm += v * v
		}
		if norm > 1e-12 {
			norm = math.Sqrt(norm)
			for j := range row {
				row[j] /= norm
			}
		}
		embedding[i] = row
	}
	return embedding
}

// buildClusters groups node IDs by cluster label.
func buildClusters(labels []int, nodeIDs []string) []Cluster {
	// Group by label.
	groups := map[int][]string{}
	for i, label := range labels {
		if i < len(nodeIDs) {
			groups[label] = append(groups[label], nodeIDs[i])
		}
	}

	// Convert to sorted slice of Clusters.
	clusterIDs := make([]int, 0, len(groups))
	for id := range groups {
		clusterIDs = append(clusterIDs, id)
	}
	sort.Ints(clusterIDs)

	clusters := make([]Cluster, len(clusterIDs))
	for i, id := range clusterIDs {
		members := groups[id]
		sort.Strings(members)
		clusters[i] = Cluster{ID: i, Members: members}
	}

	return clusters
}

// findBoundarySymbols identifies nodes with Fiedler vector components near zero.
func findBoundarySymbols(eigen *spectral.EigenResult, nodeIDs []string) []string {
	if eigen.N < 2 || len(eigen.Values) < 2 {
		return nil
	}

	// Fiedler vector is the eigenvector for λ_2 (index 1).
	fiedler := make([]float64, eigen.N)
	for i := 0; i < eigen.N; i++ {
		fiedler[i] = eigen.Vectors.At(i, 1)
	}

	// Find the standard deviation.
	mean := 0.0
	for _, v := range fiedler {
		mean += v
	}
	mean /= float64(len(fiedler))

	variance := 0.0
	for _, v := range fiedler {
		diff := v - mean
		variance += diff * diff
	}
	stddev := math.Sqrt(variance / float64(len(fiedler)))

	// Boundary nodes are within 0.2 * stddev of zero.
	threshold := 0.2 * stddev
	if threshold < 1e-6 {
		threshold = 1e-6
	}

	boundary := []string{}
	for i, v := range fiedler {
		if math.Abs(v) < threshold && i < len(nodeIDs) {
			boundary = append(boundary, nodeIDs[i])
		}
	}
	sort.Strings(boundary)

	return boundary
}

// computeCutQuality counts intra-cluster and inter-cluster edges.
func computeCutQuality(g *Graph, labels []int, uv spectral.UndirectedView) CutQuality {
	// Build node -> cluster map.
	nodeCluster := make(map[string]int, len(uv.Sort))
	for i, id := range uv.Sort {
		if i < len(labels) {
			nodeCluster[id] = labels[i]
		}
	}

	intra, inter := 0, 0
	for _, e := range g.Edges() {
		cFrom, okFrom := nodeCluster[e.From]
		cTo, okTo := nodeCluster[e.To]
		if !okFrom || !okTo {
			continue
		}
		if cFrom == cTo {
			intra++
		} else {
			inter++
		}
	}

	return CutQuality{
		IntraEdges: intra,
		InterEdges: inter,
	}
}

func roundFloat(v float64, decimals int) float64 {
	pow := math.Pow(10, float64(decimals))
	return math.Round(v*pow) / pow
}
