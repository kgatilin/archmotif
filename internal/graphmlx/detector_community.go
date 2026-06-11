package graphmlx

import (
	"fmt"
	"sort"
)

// CommunityParentMismatchDetector flags nodes whose declared structural
// parent (a hierarchical edge predecessor) disagrees with the
// community/cluster they were detected to belong to. The community
// label is read from the `community`, `cluster`, or `module` data
// attribute (whichever is present); we don't run our own community
// detection — the assumption is the input GraphML already carries
// community labels (Louvain, Leiden, etc. assigned upstream).
//
// One Finding per community whose nodes don't all share the same
// structural parent. PrimaryID is the most-common parent in the
// community; "wrong" members are flagged in Evidence.outliers.
type CommunityParentMismatchDetector struct{}

// Name returns the detector identifier.
func (CommunityParentMismatchDetector) Name() string { return "community_parent_mismatch" }

// Description returns the detector documentation string.
func (CommunityParentMismatchDetector) Description() string {
	return "flags communities whose nodes disagree with their structural parent"
}

// Detect emits one finding per community where the structural parent
// majority does not match every member.
func (d CommunityParentMismatchDetector) Detect(g *Graph) ([]Finding, error) {
	if g == nil {
		return nil, nil
	}
	parentByChild := structuralParents(g)
	communityByNode := map[string]string{}
	for _, n := range g.Nodes {
		c := pickFirst(n.Attrs, "community", "cluster", "module")
		if c != "" {
			communityByNode[n.ID] = c
		}
	}
	if len(communityByNode) == 0 {
		return nil, nil
	}
	// Group nodes by community.
	groups := map[string][]string{}
	for n, c := range communityByNode {
		groups[c] = append(groups[c], n)
	}
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make([]Finding, 0)
	for _, c := range keys {
		members := groups[c]
		sort.Strings(members)
		if len(members) < 2 {
			continue
		}
		// Tally parents.
		parentCounts := map[string]int{}
		hasAny := false
		for _, m := range members {
			p, ok := parentByChild[m]
			if !ok {
				parentCounts[""]++
				continue
			}
			hasAny = true
			parentCounts[p]++
		}
		if !hasAny {
			continue
		}
		// Determine majority parent.
		majority := ""
		majorityCount := -1
		majorityKeys := make([]string, 0, len(parentCounts))
		for p := range parentCounts {
			majorityKeys = append(majorityKeys, p)
		}
		sort.Strings(majorityKeys) // deterministic tie-break
		for _, p := range majorityKeys {
			if parentCounts[p] > majorityCount {
				majority = p
				majorityCount = parentCounts[p]
			}
		}
		// Outliers = members whose parent != majority.
		outliers := make([]string, 0)
		for _, m := range members {
			p := parentByChild[m]
			if p != majority {
				outliers = append(outliers, m)
			}
		}
		if len(outliers) == 0 {
			continue
		}
		sort.Strings(outliers)
		score := float64(len(outliers))
		out = append(out, Finding{
			Detector:  d.Name(),
			Score:     score,
			Severity:  communityMismatchSeverity(len(outliers), len(members)),
			PrimaryID: majority,
			Members:   members,
			Reason: Reason{
				Code:    "community_parent_mismatch",
				Message: fmt.Sprintf("community %s has %d/%d outlier(s) disagreeing with majority parent %s", c, len(outliers), len(members), displayID(majority)),
				Details: map[string]any{
					"community":      c,
					"majorityParent": majority,
					"outliers":       outliers,
				},
			},
			Evidence: map[string]any{
				"community":      c,
				"majorityParent": majority,
				"members":        members,
				"outliers":       outliers,
				"parentCounts":   parentCounts,
			},
		})
	}
	return out, nil
}

// structuralParents builds child->parent map from hierarchical edges.
// If a child has multiple structural parents, the lexicographically
// smallest is chosen (deterministic; multi-parent is itself a smell
// hierarchy_cycle would catch when the parents form a cycle).
func structuralParents(g *Graph) map[string]string {
	parents := map[string][]string{}
	for _, e := range g.Edges {
		if !isHierarchyKind(e.Kind) {
			continue
		}
		parents[e.To] = append(parents[e.To], e.From)
	}
	out := make(map[string]string, len(parents))
	for c, ps := range parents {
		sort.Strings(ps)
		out[c] = ps[0]
	}
	return out
}

func displayID(id string) string {
	if id == "" {
		return "(none)"
	}
	return id
}

func communityMismatchSeverity(outliers, total int) Severity {
	if total == 0 {
		return SeverityInfo
	}
	ratio := float64(outliers) / float64(total)
	switch {
	case ratio >= 0.5:
		return SeverityHigh
	case ratio >= 0.25:
		return SeverityMedium
	default:
		return SeverityLow
	}
}

func init() { Register(CommunityParentMismatchDetector{}) }
