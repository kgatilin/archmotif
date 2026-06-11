package graphmlx

import (
	"fmt"
	"sort"
)

// OrphanBucketDetector flags every group of degree-0 nodes that share
// the same `kind`/`entity`/`type` attribute. Orphans are nodes with no
// connections in or out — for code GraphML they are unreferenced
// declarations, for memory GraphML they are stranded session stubs.
//
// One Finding per bucket, members sorted ascending. Severity escalates
// with bucket size (per ADR-040 spec for next-batch optimizer).
type OrphanBucketDetector struct{}

// Name returns the detector identifier.
func (OrphanBucketDetector) Name() string { return "orphan_bucket" }

// Description returns the detector documentation string.
func (OrphanBucketDetector) Description() string {
	return "groups degree-0 nodes by labels/entity/type"
}

// Detect emits one finding per non-empty orphan bucket.
func (d OrphanBucketDetector) Detect(g *Graph) ([]Finding, error) {
	if g == nil {
		return nil, nil
	}
	// Compute touched-by-edge set in O(E).
	touched := make(map[string]struct{}, len(g.Nodes))
	for _, e := range g.Edges {
		touched[e.From] = struct{}{}
		touched[e.To] = struct{}{}
	}
	buckets := map[string][]string{}
	for _, n := range g.Nodes {
		if _, ok := touched[n.ID]; ok {
			continue
		}
		buckets[orphanBucketKey(n)] = append(buckets[orphanBucketKey(n)], n.ID)
	}
	keys := make([]string, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]Finding, 0, len(keys))
	for _, k := range keys {
		members := buckets[k]
		sort.Strings(members)
		score := float64(len(members))
		out = append(out, Finding{
			Detector:  d.Name(),
			Score:     score,
			Severity:  orphanSeverity(len(members)),
			PrimaryID: members[0],
			Members:   members,
			Reason: Reason{
				Code:    "orphan_bucket",
				Message: fmt.Sprintf("%d orphan node(s) sharing bucket %q", len(members), k),
				Details: map[string]any{
					"bucket": k,
					"count":  len(members),
				},
			},
			Evidence: map[string]any{
				"bucket":     k,
				"count":      len(members),
				"first":      members[0],
				"sample":     sampleIDs(members, 5),
				"totalNodes": len(g.Nodes),
			},
		})
	}
	return out, nil
}

func orphanBucketKey(n Node) string {
	// Prefer kind, then entity, then type — matches the issue scope.
	if v := pickFirst(n.Attrs, "kind", "entity", "type"); v != "" {
		return v
	}
	return "unknown"
}

func orphanSeverity(count int) Severity {
	switch {
	case count >= 25:
		return SeverityHigh
	case count >= 10:
		return SeverityMedium
	case count >= 3:
		return SeverityLow
	default:
		return SeverityInfo
	}
}

func sampleIDs(ids []string, max int) []string {
	if len(ids) <= max {
		out := make([]string, len(ids))
		copy(out, ids)
		return out
	}
	out := make([]string, max)
	copy(out, ids[:max])
	return out
}

func init() { Register(OrphanBucketDetector{}) }
