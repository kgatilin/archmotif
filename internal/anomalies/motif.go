package anomalies

import (
	"fmt"
	"math"
	"sort"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
	"github.com/kgatilin/archmotif/internal/metrics"
)

// MotifRedundancyDetector flags repeated isomorphic subgraphs whose
// instance count is unusually high, or which exceed an absolute
// "too many copies" floor.
//
// Per ADR-020 the score for each anomaly is the higher of:
//   - the modified z-score of the group's instance count against
//     the population of all groups, when MAD > 0;
//   - the instance count itself when ≥ MotifMinInstances (the
//     small-graph floor — synthetic fixtures often have one
//     dominant motif and no useful population).
//
// Per ADR-021 each instance becomes its own Anomaly with the same
// group canonical form in Reason.Details. Stage 5 will deduplicate
// by canonical form before proposing rewrites.
type MotifRedundancyDetector struct{}

// Per-detector defaults. Tuned against the archmotif self-graph
// (Stage 4 verify run) and the TwoStores fixture.
const (
	// MotifMinInstances is the absolute floor — three or more
	// instances of the same isomorphism class is always flagged. Two
	// instances is the minimum the metric ever emits, so flagging at
	// 2 would mean "every motif is anomalous"; 3 is the smallest
	// number that signals genuine repetition rather than coincidence.
	MotifMinInstances = 3
	// MotifModZThreshold is the modified-z threshold for flagging on
	// distributional grounds. Looser than the per-node default (3.0
	// vs 3.5) because the population is small (typically 5–20 motif
	// groups on a real graph).
	MotifModZThreshold = 3.0
)

// Name returns the detector identifier.
func (MotifRedundancyDetector) Name() string { return "motif_redundancy" }

// Metric returns the metric this detector consumes.
func (MotifRedundancyDetector) Metric() string { return "motif_redundancy" }

// Description returns the detector documentation string.
func (MotifRedundancyDetector) Description() string {
	return "flags repeated motif groups with unusually many instances"
}

// Configurable returns user-tunable knobs.
func (MotifRedundancyDetector) Configurable() map[string]any {
	return map[string]any{
		"min_instances":  MotifMinInstances,
		"modz_threshold": MotifModZThreshold,
	}
}

// Detect inspects motif region records and emits one Anomaly per
// instance of each flagged group.
func (d MotifRedundancyDetector) Detect(gr *mgraph.Graph, records []metrics.Record) ([]Anomaly, error) {
	groups := make([]motifGroup, 0, len(records))
	for _, r := range records {
		if r.Metric != d.Metric() || r.Scope != metrics.ScopeRegion {
			continue
		}
		mg := motifGroup{
			regionID: r.Target,
			count:    r.Value,
		}
		if r.Details != nil {
			if v, ok := r.Details["canonical"].(string); ok {
				mg.canonical = v
			}
			if v, ok := r.Details["size"].(int); ok {
				mg.size = v
			} else if v, ok := r.Details["size"].(float64); ok {
				mg.size = int(v)
			}
			if raw, ok := r.Details["instances"].([]any); ok {
				for _, item := range raw {
					if inst, ok := item.([]string); ok {
						mg.instances = append(mg.instances, inst)
						continue
					}
					if asAny, ok := item.([]any); ok {
						strs := make([]string, 0, len(asAny))
						for _, x := range asAny {
							if s, ok := x.(string); ok {
								strs = append(strs, s)
							}
						}
						mg.instances = append(mg.instances, strs)
					}
				}
			}
		}
		groups = append(groups, mg)
	}
	if len(groups) == 0 {
		return nil, nil
	}

	counts := make([]float64, len(groups))
	for i, mg := range groups {
		counts[i] = mg.count
	}

	out := make([]Anomaly, 0)
	for _, grp := range groups {
		modZ, modOK := modifiedZScore(grp.count, counts)
		flagged := false
		score := 0.0
		code := ""
		msg := ""
		details := map[string]any{
			"canonical":  grp.canonical,
			"size":       grp.size,
			"instances":  int(grp.count),
			"groupID":    grp.regionID,
			"population": len(counts),
		}
		if modOK && modZ >= MotifModZThreshold {
			flagged = true
			score = math.Max(score, modZ)
			code = "modz_above_threshold"
			msg = fmt.Sprintf("motif group %s has %d instances (modified z = %.2f vs population of %d groups)",
				grp.regionID, int(grp.count), modZ, len(counts))
			details["modified_z"] = modZ
			details["modz_threshold"] = MotifModZThreshold
		}
		if int(grp.count) >= MotifMinInstances {
			absScore := grp.count
			if absScore > score {
				score = absScore
			}
			if !flagged {
				code = "instance_floor"
				msg = fmt.Sprintf("motif group %s has %d instances (≥ floor of %d)",
					grp.regionID, int(grp.count), MotifMinInstances)
			}
			flagged = true
			details["min_instances"] = MotifMinInstances
		}
		if !flagged {
			continue
		}

		// Emit one Anomaly per instance of this group. They share the
		// same score and reason but each carries its own region.
		for instIdx, members := range grp.instances {
			memCopy := append([]string(nil), members...)
			sort.Strings(memCopy)
			region := Region{
				Kind:    string(metrics.ScopeRegion),
				Members: memCopy,
			}
			if len(memCopy) > 0 {
				region.PrimaryID = memCopy[0]
			}
			region = resolveRegion(gr, region)

			instDetails := make(map[string]any, len(details)+1)
			for k, v := range details {
				instDetails[k] = v
			}
			instDetails["instanceIndex"] = instIdx

			out = append(out, Anomaly{
				Metric:   d.Metric(),
				Detector: d.Name(),
				Score:    score,
				Region:   region,
				Reason: Reason{
					Code:    code,
					Message: msg,
					Details: instDetails,
				},
				SourceRecord: SourceRecord{
					Scope:   string(metrics.ScopeRegion),
					Target:  grp.regionID,
					Value:   grp.count,
					Details: cloneDetails(grp),
				},
			})
		}
	}
	return out, nil
}

// cloneDetails reproduces the metric details for SourceRecord. Kept
// separate so the SourceRecord stays a faithful snapshot of the
// metric output, independent of the detector-specific Reason.Details.
func cloneDetails(g motifGroup) map[string]any {
	out := map[string]any{
		"canonical": g.canonical,
		"size":      g.size,
	}
	insts := make([]any, 0, len(g.instances))
	for _, inst := range g.instances {
		members := make([]any, 0, len(inst))
		for _, m := range inst {
			members = append(members, m)
		}
		insts = append(insts, members)
	}
	if len(insts) > 0 {
		out["instances"] = insts
	}
	return out
}

// motifGroup consolidates the per-region motif data extracted from
// the metrics output before flagging.
type motifGroup struct {
	regionID  string
	canonical string
	size      int
	count     float64
	instances [][]string
}

func init() { Register(MotifRedundancyDetector{}) }
