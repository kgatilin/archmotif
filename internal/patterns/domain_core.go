package patterns

import (
	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// DomainCore checks whether a project exposes a cohesive domain core
// — types that depend almost exclusively on each other (and on
// language primitives), with low fan-out to adapter / infrastructure
// packages.
//
// The full check requires per-type role metadata (which types are
// "domain", which are "adapter") that is not yet present in the
// graph. Issue #28 introduces role annotations via .archmotif.yaml;
// once those land this pattern will partition types by role and
// compute fan-out from domain → non-domain.
//
// Until #28 lands the pattern returns StatusNotApplicable with a
// stable Reason. It is registered today so the CLI surface lights up
// the full V1 catalog and downstream tooling can rely on a fixed set
// of pattern IDs (per ADR-026: NotApplicable is a first-class status).
type DomainCore struct{}

const patternVersionDomainCore = "0.1.0"

// ID returns the stable pattern identifier.
func (DomainCore) ID() string { return "domain_core" }

// Version returns the pattern's semantic version.
func (DomainCore) Version() string { return patternVersionDomainCore }

// Description returns the human-readable description.
func (DomainCore) Description() string {
	return "cohesive domain types with low fan-out to adapter/infrastructure packages (requires role metadata, #28)"
}

// Run returns NotApplicable until role metadata is wired in (#28).
func (p DomainCore) Run(_ *mgraph.Graph) Report {
	return Report{
		ID:        p.ID(),
		Version:   p.Version(),
		Status:    StatusNotApplicable,
		Threshold: 0,
		Reason:    "domain_core requires per-type role metadata (issue #28 — .archmotif.yaml roles); skipping until roles are present",
		Metrics: map[string]any{
			"missing_prerequisite": "role_metadata",
		},
	}
}

func init() { Register(DomainCore{}) }
