package trophic

import "math"

// sparseSPD is a symmetric positive-definite matrix in compressed-row form:
// the diagonal apart, row i's off-diagonal entries are cols/vals in
// [rowStart[i], rowStart[i+1]).
type sparseSPD struct {
	diag     []float64
	rowStart []int
	cols     []int
	vals     []float64
}

func (a sparseSPD) mul(dst, x []float64) {
	for i := range a.diag {
		sum := a.diag[i] * x[i]
		for k := a.rowStart[i]; k < a.rowStart[i+1]; k++ {
			sum += a.vals[k] * x[a.cols[k]]
		}
		dst[i] = sum
	}
}

// solve returns x with A·x = b by Jacobi-preconditioned conjugate gradient.
// ok is false when the matrix is not positive definite or the iteration does
// not converge — the cases a Cholesky factorization would have refused.
func (a sparseSPD) solve(b []float64) (x []float64, ok bool) {
	n := len(b)
	x = make([]float64, n)
	var bNorm float64
	for _, v := range b {
		bNorm += v * v
	}
	if bNorm == 0 {
		return x, true
	}
	for _, v := range a.diag {
		if v <= 0 {
			return nil, false
		}
	}
	tol := 1e-24 * bNorm // squared: ‖r‖ ≤ 1e-12·‖b‖

	r := append([]float64(nil), b...)
	z := make([]float64, n)
	for i := range r {
		z[i] = r[i] / a.diag[i]
	}
	p := append([]float64(nil), z...)
	ap := make([]float64, n)
	rz := dot(r, z)
	// CG reaches the exact answer in n steps in exact arithmetic; the margin
	// covers rounding on badly conditioned components.
	for iter := 0; iter < 10*n+100; iter++ {
		a.mul(ap, p)
		pap := dot(p, ap)
		if pap <= 0 || math.IsNaN(pap) {
			return nil, false
		}
		alpha := rz / pap
		var rr float64
		for i := range x {
			x[i] += alpha * p[i]
			r[i] -= alpha * ap[i]
			rr += r[i] * r[i]
		}
		if rr <= tol {
			return x, true
		}
		for i := range z {
			z[i] = r[i] / a.diag[i]
		}
		rzNext := dot(r, z)
		beta := rzNext / rz
		rz = rzNext
		for i := range p {
			p[i] = z[i] + beta*p[i]
		}
	}
	return nil, false
}

func dot(a, b []float64) float64 {
	var s float64
	for i := range a {
		s += a[i] * b[i]
	}
	return s
}
