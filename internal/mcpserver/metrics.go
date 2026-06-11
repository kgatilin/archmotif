package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/kgatilin/archmotif/internal/metrics"
)

// MetricCacheDir is the on-disk location for cached metric results, relative
// to the user's home directory. The full path is resolved lazily through
// resolveMetricCacheDir so tests can redirect via $ARCHMOTIF_CACHE_HOME.
const MetricCacheDir = ".archmotif/metrics-cache"

// MetricResult is the cache-friendly wrapper around metrics.Result for one
// metric on one graph.
type MetricResult struct {
	Metric  string           `json:"metric"`
	GraphID string           `json:"graph_id"`
	Hash    string           `json:"hash"`
	Records []metrics.Record `json:"records"`
	Summary float64          `json:"summary"` // graph-scope value (NaN if metric has no graph-scope record)
}

// MetricDelta is the delta entry returned by graph_compare_metrics.
type MetricDelta struct {
	Metric string  `json:"metric"`
	A      float64 `json:"a"`
	B      float64 `json:"b"`
	Delta  float64 `json:"delta"` // B - A
	Note   string  `json:"note,omitempty"`
}

// CompareReport is the structured output of graph_compare_metrics.
type CompareReport struct {
	GraphA  string        `json:"graph_a"`
	GraphB  string        `json:"graph_b"`
	Metrics []MetricDelta `json:"metrics"`
}

// metricCacheMu serialises writes within a single process; on-disk reads are
// idempotent so concurrent reads do not race meaningfully.
var metricCacheMu sync.Mutex

// resolveMetricCacheDir returns the absolute cache directory, honoring
// $ARCHMOTIF_CACHE_HOME (used in tests) and falling back to $HOME/.archmotif/
// metrics-cache.
func resolveMetricCacheDir() (string, error) {
	if env := os.Getenv("ARCHMOTIF_CACHE_HOME"); env != "" {
		return filepath.Join(env, "metrics-cache"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolveMetricCacheDir: %w", err)
	}
	return filepath.Join(home, MetricCacheDir), nil
}

// cachePath maps (graphID, hash, metricID) onto a deterministic cache path.
func cachePath(graphID, hash, metricID string) (string, error) {
	dir, err := resolveMetricCacheDir()
	if err != nil {
		return "", err
	}
	slug, variant, err := splitGraphID(graphID)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, slug, variant, hash, Slug(metricID)+".json"), nil
}

