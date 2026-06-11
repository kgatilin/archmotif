package verify

import (
	"fmt"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// candidateSets builds, for each role in the skeleton, the list of
// graph nodes that could fill that role under the role's declared
// constraints (kind, optional method shape, optional receiver role).
//
// The receiver-role filter is applied lazily (during DFS, not here):
// the receiver node's binding is unknown until we descend the search
// tree. Here we only check that a candidate's *receiver kind* matches
// the receiver role's declared kind — the strongest filter we can
// apply without a partial assignment in hand.
//
// Returns the per-role candidate lists keyed by Role.ID, and a list of
// MissingRole entries for any role whose candidate set is empty. When
// any role is missing, the verifier short-circuits to a Mismatch
// without entering the DFS.
func candidateSets(skel Skeleton, g *mgraph.Graph) (map[string][]mgraph.Node, []MissingRole) {
	out := make(map[string][]mgraph.Node, len(skel.Roles))
	var missing []MissingRole

	for _, role := range skel.Roles {
		cands := matchCandidates(g, role)
		out[role.ID] = cands
		if len(cands) == 0 {
			missing = append(missing, missingFor(role))
		}
	}
	return out, missing
}

// matchCandidates returns the nodes in g that pass the role's local
// filters: node kind match, method shape match (if declared), and
// receiver-kind compatibility (if a receiver role is declared and
// resolvable from the skeleton).
func matchCandidates(g *mgraph.Graph, role Role) []mgraph.Node {
	out := make([]mgraph.Node, 0)
	for _, n := range g.NodesByKind(role.Kind) {
		if !shapeMatches(g, n, role.MethodShape) {
			continue
		}
		out = append(out, n)
	}
	return out
}

// shapeMatches reports whether n has a signature shape compatible with
// the role's MethodShape. Matching is kind-level (per ADR-018): we
// compare ordered param kinds and the return kind, not full type
// identity.
//
// Shape extraction from the graph is intentionally permissive: any
// edge of kind EdgeReturns out of n is treated as a return-type edge,
// and any contained NodeField with attribute role="param" is treated
// as a parameter slot. v1 graphs may not yet emit explicit param
// nodes; in that case the param-list check is skipped (we just check
// arity ≥ declared) and a follow-up tightens the shape contract once
// Stage 1 promotes parameters to first-class nodes.
func shapeMatches(g *mgraph.Graph, n mgraph.Node, shape *MethodShape) bool {
	if shape == nil {
		return true
	}
	// Return-kind check: we look for an outgoing EdgeReturns to a node
	// of the declared kind. An empty ReturnKind means unconstrained.
	if shape.ReturnKind != "" {
		matched := false
		for _, ret := range g.Neighbors(n.ID, mgraph.DirectionOut, mgraph.EdgeReturns) {
			if ret.Kind == shape.ReturnKind {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	// Param-kind check: walk the contained Field nodes and treat them
	// as positional parameter slots. A v1 graph that doesn't emit
	// param fields will pass this check trivially when ParamKinds is
	// empty; otherwise the candidate is required to have at least the
	// declared arity, and each present param's kind must match.
	if len(shape.ParamKinds) > 0 {
		params := containedFields(g, n)
		if len(params) < len(shape.ParamKinds) {
			return false
		}
		for i, want := range shape.ParamKinds {
			if want == "" {
				continue
			}
			if params[i].Kind != want {
				return false
			}
		}
	}
	return true
}

// containedFields returns the field-kind nodes contained by n in
// declaration order. Used for crude positional parameter matching
// when the graph emits parameter nodes.
func containedFields(g *mgraph.Graph, n mgraph.Node) []mgraph.Node {
	var out []mgraph.Node
	for _, child := range g.Neighbors(n.ID, mgraph.DirectionOut, mgraph.EdgeContains) {
		if child.Kind == mgraph.NodeField {
			out = append(out, child)
		}
	}
	return out
}

// missingFor builds a MissingRole record explaining why no candidate
// matched. Today the reason is always kind-level; once method-shape
// matching surfaces dedicated diagnostics this routine grows.
func missingFor(role Role) MissingRole {
	reason := fmt.Sprintf("no candidate matching kind=%s", role.Kind)
	if role.MethodShape != nil {
		reason = fmt.Sprintf("no candidate matching kind=%s with method shape %s",
			role.Kind, formatShape(role.MethodShape))
	}
	return MissingRole{
		Role:   role.ID,
		Kind:   role.Kind,
		Reason: reason,
	}
}

// formatShape renders a MethodShape compactly for diagnostics.
// Example: ([type, type] -> type) or ([] -> any).
func formatShape(s *MethodShape) string {
	if s == nil {
		return "(any)"
	}
	out := "("
	for i, p := range s.ParamKinds {
		if i > 0 {
			out += ","
		}
		if p == "" {
			out += "any"
		} else {
			out += string(p)
		}
	}
	out += ") -> "
	if s.ReturnKind == "" {
		out += "any"
	} else {
		out += string(s.ReturnKind)
	}
	return out
}
