// Package archmotifmetrics is the public bridge that lets external tools
// (notably archlint) compute archmotif's graph metrics and anomalies directly
// from an in-memory graph — no GraphML, no reflection, no subprocess.
//
// A caller builds the typed graph with pkg/archmotifimport.Builder, calls
// Build() to get a *archmotifimport.Graph, and hands it to ComputeMetrics.
// Because this package lives under the module root it may import the fork's
// internal/{metrics,anomalies} directly; the result is projected onto a small
// public surface (Metrics) so callers never see internal types.
//
// Default metric set is curated for a CI linter (Newman modularity Q + motif
// redundancy and their anomaly detectors); spectral/symmetry are available via
// ComputeMetricsNamed but intentionally not in the default to avoid academic
// overkill on large graphs (see kgatilin-reflex-archmotif research, variant A).
package archmotifmetrics

import (
	"errors"

	"github.com/kgatilin/archmotif/internal/anomalies"
	"github.com/kgatilin/archmotif/internal/metrics"
	"github.com/kgatilin/archmotif/pkg/archmotifimport"
)

// defaultMetricNames is the curated metric set computed by ComputeMetrics.
// These are the ones that feed the registered anomaly detectors below and
// stay cheap + explainable for a CI linter.
var defaultMetricNames = []string{"modularity", "motif_redundancy"}

// GraphMetric is a single graph-scope metric value (e.g. modularity Q).
type GraphMetric struct {
	Metric string  `json:"metric"`
	Value  float64 `json:"value"`
}

// Anomaly is the projected, internal-free view of one flagged region.
type Anomaly struct {
	Metric    string   `json:"metric"`
	Detector  string   `json:"detector"`
	Code      string   `json:"code"`
	Message   string   `json:"message"`
	Score     float64  `json:"score"`
	Scope     string   `json:"scope"`               // graph | region | node | edge
	PrimaryID string   `json:"primaryID,omitempty"` // central node id (empty for graph scope)
	Members   []string `json:"members,omitempty"`   // all node ids in the region
}

// Metrics is the public result of a bridge computation. It carries the headline
// modularity Q for convenience plus all graph-scope metric values, the detected
// anomalies, and bookkeeping (what ran, what errored) so a caller can decide
// whether to trust the result or fall back to its own metrics.
type Metrics struct {
	// Modularity is Newman's Q over package boundaries; HasModularity is false
	// if the metric did not run (then Modularity is the zero value, not valid).
	Modularity    float64       `json:"modularity"`
	HasModularity bool          `json:"hasModularity"`
	Graph         []GraphMetric `json:"graph"`
	Anomalies     []Anomaly     `json:"anomalies"`
	MetricsRan    []string      `json:"metricsRan"`
	DetectorsRan  []string      `json:"detectorsRan"`
	Errors        []string      `json:"errors,omitempty"`
}

// ComputeMetrics runs the curated default metric+anomaly set against g and
// projects the result onto the public Metrics surface. It never panics on a
// well-formed graph; a nil graph is the only hard error.
func ComputeMetrics(g *archmotifimport.Graph) (Metrics, error) {
	return ComputeMetricsNamed(g, defaultMetricNames, nil)
}

// ComputeMetricsNamed is ComputeMetrics with explicit selection. metricNames
// selects which metrics to compute (nil/empty = all registered, incl.
// spectral/symmetry — heavier). detectorNames selects anomaly detectors
// (nil/empty = all registered detectors over the produced records).
func ComputeMetricsNamed(g *archmotifimport.Graph, metricNames, detectorNames []string) (Metrics, error) {
	if g == nil {
		return Metrics{}, errors.New("archmotifmetrics: nil graph")
	}

	mres := metrics.Run(g, metricNames)
	ares := anomalies.Run(g, mres.Records, detectorNames)

	out := Metrics{
		MetricsRan:   mres.Ran,
		DetectorsRan: ares.Ran,
	}
	for _, r := range mres.Records {
		if r.Scope != metrics.ScopeGraph {
			continue
		}
		out.Graph = append(out.Graph, GraphMetric{Metric: r.Metric, Value: r.Value})
		if r.Metric == "modularity" {
			out.Modularity = r.Value
			out.HasModularity = true
		}
	}
	for _, a := range ares.Anomalies {
		out.Anomalies = append(out.Anomalies, Anomaly{
			Metric:    a.Metric,
			Detector:  a.Detector,
			Code:      a.Reason.Code,
			Message:   a.Reason.Message,
			Score:     a.Score,
			Scope:     a.Region.Kind,
			PrimaryID: a.Region.PrimaryID,
			Members:   a.Region.Members,
		})
	}
	for _, e := range mres.Errors {
		out.Errors = append(out.Errors, e.Error())
	}
	for _, e := range ares.Errors {
		out.Errors = append(out.Errors, e.Error())
	}
	return out, nil
}
