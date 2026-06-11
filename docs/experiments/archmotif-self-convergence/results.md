# Stage 1 results

**Date:** 2026-05-18
**Issue:** #58

## Verdict

The two-agent ArchMotif convergence setup is working for Stage 1.

The useful shape is:

- Architect: graph-only, produces boundary contracts and graph deltas.
- Reader: full repository and command access, verifies code semantics,
  transport behavior, and stale ticket claims.
- Curator: records accepted specs and graph gaps.

This is not enough to fully automate refactors. It is enough to turn graph
neighborhoods into useful feature/refactor specifications or retrospective
boundary assessments when the Reader role is mandatory, and it is useful for
iteratively improving the graph/proposal substrate when Reader finds a blind
spot.

## Episode score

| Episode | Issue | Result | contract_jaccard | reward |
| --- | --- | --- | --- | --- |
| E1 | #59 | converged | 0.86 | 1 |
| E2 | #40 | converged after substrate repair | 0.78 | 1 |
| E3 | #65 | converged | 0.80 | 1 |
| E4 | #66 | converged after substrate repair | 0.82 | 1 |
| E5 | #57 | converged with semantic gaps | 0.74 | 1 |

Three episodes converged in one Architect round. E2 and E4 deliberately fed
Reader-discovered blind spots back into the tool/graph substrate and then
reran. E5 needed one bootstrap retry because the prompt did not list the actual
browser/MCP endpoints; after the protocol fix, the graph-only assessment
converged.

## What worked

- Graph-only Architect agents consistently found large structural boundaries in
  `cmd/archmotif`.
- The unified graph server gave enough API surface for agents to inspect
  packages, neighborhoods, search results, and browser/MCP boundaries.
- Reader feedback corrected the parts the graph cannot know: stale issue text,
  Go package granularity, mcp-go transport behavior, and run-time semantics.
- The accepted specs are immediately useful as implementation plans for #59,
  #40, #65, and #66.

## First-pass gaps found by Reader

- The graph cannot identify stale tracker wording. #59 still mentioned a
  Rust/axum implementation and a separate HTTP MCP path.
- The graph cannot prove protocol-level behavior. The E3 pilot originally used
  an invalid `OPTIONS /mcp` smoke before Reader replaced it with a real
  JSON-RPC POST.
- The original graph could not see enough run semantics for convergence
  reporting. E2 needed Reader inspection of `optimize_loop.go` to distinguish
  existing command-local run structs from missing report concepts. This is now
  repaired by graph-visible `OptimizeLoopRunReport`,
  `OptimizeLoopBatchReport`, `OptimizeLoopConvergenceReport`, and production
  edges from `runOptimizeLoopInner` through convergence assembly.
- The original optimizer had a proposal gap: the oversized `cmd/archmotif`
  package was visible as an anomaly, but no proposal rule emitted an actionable
  command-package extraction contract. This is now repaired by
  `CommandPackageSplitRule`, which emits a `command_package_split` contract.
- The contract lens has a semantic sharpness gap: MCP `contracts_list` returned
  `227` contracts, all observed as public DTOs, while browser package stats
  still reported `contracts: 0`.

## Tooling fixes made

- MCP smoke now uses a real JSON-RPC `tools/call` POST instead of `OPTIONS
  /mcp`.
- Architect bootstrap now lists exact graph-only endpoints:
  `/api/layouts`, `/api/search`, `/api/graph`, and `/mcp` JSON-RPC.
- Nonexistent discovery routes such as `/api`, `/graph/summary`, and
  `/api/graph/summary` are explicitly disallowed unless the Curator provides
  them for an episode.
- E2 substrate repair: optimize-loop run, batch, and convergence report
  concepts are explicit graph nodes, and `runOptimizeLoopInner` has graph
  edges to report assembly.
- E4 substrate repair: the architecture optimizer now has a proposal rule that
  turns oversized command packages into a concrete split contract.

## Decision

Proceed with the two-agent setup for ArchMotif planning and refactor design.
Use it as a decision-support loop, not as an autonomous implementation oracle.

Required guardrails for the next stage:

- every graph-only spec gets Reader verification before implementation;
- stale issue text is treated as evidence to validate, not ground truth;
- protocol smokes must exercise real protocol calls;
- proposal gaps are either recorded explicitly or turned into narrow proposal
  rules when the target contract is structural and does not claim automatic
  materialization safety.
