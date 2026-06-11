# E2 - convergence and multi-scale optimization reports

**Date:** 2026-05-18
**Source:** #40
**Target:** contract set
**Status:** converged after substrate repair
**Round count:** 2 including repair rerun
**contract_jaccard:** 0.78
**reward:** 1

## Setup

Graph server:

```text
http://127.0.0.1:53393/
```

Architect constraint: graph server only. No repository file reads, no grep, no
git.

## spec_A summary

Architect proposed a report layer around existing optimization batches:

- add a small `internal/optimizereport` or equivalent module;
- define a run-level JSON schema with `RunReport`, ordered `BatchReport`
  entries, metric deltas, warnings/errors, and a `ConvergenceReport`;
- keep `internal/metrics` responsible only for metric execution;
- keep `internal/catalog` responsible for snapshot/diff vocabulary;
- keep `internal/graphmlx` responsible for candidate selection and batch
  evidence;
- avoid reverse dependencies from `metrics`, `catalog`, or `graphmlx` into the
  report layer.

Graph-only evidence used by Architect:

- Package graph showed `cmd/archmotif` depending on `catalog`, `metrics`, and
  `graphmlx`.
- Search for `converge` and `RunLoop` returned no results, suggesting a new
  explicit convergence boundary.
- Search for `batch` surfaced `optimizeBatch`, `batchInput`,
  `batchSelection`, `orphanBatch`, `orphanMetrics`, and `runOptimizeBatch`.
- The `internal/graphmlx` package view surfaced `OptimizeBatchOptions`,
  `OptimizeBatchResult`, `OptimizeBatch`, and `WriteJSON`.
- The `internal/catalog` package view surfaced `Snapshot.Metrics`,
  `MetricEntry`, `Diff`, `Drift`, `MetricDelta`, `WriteJSON`, and `WriteText`.
- The `internal/metrics` package view surfaced `Run`, `RunContext`, `Result`,
  `Record`, `JSON`, and `WriteJSON`.

## fb_B

Reader code inspection confirms the direction with one granularity adjustment:

- `cmd/archmotif/optimize_loop.go` already defines `optimizeLoopConfig`,
  `batchResult`, `runResult`, `runOptimizeLoopInner`, and
  `writeRunSummaryText`.
- `internal/graphmlx.OptimizeBatchResult` already carries batch-level evidence.
- `internal/metrics.Result` is the metric execution output.
- `internal/catalog.MetricDelta` and `catalog.Diff` are good vocabulary, but
  catalog snapshots are ref-level artifacts and should not become the owner of
  per-batch convergence.

Reader adjustments:

- First extend the existing optimize-loop output schema; introduce an internal
  report package only when the schema needs reuse outside the command.
- Store whether each batch is planned, simulated, or applied. Without that,
  "after" metrics are ambiguous.
- Make convergence scoring explicit. Mixed objectives such as modularity,
  cycle reduction, and orphan reduction cannot be reduced to "improved" without
  stated weights or stop criteria.

## Accepted spec

First implementation should:

- add a run-level report schema for repeated optimization batches;
- include input graph id/path, metrics requested, stop criteria, and artifact
  paths;
- record graph summary and metrics before the run, after each applied batch,
  and after the run;
- add metric deltas using catalog-style `{name, from, to, delta}` vocabulary;
- add `ConvergenceReport` with status
  `converged | max_batches | no_candidates | no_improvement | error`;
- distinguish planned, simulated, and applied batch states;
- keep metric execution in `internal/metrics`, drift vocabulary in
  `internal/catalog`, and batch candidate evidence in `internal/graphmlx`.

## Curator notes

Converged in one round. The graph-only Architect found the correct package
direction, but under-saw the existing command-local optimize-loop structures.
That is expected: the graph exposed symbols and dependencies, while the Reader
had to inspect file-level run semantics.

Disagreement categories:

- `missing_structure`: convergence is not yet a first-class graph concept.
- `missing_concept`: planned versus simulated versus applied batches are
  semantic states, not visible in the current graph.
- `excess_scope`: a new package can wait until the report schema escapes the
  command package.

## Repair rerun

Reader feedback was fed back into the tool substrate instead of left as a
permanent caveat.

Code changes:

- `cmd/archmotif/optimize_loop_report.go` adds graph-visible
  `OptimizeLoopRunReport`, `OptimizeLoopBatchReport`, and
  `OptimizeLoopConvergenceReport`.
- `runOptimizeLoopInner` now returns `*OptimizeLoopRunReport`, constructs
  `OptimizeLoopBatchReport` values, and finalizes the run report through
  `finalizeOptimizeLoopRunReport`.
- `finalizeOptimizeLoopRunReport` calls `buildOptimizeLoopConvergence`, which
  returns `OptimizeLoopConvergenceReport`.

Graph-only rerun evidence:

- `/api/search?q=runOptimizeLoopInner` finds
  `cmd/archmotif/optimize_loop.go:166:6:function:runOptimizeLoopInner`.
- `/api/search?q=OptimizeLoopConvergenceReport` finds the report type and its
  convergence fields.
- The `runOptimizeLoopInner` neighborhood shows direct edges to
  `finalizeOptimizeLoopRunReport`, `OptimizeLoopRunReport`, and
  `OptimizeLoopBatchReport`.
- The `buildOptimizeLoopConvergence` neighborhood shows `returns` and
  `usesType` edges to `OptimizeLoopConvergenceReport`.

Updated judgment: the original E2 production-link gap is closed for graph-only
planning. Full multi-scale report richness is still future product work, but
the graph can now see that optimize-loop runs assemble convergence reporting.
