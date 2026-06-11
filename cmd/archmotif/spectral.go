package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"sort"

	gn "gonum.org/v1/gonum/graph"
	gncommunity "gonum.org/v1/gonum/graph/community"
	"gonum.org/v1/gonum/graph/simple"
	"gonum.org/v1/gonum/mat"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// runSpectral implements the `archmotif spectral <graph.json>`
// subcommand. It reads an archmotif graph JSON file from disk, builds
// the symmetric adjacency / Laplacian, and emits:
//
//   - Laplacian eigenvalues λ_1 ≤ … ≤ λ_n
//   - Algebraic connectivity λ_2 (Fiedler value)
//   - Eigengap k + ratio = argmax_k λ_{k+1}/λ_k (over a useful slice)
//   - SVD singular values of the adjacency + knee-point
//   - Modularity Q at the eigengap-suggested k-partition (spectral
//     clustering on the first k Laplacian eigenvectors, then k-means
//     proxy via sign / quantile partition for k≤2 and rounded-coord
//     partition otherwise)
//
// Pure Go. Part 1/5 of #74. Leiden community detection is part 2.
//
// Output is JSON by default; --format=text renders a short
// human-readable summary.
func runSpectral(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("archmotif spectral", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("format", "json", "output format: json|text")
	maxK := fs.Int("max-k", 12, "max k considered when searching the eigengap (>=2)")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage:\n  archmotif spectral [flags] <graph.json>\n\nFlags:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() < 1 {
		fs.Usage()
		return 2
	}
	if *format != "json" && *format != "text" {
		_, _ = fmt.Fprintf(stderr, "archmotif spectral: --format=%q (want: json|text)\n", *format)
		return 2
	}
	if *maxK < 2 {
		_, _ = fmt.Fprintf(stderr, "archmotif spectral: --max-k must be >= 2 (got %d)\n", *maxK)
		return 2
	}

	path := fs.Arg(0)
	raw, err := os.ReadFile(path)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif spectral: read %s: %v\n", path, err)
		return 1
	}
	var doc mgraph.JSON
	if err := json.Unmarshal(raw, &doc); err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif spectral: parse %s: %v\n", path, err)
		return 1
	}

	report, err := computeSpectralReport(doc, *maxK)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif spectral: %v\n", err)
		return 1
	}

	switch *format {
	case "text":
		writeSpectralText(stdout, report)
	default:
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif spectral: encode json: %v\n", err)
			return 1
		}
	}
	return 0
}

// spectralReport is the JSON output schema documented in issue #75.
// Fields are intentionally fixed (no omitempty on numeric scalars) so
// downstream consumers can rely on the shape.
type spectralReport struct {
	NNodes                int       `json:"n_nodes"`
	NEdges                int       `json:"n_edges"`
	LaplacianEigenvalues  []float64 `json:"laplacian_eigenvalues"`
	AlgebraicConnectivity float64   `json:"algebraic_connectivity"`
	EigengapK             int       `json:"eigengap_k"`
	EigengapRatio         float64   `json:"eigengap_ratio"`
	SVDSingularValues     []float64 `json:"svd_singular_values"`
	SVDKnee               int       `json:"svd_knee"`
	ModularityQAtK        float64   `json:"modularity_q_at_k"`
}

