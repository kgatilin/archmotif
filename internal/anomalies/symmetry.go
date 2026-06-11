package anomalies

import (
	"fmt"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
	"github.com/kgatilin/archmotif/internal/metrics"
)

// LocalSymmetryDetector flags nodes whose symmetry score is
// unusually high: many ≤2-hop neighbours playing an interchangeable
// role. High symmetry signals an over-replicated structural pattern
// — exactly what concepts.md §5 describes as a candidate for
// extract-interface / extract-function.
//
// Per ADR-020 the detector uses a modified z-score with a stricter
// threshold (3.5 vs 3.0) because the population is large
// (one record per non-Package node, often hundreds), so the MAD is
// well-defined and we can afford a tighter cutoff.
type LocalSymmetryDetector struct{}

// LocalSymmetryModZThreshold is the modified-z floor for flagging.
const LocalSymmetryModZThreshold = 3.5

// LocalSymmetryAbsFloor is an absolute fallback when MAD is 0
// (typical on tiny graphs where every node has score 0 except a
// pocket of symmetric nodes). A node with ≥ 2 interchangeable
// neighbours where most peers have 0 is worth surfacing.
const LocalSymmetryAbsFloor = 2

// Name returns the detector identifier.
func (LocalSymmetryDetector) Name() string { return "local_symmetry" }

// Metric returns the metric this detector consumes.
func (LocalSymmetryDetector) Metric() string { return "local_symmetry" }

// Description returns the detector documentation string.
func (LocalSymmetryDetector) Description() string {
	return "flags nodes with unusually many interchangeable ≤2-hop neighbours"
}

// Configurable returns user-tunable knobs.
func (LocalSymmetryDetector) Configurable() map[string]any {
	return map[string]any{
		"modz_threshold": LocalSymmetryModZThreshold,
		"abs_floor":      LocalSymmetryAbsFloor,
	}
}

// Detect inspects local_symmetry node records and emits anomalies
// for nodes whose score crosses the modified-z threshold or the
// absolute floor (when MAD is 0).
func (d LocalSymmetryDetector) Detect(gr *mgraph.Graph, records []metrics.Record) ([]Anomaly, error) {
	type entry struct {
		target    string
		value     float64
		signature string
		matchIDs  []string
	}
	entries := make([]entry, 0, len(records))
	for _, r := range records {
		if r.Metric != d.Metric() || r.Scope != metrics.ScopeNode {
			continue
		}
		e := entry{target: r.Target, value: r.Value}
		if r.Details != nil {
			if v, ok := r.Details["signature"].(string); ok {
				e.signature = v
			}
			e.matchIDs = stringList(r.Details["matchIDs"])
		}
		entries = append(entries, e)
	}
	if len(entries) == 0 {
		return nil, nil
	}

	values := make([]float64, len(entries))
	for i, e := range entries {
		values[i] = e.value
	}

	out := make([]Anomaly, 0)
	for _, e := range entries {
		modZ, modOK := modifiedZScore(e.value, values)
		flagged := false
		score := 0.0
		code := ""
		msg := ""
		details := map[string]any{
			"signature":  e.signature,
			"score":      e.value,
			"population": len(values),
		}
		if modOK && modZ >= LocalSymmetryModZThreshold {
			flagged = true
			score = modZ
			code = "modz_above_threshold"
			msg = fmt.Sprintf("node has %d interchangeable peers (modified z = %.2f)",
				int(e.value), modZ)
			details["modified_z"] = modZ
			details["modz_threshold"] = LocalSymmetryModZThreshold
		}
		if !modOK && int(e.value) >= LocalSymmetryAbsFloor {
			flagged = true
			score = e.value
			code = "abs_floor"
			msg = fmt.Sprintf("node has %d interchangeable peers (≥ floor of %d; population MAD = 0)",
				int(e.value), LocalSymmetryAbsFloor)
			details["abs_floor"] = LocalSymmetryAbsFloor
		}
		if !flagged {
			continue
		}
		members := []string{e.target}
		region := Region{
			Kind:      string(metrics.ScopeNode),
			Members:   members,
			PrimaryID: e.target,
		}
		region = resolveRegion(gr, region)

		if len(e.matchIDs) > 0 {
			ids := make([]any, len(e.matchIDs))
			for i, id := range e.matchIDs {
				ids[i] = id
			}
			details["matchIDs"] = ids
		}

		out = append(out, Anomaly{
			Metric:   d.Metric(),
			Detector: d.Name(),
			Score:    score,
			Region:   region,
			Reason: Reason{
				Code:    code,
				Message: msg,
				Details: details,
			},
			SourceRecord: SourceRecord{
				Scope:   string(metrics.ScopeNode),
				Target:  e.target,
				Value:   e.value,
				Details: map[string]any{"signature": e.signature},
			},
		})
	}
	return out, nil
}

// stringList coerces a Details value into a []string. Accepts the
// in-process []string and the JSON-round-tripped []any forms.
func stringList(v any) []string {
	switch t := v.(type) {
	case []string:
		out := make([]string, len(t))
		copy(out, t)
		return out
	case []any:
		out := make([]string, 0, len(t))
		for _, x := range t {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func init() { Register(LocalSymmetryDetector{}) }
