// Package anomalies implements Stage 4 of archmotif: per-metric
// anomaly detection over the structured records produced by Stage 3.
//
// The package consumes []metrics.Record and emits []Anomaly: one
// detector per metric kind (registered via init() per ADR-018, which
// mirrors ADR-011 for metrics). Scoring rules and threshold defaults
// are documented in ADR-020. Region shape is documented in ADR-021.
//
// The framing is deliberate: anomaly detection is a flag for human
// inspection, not a fix-to-threshold (concepts.md §4). Detectors
// produce a ranked list with structured `Reason` payloads so Stage 5
// proposals can switch on detector kind.
package anomalies

import (
	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// Anomaly is one flagged region in the graph, attributed to one
// metric, with a score and human-readable rationale.
//
// Two anomalies are considered the "same" by Stage 5 deduplication
// when they share (Metric, Detector, Region.PrimaryID, Reason.Code).
// We do not enforce uniqueness here — emitting one anomaly per motif
// instance is intentional (see ADR-021).
type Anomaly struct {
	// Metric is the metric name from the consumed Record.Metric. Stable
	// across runs; matches metrics.Metric.Name().
	Metric string `json:"metric"`
	// Detector is the detector identifier (typically same as Metric;
	// kept separate so a future metric can have multiple detectors).
	Detector string `json:"detector"`
	// Score is a non-negative scalar where higher = more anomalous.
	// Score scale is per-detector and not comparable across detectors
	// (ADR-020).
	Score float64 `json:"score"`
	// Region identifies the graph subset this anomaly attaches to.
	Region Region `json:"region"`
	// Reason carries a structured "why anomalous" payload. Code is a
	// short stable identifier (e.g. "modz_above_threshold",
	// "scc_present", "low_spectral_gap"); Message is a human readable
	// sentence; Details mirrors metrics.Record.Details so consumers
	// don't have to re-thread the original record.
	Reason Reason `json:"reason"`
	// SourceRecord copies the metric record this anomaly was derived
	// from. Useful for the CLI to print value and metric-specific
	// details without joining back to the metrics output.
	SourceRecord SourceRecord `json:"source"`
}

// Region identifies the graph subset that an anomaly attaches to.
// Per ADR-021 the full member list is preserved; the CLI truncates
// for display but JSON keeps everything.
type Region struct {
	// Kind echoes the metric record's Scope: "graph" | "region" | "node" | "edge".
	Kind string `json:"kind"`
	// Members is the full list of stable node IDs participating in
	// the region. Sorted, deduplicated. Empty for ScopeGraph anomalies.
	Members []string `json:"members,omitempty"`
	// PrimaryID is the most-relevant single node id (centre of a
	// motif instance, the node itself for ScopeNode, the package node
	// for a community). Empty for ScopeGraph.
	PrimaryID string `json:"primaryID,omitempty"`
	// Files lists the source files the region's members touch. Sorted
	// by Path. Lines are sorted-deduped; nil when no Position is
	// available for any contributing node.
	Files []FileRef `json:"files,omitempty"`
}

// FileRef is a Path plus the lines within it that contribute to the
// region. Lines are 1-indexed, ascending, deduplicated.
type FileRef struct {
	Path  string `json:"path"`
	Lines []int  `json:"lines,omitempty"`
}

// Reason is the structured rationale for flagging.
type Reason struct {
	// Code is a stable short identifier. See per-detector files for the
	// catalogue.
	Code string `json:"code"`
	// Message is a human-readable single sentence; suitable for one-line
	// CLI output.
	Message string `json:"message"`
	// Details carries optional per-detector context (e.g. modified
	// z-score, comparison values, threshold used). JSON-serialisable.
	Details map[string]any `json:"details,omitempty"`
}

// SourceRecord is a denormalised copy of the metrics.Record this
// anomaly was derived from. Kept here so consumers don't need the
// metric output file to interpret anomalies.
type SourceRecord struct {
	Scope   string         `json:"scope"`
	Target  string         `json:"target,omitempty"`
	Value   float64        `json:"value"`
	Details map[string]any `json:"details,omitempty"`
}

// resolveRegion fills Region.Files (and validates Members) by looking
// up positions in g. Members already present in r are kept; this
// function does not deduplicate or sort them — that is the caller's
// responsibility before calling resolveRegion.
//
// Members not present in g are silently dropped from the file
// resolution but kept in r.Members so Stage 5 still sees them.
func resolveRegion(g *mgraph.Graph, r Region) Region {
	if g == nil || len(r.Members) == 0 {
		return r
	}
	type fileAcc struct {
		lines map[int]struct{}
	}
	files := map[string]*fileAcc{}
	for _, id := range r.Members {
		n, ok := g.Node(id)
		if !ok {
			continue
		}
		if n.Pos.File == "" {
			continue
		}
		acc := files[n.Pos.File]
		if acc == nil {
			acc = &fileAcc{lines: map[int]struct{}{}}
			files[n.Pos.File] = acc
		}
		if n.Pos.Line > 0 {
			acc.lines[n.Pos.Line] = struct{}{}
		}
	}
	if len(files) == 0 {
		return r
	}
	out := make([]FileRef, 0, len(files))
	for path, acc := range files {
		ref := FileRef{Path: path}
		if len(acc.lines) > 0 {
			ref.Lines = make([]int, 0, len(acc.lines))
			for ln := range acc.lines {
				ref.Lines = append(ref.Lines, ln)
			}
			intsAscending(ref.Lines)
		}
		out = append(out, ref)
	}
	stringsAscendingByPath(out)
	r.Files = out
	return r
}

// intsAscending sorts xs in place in ascending order. Tiny helper
// kept inline so the file has no extra imports for sort.
func intsAscending(xs []int) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j-1] > xs[j]; j-- {
			xs[j-1], xs[j] = xs[j], xs[j-1]
		}
	}
}

// stringsAscendingByPath sorts FileRef slices by Path in ascending
// order. Insertion sort is fine: file lists are short.
func stringsAscendingByPath(xs []FileRef) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j-1].Path > xs[j].Path; j-- {
			xs[j-1], xs[j] = xs[j], xs[j-1]
		}
	}
}
