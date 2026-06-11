package catalog

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
	"github.com/kgatilin/archmotif/internal/metrics"
	"github.com/kgatilin/archmotif/internal/patterns"
)

// CaptureOptions configures a snapshot capture.
type CaptureOptions struct {
	// Label is the snapshot's identifier in the catalog. Required.
	Label string
	// Ref is recorded verbatim for human reference (usually a git SHA).
	// Optional.
	Ref string
	// Path is the path that was scanned (recorded for traceability).
	Path string
	// Pattern is the go/packages pattern that was scanned (recorded for
	// traceability).
	Pattern string
	// CapturedAt allows tests to inject a deterministic timestamp.
	// Defaults to time.Now().UTC() when zero.
	CapturedAt time.Time
	// MaxGroups overrides MaxStoredGroups for tests; ≤ 0 means use
	// the package default.
	MaxGroups int
}

// Capture runs every registered metric and pattern against g, then
// builds a Snapshot digest from the results. The graph is read-only;
// Capture allocates its own slices for the snapshot.
//
// Errors from individual metrics are surfaced as a single error
// string — Capture returns the snapshot built from the metrics that
// did succeed, plus an error so callers can decide whether to persist
// a partial snapshot. (The CLI does persist partial snapshots and
// surfaces the error to stderr; this matches `archmotif metrics`'s
// behaviour, where one broken metric does not blank the whole run.)
func Capture(g *mgraph.Graph, opts CaptureOptions) (Snapshot, error) {
	if opts.Label == "" {
		return Snapshot{}, fmt.Errorf("catalog.Capture: empty label")
	}
	captured := opts.CapturedAt
	if captured.IsZero() {
		captured = time.Now().UTC()
	}
	maxGroups := opts.MaxGroups
	if maxGroups <= 0 {
		maxGroups = MaxStoredGroups
	}

	mres := metrics.Run(g, nil)
	pres := patterns.Run(g, nil)

	snap := Snapshot{
		Label:      opts.Label,
		Ref:        opts.Ref,
		CapturedAt: captured.UTC(),
		Path:       opts.Path,
		Pattern:    opts.Pattern,
		Metrics:    digestMetrics(mres),
		Motifs:     digestMotifs(mres, maxGroups),
		Patterns:   digestPatterns(pres),
	}

	var err error
	if len(mres.Errors) > 0 {
		msgs := make([]string, 0, len(mres.Errors))
		for _, e := range mres.Errors {
			msgs = append(msgs, e.Error())
		}
		err = fmt.Errorf("metric errors: %s", strings.Join(msgs, "; "))
	}
	return snap, err
}

// digestMetrics extracts the graph-scope records and persists their
// (name, value) pairs. Output is sorted by name for stable YAML.
func digestMetrics(r metrics.Result) []MetricEntry {
	var out []MetricEntry
	for _, rec := range r.Records {
		if rec.Scope != metrics.ScopeGraph {
			continue
		}
		// motif_redundancy emits ScopeGraph for the group count plus
		// many ScopeRegion records; we capture the graph-scope record
		// here and the per-group histogram via digestMotifs.
		out = append(out, MetricEntry{Name: rec.Metric, Value: roundMetric(rec.Value)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// roundMetric snaps a metric value to MetricEpsilon precision so that
// eigendecomposition jitter (~1e-15) does not break determinism tests.
func roundMetric(v float64) float64 {
	return math.Round(v/MetricEpsilon) * MetricEpsilon
}

// digestMotifs walks the motif_redundancy region records and builds
// the histogram. The graph-scope record is read for total_groups, and
// total_instances is summed from region values (each region's value
// is its instance count, per ADR-013).
func digestMotifs(r metrics.Result, maxGroups int) MotifSummary {
	out := MotifSummary{}
	for _, rec := range r.Records {
		if rec.Metric != "motif_redundancy" {
			continue
		}
		switch rec.Scope {
		case metrics.ScopeGraph:
			out.TotalGroups = int(rec.Value)
		case metrics.ScopeRegion:
			canon, _ := rec.Details["canonical"].(string)
			if canon == "" {
				continue
			}
			size := motifSize(rec.Details)
			count := int(rec.Value)
			out.TotalInstances += count
			out.Groups = append(out.Groups, MotifGroupEntry{
				Canonical: canon,
				Size:      size,
				Count:     count,
			})
		}
	}
	sort.SliceStable(out.Groups, func(i, j int) bool {
		if out.Groups[i].Count != out.Groups[j].Count {
			return out.Groups[i].Count > out.Groups[j].Count
		}
		return out.Groups[i].Canonical < out.Groups[j].Canonical
	})
	if maxGroups > 0 && len(out.Groups) > maxGroups {
		out.Groups = out.Groups[:maxGroups]
	}
	return out
}

// motifSize extracts the size from the region record's Details map.
// Falls back to parsing the canonical-form prefix `k=N|` when the
// numeric size key is absent (older records).
func motifSize(details map[string]any) int {
	if details == nil {
		return 0
	}
	if v, ok := details["size"]; ok {
		switch n := v.(type) {
		case int:
			return n
		case int64:
			return int(n)
		case float64:
			return int(n)
		}
	}
	if canon, ok := details["canonical"].(string); ok {
		return sizeFromCanonical(canon)
	}
	return 0
}

// sizeFromCanonical mirrors the helper inside internal/metrics: the
// canonical form starts with `k=<N>|`. Duplicated here rather than
// exported from metrics because the metric package treats it as an
// internal detail.
func sizeFromCanonical(canon string) int {
	if !strings.HasPrefix(canon, "k=") {
		return 0
	}
	rest := canon[len("k="):]
	end := strings.IndexByte(rest, '|')
	if end <= 0 {
		return 0
	}
	n, err := strconv.Atoi(rest[:end])
	if err != nil {
		return 0
	}
	return n
}

// digestPatterns persists the report header for each pattern.
func digestPatterns(r patterns.RunResult) []PatternEntry {
	out := make([]PatternEntry, 0, len(r.Reports))
	for _, rep := range r.Reports {
		out = append(out, PatternEntry{
			ID:        rep.ID,
			Version:   rep.Version,
			Status:    string(rep.Status),
			Score:     rep.Score,
			Threshold: rep.Threshold,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
