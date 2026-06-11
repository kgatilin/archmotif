E2 result from #58 self-convergence: converged after substrate repair.

Accepted next slice:

- add a run-level report schema around repeated optimization batches;
- include graph summary and metric snapshots before the run, after each applied
  batch, and after the run;
- include catalog-style metric deltas with explicit stop criteria;
- add a `ConvergenceReport` with status
  `converged | max_batches | no_candidates | no_improvement | error`;
- distinguish planned, simulated, and applied batches;
- keep metric execution in `internal/metrics`, drift vocabulary in
  `internal/catalog`, and batch evidence in `internal/graphmlx`.

Reader correction: `cmd/archmotif/optimize_loop.go` already had command-local
run/batch structs and summary writing. The first repair made those concepts
explicit graph nodes instead of introducing a reusable `internal/optimizereport`
package prematurely.

Repair evidence:

- `OptimizeLoopRunReport`, `OptimizeLoopBatchReport`, and
  `OptimizeLoopConvergenceReport` now exist as graph-searchable types.
- `runOptimizeLoopInner` returns/uses `OptimizeLoopRunReport`, uses
  `OptimizeLoopBatchReport`, and calls `finalizeOptimizeLoopRunReport`.
- `finalizeOptimizeLoopRunReport` calls `buildOptimizeLoopConvergence`, which
  returns/uses `OptimizeLoopConvergenceReport`.

Full record:
`docs/experiments/archmotif-self-convergence/episode-e2-convergence-reports.md`.
