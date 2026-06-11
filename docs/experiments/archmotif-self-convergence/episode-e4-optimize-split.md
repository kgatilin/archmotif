# E4 - split optimize orchestration out of cmd/archmotif

**Date:** 2026-05-18
**Source:** #66
**Target:** contract set
**Status:** converged after substrate repair
**Round count:** 2 including repair rerun
**contract_jaccard:** 0.82
**reward:** 1

## Setup

Graph server:

```text
http://127.0.0.1:53393/
```

Architect constraint: graph server only. No repository file reads, no grep, no
git.

## spec_A summary

Architect proposed one internal optimization orchestration module:

- leave `cmd/archmotif` as the CLI adapter for flag parsing, mode dispatch,
  option construction, stdout/stderr selection, and user-facing errors;
- move optimize-specific orchestration, reporting, DTOs, and helper clusters
  behind a narrow internal API;
- keep existing domain computations in `internal/shape`, `internal/contracts`,
  `internal/propose`, `internal/anomalies`, `internal/graph`,
  `internal/metrics`, and related packages.

Graph-only evidence used by Architect:

- Package overview showed `cmd/archmotif` with high degree and dependencies to
  many internal packages.
- Search for `optimize` showed symbols concentrated in `optimize.go`,
  `optimize_architecture.go`, `optimize_batch.go`, and `optimize_loop.go`.
- The `runOptimize` neighborhood was dense dispatcher code.
- `runOptimizeBatch` called selection, summary, prompt rendering, deterministic
  patch helpers, and `internal/shape.Options`.
- `runArchitectureOptimize` called ranking/build/report/export helpers and
  `internal/contracts.BuildOptions`.
- `runOptimizeLoopCmd` wrapped loop config, inner loop, summary writing, and
  materializer command execution.

## fb_B

Reader code inspection confirms the main boundary:

- `runOptimize` is command dispatch and flag-level option assembly.
- `runOptimizeBatch` depends on `normalizeDirection`, summary rendering, JSON
  output, prompt generation, and shape options.
- `runArchitectureOptimize` owns architecture optimize orchestration and report
  export behavior around contract/proposal packages.
- `runOptimizeLoopCmd` wraps `runOptimizeLoopInner`, `writeRunSummaryText`, and
  materializer execution.
- The largest optimize files are command package files, not internal domain
  packages.

Reader adjustments:

- Keep the first extraction narrow: CLI parsing stays in `cmd/archmotif`;
  orchestration/report types move together.
- Do not create a broad automatic materializer for "oversized command package".
  The graph evidence is a good manual refactor signal. A narrow proposal rule
  can still emit an actionable planning contract when it does not claim safe
  code movement by itself.
- If `normalizeDirection` remains CLI input normalization, keep it CLI-side. If
  internal orchestration needs normalized direction values, pass normalized
  options instead of importing command helpers.

## Accepted spec

First implementation should:

- add one internal optimize orchestration package;
- expose one main `Run(ctx, Options) (Result, error)` API and only keep
  separate `RunBatch` or `RunLoop` APIs if current CLI dispatch requires them;
- move optimize DTOs, result structs, report writers, prompt renderers, and
  orchestration helpers with the code that uses them;
- keep `cmd/archmotif` responsible for flags, usage text, mode selection,
  file/stdout/stderr plumbing, and exit-code mapping;
- keep existing graph/metric/contract/proposal/shape packages as computation
  providers rather than dumping CLI/reporting behavior into them.

## Curator notes

Converged in one round on the target boundary, then needed one substrate repair
round because the optimizer could not emit a contract for the anomaly class it
already detected.

Disagreement categories:

- `proposal_gap`: the original optimizer reported the command package as
  oversized but had no actionable contract rule for this packaging split.
- `wrong_boundary`: minor risk if the new internal package mirrors every old
  command entrypoint instead of providing a narrow orchestration API.
- `wrong_dependency`: avoid any back-edge from internal optimize code to
  `cmd/archmotif` helpers.

## Repair rerun

Code changes:

- `internal/propose.CommandPackageSplitRule` triggers on oversized `/cmd/`
  package modularity regions.
- The target subgraph has two package roles: `CLIAdapter` and
  `OptimizeOrchestration`, with `CLIAdapter -> OptimizeOrchestration`
  `dependsOn`.
- `optimize_architecture` classifies the emitted contract as
  `command_package_split` and gives it command-package-specific expected metric
  movement.

Rerun evidence:

```text
optimize architecture: 1 contract(s), graph=1643 nodes/3526 edges,
anomalies=42095, proposals=1
```

The emitted contract has kind `command_package_split`, rule
`command_package_split`, objective target
`pkg:github.com/kgatilin/archmotif/cmd/archmotif`, and expected movement:
`command package region should shrink enough to drop below the modularity
oversize threshold after graph regeneration`.

Updated judgment: the previous proposal gap is closed. The remaining #66 work
is the actual optimize orchestration extraction, not proposal generation.
