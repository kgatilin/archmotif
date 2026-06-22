package archmotifmetrics_test

import (
	"testing"

	"github.com/kgatilin/archmotif/pkg/archmotifimport"
	"github.com/kgatilin/archmotif/pkg/archmotifmetrics"
)

// TestComputeMetrics_RealGraph builds a 4-package graph with two tight
// communities ({a,b} and {c,d}) joined by a single bridge edge, then asserts
// the bridge computes Newman modularity over it without internal errors.
func TestComputeMetrics_RealGraph(t *testing.T) {
	b := archmotifimport.NewBuilder()
	for _, id := range []string{"pkg:a", "pkg:b", "pkg:c", "pkg:d"} {
		if err := b.AddPackage(id, "domain", ""); err != nil {
			t.Fatalf("AddPackage %s: %v", id, err)
		}
	}
	deps := [][2]string{
		{"pkg:a", "pkg:b"}, {"pkg:b", "pkg:a"}, // community 1
		{"pkg:c", "pkg:d"}, {"pkg:d", "pkg:c"}, // community 2
		{"pkg:b", "pkg:c"}, // bridge
	}
	for _, d := range deps {
		if err := b.AddDependency(d[0], d[1], archmotifimport.DependencyDependsOn); err != nil {
			t.Fatalf("AddDependency %v: %v", d, err)
		}
	}
	g, err := b.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	m, err := archmotifmetrics.ComputeMetrics(g)
	if err != nil {
		t.Fatalf("ComputeMetrics: %v", err)
	}
	if len(m.Errors) != 0 {
		t.Fatalf("unexpected internal errors: %v", m.Errors)
	}
	if len(m.MetricsRan) == 0 {
		t.Fatalf("no metrics ran (graph=%v)", m.Graph)
	}
	if !m.HasModularity {
		t.Fatalf("modularity not computed; ran=%v graph=%v", m.MetricsRan, m.Graph)
	}
	t.Logf("modularity Q=%.4f, metricsRan=%v, detectorsRan=%v, anomalies=%d",
		m.Modularity, m.MetricsRan, m.DetectorsRan, len(m.Anomalies))
}

func TestComputeMetrics_NilGraph(t *testing.T) {
	if _, err := archmotifmetrics.ComputeMetrics(nil); err == nil {
		t.Fatal("expected error on nil graph")
	}
}
