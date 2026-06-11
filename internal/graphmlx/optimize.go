package graphmlx

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
)

// CurrentJSONVersion is the schema version emitted by OptimizeBatch.
// Bump on breaking changes.
const CurrentJSONVersion = 1

// OptimizeBatchOptions tunes the next-batch selection.
type OptimizeBatchOptions struct {
	// MaxBatchSize is the maximum number of findings to include in
	// the produced batch. Defaults to 10 when zero.
	MaxBatchSize int
	// Detectors limits the run to specific detector names; empty/nil
	// runs all registered detectors.
	Detectors []string
	// MinSeverity drops findings whose severity rank is below this
	// floor. Empty string accepts everything.
	MinSeverity Severity
}

// MetricSummary is a per-detector roll-up: how many findings the
// detector produced, the median/max scores, etc. Used as
// `beforeMetrics` in the next-batch JSON so consumers can see what
// the graph "looked like" before the proposed batch.
type MetricSummary struct {
	Detector        string   `json:"detector"`
	FindingCount    int      `json:"findingCount"`
	MaxScore        float64  `json:"maxScore"`
	TotalScore      float64  `json:"totalScore"`
	HighestSeverity Severity `json:"highestSeverity,omitempty"`
}

// TargetMetrics describes the post-resolution expectation: if the
// caller resolves the proposed batch, what will the metrics look
// like? We compute these by subtracting the resolved findings from
// the before-snapshot. This is intentionally optimistic — actual
// resolution may reveal more findings.
type TargetMetrics struct {
	Detector     string  `json:"detector"`
	FindingCount int     `json:"findingCount"`
	MaxScore     float64 `json:"maxScore"`
	TotalScore   float64 `json:"totalScore"`
}

// EvidenceItem is a single piece of metric evidence promoted from a
// Finding.Evidence map plus the structured Reason. The selector embeds
// these in the next-batch JSON as `metricEvidence[]`.
type EvidenceItem struct {
	Detector string         `json:"detector"`
	Severity Severity       `json:"severity"`
	Score    float64        `json:"score"`
	Primary  string         `json:"primary,omitempty"`
	Members  []string       `json:"members,omitempty"`
	Reason   Reason         `json:"reason"`
	Evidence map[string]any `json:"evidence,omitempty"`
}

// OptimizeBatchResult is the full envelope written to stdout/disk by
// the optimize-batch command.
type OptimizeBatchResult struct {
	Version         int             `json:"version"`
	GraphSummary    GraphSummary    `json:"graph"`
	BeforeMetrics   []MetricSummary `json:"beforeMetrics"`
	TargetMetrics   []TargetMetrics `json:"targetMetrics"`
	Batch           []EvidenceItem  `json:"batch"`
	MetricEvidence  []EvidenceItem  `json:"metricEvidence"`
	SelectionReason string          `json:"selectionReason"`
	Errors          []string        `json:"errors,omitempty"`
}

// GraphSummary is a tiny header for the next-batch JSON: node and
// edge counts so consumers can sanity-check they fed the right file.
type GraphSummary struct {
	NodeCount int  `json:"nodeCount"`
	EdgeCount int  `json:"edgeCount"`
	Directed  bool `json:"directed"`
}

