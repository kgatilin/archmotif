package anomalies

import (
	"fmt"
	"sort"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
	"github.com/kgatilin/archmotif/internal/metrics"
)

// CycleRankDetector flags every non-trivial SCC: a cycle in the
// dependency graph is, in archmotif's framing, *always* worth a
// look. Score = SCC size (number of participants).
//
// We do not z-score against other SCCs because (a) most graphs have
// zero or one non-trivial SCC, so the population is too small for
// MAD; (b) a 2-node cycle and a 20-node cycle are both anomalies but
// the 20-node one is more pressing — using size directly gives Stage
// 5 the right ranking signal.
type CycleRankDetector struct{}

// Name returns the detector identifier.
func (CycleRankDetector) Name() string { return "cycle_rank" }

// Metric returns the metric this detector consumes.
func (CycleRankDetector) Metric() string { return "cycle_rank" }

// Description returns the detector documentation string.
func (CycleRankDetector) Description() string {
	return "flags every non-trivial SCC; score is the SCC size"
}

// Configurable returns user-tunable knobs (none today).
func (CycleRankDetector) Configurable() map[string]any { return map[string]any{} }

// Detect emits one Anomaly per non-trivial SCC region record.
func (d CycleRankDetector) Detect(gr *mgraph.Graph, records []metrics.Record) ([]Anomaly, error) {
	out := make([]Anomaly, 0)
	for _, r := range records {
		if r.Metric != d.Metric() || r.Scope != metrics.ScopeRegion {
			continue
		}
		members := membersFromDetails(r.Details)
		sort.Strings(members)
		region := Region{
			Kind:    string(metrics.ScopeRegion),
			Members: members,
		}
		if len(members) > 0 {
			region.PrimaryID = members[0]
		}
		region = resolveRegion(gr, region)

		details := map[string]any{
			"scc_size": int(r.Value),
		}
		if len(members) > 0 {
			memCopy := make([]any, len(members))
			for i, m := range members {
				memCopy[i] = m
			}
			details["members"] = memCopy
		}

		out = append(out, Anomaly{
			Metric:   d.Metric(),
			Detector: d.Name(),
			Score:    r.Value,
			Region:   region,
			Reason: Reason{
				Code:    "scc_present",
				Message: fmt.Sprintf("non-trivial SCC %s with %d participants in the dependency subgraph", r.Target, int(r.Value)),
				Details: details,
			},
			SourceRecord: SourceRecord{
				Scope:   string(metrics.ScopeRegion),
				Target:  r.Target,
				Value:   r.Value,
				Details: copyDetails(r.Details),
			},
		})
	}
	return out, nil
}

// membersFromDetails extracts the "members" list from a metric
// record's Details map. Tolerates []string and []any (the JSON
// round-trip form).
func membersFromDetails(d map[string]any) []string {
	if d == nil {
		return nil
	}
	switch v := d["members"].(type) {
	case []string:
		out := make([]string, len(v))
		copy(out, v)
		return out
	case []any:
		out := make([]string, 0, len(v))
		for _, x := range v {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// copyDetails clones a metric record's Details map so detectors
// don't accidentally share references with the metric layer.
func copyDetails(d map[string]any) map[string]any {
	if d == nil {
		return nil
	}
	out := make(map[string]any, len(d))
	for k, v := range d {
		out[k] = v
	}
	return out
}

func init() { Register(CycleRankDetector{}) }
