package anomalies

import (
	"fmt"
	"sort"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
	"github.com/kgatilin/archmotif/internal/metrics"
)

// ModularityDetector flags package communities that are unusually
// large compared to siblings — the "god package" pattern. The
// detector ignores the graph-scope Q value (a single number with no
// distribution) and inspects region-scope size records.
//
// Per ADR-020 we use the modified z-score on community sizes with
// threshold 3.0. Communities of size 1 (orphans) are excluded from
// the population to avoid a long tail of singletons compressing the
// median to 1 and surfacing every nontrivial package.
//
// When MAD is 0 (e.g. all sibling packages have the same size — the
// canonical "one big, many small" anti-pattern), the detector falls
// back to a ratio test: a community whose size is ≥ ModularityRatioFloor
// times the median is flagged with score = size / median.
type ModularityDetector struct{}

// ModularityModZThreshold is the modified-z floor for flagging an
// oversize community.
const ModularityModZThreshold = 3.0

// ModularityRatioFloor is the size-vs-median ratio used when MAD is
// 0 (all siblings the same size). 5x the median is a generous floor
// that still catches the "one giant package, many small ones" case.
const ModularityRatioFloor = 5.0

// Name returns the detector identifier.
func (ModularityDetector) Name() string { return "modularity" }

// Metric returns the metric this detector consumes.
func (ModularityDetector) Metric() string { return "modularity" }

// Description returns the detector documentation string.
func (ModularityDetector) Description() string {
	return "flags package communities significantly larger than siblings"
}

// Configurable returns user-tunable knobs.
func (ModularityDetector) Configurable() map[string]any {
	return map[string]any{
		"modz_threshold": ModularityModZThreshold,
		"ratio_floor":    ModularityRatioFloor,
	}
}

// Detect inspects modularity region records and emits anomalies for
// oversize package communities.
func (d ModularityDetector) Detect(gr *mgraph.Graph, records []metrics.Record) ([]Anomaly, error) {
	type entry struct {
		target  string
		size    float64
		members []string
	}
	entries := make([]entry, 0, len(records))
	for _, r := range records {
		if r.Metric != d.Metric() || r.Scope != metrics.ScopeRegion {
			continue
		}
		entries = append(entries, entry{
			target:  r.Target,
			size:    r.Value,
			members: membersFromDetails(r.Details),
		})
	}
	if len(entries) == 0 {
		return nil, nil
	}
	// Build the population from non-singleton communities.
	pop := make([]float64, 0, len(entries))
	for _, e := range entries {
		if e.size <= 1 {
			continue
		}
		pop = append(pop, e.size)
	}
	if len(pop) < 2 {
		return nil, nil
	}

	popMedian := median(pop)
	out := make([]Anomaly, 0)
	for _, e := range entries {
		if e.size <= 1 {
			continue
		}
		flagged := false
		score := 0.0
		code := ""
		msg := ""
		details := map[string]any{
			"size":       int(e.size),
			"population": len(pop),
		}
		if modZ, modOK := modifiedZScore(e.size, pop); modOK && modZ >= ModularityModZThreshold {
			flagged = true
			score = modZ
			code = "oversize_community"
			msg = fmt.Sprintf("package %s is unusually large (%d members, modified z = %.2f vs %d sibling packages)",
				e.target, int(e.size), modZ, len(pop))
			details["modified_z"] = modZ
			details["modz_threshold"] = ModularityModZThreshold
		} else if popMedian > 0 && e.size/popMedian >= ModularityRatioFloor {
			flagged = true
			score = e.size / popMedian
			code = "oversize_community_ratio"
			msg = fmt.Sprintf("package %s is %.1fx the median sibling size (%d vs median %.0f, MAD=0)",
				e.target, e.size/popMedian, int(e.size), popMedian)
			details["ratio"] = e.size / popMedian
			details["ratio_floor"] = ModularityRatioFloor
			details["median"] = popMedian
		}
		if !flagged {
			continue
		}
		members := append([]string(nil), e.members...)
		sort.Strings(members)
		region := Region{
			Kind:      string(metrics.ScopeRegion),
			Members:   members,
			PrimaryID: e.target,
		}
		region = resolveRegion(gr, region)

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
				Scope:  string(metrics.ScopeRegion),
				Target: e.target,
				Value:  e.size,
				Details: map[string]any{
					"members": anyifyStrings(e.members),
				},
			},
		})
	}
	return out, nil
}

// anyifyStrings converts a []string to []any for Details payloads
// destined for JSON marshalling.
func anyifyStrings(xs []string) []any {
	out := make([]any, len(xs))
	for i, x := range xs {
		out[i] = x
	}
	return out
}

func init() { Register(ModularityDetector{}) }
