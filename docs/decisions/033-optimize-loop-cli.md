# ADR-033 — `archmotif optimize-loop` durable loop CLI

**Status:** accepted
**Date:** 2026-05-06
**Stage:** post-Stage-9 — durable optimization loop (issue #38)
**Builds on:** ADR-022 (Stage 5 proposer + conflict resolution),
ADR-024 (LLM materialization), ADR-031 (Stage 9 auto-pick)

## Context

Issue #38 calls out a working *scratch script* that loops over the
Stage 1 → 5 pipeline, exports a fresh memory GraphML each iteration,
calls the deterministic next-batch selector, and feeds the resulting
prompt into a Claude command (`claude -p`). The script is used in
practice (an external memory-graph project) but is not a maintained
component — there's no test, no run-directory contract, no agreed
stop-conditions.

The scope: turn it into a supported command/workflow.

Three constraints shaped the design:

1. The materializer is **not necessarily** the in-process Anthropic
   client used by `archmotif refactor`. Operators want to plug in
   `claude -p`, `cursor-agent`, or any other shell command that
   reads a prompt on stdin and emits a unified diff on stdout.
2. The loop must be **debuggable**: every batch's contract, prompt,
   patch, validation log, and apply log must land on disk under a
   single run directory.
3. **Validation before apply** is non-negotiable. A patch that
   doesn't pass `git apply --check` must halt the loop, not corrupt
   the working tree.

## Decision

### 1. New subcommand `archmotif optimize-loop`, not an extension of `refactor`

`archmotif refactor` (ADR-031) is single-batch and assumes the
in-process Anthropic materializer. Adding a loop flag plus a
`--materializer=CMD` flag plus per-batch artifact emission to that
command would double its surface area and blur its meaning. A
separate `optimize-loop` subcommand keeps each command's promise crisp:

- `refactor` — one batch, in-process materializer, branch out.
- `optimize-loop` — N batches, configurable materializer command,
  run-directory artifacts, working-tree mutation gated by `--apply`.

Both share the deterministic Stage 5 selector via the package-private
`runProposalPipeline` and `rankProposals` helpers in
`cmd/archmotif/refactor.go`.

### 2. Materializer is a shell command, default `claude -p`

`--materializer=CMD` parses CMD with a small POSIX-style splitter
(quoted segments stay intact). The command receives the rendered
prompt on stdin; its stdout is captured as the patch. Stderr is
written to `<batch>/materializer.stderr.log`.

Default is `claude -p` per issue #38's example.

The patch-extraction logic accepts two forms:

- A `` ```diff `` fenced block (the form `internal/llm.AnthropicMaterializer`
  enforces in its prompt-response contract).
- Bare unified-diff text starting with `diff --git ` or `--- /+++`
  (the simpler form a CLI materializer can emit when configured to
  skip prose).

Either form works, so the same loop can drive either an in-process
client or a CLI tool without protocol negotiation.

### 3. Run-directory layout

Each run lives under `--run-dir=DIR` (default
`.archmotif/runs/<RFC3339-timestamp>`). One subdirectory per batch:

```
<run-dir>/
  summary.json
  batch-001/
    contract.yaml          # skeleton YAML
    graph.graphml          # snapshot at start of iteration
    prompt.txt             # rendered materializer prompt
    proposal.json          # picked proposal, full JSON
    patch.diff             # extracted patch (if materializer ran)
    validation.log         # `git apply --check` result
    apply.log              # apply outcome or "skipped"
    materializer.stderr.log
  batch-002/...
```

`summary.json` records before/after node-and-edge counts, the
materializer command, the per-batch outcomes, and the stop reason.
Operator workflows can grep on `stopped_by` and `outcome` fields
without parsing prose.

### 4. Stop conditions are explicit and deterministic

The loop halts on the first of:

- **`no-batch`** — Stage 5 produces zero proposals (clean state;
  exit 0 — same convention as ADR-031 §2).
- **`validation-failed`** — `git apply --check` rejected the patch
  (exit 1; patch.diff and validation.log are kept for inspection).
- **`apply-failed`** — `git apply` itself failed after `--check`
  passed (exit 1; rare in practice, indicates concurrent-tree
  mutation).
- **`materializer-error`** — the materializer command exited
  non-zero or hung past its OS timeout (exit 1; stderr captured).
- **`no-diff`** — materializer stdout contains neither a fenced
  diff nor a bare unified diff (exit 1; stdout captured for triage).
- **`max-batches`** — `--max-batches` reached without an earlier
  stop (exit 0; stop reason recorded in summary).
- **`dry-run`** — `--dry-run` set; loop emits artifacts for one
  batch and exits 0.

### 5. `--apply` is opt-in

Default is *validation-only*: the loop confirms the patch applies
cleanly, writes it to `patch.diff`, and stops short of mutating the
working tree. `--apply` is the explicit knob to mutate the tree on
each successful batch.

Rationale: an operator running `optimize-loop` for the first time on
a new memory wants to see what the loop *would do* before it does it.
Symmetric with `refactor --dry-run`.

### 6. Decoupled from `internal/llm`

The optimize loop does **not** import `internal/llm.AnthropicMaterializer`.
It imports only `internal/llm.RenderPrompt` (to render the prompt
the materializer should see). Reasons:

- The configurable-command materializer is the v1 surface; the
  in-process Anthropic client is the alternative, not the default.
- Keeping the two paths separate avoids coupling the loop to the
  Anthropic provider's package-private internals (e.g.
  `fencedDiffRE`, `gitApplyCheck`). The optimize loop has its own
  copies of those helpers in `cmd/archmotif/optimize_loop.go`.

If a future ticket merges these, that's a focused refactor — not a
prerequisite for landing the loop.

## Alternatives considered

- **Add a `--loop` flag to `archmotif refactor`.** Doubles the
  surface area and forces every refactor caller to think about
  multi-iteration semantics. Rejected (decision 1).
- **Define a Materializer plugin interface in `internal/llm` and
  ship a `cli` provider.** More principled, but adds an in-process
  package boundary for what is essentially `exec.Command`. Defer
  until a third concrete materializer ships.
- **Always apply patches; require `--no-apply` to opt out.** The
  scratch-script behaviour was always-apply, but operators running
  this on real memories want a review step first. Rejected (decision
  5).
- **Emit one combined log (run.jsonl) instead of per-batch
  directories.** Cheaper to grep but harder to inspect a single
  failed batch. Rejected; per-batch directories are the natural
  unit for human review.

## Consequences

- New `archmotif optimize-loop` subcommand with the surface above.
- Default run-directory pattern (`.archmotif/runs/<ts>`) commits
  archmotif to a `.archmotif/` workspace convention. Future tickets
  that need persistent state (catalog, drift — Stage 10) plug in
  here.
- The optimize loop is a natural integration point for downstream
  tickets:
  - Issue #37 (anomaly detectors) — additional triggers will flow
    through the same Stage 4 → 5 path the loop already drives; no
    optimize-side change required.
  - Issue #39 (materializer prompt protocol) — the loop already
    accepts both the fenced-diff and the bare-diff form; if #39
    introduces a third form, `extractPatch` is the single place to
    extend.
- `internal/llm.gitApplyCheck` is duplicated as `gitApplyCheck` in
  `cmd/archmotif/optimize_loop.go`. Documented in the source as
  intentional decoupling. If a follow-up promotes the helpers to a
  shared `internal/git` package, both call sites switch in one PR.
- `--apply` defaults to off, so first-time runs are
  observe-only — no surprise mutations. Operators already running
  the scratch script need to add `--apply` to keep the old
  behaviour.
