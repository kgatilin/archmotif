// Package verify implements Stage 8 of archmotif: given a target
// subgraph skeleton (from Stage 6) and a typed graph built from
// LLM-produced code (Stage 7), decide whether the code realises the
// target shape under the role mapping declared in the skeleton.
//
// The package is pinned to role-hinted backtracking with strict
// subgraph isomorphism by default (see ADR-018). Future variants —
// VF2, structural-similarity — plug in behind the Verifier interface
// without breaking callers.
//
// The types in this file are the locked public surface for #18. The
// candidate-set construction lives in candidate.go, the DFS solver in
// backtrack.go, and the diff renderer in diff.go.
package verify

import (
	"context"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// NodeID is the stable graph node identifier (per ADR-005). It is an
// alias of string today; the wrapper is kept so callers don't encode
// assumptions about the ID's representation. If the graph package ever
// promotes a typed NodeID, this alias becomes the migration shim.
type NodeID = string

// Verifier checks whether code realises a target skeleton. It is the
// seam between the strict v1 implementation in this package and any
// future variants (VF2-backed, structural-similarity, contract-aware).
type Verifier interface {
	Verify(ctx context.Context, target Skeleton, code *mgraph.Graph) Result
}

// Skeleton is the verifier's view of a Stage 6 target subgraph. It
// mirrors the YAML companion file frozen in #16 (ADR-016) and the
// proposer's TargetSubgraph (#19) — but the verifier owns this type
// locally to avoid coupling to internal/propose during parallel work
// (see ADR-018 §5).
type Skeleton struct {
	ProposalID string
	Roles      []Role
	Edges      []EdgeConstraint
}

// Role is one named slot in the target subgraph. Kind constrains the
// node kind candidates considered. Method shape, receiver, and
// realisation are optional refinements that further prune the
// candidate set during DFS.
type Role struct {
	ID           string
	Kind         mgraph.NodeKind
	MethodShape  *MethodShape // optional; for method/function roles
	ReceiverRole string       // optional; for method roles
	Realises     *Realisation // optional; for method roles realising interface methods
}

// EdgeConstraint is a directed edge between two roles that the
// verifier requires in the candidate graph. From and To are role IDs
// (matching Role.ID); Kind is one of graph.EdgeKind.
type EdgeConstraint struct {
	From string
	To   string
	Kind mgraph.EdgeKind
}

// MethodShape captures the rough signature shape of a method or
// function role. ParamKinds is the ordered list of parameter node
// kinds (typically NodeType for typed params, but kept open). ReturnKind
// is optional — the empty string means "any return / unconstrained".
//
// Per ADR-018, shape matching compares kinds, not full type identity.
// The skeleton names types via role placeholders, and kind-level
// matching preserves that abstraction.
type MethodShape struct {
	ParamKinds []mgraph.NodeKind
	ReturnKind mgraph.NodeKind
}

// Realisation declares that a method role realises a method on an
// interface role. Role is the interface role's ID; Method is the
// method-role's ID (typically the same name in v1 skeletons).
type Realisation struct {
	Role   string
	Method string
}

// Result is the verifier's verdict.
//
// On Match: Mapping holds role-ID → graph node ID, and Bindings holds
// role-ID → human-readable instance name (e.g. "<Iface>"="UserStore").
// On !Match: Diff holds the diagnostic (see diff.go for rendering).
type Result struct {
	Match    bool
	Mapping  map[string]NodeID
	Bindings map[string]string
	Diff     *Diff
}

// Diff is the structured mismatch envelope. The text and JSON
// renderers in diff.go consume this struct; tests assert on it
// directly.
type Diff struct {
	// Reason is a short human-readable summary used as the headline.
	Reason string
	// MissingRoles lists roles whose candidate set was empty (the
	// strongest, earliest failure mode).
	MissingRoles []MissingRole
	// FailingEdges lists edge constraints that could not be satisfied
	// in the partial assignment that the DFS reached deepest.
	FailingEdges []FailingEdge
	// PartialMapping is the deepest partial role→node assignment
	// the DFS reached before backtracking out completely. Useful when
	// reporting "Role X bound to N, but expected edge Y" diagnostics.
	PartialMapping map[string]NodeID
}

// MissingRole is a role with zero candidates after kind / shape /
// receiver filtering.
type MissingRole struct {
	Role    string
	Kind    mgraph.NodeKind
	Reason  string
	Details map[string]any
}

// FailingEdge is an edge constraint from the skeleton that could not
// be satisfied in G_new. Both endpoints were assigned in the partial
// mapping (otherwise the constraint would not yet be checkable);
// either no edge of the required kind connects them, or the only
// edges connecting them have a different kind.
type FailingEdge struct {
	Edge       EdgeConstraint
	From       NodeID
	To         NodeID
	FoundKinds []mgraph.EdgeKind // edge kinds that DO connect From→To, if any
	Reason     string
}
