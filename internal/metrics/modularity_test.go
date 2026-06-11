package metrics_test

import (
	"context"
	"math"
	"testing"

	"github.com/kgatilin/archmotif/internal/metrics"
	"github.com/kgatilin/archmotif/internal/metrics/metricstest"
)

func TestModularity_PerfectPackageCommunities(t *testing.T) {
	// PackageWithChildren: three packages each containing one File +
	// one Function, no inter-package edges. Q for perfectly separated
	// communities depends only on edge count and degree distribution;
	// for this fixture, all edges are intra-community → Q is at its
	// maximum for the partition. Specifically, we expect Q > 0.5.
	g := metricstest.PackageWithChildren()
	recs, err := metrics.Modularity{}.Compute(context.Background(), g)
	if err != nil {
		t.Fatal(err)
	}
	rec := findGraph(recs, "modularity")
	if rec == nil {
		t.Fatal("missing graph record")
	}
	if rec.Value <= 0.5 {
		t.Fatalf("Q = %v, want > 0.5 (well-separated packages)", rec.Value)
	}
	regions := byScope(recs, metrics.ScopeRegion)
	if len(regions) != 3 {
		t.Fatalf("regions = %d, want 3 (one per package)", len(regions))
	}
}

func TestModularity_SinglePackageIsZero(t *testing.T) {
	// All nodes in one package + no inter-package edges → only one
	// community. Modularity for a single community is 0.
	g := metricstest.FourClique()
	recs, err := metrics.Modularity{}.Compute(context.Background(), g)
	if err != nil {
		t.Fatal(err)
	}
	rec := findGraph(recs, "modularity")
	if math.Abs(rec.Value) > 1e-9 {
		t.Fatalf("single-package graph Q = %v, want 0", rec.Value)
	}
}
