# ADR-031 — Stage 9 closure: auto-pick the top proposal in `archmotif refactor`

**Status:** accepted
**Date:** 2026-05-05
**Stage:** 9 — End-to-end refactor demo (issue #10)
**Builds on:** ADR-022 (Stage 5 proposer + conflict resolution),
ADR-024 (LLM materialization), ADR-028 (post-Apply verification)

## Context

ROADMAP §"Stage 9 — End-to-end refactor demo" says:

> `archmotif refactor <pkg>` orchestrates Stages 1–8: build graph,
> compute metrics, detect top anomaly, propose transformation, render
> skeleton, call LLM, verify result, output branch.

Issue #10 narrows the missing piece:

> Single-command demo on archmotif itself produces a usable branch.
> Open question to resolve in-flight: no anomalies above threshold —
> exit gracefully with "nothing to propose".

The current `archmotif refactor` (post-ADR-024 / ADR-028) already
runs Stages 3 → 4 → 5 to find proposals, builds the LLM proposal,
calls Anthropic, applies the diff on a fresh branch, and runs the
Stage 8 verifier. The remaining gap: **`--id` is mandatory**, so
"single-command demo" requires the operator to first run
`archmotif propose .`, copy an ID, and only then invoke `refactor`.
That's not a demo — it's a workflow.

Three independent decisions were on the table:

1. **How to pick** when `--id` is omitted: highest-score proposal,
   first-in-trigger-order, or fail loudly?
2. **Empty-pipeline behaviour**: the open question from issue #10.
3. **Discoverability**: should the operator have a way to *see* the
   proposals before committing to one?

## Decision

### 1. `--id` becomes optional; auto-pick by Trigger.Value desc, ID asc

When `--id` is empty, the orchestrator selects the proposal with
the **highest `Trigger.Value`** (i.e. the underlying metric record
value — for motif redundancy, the instance count). Ties break by
`Proposal.ID` ascending so the choice is reproducible across runs.

Rationale:

- `Trigger.Value` is the only impact-flavoured scalar surfaced on
  `propose.Proposal` today (the anomaly score `a.Score` lives on
  `anomalies.Anomaly` and isn't propagated). Using it keeps the
  pick deterministic and avoids a downstream API change.
- `Proposer.Propose` (ADR-022) already runs greedy-by-score
  conflict resolution; the surfaced `Proposals` slice is in
  trigger order, **not** score order, so auto-pick has to do its
  own ordering.
- For motif-redundancy (the only v1 trigger per ADR-019), instance
  count = motif size `n`, which is exactly the "bigger refactor
  opportunity" signal the operator wants the demo to surface.
- Tiebreaker by ID asc keeps results stable when multiple motifs
  fire with identical instance counts (common on synthetic
  fixtures).

`--id` continues to work for explicit selection — CI scripts and
deterministic regression tests stay supported.

### 2. Empty pipeline → exit 0 with "nothing to propose"

If the Stage 3 → 4 → 5 pipeline produces zero proposals, the
orchestrator prints `archmotif refactor: nothing to propose` to
stdout (not stderr — the message is the *result*, not an error)
and exits **0**. Per issue #10's open question.

Rationale:

- The codebase under analysis may have no anomalies above the
  detector thresholds. That's a clean state, not a failure.
- Exit 0 lets CI scripts run `archmotif refactor` periodically
  without alerting on "no work to do".
- The message goes to stdout so a script that consumes the output
  doesn't have to merge stderr.

When `--id` is given but matches no proposal, the existing error
path stays: stderr message + exit 1. That's a typo / stale ID, not
an empty pipeline.

### 3. New `--list` flag for previewing without commitment

Adding a `--list` flag to `archmotif refactor` is the smallest
surface change that closes the discoverability gap:

```
archmotif refactor --list <path>
```

Prints one proposal per line in auto-pick order:

```
extract_interface-motif-0  value=5  extract shared interface from 5 isomorphic motif instances
extract_interface-motif-1  value=3  extract shared interface from 3 isomorphic motif instances
```

Exit 0 always. No LLM call, no graph rebuild, no branch — just the
proposal stream. Stage 9's "demoable" requirement is satisfied by
auto-pick alone; `--list` is the operator's escape hatch.

We considered adding a separate `archmotif demo` subcommand instead
(symmetric with `archmotif graph --summary` etc.), but that
duplicates the Stage 3 → 4 → 5 pipeline plumbing for marginal
clarity. `--list` reuses the existing entry point and keeps the
single-command-demo promise intact.

### 4. Output format: human-readable line on auto-pick

When `--id` is empty and a proposal is auto-picked, the orchestrator
prints one line to stderr before the LLM call:

```
archmotif refactor: auto-picked extract_interface-motif-0 (value=5; 4 candidates)
```

Stderr (not stdout) so scripts that pipe stdout into a file still
get a clean output stream. Same format on dry-run, so the operator
can preview which proposal would be chosen without committing.

## Alternatives considered

- **Keep `--id` mandatory; add a separate `archmotif demo` subcommand.**
  Doubles the orchestration code path. Rejected (decision 3).
- **Auto-pick by anomaly score (`a.Score`) instead of `Trigger.Value`.**
  More principled in theory (Score is the per-detector anomaly
  scalar), but requires propagating Score onto `propose.Proposal`,
  which is API surface. Rejected for v1; revisit when a second
  metric ships and the cross-detector ranking question becomes
  real.
- **Empty pipeline → exit 1.** Wrong signal. CI scripts that ignore
  exit 1 from "no work" lose the ability to react to real failures.
  Rejected.
- **Auto-pick the first proposal in trigger order.** Order is stable
  but not impact-correlated; a later motif with 100 instances would
  lose to an earlier motif with 3. Rejected.

## Consequences

- The single-command demo promised in issue #10 / ROADMAP §Stage 9
  works: `archmotif refactor <path>` produces a branch (or a
  graceful "nothing to propose"), with the verifier run inline
  per ADR-028.
- The change is additive: existing `--id <id>` callers see no
  behaviour change. New callers (the demo) can omit `--id`.
- `make demo` is added as a Makefile target that runs the auto-pick
  path on archmotif itself with `--dry-run`, so contributors can
  see the orchestration without spending API tokens.
- `--list` is the documented way to preview proposals without an
  LLM call. Mirrors the convention of `archmotif metrics --list`
  (ADR-011 / metrics CLI).
- The auto-pick rule is documented and tested. Future tickets that
  add a second trigger metric must either propagate the anomaly
  score or accept that motif-redundancy keeps winning ties.
