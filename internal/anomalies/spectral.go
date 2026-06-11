package anomalies

import (
	"fmt"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
	"github.com/kgatilin/archmotif/internal/metrics"
)

// SpectralGapDetector flags graphs whose algebraic connectivity is
// dangerously low: ≈ 0 means disconnected components, very small
// values mean a near-cut bottleneck. There is no per-graph
// distribution to z-score against, so the detector uses fixed
// thresholds (per ADR-020).
//
// Score is 1 / (gap + ε) so smaller gaps rank higher. The single
// graph-scope record produces at most one Anomaly.
type SpectralGapDetector struct{}

// SpectralGapDisconnected is the threshold below which the graph is
// considered disconnected (counting numerical noise).
const SpectralGapDisconnected = 1e-6

// SpectralGapFragile is the threshold below which the graph is
// flagged as having a near-cut bottleneck. Graphs above this are
// reasonably well-connected; below it, removal of a small edge set
// would split the graph.
const SpectralGapFragile = 0.05

// SpectralGapEpsilon prevents division by zero in score calculation.
const SpectralGapEpsilon = 1e-9

// Name returns the detector identifier.
func (SpectralGapDetector) Name() string { return "spectral_gap" }

// Metric returns the metric this detector consumes.
func (SpectralGapDetector) Metric() string { return "spectral_gap" }

// Description returns the detector documentation string.
func (SpectralGapDetector) Description() string {
	return "flags graphs that are disconnected or near-cut (low algebraic connectivity)"
}

// Configurable returns user-tunable knobs.
func (SpectralGapDetector) Configurable() map[string]any {
	return map[string]any{
		"disconnected_threshold": SpectralGapDisconnected,
		"fragile_threshold":      SpectralGapFragile,
	}
}

// Detect inspects spectral_gap graph records and emits at most one
// Anomaly when the gap crosses a threshold.
func (d SpectralGapDetector) Detect(_ *mgraph.Graph, records []metrics.Record) ([]Anomaly, error) {
	for _, r := range records {
		if r.Metric != d.Metric() || r.Scope != metrics.ScopeGraph {
			continue
		}
		gap := r.Value
		var (
			code string
			msg  string
		)
		switch {
		case gap < SpectralGapDisconnected:
			code = "disconnected"
			msg = fmt.Sprintf("graph is disconnected (algebraic connectivity = %.6f)", gap)
		case gap < SpectralGapFragile:
			code = "near_cut"
			msg = fmt.Sprintf("graph has a near-cut bottleneck (algebraic connectivity = %.6f, fragile threshold = %.3f)", gap, SpectralGapFragile)
		default:
			return nil, nil
		}
		score := 1.0 / (gap + SpectralGapEpsilon)
		details := map[string]any{
			"algebraic_connectivity": gap,
			"disconnected_threshold": SpectralGapDisconnected,
			"fragile_threshold":      SpectralGapFragile,
		}
		return []Anomaly{{
			Metric:   d.Metric(),
			Detector: d.Name(),
			Score:    score,
			Region:   Region{Kind: string(metrics.ScopeGraph)},
			Reason:   Reason{Code: code, Message: msg, Details: details},
			SourceRecord: SourceRecord{
				Scope:   string(metrics.ScopeGraph),
				Value:   gap,
				Details: copyDetails(r.Details),
			},
		}}, nil
	}
	return nil, nil
}

func init() { Register(SpectralGapDetector{}) }
