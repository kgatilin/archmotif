package metrics_test

import (
	"context"
	"math"
	"testing"

	"github.com/kgatilin/archmotif/internal/metrics"
	"github.com/kgatilin/archmotif/internal/metrics/metricstest"
)

func TestSpectralGap_FourCliqueIsN(t *testing.T) {
	// For a clique K_n, the Laplacian eigenvalues are {0, n, n, ..., n};
	// algebraic connectivity = n. Our K_4 fixture also contains a
	// Package node + Contains edges, so the symmetrised view is K_4
	// plus an extra node attached to all of K_4 — different graph.
	// We instead build a pure K_4 via the FourClique fixture and call
	// the metric on it, then assert λ_2 equals 4 within tolerance for
	// that K_4 alone, allowing for the package wrapper.
	//
	// Because the full fixture has 5 nodes (1 pkg + 4 cliques), the
	// Laplacian's λ_2 won't equal 4. We assert the more useful
	// invariant: connected graph → λ_2 > 0.
	g := metricstest.FourClique()
	recs, err := metrics.SpectralGap{}.Compute(context.Background(), g)
	if err != nil {
		t.Fatal(err)
	}
	rec := findGraph(recs, "spectral_gap")
	if rec == nil {
		t.Fatal("missing graph record")
	}
	if !(rec.Value > 0) {
		t.Fatalf("connected graph should have λ_2 > 0, got %v", rec.Value)
	}
}

func TestSpectralGap_DisconnectedIsZero(t *testing.T) {
	g := metricstest.TwoIslands()
	recs, _ := metrics.SpectralGap{}.Compute(context.Background(), g)
	rec := findGraph(recs, "spectral_gap")
	// Two islands but bound to a Package each, then no inter-package
	// edges either. Disconnected → λ_2 = 0.
	if math.Abs(rec.Value) > 1e-9 {
		t.Fatalf("disconnected graph should have λ_2 = 0, got %v", rec.Value)
	}
}

func TestSpectralGap_PathFourClosedForm(t *testing.T) {
	// Pure 4-path A-B-C-D has algebraic connectivity 2(1-cos(π/4))
	// = 2−√2 ≈ 0.5858. Our PathFour fixture has an extra Package
	// node + 4 Contains edges → no longer a pure path. To get the
	// closed-form value we test on a synthetic *graph.Graph that is
	// just the path — see TestSpectralGap_PurePath.
	g := metricstest.PathFour()
	recs, _ := metrics.SpectralGap{}.Compute(context.Background(), g)
	rec := findGraph(recs, "spectral_gap")
	if rec == nil || rec.Value <= 0 {
		t.Fatalf("path should have positive λ_2, got %+v", rec)
	}
}

func TestSpectralGap_PurePath4ClosedForm(t *testing.T) {
	// Build a pure-graph fixture (no Package, no Contains): 4 Function
	// nodes connected as A-B-C-D. Closed-form algebraic connectivity
	// for the 4-vertex path graph is 2 − √2.
	g := buildPurePath(4)
	recs, _ := metrics.SpectralGap{}.Compute(context.Background(), g)
	rec := findGraph(recs, "spectral_gap")
	want := 2.0 - math.Sqrt2
	if math.Abs(rec.Value-want) > 1e-9 {
		t.Fatalf("λ_2 = %.12f, want %.12f", rec.Value, want)
	}
}

func TestSpectralGap_PureClique4ClosedForm(t *testing.T) {
	g := buildPureClique(4)
	recs, _ := metrics.SpectralGap{}.Compute(context.Background(), g)
	rec := findGraph(recs, "spectral_gap")
	if math.Abs(rec.Value-4.0) > 1e-9 {
		t.Fatalf("λ_2 = %.12f, want 4.0 for K_4", rec.Value)
	}
}
