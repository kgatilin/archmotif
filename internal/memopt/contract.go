// Package memopt holds the materializer prompt protocol for memory-
// graph optimization batches. See ADR-032.
//
// Issue #39 introduces a second materializer track distinct from the
// Stage 7 code-refactor materializer in internal/llm: instead of
// emitting a unified diff against a Go module, this materializer
// rewrites a small batch of nodes inside a memory GraphML by gathering
// memory context, proposing a regrouping, and reporting back.
//
// Three things live in this package:
//
//   - Contract: the structural input the orchestrator builds for every
//     batch the next-batch selector emits. Pins which node IDs and
//     titles the materializer is allowed to touch and which patch
//     operations it may use.
//   - Prompt: the rendered instructions sent to the LLM. The template
//     embeds the contract, instructs bounded-batch context fetching,
//     and pins the exact JSON output shape.
//   - Patch + Validate: the materializer's reply and the structural
//     gate that rejects malformed outputs (shape changes, missing or
//     extra nodes, forbidden removals, missing rationale, missing
//     context-source report).
//
// The package has no LLM/HTTP code; that lives in #38 (loop CLI) which
// will call into here for prompt rendering and validation. Tests use
// hand-built fixtures so this package can be exercised end-to-end
// without an API key.
package memopt

import (
	"errors"
	"fmt"
)

// Operation is one of the structural edits a contract may permit.
// Operations are spelled in lowerCamelCase to match the JSON wire
// shape the materializer emits.
type Operation string

const (
	// OpRegroup moves a node under a different parent / community.
	// Allowed by orphan-batch and flat-star-hub contracts.
	OpRegroup Operation = "regroup"
	// OpMerge merges two existing nodes into one, preserving title
	// history on the survivor. Required by duplicate-titles batches.
	OpMerge Operation = "merge"
	// OpRetitle rewrites a node's display title. Required by
	// near-duplicate-titles batches; safe in any contract that lists it.
	OpRetitle Operation = "retitle"
	// OpAnnotate attaches a tag/label to a node. Lowest-risk operation;
	// most contracts allow it.
	OpAnnotate Operation = "annotate"
)

// Contract is the structural input the orchestrator hands to the
// materializer for one batch. It declares the closed set of node IDs
// (and parallel titles for human readability) the materializer is
// allowed to touch, and the closed set of operations it may apply.
//
// Field semantics:
//
//   - ID identifies the contract across the run (e.g. "orphan-2025-05").
//     It rides through into Patch.ContractID so logs correlate.
//   - Kind is the batch shape ("orphan_bucket", "flat_star_hub", …)
//     surfaced by the next-batch selector. The materializer reads this
//     to pick the right rewrite strategy and the prompt template
//     parametrises wording on it.
//   - Selected lists the (id, title) pairs the materializer is allowed
//     to operate on. The validator rejects any patch operation whose
//     target is not in this list.
//   - AllowedOps is the closed set of Operations the materializer may
//     apply. Any Patch operation outside this set is rejected.
//   - ForbidRemovals, when true, prohibits any patch operation whose
//     net effect is removing a node from the memory graph (merge with
//     no survivor, regroup-to-tombstone). Most safety-conscious
//     contracts set this; cleanup-only contracts may relax it.
//   - ContextLimit caps how many memory context items the materializer
//     may request. The prompt threads this through as "fetch in
//     bounded batches of N"; the validator does not enforce it
//     (the loop CLI in #38 polices request sizes), but recording it on
//     the contract keeps the prompt rendering deterministic.
type Contract struct {
	ID             string      `json:"id"`
	Kind           string      `json:"kind"`
	Selected       []Selection `json:"selected"`
	AllowedOps     []Operation `json:"allowedOps"`
	ForbidRemovals bool        `json:"forbidRemovals"`
	ContextLimit   int         `json:"contextLimit,omitempty"`
}

// Selection is one (id, title) pair on the contract's allow-list. The
// title is informational; matching is by ID.
type Selection struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// SelectedSet returns the contract's selected IDs as a lookup set so
// the validator can answer "is X allowed?" in O(1).
func (c *Contract) SelectedSet() map[string]struct{} {
	out := make(map[string]struct{}, len(c.Selected))
	for _, s := range c.Selected {
		out[s.ID] = struct{}{}
	}
	return out
}

// AllowedOpSet returns the contract's AllowedOps as a lookup set.
func (c *Contract) AllowedOpSet() map[Operation]struct{} {
	out := make(map[Operation]struct{}, len(c.AllowedOps))
	for _, op := range c.AllowedOps {
		out[op] = struct{}{}
	}
	return out
}

// Validate confirms the contract is internally consistent before the
// orchestrator renders a prompt from it. Catches caller bugs early so
// they don't silently produce a degenerate prompt.
func (c *Contract) Validate() error {
	if c == nil {
		return errors.New("memopt: nil contract")
	}
	if c.ID == "" {
		return errors.New("memopt: contract has empty ID")
	}
	if c.Kind == "" {
		return errors.New("memopt: contract has empty Kind")
	}
	if len(c.Selected) == 0 {
		return errors.New("memopt: contract has no Selected nodes")
	}
	seen := make(map[string]struct{}, len(c.Selected))
	for i, s := range c.Selected {
		if s.ID == "" {
			return fmt.Errorf("memopt: contract Selected[%d] has empty ID", i)
		}
		if _, dup := seen[s.ID]; dup {
			return fmt.Errorf("memopt: contract Selected has duplicate ID %q", s.ID)
		}
		seen[s.ID] = struct{}{}
	}
	if len(c.AllowedOps) == 0 {
		return errors.New("memopt: contract has no AllowedOps")
	}
	for _, op := range c.AllowedOps {
		switch op {
		case OpRegroup, OpMerge, OpRetitle, OpAnnotate:
			// known
		default:
			return fmt.Errorf("memopt: contract AllowedOps contains unknown operation %q", op)
		}
	}
	return nil
}
