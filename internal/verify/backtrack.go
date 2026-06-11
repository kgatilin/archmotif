package verify

import (
	"context"
	"fmt"
	"sort"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// BacktrackVerifier is the v1 implementation of Verifier. It performs
// role-hinted DFS over candidate sets with strict subgraph isomorphism
// (per ADR-018). The zero value is ready to use.
type BacktrackVerifier struct{}

// NewBacktrackVerifier returns the default Verifier implementation.
func NewBacktrackVerifier() Verifier { return BacktrackVerifier{} }

// Verify runs the role-hinted DFS. Behaviour:
//
//   - Build candidate sets from the skeleton + graph. If any role has
//     zero candidates, return a Mismatch with MissingRoles populated.
//   - Order roles by ascending |candidates| (smallest-first), with
//     ties broken by Role.ID for determinism.
//   - DFS over assignments; whenever both endpoints of a skeleton edge
//     are bound, require the matching edge in g (strict).
//   - On success, return Match with the full mapping and a name
//     binding map.
//   - On exhaustion, return Mismatch with the deepest partial
//     assignment and the first failing edge constraint.
func (BacktrackVerifier) Verify(ctx context.Context, skel Skeleton, g *mgraph.Graph) Result {
	if g == nil {
		return Result{
			Match: false,
			Diff: &Diff{
				Reason: "verifier received nil graph",
			},
		}
	}

	cands, missing := candidateSets(skel, g)
	if len(missing) > 0 {
		return Result{
			Match: false,
			Diff: &Diff{
				Reason:       fmt.Sprintf("%d role(s) have no candidate in code", len(missing)),
				MissingRoles: missing,
			},
		}
	}

	// Order roles smallest-first for branching efficiency, with a
	// deterministic tie-break on Role.ID.
	order := make([]Role, len(skel.Roles))
	copy(order, skel.Roles)
	sort.SliceStable(order, func(i, j int) bool {
		ci := len(cands[order[i].ID])
		cj := len(cands[order[j].ID])
		if ci != cj {
			return ci < cj
		}
		return order[i].ID < order[j].ID
	})

	st := &searchState{
		skel:      skel,
		graph:     g,
		order:     order,
		cands:     cands,
		mapping:   make(map[string]NodeID, len(skel.Roles)),
		used:      make(map[NodeID]string, len(skel.Roles)),
		bestDepth: -1,
		bestMap:   make(map[string]NodeID),
	}

	if ok := st.dfs(ctx, 0); ok {
		return Result{
			Match:    true,
			Mapping:  cloneStringMap(st.mapping),
			Bindings: bindingsFromMapping(st.mapping, g),
		}
	}

	// DFS exhausted without success. Build a Mismatch from the deepest
	// partial assignment and the first failing edge it encountered.
	return Result{
		Match: false,
		Diff: &Diff{
			Reason:         "no role assignment satisfies all edge constraints",
			FailingEdges:   st.firstFailing,
			PartialMapping: cloneStringMap(st.bestMap),
		},
	}
}

// searchState carries the per-Verify mutable scratch.
type searchState struct {
	skel  Skeleton
	graph *mgraph.Graph
	order []Role
	cands map[string][]mgraph.Node

	// mapping is the live partial assignment; used is its inverse so
	// we can reject a candidate already taken by another role
	// (injectivity — no two roles bind to the same node).
	mapping map[string]NodeID
	used    map[NodeID]string

	// best* track the deepest partial assignment we reached; used for
	// diagnostics when DFS fails completely.
	bestDepth    int
	bestMap      map[string]NodeID
	firstFailing []FailingEdge
}

// dfs assigns the role at position i in st.order to one of its
// candidates, then recurses. Returns true once a complete satisfying
// assignment is found.
func (st *searchState) dfs(ctx context.Context, i int) bool {
	if err := ctx.Err(); err != nil {
		return false
	}
	if i == len(st.order) {
		// Final guard: every edge constraint must hold (the partial
		// check inside the loop short-circuits once both endpoints
		// are bound, so this is mostly defensive).
		if fail, ok := st.checkAllEdges(); !ok {
			st.recordFailure(fail)
			return false
		}
		return true
	}

	role := st.order[i]
	for _, cand := range st.cands[role.ID] {
		if _, taken := st.used[cand.ID]; taken {
			continue
		}
		// Receiver-role check: if this role has a ReceiverRole and
		// that role is already bound, require an EdgeContains edge
		// from the receiver to this candidate.
		if !st.receiverOK(role, cand) {
			continue
		}
		st.mapping[role.ID] = cand.ID
		st.used[cand.ID] = role.ID

		if depth := len(st.mapping); depth > st.bestDepth {
			st.bestDepth = depth
			st.bestMap = cloneStringMap(st.mapping)
		}

		// Edge check on the partial assignment: any skeleton edge
		// whose endpoints are both bound must have a matching edge
		// in g_new. If not, prune.
		if fail, ok := st.checkBoundEdges(); ok {
			if st.dfs(ctx, i+1) {
				return true
			}
		} else {
			st.recordFailure(fail)
		}

		delete(st.mapping, role.ID)
		delete(st.used, cand.ID)
	}
	return false
}

// receiverOK enforces the optional ReceiverRole constraint: if role's
// receiver is already bound in the partial mapping, cand must have an
// EdgeContains edge from the bound receiver. If the receiver isn't
// bound yet, defer the check.
func (st *searchState) receiverOK(role Role, cand mgraph.Node) bool {
	if role.ReceiverRole == "" {
		return true
	}
	recvID, bound := st.mapping[role.ReceiverRole]
	if !bound {
		return true
	}
	for _, n := range st.graph.Neighbors(recvID, mgraph.DirectionOut, mgraph.EdgeContains) {
		if n.ID == cand.ID {
			return true
		}
	}
	return false
}

// checkBoundEdges walks every skeleton edge whose endpoints are both
// bound and verifies a matching edge exists in g. Returns the first
// failing edge (if any) and a bool reporting overall success.
func (st *searchState) checkBoundEdges() ([]FailingEdge, bool) {
	var fails []FailingEdge
	for _, ec := range st.skel.Edges {
		fromID, fok := st.mapping[ec.From]
		toID, tok := st.mapping[ec.To]
		if !fok || !tok {
			continue
		}
		if !st.edgeExists(fromID, toID, ec.Kind) {
			fails = append(fails, st.buildFailingEdge(ec, fromID, toID))
			return fails, false
		}
	}
	return nil, true
}

// checkAllEdges runs checkBoundEdges with a complete mapping; renamed
// for clarity at the recursion exit.
func (st *searchState) checkAllEdges() ([]FailingEdge, bool) {
	return st.checkBoundEdges()
}

// edgeExists reports whether g contains a from→to edge of the given
// kind. Self-edges are honoured because graph.IncidentEdges enumerates
// the underlying typed edge slice.
func (st *searchState) edgeExists(from, to NodeID, kind mgraph.EdgeKind) bool {
	for _, e := range st.graph.IncidentEdges(from, mgraph.DirectionOut, kind) {
		if e.To == to {
			return true
		}
	}
	return false
}

// buildFailingEdge synthesises the diagnostic for a missing edge. It
// records every edge kind that *does* connect from→to so the diff can
// say "expected calls, found contains" rather than just "no edge".
func (st *searchState) buildFailingEdge(ec EdgeConstraint, fromID, toID NodeID) FailingEdge {
	var found []mgraph.EdgeKind
	for _, e := range st.graph.IncidentEdges(fromID, mgraph.DirectionOut, "") {
		if e.To == toID {
			found = append(found, e.Kind)
		}
	}
	reason := fmt.Sprintf("expected edge (%s, %s, %s); ", ec.From, ec.Kind, ec.To)
	if len(found) == 0 {
		reason += fmt.Sprintf("no edge from %s to %s", fromID, toID)
	} else {
		reason += fmt.Sprintf("found kinds %v instead", found)
	}
	return FailingEdge{
		Edge:       ec,
		From:       fromID,
		To:         toID,
		FoundKinds: found,
		Reason:     reason,
	}
}

// recordFailure stores the first failing edge we hit at the deepest
// level reached so far. Subsequent failures at shallower depths are
// ignored to keep the diagnostic stable on the most informative
// partial assignment.
func (st *searchState) recordFailure(fails []FailingEdge) {
	if len(fails) == 0 {
		return
	}
	if len(st.firstFailing) == 0 || len(st.mapping) >= st.bestDepth {
		st.firstFailing = fails
	}
}

// bindingsFromMapping renders role-ID → human-readable instance name
// (the bound node's Name attribute, or its ID as a fallback).
func bindingsFromMapping(m map[string]NodeID, g *mgraph.Graph) map[string]string {
	out := make(map[string]string, len(m))
	for role, id := range m {
		if n, ok := g.Node(id); ok && n.Name != "" {
			out[role] = n.Name
			continue
		}
		out[role] = id
	}
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
