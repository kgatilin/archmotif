Child of #58.

## Problem

The Stage 1 self-convergence baseline found `cmd/archmotif` as a structural
hotspot, with the optimize command family forming a second obvious extraction
candidate:

- `cmd/archmotif/optimize_batch.go` is `943` lines.
- `cmd/archmotif/optimize_architecture.go` is `805` lines.
- `cmd/archmotif/optimize_loop.go` is `645` lines.
- `optimize --mode=architecture --pattern ./cmd/archmotif/...` reported
  `40845` anomalies but `0` proposals and `0` contracts.

That last point is the important experiment signal: the graph/anomaly layer can
see a problem, but the proposal layer has no rule that maps an oversized command
package into a concrete refactor contract.

## Goal

Separate optimize orchestration/reporting from CLI flag parsing and decide
whether ArchMotif should add a proposal rule for oversized command packages.

## Scope

- Identify the smallest package boundary that moves optimization orchestration
  out of `cmd/archmotif`.
- Keep CLI-specific flag parsing and exit-code handling in `cmd/archmotif`.
- Preserve existing `optimize`, `optimize-batch`, and `optimize-loop` behavior.
- Decide whether the command-package anomaly should become a new proposal rule
  or remain a manual research pattern for now.

## Stage 1 experiment angle

Use this as an Architect/Reader episode:

- Architect sees only graph metrics/anomalies and proposes the boundary.
- Reader inspects code dependencies, tests, and implementation coupling.
- Curator classifies disagreement as `proposal_gap` if the architecture metric
  is real but not yet convertible to a contract.

## Acceptance criteria

- [ ] Proposed package boundary is recorded before implementation.
- [ ] `cmd/archmotif` no longer owns optimize orchestration internals after the
      refactor, only command wiring.
- [ ] Existing optimize tests pass.
- [ ] `optimize --mode=architecture --pattern ./cmd/archmotif/... .` is rerun
      after the change and compared to the baseline.
- [ ] Stage 1 episode log records Architect spec, Reader feedback, and
      convergence score.

