package anomalies

import (
	"math"
	"testing"
)

func TestModifiedZScore_Mad(t *testing.T) {
	// Population with a clear outlier.
	xs := []float64{1, 2, 3, 4, 5, 100}
	z, ok := modifiedZScore(100, xs)
	if !ok {
		t.Fatal("expected modified z to be computable")
	}
	if z < 30 {
		t.Fatalf("modified z for 100 vs %v should be large; got %v", xs, z)
	}
	// Median of mid-range values should be near zero.
	z3, _ := modifiedZScore(3, xs)
	if math.Abs(z3) > 1 {
		t.Fatalf("modified z for 3 should be near 0; got %v", z3)
	}
}

func TestModifiedZScore_ZeroMad(t *testing.T) {
	xs := []float64{5, 5, 5}
	if _, ok := modifiedZScore(5, xs); ok {
		t.Fatal("modified z should be undefined when MAD is 0")
	}
}

func TestMedian(t *testing.T) {
	cases := []struct {
		in   []float64
		want float64
	}{
		{[]float64{1, 2, 3}, 2},
		{[]float64{1, 2, 3, 4}, 2.5},
		{[]float64{}, 0},
	}
	for _, c := range cases {
		if got := median(c.in); got != c.want {
			t.Errorf("median(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}
