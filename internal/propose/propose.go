// Package propose implements Stage 5 of archmotif: per-anomaly
// transformation rules that turn structural metric anomalies (Stage 3
// + Stage 4) into concrete refactoring proposals.
//
// This package is the single source of truth for Proposal,
// TargetSubgraph, Role, EdgeConstraint, and AnomalyRef types — issue
// #16 (skeleton renderer) and issue #18 (verifier) import these
// definitions. ADR-019 records the rationale.
//
// The package owns three small things:
//
//   - The Proposal data shape (this file).
//   - A pluggable rule registry (registry.go), mirroring the metrics
//     ADR-011 init() pattern: adding a rule is one new file with one
//     init() call.
//   - Concrete rules (extract_interface.go is the v1 entry; ADR-019
//     pins it as the only rule for v1).
//
// The Proposer consumes Stage 4's anomaly stream
// ([]anomalies.Anomaly) directly, per ADR-022. Tests that hand-build
// metrics.Records can use Proposer.ProposeFromRecords as a
// zero-score back-compat path.
package propose

import (
	"github.com/kgatilin/archmotif/internal/metrics"
	"github.com/kgatilin/archmotif/internal/proposal"
)

// Proposal is the unit of output from a transformation rule.
//
// A Proposal answers four questions for downstream stages:
//
//  1. Why does this proposal exist? — Trigger points back to the
//     specific metric Record that produced it.
//  2. What shape should the new code have? — TargetSubgraph names
//     roles and the edges between them, independent of any chosen
//     names.
//  3. Where does the change land? — AffectedFiles enumerates the
//     pkg/file paths the rewrite touches.
//  4. What does this look like concretely? — Samples maps each role
//     to the existing instance names in the current graph, so Stage 6
//     (skeleton renderer) and Stage 7 (LLM) can show the real-world
//     anchors alongside the placeholders.
// The target-architecture data model lives in internal/proposal (a leaf
// package) so targetcontract can consume it without depending on this
// pipeline package. These aliases keep propose's public surface and all
// in-package references unchanged.
type (
	Proposal       = proposal.Proposal
	AnomalyRef     = proposal.AnomalyRef
	TargetSubgraph = proposal.TargetSubgraph
	Role           = proposal.Role
	EdgeConstraint = proposal.EdgeConstraint
)

// AnomalyRefFrom builds an AnomalyRef from a metrics.Record. Helper
// for rule implementations.
func AnomalyRefFrom(rec metrics.Record) *AnomalyRef {
	return &AnomalyRef{
		Metric: rec.Metric,
		Scope:  rec.Scope,
		Target: rec.Target,
		Value:  rec.Value,
	}
}
