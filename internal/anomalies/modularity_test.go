package anomalies_test

import (
	"context"
	"testing"

	"github.com/kgatilin/archmotif/internal/anomalies"
	"github.com/kgatilin/archmotif/internal/anomalies/anomaliestest"
	"github.com/kgatilin/archmotif/internal/metrics"
)

func TestModularity_FlagsOversizePackage(t *testing.T) {
	// One "big" package with 50 functions, five "small"
	// packages with 2 each. The big one should be flagged.
	g := anomaliestest.OversizePackage(50, 2)
	recs, err := metrics.Modularity{}.Compute(context.Background(), g)
	if err != nil {
		t.Fatal(err)
	}
	res := anomalies.Run(g, recs, []string{"modularity"})
	if len(res.Anomalies) == 0 {
		t.Fatalf("expected ≥1 modularity anomaly; got 0 (records=%+v)", recs)
	}
	found := false
	for _, a := range res.Anomalies {
		if a.Region.PrimaryID == "pkg:fixture/big" {
			found = true
			if a.Reason.Code != "oversize_community" && a.Reason.Code != "oversize_community_ratio" {
				t.Errorf("expected oversize_community[*]; got %s", a.Reason.Code)
			}
		}
	}
	if !found {
		t.Errorf("expected fixture/big to be flagged; got anomalies=%+v", res.Anomalies)
	}
}

func TestModularity_NoFlagOnEvenSizes(t *testing.T) {
	// All packages have 4 functions — no oversize.
	g := anomaliestest.OversizePackage(4, 4)
	recs, _ := metrics.Modularity{}.Compute(context.Background(), g)
	res := anomalies.Run(g, recs, []string{"modularity"})
	if len(res.Anomalies) > 0 {
		t.Fatalf("expected 0 anomalies on even-sized packages; got %+v", res.Anomalies)
	}
}
