package anomalies

import (
	"math"
	"sort"
)

// modifiedZScore computes the modified z-score of x against the
// population xs (per Iglewicz & Hoaglin 1993; see ADR-020):
//
//	M_i = 0.6745 * (x - median(xs)) / MAD
//
// where MAD = median(|x_i - median(xs)|). Returns (0, false) when MAD
// is 0 (all xs equal, or xs empty / one element) so callers can fall
// back to absolute thresholds.
//
// The 0.6745 constant scales MAD to be comparable to standard
// deviation under a normal distribution: σ ≈ MAD / 0.6745, so
// (x − median) / σ ≈ 0.6745 * (x − median) / MAD.
func modifiedZScore(x float64, xs []float64) (float64, bool) {
	if len(xs) < 2 {
		return 0, false
	}
	med := median(xs)
	deviations := make([]float64, len(xs))
	for i, v := range xs {
		deviations[i] = math.Abs(v - med)
	}
	mad := median(deviations)
	if mad == 0 {
		return 0, false
	}
	return 0.6745 * (x - med) / mad, true
}

// median returns the median of xs. Mutates a copy, not xs. Empty xs
// returns 0; treat the result as undefined in that case.
func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	cp := make([]float64, len(xs))
	copy(cp, xs)
	sort.Float64s(cp)
	n := len(cp)
	if n%2 == 1 {
		return cp[n/2]
	}
	return (cp[n/2-1] + cp[n/2]) / 2
}
