# ADR-032 — Materializer prompt protocol with memory context gathering

**Status:** accepted
**Date:** 2026-05-06
**Stage:** memory-graph optimization track (parallel to Stages 7–9)
**Supersedes:** —
**Refines:** —
**Related:** ADR-017, ADR-024 (code-refactor materializer); issues #37, #38

## Context

archmotif's first materializer (ADR-017 / ADR-024) emits a unified Go
diff for code refactors. Issue #39 introduces a second, narrower
materializer that operates on **memory GraphML** instead of code: the
optimizer (#37) selects a small batch of nodes, the loop CLI (#38)
hands the batch to an LLM as a contract, the LLM regroups / merges /
annotates the nodes and returns a structured patch.

The two tracks share no code — different inputs, different outputs,
different safety properties. They share only an architectural pattern:
the orchestrator hands the LLM a closed-set contract and refuses to
apply replies that violate it.

This ADR records the protocol the memory-graph materializer follows.

## Decisions

### Closed-set contract

The contract handed to the materializer declares:

- a list of `selected` `(id, title)` pairs — the only nodes the
  materializer is allowed to operate on,
- a list of `allowedOps` — the only operation kinds it may apply
  (`regroup`, `merge`, `retitle`, `annotate`),
- a `forbidRemovals` flag — when true, every `merge` and `regroup`
  must name a `secondaryId` so no node ends up tombstoned,
- a `contextLimit` — the bounded-batch size the prompt threads through
  for memory-context fetching.

Closed-set is load-bearing: the validator rejects any patch operation
whose target is not on `selected` or whose op is not on `allowedOps`.
This is the structural equivalent of "you may only edit these files"
in the code track.

### Bounded-batch memory context gathering

The prompt instructs the materializer to fetch memory context **by ID
or title** in batches of at most `contextLimit` items per request.
This is the issue-#39 "memory context gathering" rule made concrete:

- prefer ID lookups; fall back to exact-title lookups only when an ID
  fetch returns nothing,
- stop fetching once every selected node has enough context for an
  assignment decision,
- report every fetched item in `contextSourcesUsed`.

`contextSourcesUsed` is mandatory. An empty list is treated as a
non-response (the materializer hallucinated, or skipped context, or
both) and the validator rejects it.

### Output shape: one fenced JSON block

Mirroring the code-track contract (one fenced ` ```diff ` block,
nothing before or after), the memory-track output is one fenced
` ```json ` block. The decoder uses `DisallowUnknownFields` so an LLM
that adds a field (or misspells one) fails parse and surfaces the
typo immediately rather than silently dropping data.

The patch carries five fields:

- `contractId` — must equal the contract's id.
- `operations` — the structural edits, each `{op, targetId,
  secondaryId?, rationale?}`.
- `assignmentValidation` — exactly one entry per selected node, each
  `{nodeId, outcome, note?}` with `outcome` ∈
  `{kept, regrouped, merged, retitled, annotated}`.
- `groupingRationale` — non-empty paragraph; required when any op is
  a `regroup` or `merge`.
- `contextSourcesUsed` — the memory items the materializer fetched.

`assignmentValidation` is the heart of the protocol: it forces the
materializer to take a position on **every** selected node, not just
the ones it edited. A node that ends up unchanged shows up as `kept`.
A patch missing entries is rejected with `ErrMissingNode` and the
error names the missing IDs.

### Validator: distinct sentinels per failure mode

The validator returns one error sentinel per failure mode so the
loop CLI can map exit codes and log lines consistently:

| Sentinel                    | Catches                                                |
|-----------------------------|--------------------------------------------------------|
| `ErrContractIDMismatch`     | wrong `contractId`                                     |
| `ErrShapeChange`            | op outside `allowedOps` (e.g. merge on annotate-only)  |
| `ErrUnknownOp`              | typo in `op` field                                     |
| `ErrExtraNode`              | op or assignment names a non-`selected` id             |
| `ErrMissingNode`            | `assignmentValidation` missing entries for selected    |
| `ErrForbiddenRemoval`       | regroup/merge with empty `secondaryId` under `forbidRemovals` |
| `ErrMissingRationale`       | regroup/merge present but `groupingRationale` empty    |
| `ErrMissingContextSources`  | empty `contextSourcesUsed`                             |
| `ErrUnknownOutcome`         | `outcome` outside the closed set                       |
| `ErrDuplicateAssignment`    | two assignment entries for the same nodeId             |

Distinct sentinels, not a generic "validation failed", is the same
pattern ADR-024 took for the code-track parse failures. It makes
`errors.Is` the right tool for the loop CLI's failure handling.

### Prompt versioning

The template lives at `internal/memopt/prompts/v1.tmpl` and is
embedded via `//go:embed`. `PromptVersion = "v1"` ships as a package
constant; the loop CLI records this string in the run log so a
reviewer can correlate an applied patch with the prompt version that
produced it. A v2 template lives at `prompts/v2.tmpl` when a future
PR adjusts wording or adds a field.

### Default context limit = 20

When a contract leaves `contextLimit` at zero the prompt renders
"bounded batches of at most 20". Twenty items sits comfortably above
the typical batch shapes (≤ 12 nodes) while staying small enough to
keep the response window predictable.

## Alternatives considered

- **Separate prompt per batch shape (`orphan_v1.tmpl`,
  `flat_star_v1.tmpl`).** Rejected. Branching on `kind` inside one
  template keeps the protocol stable and avoids a combinatorial blow-up
  as new detectors land in #37.
- **YAML output instead of JSON.** Rejected. JSON has stdlib
  decoding, schema validation via `DisallowUnknownFields`, and trivial
  fenced-block extraction. YAML buys nothing here.
- **Tools API (forced JSON).** Worth evaluating once #38 has baseline
  numbers. Non-breaking; the validator already speaks Go structs and
  doesn't care how the LLM produced the bytes.
- **Multi-error reporting on validation.** Rejected for v1. The loop
  CLI re-runs validation after each materializer correction, so first-
  failure reporting is sufficient and keeps the implementation small.

## Consequences

- Issue #38 (loop CLI) imports `internal/memopt` and depends on this
  protocol; no other package does.
- Issue #37 (anomaly detectors) does not depend on this package; it
  produces the structural input the orchestrator turns into a
  `Contract`.
- Adding a new operation kind (e.g. `split`) is one constant + one
  validator switch arm + one ADR.
- Adding a new batch shape (e.g. `community_parent_mismatch` from #37)
  is zero changes here: the prompt templates the `kind` field; the
  validator is shape-agnostic.
