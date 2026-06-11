package patterns

import (
	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// ForbiddenRoleEdges flags edges that violate declared role
// boundaries — e.g. a domain type calling into an inbound-adapter
// detail. The check is structurally simple ("is there an edge from
// role A to role B that the user marked forbidden?") but requires
// role metadata that is not yet present in the graph.
//
// Issue #28 introduces the role catalogue via .archmotif.yaml. Once
// roles are available this pattern will:
//   - collect every edge whose endpoints carry roles,
//   - compare (fromRole, toRole) against a forbidden-pairs list,
//   - emit a Mismatch with one violation per offending edge.
//
// Until then the pattern returns StatusNotApplicable. Registering it
// today lets the CLI surface and the Report schema stay stable as
// later stages add prerequisites (per ADR-026).
type ForbiddenRoleEdges struct{}

const patternVersionForbiddenRoleEdges = "0.1.0"

// ID returns the stable pattern identifier.
func (ForbiddenRoleEdges) ID() string { return "forbidden_role_edges" }

// Version returns the pattern's semantic version.
func (ForbiddenRoleEdges) Version() string { return patternVersionForbiddenRoleEdges }

// Description returns the human-readable description.
func (ForbiddenRoleEdges) Description() string {
	return "edges that cross declared role boundaries (e.g. domain → adapter); requires role metadata (#28)"
}

// Run returns NotApplicable until role metadata is wired in (#28).
func (p ForbiddenRoleEdges) Run(_ *mgraph.Graph) Report {
	return Report{
		ID:        p.ID(),
		Version:   p.Version(),
		Status:    StatusNotApplicable,
		Threshold: 0,
		Reason:    "forbidden_role_edges requires per-node role metadata (issue #28 — .archmotif.yaml roles); skipping until roles are present",
		Metrics: map[string]any{
			"missing_prerequisite": "role_metadata",
		},
	}
}

func init() { Register(ForbiddenRoleEdges{}) }
