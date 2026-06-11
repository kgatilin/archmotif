// Package llm wires the Stage 7 LLM materializer: turning a structural
// skeleton (Stage 6) into a concrete code change.
//
// Per ADR-017 the package commits a small interface (Materializer),
// the input/output value types (Proposal, Branch), an Anthropic
// provider scaffold (full HTTP impl lands in Stage 7), pricing
// constants, and the v1 prompt template. The interface is the swap
// point: any future provider (OpenAI, local model) implements
// Materializer; the orchestrator never imports a concrete provider.
//
// This package is wiring-only in v1. No HTTP, no real LLM call. Stage
// 7 fills in AnthropicMaterializer.Apply.
package llm

import "context"

// Materializer renders a structural Proposal into a concrete code
// change wrapped in a Branch. Implementations are responsible for
// building the prompt, calling their underlying model, validating the
// returned diff, and recording usage. The orchestrator (Stage 9) holds
// a single Materializer reference and is provider-agnostic.
//
// Apply is expected to be slow (network) and must respect ctx
// cancellation. Apply does not commit, push, or otherwise mutate the
// repo; it returns a Branch describing what should be applied. Stage 7
// adds the apply / commit plumbing on top.
type Materializer interface {
	Apply(ctx context.Context, p Proposal) (Branch, error)
}

// Proposal is the input bundle for a single materialization call. The
// shape is fixed by ADR-017; new fields are additive.
//
// Field semantics:
//   - ID identifies the proposal across stages (e.g. "motif-001"). Stage
//     5 mints the ID; subsequent stages thread it through unchanged.
//   - Description is the human-readable summary from Stage 5
//     ("extract interface from N repeated method shapes").
//   - SkeletonGo carries the annotated-Go target shape from Stage 6,
//     including <Placeholder> identifiers the LLM must resolve into
//     domain-meaningful names.
//   - SkeletonYAML carries the companion YAML the Stage 8 verifier
//     consumes. It travels with the proposal so verification runs
//     against the same skeleton instance the LLM saw, even if the
//     materializer itself does not include the YAML in the prompt.
//   - Samples is the existing-instance fixtures from Stage 6, formatted
//     as role → instance-name maps. Multiple samples are listed
//     verbatim in the prompt so the model can mimic existing naming.
//   - AffectedFiles maps repo-relative paths to their current contents.
//     The materializer is expected to produce a diff that touches only
//     these files (plus possibly new files); enforcement is Stage 7's
//     job.
//   - Model overrides the provider default. Empty string means "use
//     the provider's default" (Sonnet 4.6 for Anthropic per ADR-017).
type Proposal struct {
	ID            string
	Description   string
	SkeletonGo    []byte
	SkeletonYAML  []byte
	Samples       []map[string]string
	AffectedFiles map[string][]byte
	Model         string
}

// Branch is the materializer's output: a not-yet-applied refactor.
//
// Field semantics:
//   - Name is the suggested git branch name. Convention is
//     "archmotif/refactor/<proposal-id>" so multiple refactors in a
//     repo don't collide. The orchestrator (Stage 9) creates the actual
//     branch.
//   - Diff is the unified-diff bytes returned by the model, already
//     extracted from the fenced ```diff block but not yet applied. It
//     is expected to apply cleanly with `git apply` from the module
//     root; the materializer validates this before returning.
//   - AppliedAt is an ISO-8601 timestamp recording when the diff was
//     produced, not when it was written to disk. The orchestrator may
//     use it to correlate with usage.jsonl entries.
type Branch struct {
	Name      string
	Diff      []byte
	AppliedAt string
}
