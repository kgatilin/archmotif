package anomalies_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/kgatilin/archmotif/internal/anomalies"
	"github.com/kgatilin/archmotif/internal/anomalies/anomaliestest"
	"github.com/kgatilin/archmotif/internal/metrics"
)

func TestRegistry_AllBuiltInsRegistered(t *testing.T) {
	want := []string{"cycle_rank", "local_symmetry", "modularity", "motif_redundancy", "spectral_gap"}
	got := anomalies.Names()
	if len(got) != len(want) {
		t.Fatalf("registered detectors: got %v, want %v", got, want)
	}
	for i, n := range want {
		if got[i] != n {
			t.Errorf("detector[%d] = %s, want %s", i, got[i], n)
		}
	}
}

func TestRegistry_LookupAndByMetric(t *testing.T) {
	d, ok := anomalies.Lookup("cycle_rank")
	if !ok {
		t.Fatal("lookup cycle_rank failed")
	}
	if d.Metric() != "cycle_rank" {
		t.Errorf("metric() = %s, want cycle_rank", d.Metric())
	}
	if got := anomalies.ByMetric("cycle_rank"); len(got) != 1 || got[0].Name() != "cycle_rank" {
		t.Errorf("byMetric(cycle_rank) = %v, want [cycle_rank]", got)
	}
}

func TestRunner_ResultJSONRoundtrip(t *testing.T) {
	g := anomaliestest.PlantedMotif(8)
	mres := metrics.Run(g, nil)
	res := anomalies.Run(g, mres.Records, nil)
	if len(res.Anomalies) == 0 {
		t.Fatal("expected ≥1 anomaly")
	}
	var buf bytes.Buffer
	if err := res.WriteJSON(&buf); err != nil {
		t.Fatal(err)
	}
	var env anomalies.JSON
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Version != anomalies.CurrentJSONVersion {
		t.Errorf("version=%d, want %d", env.Version, anomalies.CurrentJSONVersion)
	}
	if len(env.Anomalies) != len(res.Anomalies) {
		t.Errorf("anomalies len mismatch")
	}
}

func TestRunner_TableFormat(t *testing.T) {
	g := anomaliestest.CycleGraph(2)
	mres := metrics.Run(g, nil)
	res := anomalies.Run(g, mres.Records, []string{"cycle_rank"})
	var buf bytes.Buffer
	if err := res.WriteTable(&buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "anomalies:") {
		t.Errorf("expected table header; got %q", out)
	}
	if !strings.Contains(out, "cycle_rank") {
		t.Errorf("expected cycle_rank row; got %q", out)
	}
}

func TestRunner_RankedDescending(t *testing.T) {
	g := anomaliestest.PlantedMotif(15)
	mres := metrics.Run(g, nil)
	res := anomalies.Run(g, mres.Records, nil)
	prev := -1.0
	for i, a := range res.Anomalies {
		if i > 0 && a.Score > prev {
			t.Errorf("anomaly[%d] score %.2f > previous %.2f — not sorted descending", i, a.Score, prev)
		}
		prev = a.Score
	}
}

func TestRunner_MissingDetectorErrors(t *testing.T) {
	res := anomalies.Run(nil, nil, []string{"nonexistent"})
	if len(res.Errors) != 1 {
		t.Fatalf("expected 1 error; got %d", len(res.Errors))
	}
}
