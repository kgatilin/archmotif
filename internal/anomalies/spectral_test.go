package anomalies_test

import (
	"context"
	"testing"

	"github.com/kgatilin/archmotif/internal/anomalies"
	"github.com/kgatilin/archmotif/internal/metrics"
	"github.com/kgatilin/archmotif/internal/metrics/metricstest"
)

func TestSpectralGap_FlagsDisconnected(t *testing.T) {
	g := metricstest.TwoIslands()
	recs, err := metrics.SpectralGap{}.Compute(context.Background(), g)
	if err != nil {
		t.Fatal(err)
	}
	res := anomalies.Run(g, recs, []string{"spectral_gap"})
	if len(res.Anomalies) != 1 {
		t.Fatalf("expected exactly 1 anomaly on disconnected graph; got %+v", res.Anomalies)
	}
	a := res.Anomalies[0]
	if a.Reason.Code != "disconnected" {
		t.Errorf("expected disconnected code; got %s", a.Reason.Code)
	}
	if a.Region.Kind != string(metrics.ScopeGraph) {
		t.Errorf("expected graph-scope region; got %q", a.Region.Kind)
	}
}

func TestSpectralGap_NoFlagOnWellConnected(t *testing.T) {
	g := metricstest.FourClique()
	recs, _ := metrics.SpectralGap{}.Compute(context.Background(), g)
	res := anomalies.Run(g, recs, []string{"spectral_gap"})
	if len(res.Anomalies) > 0 {
		t.Fatalf("expected 0 anomalies on K4 (well-connected); got %+v", res.Anomalies)
	}
}
