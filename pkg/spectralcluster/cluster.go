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
	// Eigenvalues are the smallest Laplacian eigenvalues (ascending), so a
	// caller can see the spectrum the K choice was read from.
	Eigenvalues []float64 `json:"eigenvalues"`
	// Modularity is the Newman modularity of the chosen partition (higher =
	// stronger community structure; ~0 or below = no real modules / hairball).
	Modularity float64 `json:"modularity"`
}

// KCandidate describes a potential K value with its eigengap strength.
type KCandidate struct {
	K          int     `json:"k"`
	Gap        float64 `json:"gap"`        // absolute eigengap λ_{k+1} − λ_k (selection metric)
	GapRatio   float64 `json:"gap_ratio"`  // λ_{k+1} / λ_k (reported for context only)
	Modularity float64 `json:"modularity"` // modularity of the k-cluster partition (0 if not evaluated)
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

	// Step 4: Determine K. Auto-K reads the eigengap for candidates, then
	// validates them by clustering quality (modularity) so it does not get
	// trapped by the near-zero instability that biased the old ratio heuristic
	// toward tiny K.
	numComponents := countComponents(eigen.Values)
	candidates := computeKCandidates(eigen.Values)
	k := opts.K
	kSource := "explicit"
	if k <= 0 {
		k = selectKByModularity(eigen, uv, candidates, numComponents, nodeCount)
		kSource = "auto"
	}

	// Validate K.
	if k > nodeCount {
		k = nodeCount
	}
	if k < 1 {
		k = 1
	}
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

	// Step 9: Compute cut quality and modularity of the chosen partition.
	cutQuality := computeCutQuality(subgraph, labels, uv)
	mod := modularity(uv, labels)

	return ClusterResult{
		ChosenK:         k,
		KSource:         kSource,
		Candidates:      candidates,
		Clusters:        clusters,
		BoundarySymbols: boundary,
		CutQuality:      cutQuality,
		Eigenvalues:     topEigenvalues(eigen.Values, 24),
		Modularity:      roundFloat(mod, 4),
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

// computeKCandidates finds potential K values from the eigengap, ranked by the
// ABSOLUTE gap δ_k = λ_{k+1} − λ_k rather than the ratio λ_{k+1}/λ_i. Ratios of
// near-zero eigenvalues are numerically unstable and blow up at the bottom of
// the spectrum, which biased the old heuristic toward tiny K; absolute gaps do
// not. Confidence is the gap's size relative to the mean gap.
func computeKCandidates(eigenvalues []float64) []KCandidate {
	if len(eigenvalues) < 2 {
		return nil
	}

	const eps = 1e-9
	// Skip the near-zero band (one near-zero eigenvalue per component).
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

	maxCheck := len(eigenvalues)
	if maxCheck > 20 {
		maxCheck = 20
	}

	type gap struct {
		k            int
		delta, ratio float64
	}
	gaps := []gap{}
	for i := startIdx; i < maxCheck-1 && i < len(eigenvalues)-1; i++ {
		prev := eigenvalues[i]
		next := eigenvalues[i+1]
		ratio := 0.0
		if prev > eps {
			ratio = next / prev
		}
		gaps = append(gaps, gap{k: i + 1, delta: next - prev, ratio: ratio})
	}
	if len(gaps) == 0 {
		return nil
	}

	mean := 0.0
	for _, g := range gaps {
		mean += g.delta
	}
	mean /= float64(len(gaps))

	sort.Slice(gaps, func(i, j int) bool {
		if gaps[i].delta != gaps[j].delta {
			return gaps[i].delta > gaps[j].delta
		}
		return gaps[i].k < gaps[j].k
	})

	candidates := make([]KCandidate, 0, len(gaps))
	for _, g := range gaps {
		conf := "weak"
		if mean > 0 {
			switch {
			case g.delta > 3.0*mean:
				conf = "strong"
			case g.delta > 1.8*mean:
				conf = "moderate"
			}
		}
		candidates = append(candidates, KCandidate{
			K:          g.k,
			Gap:        roundFloat(g.delta, 4),
			GapRatio:   roundFloat(g.ratio, 4),
			Confidence: conf,
		})
	}
	if len(candidates) > 6 {
		candidates = candidates[:6]
	}
	return candidates
}

// selectKByModularity clusters at each candidate K and returns the K whose
// partition has the highest modularity. The eigengap proposes candidates; the
// clustering quality disambiguates — so a graph with no real structure (a
// hairball, where every K scores poorly) no longer collapses onto whatever weak
// gap happened to be largest. Records each evaluated candidate's modularity.
func selectKByModularity(eigen *spectral.EigenResult, uv spectral.UndirectedView, candidates []KCandidate, numComponents, nodeCount int) int {
	bestK := 0
	bestQ := math.Inf(-1)
	tried := map[int]bool{}

	consider := func(k int) {
		if k < 1 || k > nodeCount || tried[k] {
			return
		}
		if k < numComponents {
			return
		}
		tried[k] = true
		labels := spectral.KMeans(extractEmbedding(eigen, k), k, 100)
		q := modularity(uv, labels)
		for i := range candidates {
			if candidates[i].K == k {
				candidates[i].Modularity = roundFloat(q, 4)
			}
		}
		if q > bestQ {
			bestQ = q
			bestK = k
		}
	}

	for _, c := range candidates {
		consider(c.K)
	}
	if bestK == 0 {
		bestK = numComponents
		if bestK < 2 && nodeCount >= 2 {
			bestK = 2
		}
		if bestK < 1 {
			bestK = 1
		}
	}
	return bestK
}

// modularity is the Newman modularity Q of a label assignment over the
// undirected view that was actually clustered: Q = Σ_c [ L_c/m − (d_c/2m)² ],
// where L_c is the internal edge count of community c, d_c its total degree,
// and m the edge count. Q in (~0, 1]; ~0 or below means no community structure.
func modularity(uv spectral.UndirectedView, labels []int) float64 {
	clusterOf := make(map[int64]int, len(uv.Sort))
	for i, sid := range uv.Sort {
		if i < len(labels) {
			clusterOf[uv.Idx[sid]] = labels[i]
		}
	}
	deg := map[int]float64{}
	intra := map[int]float64{}
	m := 0.0
	edges := uv.G.Edges()
	for edges.Next() {
		e := edges.Edge()
		cu, oku := clusterOf[e.From().ID()]
		cv, okv := clusterOf[e.To().ID()]
		if !oku || !okv {
			continue
		}
		m++
		deg[cu]++
		deg[cv]++
		if cu == cv {
			intra[cu]++
		}
	}
	if m == 0 {
		return 0
	}
	q := 0.0
	for c, d := range deg {
		expect := d / (2 * m)
		q += intra[c]/m - expect*expect
	}
	return q
}

// topEigenvalues returns the first n eigenvalues (ascending), rounded.
func topEigenvalues(values []float64, n int) []float64 {
	if n > len(values) {
		n = len(values)
	}
	out := make([]float64, n)
	for i := 0; i < n; i++ {
		out[i] = roundFloat(values[i], 6)
	}
	return out
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
