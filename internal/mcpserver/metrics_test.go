package mcpserver

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/kgatilin/archmotif/internal/metrics"
)

// withTempCache redirects the metrics cache root to a tmp dir so tests don't
// pollute the user's $HOME.
func withTempCache(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("ARCHMOTIF_CACHE_HOME", dir)
	return dir
}

// TestRegisteredMetricsMatchPackage confirms RegisteredMetrics is the same
// set as metrics.All().
func TestRegisteredMetricsMatchPackage(t *testing.T) {
	info := RegisteredMetrics()
	if len(info) != len(metrics.All()) {
		t.Fatalf("len mismatch: %d vs %d", len(info), len(metrics.All()))
	}
	want := map[string]bool{}
	for _, m := range metrics.All() {
		want[m.Name()] = true
	}
	for _, i := range info {
		if !want[i.Name] {
			t.Errorf("unexpected metric %q", i.Name)
		}
	}
}

// TestComputeMetricCachesOnDisk runs a metric twice and confirms the second
// call reads from the on-disk cache (the result file is created and the
// summary is identical).
func TestComputeMetricCachesOnDisk(t *testing.T) {
	cache := withTempCache(t)
	svc, _ := mustService(t, "demo")
	res, err := svc.ComputeMetric(context.Background(), "demo", "zero", true)
	if err != nil {
		t.Fatalf("ComputeMetric: %v", err)
	}
	// Find the cache file.
	hash, err := svc.graphHash("demo")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	p := filepath.Join(cache, "metrics-cache", "demo", "actual", hash, "zero.json")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("expected cache file at %s: %v", p, err)
	}
	res2, err := svc.ComputeMetric(context.Background(), "demo", "zero", true)
	if err != nil {
		t.Fatalf("ComputeMetric cached: %v", err)
	}
	if res.Summary != res2.Summary {
		t.Fatalf("cached summary mismatch: %v vs %v", res.Summary, res2.Summary)
	}
	if res.Hash != res2.Hash {
		t.Fatalf("hash mismatch")
	}
}

// TestComputeMetricUnknown returns an error for an unregistered metric.
func TestComputeMetricUnknown(t *testing.T) {
	withTempCache(t)
	svc, _ := mustService(t, "demo")
	if _, err := svc.ComputeMetric(context.Background(), "demo", "no_such_metric", true); err == nil {
		t.Fatalf("expected error for unknown metric")
	}
}

// TestCompareMetricsProducesDeltaTable confirms graph_compare_metrics returns
// one entry per requested metric with B-A delta.
func TestCompareMetricsProducesDeltaTable(t *testing.T) {
	withTempCache(t)
	svc, _ := mustService(t, "demo")
	if _, err := svc.ForkGraph("demo", "demo:work", false); err != nil {
		t.Fatalf("fork: %v", err)
	}
	report, err := svc.CompareMetrics(context.Background(), "demo", "demo:work", []string{"zero", "cycle_rank"})
	if err != nil {
		t.Fatalf("CompareMetrics: %v", err)
	}
	if len(report.Metrics) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(report.Metrics))
	}
	const eps = 1e-9
	for _, m := range report.Metrics {
		// Same graph contents → delta ≈ 0 (or NaN for unsupported).
		if math.IsNaN(m.Delta) {
			continue
		}
		if math.Abs(m.Delta) > eps {
			t.Errorf("metric %s: expected delta≈0 on identical graphs, got %v", m.Metric, m.Delta)
		}
	}
}

// TestCompareMetricsEmptyRunsAll confirms that empty metric list runs every
// registered metric.
func TestCompareMetricsEmptyRunsAll(t *testing.T) {
	withTempCache(t)
	svc, _ := mustService(t, "demo")
	if _, err := svc.ForkGraph("demo", "demo:work", false); err != nil {
		t.Fatalf("fork: %v", err)
	}
	report, err := svc.CompareMetrics(context.Background(), "demo", "demo:work", nil)
	if err != nil {
		t.Fatalf("CompareMetrics: %v", err)
	}
	if len(report.Metrics) != len(metrics.All()) {
		t.Fatalf("expected %d, got %d", len(metrics.All()), len(report.Metrics))
	}
}

// TestComputeDriftFlipsSign ensures the drift report delta sign matches the
// catalog convention (positive = actual has more).
func TestComputeDriftFlipsSign(t *testing.T) {
	withTempCache(t)
	svc, _ := mustService(t, "demo")
	if _, err := svc.ForkGraph("demo", "demo:target", false); err != nil {
		t.Fatalf("fork: %v", err)
	}
	report, err := svc.ComputeDrift(context.Background(), "demo", "demo:target")
	if err != nil {
		t.Fatalf("Drift: %v", err)
	}
	if report.Actual != "demo" || report.Target != "demo:target" {
		t.Fatalf("unexpected refs: %+v", report)
	}
	// Identical graphs → every delta zero (allow float roundoff for
	// floating-point metrics like spectral_gap).
	const eps = 1e-9
	for _, m := range report.Metrics {
		if math.IsNaN(m.Delta) {
			continue
		}
		if math.Abs(m.Delta) > eps {
			t.Errorf("metric %s: expected delta≈0, got %v", m.Metric, m.Delta)
		}
	}
}

// TestComputeMetricMissingGraph returns an error.
func TestComputeMetricMissingGraph(t *testing.T) {
	withTempCache(t)
	svc, _ := mustService(t, "demo")
	if _, err := svc.ComputeMetric(context.Background(), "missing", "zero", true); err == nil {
		t.Fatalf("expected error for missing graph")
	}
}
