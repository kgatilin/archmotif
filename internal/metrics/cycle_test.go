package metrics_test

import (
	"context"
	"testing"

	"github.com/kgatilin/archmotif/internal/metrics"
	"github.com/kgatilin/archmotif/internal/metrics/metricstest"
)

func TestCycleRank_FourCycle(t *testing.T) {
	g := metricstest.FourCycle()
	recs, err := metrics.CycleRank{}.Compute(context.Background(), g)
	if err != nil {
		t.Fatal(err)
	}
	graphRec := findGraph(recs, "cycle_rank")
	if graphRec == nil {
		t.Fatal("missing graph-scope record")
	}
	if graphRec.Value != 1 {
		t.Fatalf("graph value = %v, want 1", graphRec.Value)
	}
	regions := byScope(recs, metrics.ScopeRegion)
	if len(regions) != 1 {
		t.Fatalf("regions = %d, want 1 (the only SCC)", len(regions))
	}
	if regions[0].Value != 4 {
		t.Fatalf("SCC size = %v, want 4", regions[0].Value)
	}
}

func TestCycleRank_TwoTriangles(t *testing.T) {
	g := metricstest.TwoTriangles()
	recs, err := metrics.CycleRank{}.Compute(context.Background(), g)
	if err != nil {
		t.Fatal(err)
	}
	graphRec := findGraph(recs, "cycle_rank")
	if graphRec == nil || graphRec.Value != 2 {
		t.Fatalf("graph value = %+v, want value 2", graphRec)
	}
	regions := byScope(recs, metrics.ScopeRegion)
	if len(regions) != 2 {
		t.Fatalf("regions = %d, want 2", len(regions))
	}
}

func TestCycleRank_PathHasNoCycle(t *testing.T) {
	g := metricstest.PathFour()
	recs, err := metrics.CycleRank{}.Compute(context.Background(), g)
	if err != nil {
		t.Fatal(err)
	}
	graphRec := findGraph(recs, "cycle_rank")
	if graphRec.Value != 0 {
		t.Fatalf("graph value = %v, want 0 (path has no cycle)", graphRec.Value)
	}
}
