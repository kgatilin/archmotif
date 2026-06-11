E4 result from #58 self-convergence: converged after substrate repair.

Accepted next slice:

- keep `cmd/archmotif` as the CLI adapter for flags, usage, mode selection,
  file/stdout/stderr plumbing, and exit-code mapping;
- extract optimize orchestration, DTOs, result structs, report writers, prompt
  renderers, and helpers into one internal optimize orchestration package;
- expose one main `Run(ctx, Options) (Result, error)` API and only add
  `RunBatch` or `RunLoop` if current CLI dispatch requires separate entrypoints;
- keep existing graph/metric/contract/proposal/shape packages as computation
  providers, not dumping grounds for CLI/reporting behavior.

Repair evidence:

- `internal/propose.CommandPackageSplitRule` now turns oversized `/cmd/`
  package modularity regions into a planning contract.
- `optimize --mode=architecture --pattern ./cmd/archmotif/... .` now reports
  `1` proposal and `1` `command_package_split` contract for
  `cmd/archmotif`.
- The target graph has `CLIAdapter` and `OptimizeOrchestration` package roles.

This is an actionable proposal contract, not an automatic safe materializer.

Full record:
`docs/experiments/archmotif-self-convergence/episode-e4-optimize-split.md`.
