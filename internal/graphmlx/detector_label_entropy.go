package graphmlx

import (
	"fmt"
	"math"
	"sort"
)

// LabelEntropyHubDetector flags high-fanout parents whose children mix
// many labels/entity types. The signal: when a single node owns 10
// children but those children fall into 7 distinct kinds, the parent
// is acting as a "junk drawer" and should be split.
//
// Score is the Shannon entropy of the children's `kind`/`entity`/`type`
// distribution multiplied by the fanout (so high entropy on a 3-child
// node ranks below modest entropy on a 30-child node). Children-of are
// determined by a structural edge: kind in {"contains", "Contains",
// "part-of", "part_of", "parent", "child"} OR (when no such kind is
// present in the document) the directed edge target.
type LabelEntropyHubDetector struct {
	// MinFanout is the minimum number of children a parent must have
	// before it is considered. Defaults to 5; tuned to skip leaf nodes.
	MinFanout int
}

// Name returns the detector identifier.
func (LabelEntropyHubDetector) Name() string { return "label_entropy_hub" }

// Description returns the detector documentation string.
func (LabelEntropyHubDetector) Description() string {
	return "flags high-fanout parents whose children mix many labels/entity types"
}

// Detect emits one finding per parent above MinFanout with H > 0.
func (d LabelEntropyHubDetector) Detect(g *Graph) ([]Finding, error) {
	if g == nil {
		return nil, nil
	}
	minFanout := d.MinFanout
	if minFanout <= 0 {
		minFanout = 5
	}
	parents := childrenByParent(g)
	parentIDs := make([]string, 0, len(parents))
	for p := range parents {
		parentIDs = append(parentIDs, p)
	}
	sort.Strings(parentIDs)
	out := make([]Finding, 0)
	for _, p := range parentIDs {
		children := parents[p]
		if len(children) < minFanout {
			continue
		}
		dist := childKindDistribution(g, children)
		entropy := shannonEntropy(dist)
		if entropy <= 0 {
			continue
		}
		score := entropy * float64(len(children))
		members := append([]string{p}, append([]string(nil), children...)...)
		sort.Strings(members)
		dKinds := make([]string, 0, len(dist))
		for k := range dist {
			dKinds = append(dKinds, k)
		}
		sort.Strings(dKinds)
		out = append(out, Finding{
			Detector:  d.Name(),
			Score:     score,
			Severity:  labelEntropySeverity(entropy, len(children)),
			PrimaryID: p,
			Members:   members,
			Reason: Reason{
				Code:    "label_entropy_hub",
				Message: fmt.Sprintf("parent %s has %d children spanning %d distinct kinds (entropy=%.2f)", p, len(children), len(dist), entropy),
				Details: map[string]any{
					"fanout":  len(children),
					"kinds":   dKinds,
					"entropy": entropy,
				},
			},
			Evidence: map[string]any{
				"parent":   p,
				"fanout":   len(children),
				"kinds":    dKinds,
				"entropy":  entropy,
				"children": append([]string(nil), children...),
			},
		})
	}
	return out, nil
}

// childrenByParent groups child node IDs by their parent ID. We treat
// "structural" edges as parent→child when their kind matches the
// hierarchy whitelist; otherwise we fall back to all directed edges.
// The fallback is conservative — many memory GraphML producers don't
// declare an explicit "contains" kind.
func childrenByParent(g *Graph) map[string][]string {
	hierarchical := map[string][]string{}
	any := map[string][]string{}
	hasHierarchy := false
	for _, e := range g.Edges {
		if isHierarchyKind(e.Kind) {
			hierarchical[e.From] = append(hierarchical[e.From], e.To)
			hasHierarchy = true
		}
		any[e.From] = append(any[e.From], e.To)
	}
	src := any
	if hasHierarchy {
		src = hierarchical
	}
	for k, v := range src {
		uniq := dedupSorted(v)
		src[k] = uniq
	}
	return src
}

// isHierarchyKind returns true for edge kinds that encode a structural
// parent→child relationship. Compare lowercased to be tolerant of
// "Contains" vs "contains" vs "CONTAINS".
func isHierarchyKind(k string) bool {
	switch k {
	case "contains", "Contains", "CONTAINS",
		"part-of", "part_of", "partOf", "PartOf",
		"parent", "child", "parent_of", "parentOf",
		"has", "owns":
		return true
	}
	return false
}

// childKindDistribution counts the number of children per `kind`
// attribute (falling back to `entity`, `type`, then "unknown").
func childKindDistribution(g *Graph, children []string) map[string]int {
	out := map[string]int{}
	for _, c := range children {
		n, ok := g.Node(c)
		if !ok {
			out["unknown"]++
			continue
		}
		k := pickFirst(n.Attrs, "kind", "entity", "type")
		if k == "" {
			k = "unknown"
		}
		out[k]++
	}
	return out
}

// shannonEntropy computes H = -sum(p_i * log2(p_i)) over the
// distribution. Returns 0 when total is zero.
func shannonEntropy(dist map[string]int) float64 {
	total := 0
	for _, v := range dist {
		total += v
	}
	if total == 0 {
		return 0
	}
	h := 0.0
	for _, v := range dist {
		if v == 0 {
			continue
		}
		p := float64(v) / float64(total)
		h -= p * math.Log2(p)
	}
	return h
}

func labelEntropySeverity(entropy float64, fanout int) Severity {
	score := entropy * float64(fanout)
	switch {
	case score >= 30:
		return SeverityHigh
	case score >= 12:
		return SeverityMedium
	case score >= 4:
		return SeverityLow
	default:
		return SeverityInfo
	}
}

func dedupSorted(xs []string) []string {
	if len(xs) == 0 {
		return nil
	}
	cp := append([]string(nil), xs...)
	sort.Strings(cp)
	out := cp[:1]
	for _, s := range cp[1:] {
		if s == out[len(out)-1] {
			continue
		}
		out = append(out, s)
	}
	return out
}

func init() { Register(LabelEntropyHubDetector{}) }
