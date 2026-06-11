package anomalies_test

import (
	"context"
	"strings"
	"testing"

	"github.com/kgatilin/archmotif/internal/anomalies"
	"github.com/kgatilin/archmotif/internal/anomalies/anomaliestest"
	"github.com/kgatilin/archmotif/internal/metrics"
)

// TestMotifRedundancy_PlantedMotifFlagged is the Stage 4 verify case
// from issue #5: a synthetic graph with an obvious motif × N is
// flagged with high score by the motif_redundancy detector.
func TestMotifRedundancy_PlantedMotifFlagged(t *testing.T) {
	g := anomaliestest.PlantedMotif(10)
	recs, err := metrics.MotifRedundancy{}.Compute(context.Background(), g)
	if err != nil {
		t.Fatal(err)
	}
	res := anomalies.Run(g, recs, []string{"motif_redundancy"})
	if len(res.Errors) > 0 {
		t.Fatalf("unexpected detector errors: %+v", res.Errors)
	}
	if len(res.Anomalies) == 0 {
		t.Fatalf("expected ≥1 anomaly; got 0 (records=%+v)", recs)
	}
	// All anomalies should be from motif_redundancy.
	for _, a := range res.Anomalies {
		if a.Metric != "motif_redundancy" {
			t.Fatalf("unexpected metric in anomaly: %s", a.Metric)
		}
		if a.Score < 3 {
			t.Fatalf("planted motif anomaly score should be high; got %.2f", a.Score)
		}
		if a.Region.PrimaryID == "" {
			t.Fatalf("expected non-empty PrimaryID; got %+v", a.Region)
		}
		if a.Reason.Code == "" || a.Reason.Message == "" {
			t.Fatalf("expected populated reason; got %+v", a.Reason)
		}
	}
}

// TestMotifRedundancy_PopulatesFiles checks the resolveRegion plumbing:
// when the graph contains source positions, anomalies should carry
// FileRefs pointing back at the source files.
func TestMotifRedundancy_PopulatesFiles(t *testing.T) {
	g := anomaliestest.PlantedMotif(5)
	recs, _ := metrics.MotifRedundancy{}.Compute(context.Background(), g)
	res := anomalies.Run(g, recs, []string{"motif_redundancy"})
	if len(res.Anomalies) == 0 {
		t.Fatal("expected anomalies from planted motif")
	}
	a := res.Anomalies[0]
	if len(a.Region.Files) == 0 {
		t.Fatalf("expected file refs; got region=%+v", a.Region)
	}
	for _, f := range a.Region.Files {
		if !strings.HasPrefix(f.Path, "fixture/store/") {
			t.Errorf("unexpected file path: %s", f.Path)
		}
	}
}

// TestMotifRedundancy_NoFalsePositiveOnSparseGraph checks the
// detector doesn't flag a graph where the metric returned no
// repeated-motif region records.
func TestMotifRedundancy_NoFalsePositiveOnSparseGraph(t *testing.T) {
	// A single isolated function and package — no motifs.
	g := anomaliestest.PlantedMotif(1)
	recs, _ := metrics.MotifRedundancy{}.Compute(context.Background(), g)
	res := anomalies.Run(g, recs, []string{"motif_redundancy"})
	if len(res.Anomalies) > 0 {
		t.Fatalf("expected 0 anomalies on N=1 fixture; got %+v", res.Anomalies)
	}
}
