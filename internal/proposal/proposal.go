// Package proposal holds the target-architecture data model — the shared
// "what the post-refactor code should look like" types. It is a leaf package
// (depends only on graph + metrics value types) so both the propose pipeline
// (which produces proposals) and targetcontract (which consumes them) can
// depend on it without targetcontract having to depend on the pipeline.
//
// Extracted from internal/propose to invert a targetcontract -> propose
// dependency (a foundational contract type depending upward on the pipeline);
// see docs/prd/archmotif-graph-metrics-library.md PoC.
package proposal

import (
	mgraph "github.com/kgatilin/archmotif/internal/graph"
	"github.com/kgatilin/archmotif/internal/metrics"
)

// Proposal is one small, local refactoring the proposer suggests.
type Proposal struct {
	ID             string              `json:"id"`
	Description    string              `json:"description"`
	Trigger        *AnomalyRef         `json:"trigger,omitempty"`
	AffectedFiles  []string            `json:"affectedFiles,omitempty"`
	TargetSubgraph TargetSubgraph      `json:"targetSubgraph"`
	Samples        []map[string]string `json:"samples,omitempty"`
}

// AnomalyRef points back at the metric Record that triggered a proposal.
type AnomalyRef struct {
	Metric string        `json:"metric"`
	Scope  metrics.Scope `json:"scope"`
	Target string        `json:"target"`
	Value  float64       `json:"value"`
}

// TargetSubgraph is the structural blueprint for the post-refactor code shape.
type TargetSubgraph struct {
	Roles []Role           `json:"roles"`
	Edges []EdgeConstraint `json:"edges"`
}

// Role is one participant in a TargetSubgraph.
type Role struct {
	Name        string          `json:"name"`
	Kind        mgraph.NodeKind `json:"kind"`
	Cardinality int             `json:"cardinality"`
	Attrs       map[string]any  `json:"attrs,omitempty"`
}

// EdgeConstraint is one typed relationship between two roles.
type EdgeConstraint struct {
	From string          `json:"from"`
	To   string          `json:"to"`
	Kind mgraph.EdgeKind `json:"kind"`
}
