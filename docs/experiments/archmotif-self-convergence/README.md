# ArchMotif self-convergence experiment

**Issue:** #58
**Status:** Stage 1 completed; substrate repair pass completed
**Date:** 2026-05-18
**Codebase:** `kgatilin/archmotif`

This experiment dogfoods ArchMotif on ArchMotif itself. `deskd` was the
original suggested small codebase, but it is Rust and the current ArchMotif
extractor is Go-only. ArchMotif is the correct Stage 1 target because the graph
substrate can inspect the real implementation under test.

## Objective

Measure whether an Architect agent that only sees the ArchMotif graph can
produce useful feature/refactor specs that converge with a Reader agent that
can inspect files and run commands.

Stage 1 is manual. There is no automated Curator yet; the human operator
curates graph gaps between episodes.

## Conclusion

The two-agent setup is workable for bounded ArchMotif feature and refactor
specification:

- Architect sees only the graph server and produces contracts, boundaries, and
  graph deltas.
- Reader inspects files, commands, and protocol details, then accepts or
  corrects the graph-only spec.
- Curator records the accepted spec and the graph gaps as training signal.

Stage 1 ran five episodes. All five produced useful accepted specs or
assessments once the graph-only API bootstrap was explicit. The setup is
strongest for structural refactors where package/function neighborhoods expose
real boundaries.

The first pass also found two graph-substrate gaps that had to be repaired
instead of left as caveats:

- E2 could not prove optimize-loop convergence reporting production from graph
  evidence alone.
- E4 could see the oversized `cmd/archmotif` package anomaly but the optimizer
  emitted no actionable command-package split contract.

Both gaps now have code-level substrate fixes and graph-only rerun evidence.
There is no unresolved experiment tooling blocker. The graph server, browser
HTTP API, and MCP endpoint were smoke-tested during the run. The other tooling
corrections were replacing an invalid `OPTIONS /mcp` probe with a real JSON-RPC
`tools/call` POST, and making the Architect bootstrap list exact browser/MCP
endpoints instead of relying on nonexistent discovery routes.

## Current baseline

The `cmd/archmotif` package is the first metrics-derived refactor candidate:

- `go run ./cmd/archmotif graph --summary --pattern ./cmd/archmotif/... .`
  returned `1643` nodes and `3526` edges after the target-contract pass.
- The package has `43` Go files and `9302` total lines including tests.
- The biggest files are `view.go` (`1469` lines), `optimize_batch.go` (`943`),
  `optimize_architecture.go` (`821`), and `optimize_loop.go` (`616`).
- `anomalies --pattern ./cmd/archmotif/...` reported the top anomaly as
  modularity score `442.47`: `cmd/archmotif` has `1317` members versus its
  package siblings.
- `optimize --mode=architecture --pattern ./cmd/archmotif/...` found
  `42095` anomalies, `1` proposal, and `1` contract after the target-contract
  pass.

Interpretation: the current graph sees the CLI package as structurally
oversized, and the proposal layer now turns that anomaly into a
`command_package_split` refactor contract with `CLIAdapter` and
`OptimizeOrchestration` package roles. The contract is still a planning
artifact; the actual optimize orchestration extraction remains future work.

## Episode set

| Episode | Source | Target | Status |
| --- | --- | --- | --- |
| E1 | #59 | Live graph watch over the unified Go graph server | converged |
| E2 | #40 | Convergence and multi-scale optimization reports | converged after substrate repair |
| E3 | #65 | Split browser graph server/view model out of `cmd/archmotif` | pilot converged |
| E4 | #66 | Split optimize orchestration out of `cmd/archmotif` | converged after substrate repair |
| E5 | #57 | Retrospective contract-lens boundary assessment | converged with semantic gaps |
| E6 | #58 process | Run the first A/B convergence episode and record baseline | complete |

Use `protocol.md` for role prompts and scoring, and `episodes.md` as the
append-only run log.

See `results.md` for the aggregate score and `tooling.md` for smoke evidence.

## Graph server

Start the graph server from the repo root:

```bash
go run ./cmd/archmotif view \
  --root /tmp/archmotif-self-convergence \
  --graph-id archmotif \
  --http 127.0.0.1:7140 \
  .
```

The browser is at `/`; streamable HTTP MCP is at `/mcp`. Stdio MCP clients can
also use:

```bash
go run ./cmd/archmotif mcp serve --root /tmp/archmotif-self-convergence
```

Both transports operate on the same workspace graph
`graphs/archmotif/actual.graphml`.
