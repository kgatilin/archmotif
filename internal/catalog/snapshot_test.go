package catalog_test

import (
	"testing"
	"time"

	"github.com/kgatilin/archmotif/internal/catalog"
	"github.com/kgatilin/archmotif/internal/metrics/metricstest"
)

// TestCaptureFromSyntheticGraph wires Capture through the real
// metric and pattern runners against a hand-crafted graph so the
// digest helpers are exercised end-to-end without reaching for the
// parser.
func TestCaptureFromSyntheticGraph(t *testing.T) {
	g := metricstest.FourCycle()
	when := time.Date(2026, 5, 6, 10, 30, 0, 0, time.UTC)

	snap, err := catalog.Capture(g, catalog.CaptureOptions{
		Label:      "fixture",
		Ref:        "deadbeef",
		Path:       ".",
		Pattern:    "./...",
		CapturedAt: when,
	})
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}

	if snap.Label != "fixture" {
		t.Fatalf("label: got %q, want fixture", snap.Label)
	}
	if !snap.CapturedAt.Equal(when) {
		t.Fatalf("captured_at: got %v, want %v", snap.CapturedAt, when)
	}

	// The cycle metric must produce a graph-scope record on the 4-cycle
	// fixture. Stage 10 picks up exactly the graph-scope records, so
	// cycle_rank must appear in the digest.
	gotCycle := false
	for _, m := range snap.Metrics {
		if m.Name == "cycle_rank" {
			gotCycle = true
			if m.Value <= 0 {
				t.Fatalf("cycle_rank value: got %v, want > 0 on FourCycle fixture", m.Value)
			}
		}
	}
	if !gotCycle {
		t.Fatalf("expected cycle_rank in metrics: %+v", snap.Metrics)
	}

	// Motif summary is well-formed: total counts agree with the per-
	// group histogram.
	sum := 0
	for _, gr := range snap.Motifs.Groups {
		sum += gr.Count
		if gr.Count < 2 {
			t.Fatalf("motif group with count < 2 should be filtered: %+v", gr)
		}
		if gr.Size < 3 {
			t.Fatalf("motif group with size < 3: %+v", gr)
		}
	}
	if sum != snap.Motifs.TotalInstances {
		t.Fatalf("motif total_instances: got %d, sum of groups %d", snap.Motifs.TotalInstances, sum)
	}
}

func TestCaptureRequiresLabel(t *testing.T) {
	g := metricstest.FourCycle()
	_, err := catalog.Capture(g, catalog.CaptureOptions{})
	if err == nil {
		t.Fatalf("Capture with empty label: want error")
	}
}

func TestCaptureSnapshotIsDeterministic(t *testing.T) {
	g := metricstest.FourCycle()
	opts := catalog.CaptureOptions{
		Label:      "fix",
		Path:       ".",
		CapturedAt: time.Date(2026, 5, 6, 10, 30, 0, 0, time.UTC),
	}
	a, err := catalog.Capture(g, opts)
	if err != nil {
		t.Fatalf("Capture #1: %v", err)
	}
	b, err := catalog.Capture(g, opts)
	if err != nil {
		t.Fatalf("Capture #2: %v", err)
	}
	if len(a.Metrics) != len(b.Metrics) {
		t.Fatalf("metric counts differ: %d vs %d", len(a.Metrics), len(b.Metrics))
	}
	for i := range a.Metrics {
		if a.Metrics[i] != b.Metrics[i] {
			t.Fatalf("metric[%d]: %+v vs %+v", i, a.Metrics[i], b.Metrics[i])
		}
	}
	if len(a.Motifs.Groups) != len(b.Motifs.Groups) {
		t.Fatalf("motif group counts differ: %d vs %d", len(a.Motifs.Groups), len(b.Motifs.Groups))
	}
	for i := range a.Motifs.Groups {
		if a.Motifs.Groups[i] != b.Motifs.Groups[i] {
			t.Fatalf("motif[%d]: %+v vs %+v", i, a.Motifs.Groups[i], b.Motifs.Groups[i])
		}
	}
}
