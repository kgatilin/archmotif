package metrics_test

import (
	"context"
	"testing"

	"github.com/kgatilin/archmotif/internal/metrics"
	"github.com/kgatilin/archmotif/internal/metrics/metricstest"
)

func TestCycleMatrix_FourCycle_AllNodesReportLength4(t *testing.T) {
	g := metricstest.FourCycle()
	m, ok := metrics.Lookup("cycle_matrix")
	if !ok {
		t.Fatal("cycle_matrix not registered")
	}
	recs, err := m.Compute(context.Background(), g)
	if err != nil {
		t.Fatal(err)
	}
	graphRec := findGraph(recs, "cycle_matrix")
	if graphRec == nil {
		t.Fatal("missing graph-scope record")
	}
	if graphRec.Value != 4 {
		t.Fatalf("cycling nodes = %v, want 4 (the four cycle members)", graphRec.Value)
	}
	nodes := byScope(recs, metrics.ScopeNode)
	if len(nodes) != 4 {
		t.Fatalf("node records = %d, want 4", len(nodes))
	}
	for _, r := range nodes {
		if r.Value != 4 {
			t.Fatalf("shortest cycle for %s = %v, want 4", r.Target, r.Value)
		}
	}
}

func TestCycleMatrix_Path_HasNoCycles(t *testing.T) {
	g := metricstest.PathFour()
	m, _ := metrics.Lookup("cycle_matrix")
	recs, err := m.Compute(context.Background(), g)
	if err != nil {
		t.Fatal(err)
	}
	graphRec := findGraph(recs, "cycle_matrix")
	if graphRec.Value != 0 {
		t.Fatalf("cycling nodes = %v, want 0 (path has no cycle)", graphRec.Value)
	}
	if nodes := byScope(recs, metrics.ScopeNode); len(nodes) != 0 {
		t.Fatalf("node records = %d, want 0", len(nodes))
	}
}
