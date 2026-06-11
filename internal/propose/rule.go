package propose

import (
	mgraph "github.com/kgatilin/archmotif/internal/graph"
	"github.com/kgatilin/archmotif/internal/metrics"
)

// Rule is the contract every transformation rule must satisfy.
//
// A Rule is two pure functions plus a stable Name():
//
//   - Trigger(rec, g) bool — does this rule apply to the given anomaly
//     Record? Trigger is the cheap pre-filter: it inspects the
//     Record's metric, scope, value, and details, and (when needed)
//     looks up participants in g to apply contract / kind exclusions.
//     Trigger MUST be cheap; the proposer calls it once per (rule,
//     record) pair.
//   - Apply(g, rec) (*Proposal, error) — the heavy step: build the
//     full Proposal. Apply is only called when Trigger returned true,
//     so it can assume the Record matches.
//
// Rules are stateless. They register themselves into the package-level
// registry from init() (see registry.go).
type Rule interface {
	// Name returns the rule's stable identifier (e.g.
	// "extract_interface"). Used by the CLI's `--list` flag and by
	// proposal IDs.
	Name() string
	// Description returns a one-line human-readable summary of what
	// the rule does and what anomaly it consumes.
	Description() string
	// Trigger reports whether the rule applies to rec. Implementations
	// should return false fast and only consult g when the contract /
	// kind exclusion check requires it.
	Trigger(rec metrics.Record, g *mgraph.Graph) bool
	// Apply builds a Proposal from rec. Returning (nil, nil) means
	// the rule recognised the anomaly but declines to propose (e.g.
	// degenerate participants); returning an error means the input
	// was malformed in a way the proposer should surface.
	Apply(g *mgraph.Graph, rec metrics.Record) (*Proposal, error)
}