// computeSpectralReport performs the full single-shot computation over
// the deserialised graph JSON. The input is symmetrised (directed
// archmotif edges become undirected gonum edges); self-edges are
// dropped because gonum simple graphs reject them.
func computeSpectralReport(doc mgraph.JSON, maxK int) (spectralReport, error) {
	report := spectralReport{
		NNodes: len(doc.Nodes),
		NEdges: len(doc.Edges),
		// Initialise as empty slices (not nil) so JSON emits "[]"
		// when the graph is too small to decompose.
		LaplacianEigenvalues: []float64{},
		SVDSingularValues:    []float64{},
	}
	n := len(doc.Nodes)
	if n == 0 {
		return report, nil
	}

	// Stable ID -> dense index. Sort IDs so consecutive runs over the
	// same graph emit identical numeric output (eigenvectors are sign-
	// ambiguous; eigenvalues are determined).
	ids := make([]string, 0, n)
	for _, node := range doc.Nodes {
		ids = append(ids, node.ID)
	}
	sort.Strings(ids)
	idx := make(map[string]int, n)
	for i, id := range ids {
		idx[id] = i
	}

	// Symmetric adjacency. Multi-edges (same endpoints, different
	// kinds) collapse to a single undirected edge so the spectrum
	// describes the bare topology rather than the multiplicity. This
	// matches the convention in internal/metrics/gonum.go.
	adj := mat.NewSymDense(n, nil)
	seen := make(map[[2]int]struct{})
	for _, e := range doc.Edges {
		fi, ok1 := idx[e.From]
		ti, ok2 := idx[e.To]
		if !ok1 || !ok2 || fi == ti {
			continue
		}
		a, b := fi, ti
		if a > b {
			a, b = b, a
		}
		if _, dup := seen[[2]int{a, b}]; dup {
			continue
		}
		seen[[2]int{a, b}] = struct{}{}
		adj.SetSym(fi, ti, 1)
	}

	// Laplacian L = D - A.
	lap := mat.NewSymDense(n, nil)
	for i := 0; i < n; i++ {
		deg := 0.0
		for j := 0; j < n; j++ {
			if i == j {
				continue
			}
			deg += adj.At(i, j)
		}
		for j := 0; j < n; j++ {
			if i == j {
				lap.SetSym(i, j, deg)
			} else {
				lap.SetSym(i, j, -adj.At(i, j))
			}
		}
	}

	// Eigendecomposition of the Laplacian. We need eigenvectors for
	// spectral clustering, so factor with vectors=true.
	var es mat.EigenSym
	if ok := es.Factorize(lap, true); !ok {
		return report, fmt.Errorf("EigenSym.Factorize failed on Laplacian (n=%d)", n)
	}
	vals := es.Values(nil)
	// Sort eigenvalues ascending together with their eigenvectors.
	var vecs mat.Dense
	es.VectorsTo(&vecs)
	order := make([]int, len(vals))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(i, j int) bool { return vals[order[i]] < vals[order[j]] })
	sortedVals := make([]float64, len(vals))
	sortedVecs := mat.NewDense(n, n, nil)
	for newI, oldI := range order {
		sortedVals[newI] = vals[oldI]
		col := mat.Col(nil, oldI, &vecs)
		sortedVecs.SetCol(newI, col)
	}
	// Clamp small negative eigenvalues to zero (Laplacian is PSD
	// analytically; floating-point noise can produce tiny negatives).
	for i, v := range sortedVals {
		if v < 0 && v > -1e-9 {
			sortedVals[i] = 0
		}
	}
	report.LaplacianEigenvalues = sortedVals
	if n >= 2 {
		report.AlgebraicConnectivity = sortedVals[1]
	}

	// Eigengap: largest λ_{k+1}/λ_k over k in [2, maxK] (well-defined
	// only when λ_k > 0, so we skip the first zero eigenvalue and any
	// other near-zero entries arising from disconnected components).
	report.EigengapK, report.EigengapRatio = eigengap(sortedVals, maxK)

	// SVD of the adjacency. Singular values of a symmetric matrix
	// equal the absolute values of its eigenvalues, but mat.SVD
	// returns them sorted descending which is the convention we want
	// for the knee detection.
	var svd mat.SVD
	if ok := svd.Factorize(adj, mat.SVDThin); !ok {
		return report, fmt.Errorf("SVD.Factorize failed on adjacency (n=%d)", n)
	}
	sv := svd.Values(nil)
	// mat.SVD already returns descending order; assert and copy.
	report.SVDSingularValues = sv
	report.SVDKnee = sigmaKnee(sv)

	// Spectral clustering at the eigengap-suggested k, then Newman
	// modularity Q over that partition.
	k := report.EigengapK
	if k < 2 {
		k = 2
	}
	report.ModularityQAtK = modularityAtK(sortedVecs, ids, k, doc.Edges, idx)
	return report, nil
}

// eigengap returns the k in [2, maxK] that maximises λ_{k+1}/λ_k, plus
// the ratio. The first eigenvalue (λ_1 ≈ 0 for connected graphs, or
// multiple zeros for disconnected) is skipped to avoid divide-by-zero
// blow-ups dominating the search. If no informative gap exists the
// function returns k=2, ratio=0.
func eigengap(vals []float64, maxK int) (int, float64) {
	n := len(vals)
	if n < 3 {
		return 2, 0
	}
	// Find the index of the smallest strictly-positive eigenvalue.
	// For a graph with c connected components the first c eigenvalues
	// are zero; the eigengap inside the zero block is meaningless.
	start := 0
	for start < n && vals[start] <= 1e-9 {
		start++
	}
	// Search ends so we always have a λ_{k+1} to divide by.
	if maxK < 2 {
		maxK = 2
	}
	if maxK > n-1 {
		maxK = n - 1
	}
	bestK := 2
	bestRatio := 0.0
	// k is 1-indexed in the report (k=2 = λ_2/λ_1 ratio in user-facing
	// terms). Internally we map vals[i] to user-index i+1.
	for i := start; i+1 < n && (i+1) <= maxK; i++ {
		denom := vals[i]
		if denom <= 1e-9 {
			continue
		}
		ratio := vals[i+1] / denom
		if ratio > bestRatio {
			bestRatio = ratio
			// Report k as the number of "small" eigenvalues (i.e. the
			// detected cluster count is the position of the gap).
			bestK = i + 1
		}
	}
	return bestK, bestRatio
}

