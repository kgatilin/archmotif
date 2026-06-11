package anomalies_test

import (
	"context"
	"testing"

	"github.com/kgatilin/archmotif/internal/anomalies"
	"github.com/kgatilin/archmotif/internal/metrics"
	"github.com/kgatilin/archmotif/internal/metrics/metricstest"
)

// TestLocalSymmetry_FlagsSymmetricFanout checks the detector flags
// the methods of a symmetric-fanout type fixture, where each method
// has the same role signature.
func TestLocalSymmetry_FlagsSymmetricFanout(t *testing.T) {
	g := metricstest.SymmetricFanout()
	recs, err := metrics.LocalSymmetry{}.Compute(context.Background(), g)
	if err != nil {
		t.Fatal(err)
	}
	res := anomalies.Run(g, recs, []string{"local_symmetry"})
	if len(res.Anomalies) == 0 {
		t.Fatalf("expected ≥1 symmetry anomaly on symmetric fanout fixture; got 0 (records=%+v)", recs)
	}
	for _, a := range res.Anomalies {
		if a.Metric != "local_symmetry" {
			t.Fatalf("unexpected metric: %s", a.Metric)
		}
		if a.Region.Kind != string(metrics.ScopeNode) {
			t.Errorf("expected node-scope region; got %q", a.Region.Kind)
		}
	}
}
