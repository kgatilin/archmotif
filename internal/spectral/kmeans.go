package spectral

import (
	"math"
	"sort"
)

// KMeans performs k-means clustering on the rows of the data matrix.
// Uses k-means++ initialization with a deterministic seed based on the
// data ordering (no random initialization) for reproducibility.
//
// data: n x dims matrix where each row is a point.
// k: number of clusters.
// maxIters: maximum iterations (100 is typical).
//
// Returns cluster assignments (0 to k-1) for each row.
func KMeans(data [][]float64, k, maxIters int) []int {
	n := len(data)
	if n == 0 || k <= 0 {
		return nil
	}
	if k >= n {
		// Each point is its own cluster.
		labels := make([]int, n)
		for i := range labels {
			labels[i] = i
		}
		return labels
	}

	dims := len(data[0])
	centroids := kMeansPlusPlusInit(data, k)
	labels := make([]int, n)

	for iter := 0; iter < maxIters; iter++ {
		// Assign step: assign each point to nearest centroid.
		changed := false
		for i, pt := range data {
			nearest := nearestCentroid(pt, centroids)
			if labels[i] != nearest {
				changed = true
				labels[i] = nearest
			}
		}
		if !changed && iter > 0 {
			break
		}

		// Update step: recompute centroids.
		newCentroids := make([][]float64, k)
		counts := make([]int, k)
		for c := 0; c < k; c++ {
			newCentroids[c] = make([]float64, dims)
		}
		for i, pt := range data {
			c := labels[i]
			counts[c]++
			for d := 0; d < dims; d++ {
				newCentroids[c][d] += pt[d]
			}
		}
		for c := 0; c < k; c++ {
			if counts[c] > 0 {
				for d := 0; d < dims; d++ {
					newCentroids[c][d] /= float64(counts[c])
				}
			} else {
				// Empty cluster: keep old centroid.
				copy(newCentroids[c], centroids[c])
			}
		}
		centroids = newCentroids
	}

	return labels
}

// kMeansPlusPlusInit initializes k centroids using k-means++ but with
// deterministic tie-breaking (first point wins ties).
func kMeansPlusPlusInit(data [][]float64, k int) [][]float64 {
	n := len(data)
	dims := len(data[0])

	// Start with the point that has the largest L2 norm (deterministic choice).
	centroids := make([][]float64, 0, k)
	firstIdx := 0
	maxNorm := -1.0
	for i, pt := range data {
		norm := l2Norm(pt)
		if norm > maxNorm {
			maxNorm = norm
			firstIdx = i
		}
	}
	centroids = append(centroids, copySlice(data[firstIdx]))

	// Distance from each point to nearest centroid.
	dists := make([]float64, n)
	for i, pt := range data {
		dists[i] = euclideanDist(pt, centroids[0])
	}

	for len(centroids) < k {
		// Choose the point with maximum distance to nearest centroid.
		// Deterministic tie-breaking: lowest index wins.
		maxDist := -1.0
		nextIdx := 0
		for i, d := range dists {
			if d > maxDist {
				maxDist = d
				nextIdx = i
			}
		}
		centroids = append(centroids, copySlice(data[nextIdx]))

		// Update distances.
		newCentroid := centroids[len(centroids)-1]
		for i, pt := range data {
			d := euclideanDist(pt, newCentroid)
			if d < dists[i] {
				dists[i] = d
			}
		}
	}

	// Ensure centroids are dim-correct.
	result := make([][]float64, k)
	for i := 0; i < k; i++ {
		result[i] = make([]float64, dims)
		copy(result[i], centroids[i])
	}
	return result
}

func nearestCentroid(pt []float64, centroids [][]float64) int {
	minDist := math.MaxFloat64
	nearest := 0
	for i, c := range centroids {
		d := euclideanDist(pt, c)
		if d < minDist {
			minDist = d
			nearest = i
		}
	}
	return nearest
}

func euclideanDist(a, b []float64) float64 {
	sum := 0.0
	for i := range a {
		if i < len(b) {
			diff := a[i] - b[i]
			sum += diff * diff
		}
	}
	return math.Sqrt(sum)
}

func l2Norm(v []float64) float64 {
	sum := 0.0
	for _, x := range v {
		sum += x * x
	}
	return math.Sqrt(sum)
}

func copySlice(s []float64) []float64 {
	c := make([]float64, len(s))
	copy(c, s)
	return c
}

// sortClustersBySize renumbers cluster labels so that clusters are
// numbered 0..k-1 in decreasing order of size. Returns the new labels
// and the mapping from old to new.
func sortClustersBySize(labels []int) ([]int, map[int]int) {
	// Count members per cluster.
	counts := map[int]int{}
	for _, l := range labels {
		counts[l]++
	}

	// Sort cluster IDs by count descending, then by ID ascending for ties.
	clusterIDs := make([]int, 0, len(counts))
	for id := range counts {
		clusterIDs = append(clusterIDs, id)
	}
	sort.Slice(clusterIDs, func(i, j int) bool {
		if counts[clusterIDs[i]] != counts[clusterIDs[j]] {
			return counts[clusterIDs[i]] > counts[clusterIDs[j]]
		}
		return clusterIDs[i] < clusterIDs[j]
	})

	// Build remapping.
	remap := make(map[int]int, len(clusterIDs))
	for newID, oldID := range clusterIDs {
		remap[oldID] = newID
	}

	// Apply remapping.
	newLabels := make([]int, len(labels))
	for i, l := range labels {
		newLabels[i] = remap[l]
	}

	return newLabels, remap
}