// OptimizeBatch runs detectors against g, ranks the findings, picks
// the top-N as a batch, and computes before/target metric snapshots.
//
// Selection algorithm (deterministic):
//  1. Run each detector; collect findings.
//  2. Filter by MinSeverity.
//  3. Sort using SortFindings (severity desc, score desc, detector asc, primaryID asc, members asc).
//  4. Take the first MaxBatchSize as the batch.
//  5. SelectionReason summarises the rule applied.
//
// The result is byte-stable across runs given the same input GraphML.
func OptimizeBatch(g *Graph, opts OptimizeBatchOptions) OptimizeBatchResult {
	max := opts.MaxBatchSize
	if max <= 0 {
		max = 10
	}
	allFindings, errs := RunNamed(g, opts.Detectors)

	floor := SeverityRank(opts.MinSeverity)
	filtered := allFindings
	if floor > 0 {
		filtered = make([]Finding, 0, len(allFindings))
		for _, f := range allFindings {
			if SeverityRank(f.Severity) >= floor {
				filtered = append(filtered, f)
			}
		}
	}

	// Already sorted by RunNamed, but re-sort the filtered slice to be
	// safe (filtering preserves order; explicit sort is cheap and
	// ensures consumers can't accidentally pass an unsorted slice).
	SortFindings(filtered)

	beforeMap := summarize(allFindings)
	chosen := filtered
	if len(chosen) > max {
		chosen = chosen[:max]
	}
	chosenSet := make(map[string]struct{}, len(chosen))
	for _, f := range chosen {
		chosenSet[findingKey(f)] = struct{}{}
	}
	remaining := make([]Finding, 0, len(allFindings)-len(chosen))
	for _, f := range allFindings {
		if _, in := chosenSet[findingKey(f)]; in {
			continue
		}
		remaining = append(remaining, f)
	}
	targetMap := summarize(remaining)

	res := OptimizeBatchResult{
		Version: CurrentJSONVersion,
		GraphSummary: GraphSummary{
			NodeCount: len(g.Nodes),
			EdgeCount: len(g.Edges),
			Directed:  g.Directed,
		},
		BeforeMetrics:   metricSummaryList(beforeMap),
		TargetMetrics:   targetMetricList(targetMap),
		Batch:           toEvidenceItems(chosen),
		MetricEvidence:  toEvidenceItems(chosen),
		SelectionReason: selectionReason(opts, len(allFindings), len(chosen)),
	}
	for _, e := range errs {
		res.Errors = append(res.Errors, e.Error())
	}
	return res
}

// findingKey returns a stable composite key identifying a finding
// for set-membership use.
func findingKey(f Finding) string {
	return f.Detector + "\x1f" + f.PrimaryID + "\x1f" + joinIDs(f.Members) + "\x1f" + f.Reason.Code
}

func summarize(fs []Finding) map[string]*MetricSummary {
	out := map[string]*MetricSummary{}
	for _, f := range fs {
		s, ok := out[f.Detector]
		if !ok {
			s = &MetricSummary{Detector: f.Detector}
			out[f.Detector] = s
		}
		s.FindingCount++
		s.TotalScore += f.Score
		if f.Score > s.MaxScore {
			s.MaxScore = f.Score
		}
		if SeverityRank(f.Severity) > SeverityRank(s.HighestSeverity) {
			s.HighestSeverity = f.Severity
		}
	}
	return out
}

func metricSummaryList(m map[string]*MetricSummary) []MetricSummary {
	out := make([]MetricSummary, 0, len(m))
	for _, v := range m {
		out = append(out, *v)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Detector < out[j].Detector })
	return out
}

func targetMetricList(m map[string]*MetricSummary) []TargetMetrics {
	out := make([]TargetMetrics, 0, len(m))
	for _, v := range m {
		out = append(out, TargetMetrics{
			Detector:     v.Detector,
			FindingCount: v.FindingCount,
			MaxScore:     v.MaxScore,
			TotalScore:   v.TotalScore,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Detector < out[j].Detector })
	return out
}

func toEvidenceItems(fs []Finding) []EvidenceItem {
	out := make([]EvidenceItem, 0, len(fs))
	for _, f := range fs {
		out = append(out, EvidenceItem{
			Detector: f.Detector,
			Severity: f.Severity,
			Score:    f.Score,
			Primary:  f.PrimaryID,
			Members:  f.Members,
			Reason:   f.Reason,
			Evidence: f.Evidence,
		})
	}
	return out
}

func selectionReason(opts OptimizeBatchOptions, totalFindings, chosen int) string {
	max := opts.MaxBatchSize
	if max <= 0 {
		max = 10
	}
	floor := opts.MinSeverity
	if floor == "" {
		floor = SeverityInfo
	}
	return fmt.Sprintf(
		"selected top %d/%d findings sorted by (severity desc, score desc, detector asc, primary asc); min severity=%s; max batch=%d",
		chosen, totalFindings, floor, max,
	)
}

// WriteJSON emits the result as pretty-printed JSON.
func (r OptimizeBatchResult) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(r)
}
