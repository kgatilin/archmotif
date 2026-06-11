# ADR-028 — Stage 8 closure: run verifier after `archmotif refactor`

**Status:** accepted
**Date:** 2026-05-05
**Stage:** 8 — Verification linter
**Supersedes:** —
**Builds on:** ADR-018 (subgraph isomorphism + Verifier interface), ADR-024 (LLM materialization), ADR-023 (skeleton renderer)

## Context

Issue #9 (Stage 8 — Verification linter) requires:

> CLI: `archmotif verify <proposal-id>` exits 0 on match, non-zero
> with diff on mismatch. **Also runs as part of `archmotif refactor`
> after the LLM call.**

The standalone `archmotif verify` command already shipped (ADR-018,
PR #22). The remaining piece is wiring the verifier into the refactor
flow so the loop closes: build graph → metrics → anomalies →
proposals → skeleton → LLM diff → **verify** → exit code.

Three independent decisions were on the table:

1. **Where to run.** Inside `llm.AnthropicMaterializer.Apply`, or
   one level up in `cmd/archmotif`?
2. **How to construct the skeleton.** Render YAML and parse it back,
   or convert `propose.Proposal` → `verify.Skeleton` in-memory?
3. **Failure policy.** On mismatch, roll back the branch, keep it,
   or only surface the diagnostic and exit non-zero?

## Decision

### 1. Run verify in `cmd/archmotif/refactor.go`, not inside the materializer

Verification belongs in the orchestrator, not the LLM provider. The
Materializer interface (ADR-017) is intentionally narrow — "render a
prompt, call a model, return a Branch" — and embedding a Stage 1 +
Stage 8 dependency inside it would couple every future provider
(OpenAI, local) to the verifier. The CLI is the orchestrator today;
Stage 9 will promote that orchestration to its own package without
changing where verification lives.

Concretely: after `mat.Apply` returns, the working tree is on the LLM
branch with the diff applied (per ADR-024 / `createAndApplyBranch`).
We rebuild the typed graph at `repoDir` and run the verifier against
the proposal's target subgraph.

### 2. Render-and-parse the skeleton, don't shortcut via in-memory conversion

The verifier loads `Skeleton` from YAML (`verify.LoadSkeletonFile` /
`ParseSkeleton`). The proposer mints `propose.Proposal` /
`TargetSubgraph`. There is an obvious shortcut — write a
`proposalToSkeleton` adapter — but we do **not** take it, for three
reasons:

- The standalone `archmotif verify` command already round-trips
  through YAML. Refactor doing the same gives one canonical execution
  path; a regression in either surface is caught by the same tests.
- ADR-018 §5 deliberately decoupled `verify.Skeleton` from
  `propose.TargetSubgraph` to keep the verifier shippable in parallel
  with the proposer. Reintroducing a direct conversion routes the
  coupling back through `cmd/archmotif`, where it's even harder to
  evolve cleanly than a single shared package would have been.
- The cost is negligible: `skeleton.RenderYAML(p)` produces ~1KB,
  `verify.ParseSkeleton` consumes it in microseconds. The render
  is already exercised by `archmotif skeleton` and by Stage 7's
  prompt assembly.

The orchestrator therefore calls `skeleton.RenderYAML(prop)` →
`verify.ParseSkeleton(bytes.NewReader(...))` → `parser.Build(...)` →
`verify.NewBacktrackVerifier().Verify(...)`. One execution path,
shared with the standalone command.

### 3. On mismatch: keep the branch, surface the diagnostic, exit 1

The LLM may have produced a buildable diff that just doesn't satisfy
the target shape. Three options were considered:

- **Roll back** (delete the branch). Loses the artefact the human
  needs to review. Reject.
- **Keep silently.** Lets a bad refactor land downstream. Reject.
- **Keep + surface diagnostic + exit 1.** The branch is preserved
  for inspection, the operator (and Stage 9 orchestrator) gets the
  same `Diff` envelope `archmotif verify` produces, and the non-zero
  exit code prevents pipeline scripts from auto-merging. **Accepted.**

This matches ADR-017's "fail loud, don't retry blindly; surface diff
for human" stance.

### 4. CLI surface

```
archmotif refactor --id=<proposal-id> [flags] <path>
  --no-verify           skip the post-Apply verification step
  --verify-format=text|json
                        format for the verification verdict (default: text)
```

`--no-verify` is the escape hatch for cases where the verifier is
known to be over-strict (e.g. a manual override). Default is **on**:
ADR-018 commits to strict subgraph match by default, and the closing
of the loop is the headline result of Stage 8.

`--dry-run` continues to skip both the API call and verification —
there is nothing to verify when no diff is produced.

### 5. Exit codes

| Code | Meaning |
|------|---------|
| 0    | LLM succeeded **and** verifier matched (or `--no-verify`) |
| 1    | LLM error, or verifier mismatched (branch is kept; verdict on stdout) |
| 2    | Argument or load error |

Exit 1 is overloaded — pipeline error vs verifier mismatch — but the
verdict format on stdout is unambiguous (`PASS …` vs `Mismatch …`),
and Stage 9 can disambiguate via the JSON envelope when needed.

## Alternatives considered

- **Run verify inside `Apply`.** Couples every Materializer to
  Stage 1 + Stage 8. Rejected (decision 1 above).
- **In-memory `Proposal → Skeleton` adapter.** Faster, but introduces
  a second conversion path that drifts from the standalone command.
  Rejected (decision 2 above).
- **Auto-rollback on mismatch.** Discards the artefact the operator
  needs to fix. Rejected (decision 3 above).
- **Always exit 0, write verdict to a sidecar file.** Hides the
  failure from automation. Rejected.

## Consequences

- The refactor command now has a hard dependency on the parser,
  skeleton renderer, and verifier packages (it already had the parser
  via the upstream pipeline; the new edges are skeleton + verify).
  All three are internal to archmotif — no new external deps.
- `--no-verify` is the documented opt-out. If experience shows the
  verifier is too strict in practice, the next ADR will revisit
  ADR-018 §"strictness" rather than make `--no-verify` the default.
- Tests live in `cmd/archmotif/refactor_verify_test.go` and exercise
  the `verifyAfterRefactor` helper directly, without invoking the LLM.
  Three scenarios cover issue #9's verify list:
  - matching shape → PASS,
  - mismatched shape → mismatch with role/edge diagnostic,
  - matching shape but contract method renamed → mismatch with
    missing-role diagnostic.
- The verdict format mirrors the standalone command's `--format`
  output, so existing parsers that consume `archmotif verify` work
  against `archmotif refactor` output too.