// sigmaKnee returns a knee-point index in a descending list of
// singular values. We use the elbow method: the point with maximum
// distance to the straight line from (0, σ_0) to (n-1, σ_{n-1}).
// Returns 1 for trivially small or constant inputs.
func sigmaKnee(sv []float64) int {
	n := len(sv)
	if n <= 2 {
		return n
	}
	x0, y0 := 0.0, sv[0]
	x1, y1 := float64(n-1), sv[n-1]
	dx, dy := x1-x0, y1-y0
	denom := math.Hypot(dx, dy)
	if denom == 0 {
		return 1
	}
	best := 1
	bestDist := -1.0
	for i := 1; i < n-1; i++ {
		// Perpendicular distance from (i, sv[i]) to the line
		// (x0,y0)-(x1,y1). Use the area-of-triangle formula.
		num := math.Abs(dx*(y0-sv[i]) - (x0-float64(i))*dy)
		dist := num / denom
		if dist > bestDist {
			bestDist = dist
			best = i + 1 // 1-indexed "first k effective directions"
		}
	}
	return best
}

// modularityAtK partitions the nodes via spectral embedding (first k
// Laplacian eigenvectors after the zero one) and returns Newman's Q
// over an undirected projection of the input edges.
//
// The partitioning is intentionally simple — full k-means with
// random restarts belongs in part 2. We use:
//   - k=1 → all in one cluster (Q = 0).
//   - k=2 → sign of the Fiedler vector.
//   - k>=3 → first k Lloyd iterations starting from the k coords
//     with greatest pairwise distance. Deterministic seed.
//
// vecs[:, j] is the j-th eigenvector (columns sorted ascending by
// eigenvalue). ids[i] is the stable node ID at row i. The input
// `edges` is used to build the gonum undirected projection for Q.
func modularityAtK(vecs *mat.Dense, ids []string, k int, edges []mgraph.Edge, idx map[string]int) float64 {
	n := len(ids)
	if n == 0 || k <= 1 {
		return 0
	}
	if k > n {
		k = n
	}
	var assignment []int
	switch {
	case k == 2:
		assignment = make([]int, n)
		// Use the second eigenvector (the Fiedler vector). Column 1
		// after ascending sort.
		fcol := 1
		if fcol >= n {
			fcol = n - 1
		}
		for i := 0; i < n; i++ {
			if vecs.At(i, fcol) >= 0 {
				assignment[i] = 1
			} else {
				assignment[i] = 0
			}
		}
	default:
		// Spectral embedding: rows of the matrix formed by columns
		// vecs[:, 1..k] (skip the zero eigenvector). Then run a
		// short deterministic Lloyd / k-means.
		dim := k - 1
		if dim < 1 {
			dim = 1
		}
		embedding := make([][]float64, n)
		for i := 0; i < n; i++ {
			row := make([]float64, dim)
			for j := 0; j < dim; j++ {
				col := j + 1
				if col >= n {
					col = n - 1
				}
				row[j] = vecs.At(i, col)
			}
			embedding[i] = row
		}
		assignment = kmeansDeterministic(embedding, k, 20)
	}

	// Build the gonum undirected graph from the deserialised edges to
	// keep the Modularity computation consistent with how the rest
	// of archmotif derives "communities * undirected projection".
	ug := simple.NewUndirectedGraph()
	for i := 0; i < n; i++ {
		ug.AddNode(simple.Node(int64(i)))
	}
	for _, e := range edges {
		fi, ok1 := idx[e.From]
		ti, ok2 := idx[e.To]
		if !ok1 || !ok2 || fi == ti {
			continue
		}
		if ug.HasEdgeBetween(int64(fi), int64(ti)) {
			continue
		}
		ug.SetEdge(simple.Edge{F: simple.Node(int64(fi)), T: simple.Node(int64(ti))})
	}

	// Group by assignment, build [][]graph.Node.
	groups := make(map[int][]gn.Node)
	for i, c := range assignment {
		groups[c] = append(groups[c], simple.Node(int64(i)))
	}
	comms := make([][]gn.Node, 0, len(groups))
	keys := make([]int, 0, len(groups))
	for c := range groups {
		keys = append(keys, c)
	}
	sort.Ints(keys)
	for _, c := range keys {
		comms = append(comms, groups[c])
	}
	return gncommunity.Q(ug, comms, 1.0)
}