// loadCachedMetric reads a previously-saved metric result. Missing files
// return (nil, nil).
func loadCachedMetric(graphID, hash, metricID string) (*MetricResult, error) {
	p, err := cachePath(graphID, hash, metricID)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out MetricResult
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// saveCachedMetric writes one metric result to disk. The write is atomic
// (temp + rename).
func saveCachedMetric(graphID, hash, metricID string, res *MetricResult) error {
	metricCacheMu.Lock()
	defer metricCacheMu.Unlock()

	p, err := cachePath(graphID, hash, metricID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), filepath.Base(p)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if tmp != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()
	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(res); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	tmp = nil
	return os.Rename(tmpName, p)
}

// ComputeMetric runs the named metric against graphID, caching the result on
// disk under (graphID, hash, metric). If a cached result exists and the graph
// hash hasn't changed, the cached value is returned without recomputing.
//
// useCache=false forces a fresh run (used by `force=true` callers) but still
// writes the new result to disk.
//
// ctx is forwarded to the metric's Compute so a slow metric does not stall
// the calling MCP worker thread; cancel ctx to bail.
func (s *Service) ComputeMetric(ctx context.Context, graphID, metricID string, useCache bool) (*MetricResult, error) {
	m, ok := metrics.Lookup(metricID)
	if !ok {
		return nil, fmt.Errorf("ComputeMetric: metric %q not registered", metricID)
	}
	hash, err := s.graphHash(graphID)
	if err != nil {
		return nil, fmt.Errorf("ComputeMetric: hash: %w", err)
	}
	if useCache {
		if cached, err := loadCachedMetric(graphID, hash, metricID); err == nil && cached != nil {
			return cached, nil
		}
	}
	tg, err := s.loadTypedGraph(graphID)
	if err != nil {
		return nil, err
	}
	records, err := m.Compute(ctx, tg)
	if err != nil {
		return nil, fmt.Errorf("ComputeMetric: %w", err)
	}
	summary := math.NaN()
	for _, rec := range records {
		if rec.Scope == metrics.ScopeGraph {
			summary = rec.Value
			break
		}
	}
	res := &MetricResult{
		Metric:  metricID,
		GraphID: graphID,
		Hash:    hash,
		Records: records,
		Summary: summary,
	}
	if err := saveCachedMetric(graphID, hash, metricID, res); err != nil {
		// Cache write failure is non-fatal — return the live result.
		_ = err
	}
	return res, nil
}

// CompareMetrics runs each named metric on graphA and graphB and returns one
// delta per metric. Empty metricIDs means "every registered metric". ctx is
// forwarded to each metric so cancellation aborts the whole run.
func (s *Service) CompareMetrics(ctx context.Context, aID, bID string, metricIDs []string) (CompareReport, error) {
	if len(metricIDs) == 0 {
		for _, m := range metrics.All() {
			metricIDs = append(metricIDs, m.Name())
		}
	}
	sort.Strings(metricIDs)
	out := CompareReport{GraphA: aID, GraphB: bID}
	for _, id := range metricIDs {
		entry := MetricDelta{Metric: id}
		a, errA := s.ComputeMetric(ctx, aID, id, true)
		b, errB := s.ComputeMetric(ctx, bID, id, true)
		if errA != nil && errB != nil {
			entry.Note = fmt.Sprintf("both sides failed: a=%v b=%v", errA, errB)
			out.Metrics = append(out.Metrics, entry)
			continue
		}
		if errA != nil {
			entry.Note = fmt.Sprintf("graph_a failed: %v", errA)
		}
		if errB != nil {
			entry.Note = fmt.Sprintf("graph_b failed: %v", errB)
		}
		if a != nil {
			entry.A = a.Summary
		} else {
			entry.A = math.NaN()
		}
		if b != nil {
			entry.B = b.Summary
		} else {
			entry.B = math.NaN()
		}
		entry.Delta = entry.B - entry.A
		out.Metrics = append(out.Metrics, entry)
	}
	return out, nil
}

// DriftReport mirrors the catalog.Drift shape but is built directly from the
// in-memory metric results so the MCP server does not need a yaml catalog on
// disk. Each metric delta is "actual - target": positive means the actual
// graph has more of the metric than the target.
type DriftReport struct {
	Actual  string        `json:"actual"`
	Target  string        `json:"target"`
	Metrics []MetricDelta `json:"metrics"`
}

// ComputeDrift computes the per-metric delta between actualID and targetID.
// It is the MCP-callable form of `archmotif drift` for two graphs known to
// the workspace; the catalog file is bypassed in favour of live computation
// so drift can be checked against pre-merge experiment graphs. ctx is
// forwarded to each underlying metric so callers can cancel a slow run.
func (s *Service) ComputeDrift(ctx context.Context, actualID, targetID string) (DriftReport, error) {
	cmp, err := s.CompareMetrics(ctx, actualID, targetID, nil)
	if err != nil {
		return DriftReport{}, err
	}
	// "drift" wording: positive = regression on the actual side relative to
	// the target. The delta sign convention matches catalog.Drift.
	for i := range cmp.Metrics {
		// CompareMetrics returns (B - A) where A=actual, B=target. Flip so a
		// positive number means actual has more (matches catalog convention).
		cmp.Metrics[i].Delta = -cmp.Metrics[i].Delta
	}
	return DriftReport{
		Actual:  actualID,
		Target:  targetID,
		Metrics: cmp.Metrics,
	}, nil
}

// MetricInfo describes one registered metric for tools/list metadata.
type MetricInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// RegisteredMetrics returns one entry per metric in the global registry.
func RegisteredMetrics() []MetricInfo {
	all := metrics.All()
	out := make([]MetricInfo, 0, len(all))
	for _, m := range all {
		out = append(out, MetricInfo{Name: m.Name(), Description: m.Description()})
	}
	return out
}
