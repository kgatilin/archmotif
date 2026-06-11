// Package patterns implements deterministic architecture-pattern reports
// over the typed graph. Each Pattern looks at the graph (and, in future
// stages, role metadata such as that proposed in #28) and emits a Report
// describing whether a known motif is satisfied, near-satisfied, or
// violated by the code under analysis.
//
// Pattern reports are intentionally deterministic and free of LLM
// interpretation: callers want machine-readable evidence so they can
// diff runs across commits and gate CI on architectural regressions.
//
// See ADR-026 for the schema and status-enum semantics.
package patterns

import (
	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// Status is the verdict of a single pattern run against a graph.
//
// Stable string values — they appear in JSON output and downstream
// tooling will rank/filter on them.
type Status string

// Status constants. The four-state enum is deliberate: callers must be
// able to distinguish "this pattern doesn't apply to your code" from
// "this pattern fails on your code", so a missing-prerequisite pattern
// (e.g. one that needs role metadata which isn't yet present in the
// graph) returns NotApplicable rather than silently disappearing.
const (
	// StatusMatch means the pattern's positive criteria are fully met.
	StatusMatch Status = "match"
	// StatusNearMatch means the pattern is mostly satisfied but one or
	// more soft criteria fall under threshold. Evidence and violations
	// describe the gap.
	StatusNearMatch Status = "near_match"
	// StatusMismatch means the pattern's positive criteria are violated
	// outright. Evidence and violations explain the failure.
	StatusMismatch Status = "mismatch"
	// StatusNotApplicable means the pattern could not run because a
	// prerequisite (role metadata, contract annotations, …) is absent
	// from the graph. The CLI surface stays stable as new prerequisites
	// land in later stages — see ADR-026.
	StatusNotApplicable Status = "not_applicable"
)

// EvidenceNode is a node referenced by a pattern report. The ID is the
// stable graph ID; Reason is a short, human-readable explanation of why
// the node was selected.
type EvidenceNode struct {
	ID     string `json:"id"`
	Kind   string `json:"kind,omitempty"`
	Name   string `json:"name,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// EvidenceEdge is an edge referenced by a pattern report.
type EvidenceEdge struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Kind   string `json:"kind"`
	Reason string `json:"reason,omitempty"`
}

// Violation captures a specific rule breach found by the pattern. Each
// violation should reference one or more pieces of evidence the caller
// can inspect; the Recommendation field on Report addresses the report
// as a whole.
type Violation struct {
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Nodes   []string `json:"nodes,omitempty"`
	Edges   []string `json:"edges,omitempty"`
}

// Report is the result of running a single pattern.
//
// Score and Threshold are pattern-specific real numbers; a status of
// Match/NearMatch/Mismatch is determined by the pattern's own
// comparison of Score against Threshold. Pattern docs explain the
// units.
//
// Metrics carries the raw inputs the pattern fed into its decision so
// downstream tools can re-derive the verdict from saved JSON.
type Report struct {
	ID              string         `json:"id"`
	Version         string         `json:"version"`
	Status          Status         `json:"status"`
	Score           float64        `json:"score"`
	Threshold       float64        `json:"threshold"`
	EvidenceNodes   []EvidenceNode `json:"evidence_nodes,omitempty"`
	EvidenceEdges   []EvidenceEdge `json:"evidence_edges,omitempty"`
	Violations      []Violation    `json:"violations,omitempty"`
	Recommendations []string       `json:"recommendations,omitempty"`
	Metrics         map[string]any `json:"metrics,omitempty"`
	// Reason is a short, top-level explanation. For NotApplicable it
	// names the missing prerequisite.
	Reason string `json:"reason,omitempty"`
}

// Pattern is the contract implemented by every deterministic
// architecture pattern.
//
// Run is pure: it must not mutate g and must be deterministic for a
// given graph. Patterns that need data not yet present in the graph
// (role metadata, contract annotations, …) return a Report with
// StatusNotApplicable and a Reason naming the missing prerequisite —
// they must NOT return an error.
type Pattern interface {
	ID() string
	Version() string
	Description() string
	Run(g *mgraph.Graph) Report
}