// kmeansDeterministic runs k-means without RNG: it seeds with the k
// points whose pairwise distances maximise total spread (farthest-
// first traversal), then iterates Lloyd up to maxIter steps. Returns
// the assignment slice (length = len(points)).
func kmeansDeterministic(points [][]float64, k, maxIter int) []int {
	n := len(points)
	if k <= 1 || n == 0 {
		return make([]int, n)
	}
	if k > n {
		k = n
	}
	dim := len(points[0])
	// Farthest-first seeding starting from index 0 (deterministic).
	centroids := make([][]float64, k)
	chosen := []int{0}
	centroids[0] = appendCopy(nil, points[0])
	for ci := 1; ci < k; ci++ {
		best := -1
		bestDist := -1.0
		for i := 0; i < n; i++ {
			minD := math.Inf(1)
			for _, c := range chosen {
				d := sqDist(points[i], points[c])
				if d < minD {
					minD = d
				}
			}
			if minD > bestDist {
				bestDist = minD
				best = i
			}
		}
		if best < 0 {
			best = ci % n
		}
		chosen = append(chosen, best)
		centroids[ci] = appendCopy(nil, points[best])
	}

	assignment := make([]int, n)
	for iter := 0; iter < maxIter; iter++ {
		// Assign.
		changed := false
		for i := 0; i < n; i++ {
			bestC := 0
			bestD := math.Inf(1)
			for c := 0; c < k; c++ {
				d := sqDist(points[i], centroids[c])
				if d < bestD {
					bestD = d
					bestC = c
				}
			}
			if assignment[i] != bestC {
				assignment[i] = bestC
				changed = true
			}
		}
		// Update.
		sums := make([][]float64, k)
		counts := make([]int, k)
		for c := 0; c < k; c++ {
			sums[c] = make([]float64, dim)
		}
		for i := 0; i < n; i++ {
			c := assignment[i]
			for d := 0; d < dim; d++ {
				sums[c][d] += points[i][d]
			}
			counts[c]++
		}
		for c := 0; c < k; c++ {
			if counts[c] == 0 {
				continue
			}
			for d := 0; d < dim; d++ {
				centroids[c][d] = sums[c][d] / float64(counts[c])
			}
		}
		if !changed {
			break
		}
	}
	return assignment
}

func sqDist(a, b []float64) float64 {
	sum := 0.0
	for i := range a {
		d := a[i] - b[i]
		sum += d * d
	}
	return sum
}

func appendCopy(dst, src []float64) []float64 {
	out := append(dst, make([]float64, len(src))...)
	copy(out[len(out)-len(src):], src)
	return out
}

// writeSpectralText renders the report as a short human-readable
// summary on w. The first few eigenvalues and singular values are
// surfaced inline; long lists are truncated.
func writeSpectralText(w io.Writer, r spectralReport) {
	_, _ = fmt.Fprintf(w, "spectral report\n")
	_, _ = fmt.Fprintf(w, "  nodes: %d\n", r.NNodes)
	_, _ = fmt.Fprintf(w, "  edges: %d\n", r.NEdges)
	_, _ = fmt.Fprintf(w, "  algebraic connectivity (λ_2): %.6f\n", r.AlgebraicConnectivity)
	_, _ = fmt.Fprintf(w, "  eigengap: k=%d ratio=%.4f\n", r.EigengapK, r.EigengapRatio)
	_, _ = fmt.Fprintf(w, "  svd knee: %d\n", r.SVDKnee)
	_, _ = fmt.Fprintf(w, "  modularity Q at k=%d: %.6f\n", r.EigengapK, r.ModularityQAtK)
	_, _ = fmt.Fprintf(w, "  first eigenvalues: %s\n", floatHead(r.LaplacianEigenvalues, 8))
	_, _ = fmt.Fprintf(w, "  first singular values: %s\n", floatHead(r.SVDSingularValues, 8))
}

// floatHead renders the first n entries of vs as a comma-separated
// string with " …" appended when truncated.
func floatHead(vs []float64, n int) string {
	if n > len(vs) {
		n = len(vs)
	}
	if n == 0 {
		return "[]"
	}
	out := "["
	for i := 0; i < n; i++ {
		if i > 0 {
			out += ", "
		}
		out += fmt.Sprintf("%.4f", vs[i])
	}
	if n < len(vs) {
		out += ", …"
	}
	out += "]"
	return out
}
