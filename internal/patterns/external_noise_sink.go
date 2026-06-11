package patterns

import (
	"fmt"
	"sort"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// ExternalNoiseSink flags Type nodes that act as high-degree drains
// for inbound graph edges (Calls, Implements, Embeds, Returns, …)
// where the majority of those edges originate outside the analysed
// module. Such types are typically third-party helpers, generated
// stubs, or framework base classes; they inflate metric output for
// the in-module code without representing local design decisions.
//
// The pattern is fully topology-driven and works today using only
// node Attrs["foreign"] (set by the parser, see
// internal/parser/build.go). It does not require role metadata, so
// it returns Match / NearMatch / Mismatch — never NotApplicable.
//
// Decision rule:
//   - Consider every non-foreign Type node with InboundDegree ≥
//     MinDegree (default 5) across structural edge kinds (Calls,
//     CallsFrom, Implements, Embeds, Returns).
//   - A "noise sink" is a candidate whose ratio of inbound edges
//     originating from foreign packages exceeds ExternalRatioThreshold
//     (default 0.7).
//   - Any candidate that exceeds the threshold flips the report to
//     StatusMismatch and is listed as evidence + violation.
//   - If no candidate exceeds the threshold the report is Match.
//
// Score is the highest external-ratio observed across candidates;
// Threshold is ExternalRatioThreshold. This makes the report's
// Score/Threshold pair directly comparable across runs.
type ExternalNoiseSink struct {
	// MinDegree is the minimum total inbound structural degree a Type
	// must have to be considered a sink candidate. Below this floor a
	// type's external-ratio is statistically meaningless.
	MinDegree int
	// ExternalRatioThreshold is the fraction of inbound edges that
	// must originate outside the module before the candidate is
	// flagged. Range [0, 1].
	ExternalRatioThreshold float64
}

// Default knobs for ExternalNoiseSink. Conservative — tuned to
// surface only the worst offenders on archmotif's own graph.
const (
	defaultExternalNoiseMinDegree = 5
	defaultExternalNoiseRatio     = 0.70
)

// patternVersionExternalNoiseSink follows semver. Bump major when the
// schema or rule semantics change.
const patternVersionExternalNoiseSink = "1.0.0"

// ID returns the stable pattern identifier.
func (ExternalNoiseSink) ID() string { return "external_noise_sink" }

// Version returns the pattern's semantic version.
func (ExternalNoiseSink) Version() string { return patternVersionExternalNoiseSink }

// Description returns the human-readable description.
func (ExternalNoiseSink) Description() string {
	return "high-degree types whose inbound edges are dominated by external (foreign) packages"
}

// edgeKindsExternalNoiseSink defines the structural edges considered
// when computing inbound degree. Contains is excluded because it is
// purely structural (file-contains-type) and would dilute the signal.
// DependsOn is excluded for the same reason — it sits at the package
// level rather than the symbol level.
var edgeKindsExternalNoiseSink = map[mgraph.EdgeKind]bool{
	mgraph.EdgeCalls:      true,
	mgraph.EdgeCallsFrom:  true,
	mgraph.EdgeImplements: true,
	mgraph.EdgeEmbeds:     true,
	mgraph.EdgeReturns:    true,
}

// Run inspects g and produces a Report.
func (p ExternalNoiseSink) Run(g *mgraph.Graph) Report {
	minDegree := p.MinDegree
	if minDegree <= 0 {
		minDegree = defaultExternalNoiseMinDegree
	}
	threshold := p.ExternalRatioThreshold
	if threshold <= 0 {
		threshold = defaultExternalNoiseRatio
	}

	// Pre-compute foreign-ness per node so the inner loop is cheap.
	foreign := make(map[string]bool, g.NodeCount())
	for _, n := range g.Nodes() {
		if v, ok := n.Attrs["foreign"]; ok {
			if b, ok2 := v.(bool); ok2 && b {
				foreign[n.ID] = true
			}
		}
	}

	type candidate struct {
		node     mgraph.Node
		total    int
		external int
		ratio    float64
		edges    []mgraph.Edge
	}
	var candidates []candidate

	for _, n := range g.NodesByKind(mgraph.NodeType) {
		if foreign[n.ID] {
			continue
		}
		var total, external int
		var edges []mgraph.Edge
		for _, e := range g.IncidentEdges(n.ID, mgraph.DirectionIn, "") {
			if !edgeKindsExternalNoiseSink[e.Kind] {
				continue
			}
			total++
			if foreign[e.From] {
				external++
				edges = append(edges, e)
			}
		}
		if total < minDegree {
			continue
		}
		ratio := float64(external) / float64(total)
		candidates = append(candidates, candidate{
			node: n, total: total, external: external, ratio: ratio, edges: edges,
		})
	}

	// Stable order: descending ratio, then descending external count,
	// then by node ID.
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].ratio != candidates[j].ratio {
			return candidates[i].ratio > candidates[j].ratio
		}
		if candidates[i].external != candidates[j].external {
			return candidates[i].external > candidates[j].external
		}
		return candidates[i].node.ID < candidates[j].node.ID
	})

	report := Report{
		ID:        p.ID(),
		Version:   p.Version(),
		Threshold: threshold,
		Metrics: map[string]any{
			"min_degree":               minDegree,
			"external_ratio_threshold": threshold,
			"candidate_count":          len(candidates),
		},
	}

	var topRatio float64
	var flagged []candidate
	for _, c := range candidates {
		if c.ratio > topRatio {
			topRatio = c.ratio
		}
		if c.ratio >= threshold {
			flagged = append(flagged, c)
		}
	}
	report.Score = topRatio

	switch {
	case len(candidates) == 0:
		report.Status = StatusMatch
		report.Reason = "no in-module Type meets the inbound-degree floor; nothing to flag"
	case len(flagged) == 0:
		report.Status = StatusMatch
		report.Reason = fmt.Sprintf(
			"%d candidate type(s) considered; highest external-edge ratio %.2f is below threshold %.2f",
			len(candidates), topRatio, threshold,
		)
	default:
		report.Status = StatusMismatch
		report.Reason = fmt.Sprintf(
			"%d type(s) act as external-noise sinks (external-edge ratio ≥ %.2f)",
			len(flagged), threshold,
		)
		for _, c := range flagged {
			report.EvidenceNodes = append(report.EvidenceNodes, EvidenceNode{
				ID:   c.node.ID,
				Kind: string(c.node.Kind),
				Name: c.node.Name,
				Reason: fmt.Sprintf(
					"%d/%d inbound structural edges (%.0f%%) originate from foreign packages",
					c.external, c.total, c.ratio*100,
				),
			})
			for _, e := range c.edges {
				report.EvidenceEdges = append(report.EvidenceEdges, EvidenceEdge{
					From: e.From,
					To:   e.To,
					Kind: string(e.Kind),
				})
			}
			report.Violations = append(report.Violations, Violation{
				Code: "external_noise_sink.high_external_ratio",
				Message: fmt.Sprintf(
					"%s receives %d/%d inbound structural edges (%.0f%%) from foreign packages",
					c.node.Name, c.external, c.total, c.ratio*100,
				),
				Nodes: []string{c.node.ID},
			})
		}
		report.Recommendations = []string{
			"consider suppressing or downweighting the flagged types in metric output (Stage 4 anomaly detector knob)",
			"if a flagged type is project-owned, review whether its API is too welcoming to external callers",
		}
	}
	return report
}

func init() { Register(ExternalNoiseSink{}) }
