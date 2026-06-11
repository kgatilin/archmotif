package anomalies_test

import (
	"context"
	"testing"

	"github.com/kgatilin/archmotif/internal/anomalies"
	"github.com/kgatilin/archmotif/internal/anomalies/anomaliestest"
	"github.com/kgatilin/archmotif/internal/metrics"
)

func TestCycleRank_FlagsEverySCC(t *testing.T) {
	g := anomaliestest.CycleGraph(3)
	recs, err := metrics.CycleRank{}.Compute(context.Background(), g)
	if err != nil {
		t.Fatal(err)
	}
	res := anomalies.Run(g, recs, []string{"cycle_rank"})
	if len(res.Anomalies) != 3 {
		t.Fatalf("expected 3 anomalies (one per SCC); got %d", len(res.Anomalies))
	}
	for _, a := range res.Anomalies {
		if a.Metric != "cycle_rank" {
			t.Fatalf("unexpected metric: %s", a.Metric)
		}
		if a.Reason.Code != "scc_present" {
			t.Errorf("expected scc_present, got %s", a.Reason.Code)
		}
		if a.Score != 3 {
			t.Errorf("expected score=3 (3-cycle); got %v", a.Score)
		}
		if len(a.Region.Members) != 3 {
			t.Errorf("expected 3 members; got %d", len(a.Region.Members))
		}
	}
}

func TestCycleRank_NoSCCMeansNoAnomaly(t *testing.T) {
	g := anomaliestest.CycleGraph(0)
	recs, _ := metrics.CycleRank{}.Compute(context.Background(), g)
	res := anomalies.Run(g, recs, []string{"cycle_rank"})
	if len(res.Anomalies) != 0 {
		t.Fatalf("expected 0 anomalies (no cycles); got %d", len(res.Anomalies))
	}
}
