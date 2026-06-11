# Episode log

Append one section per run. Keep failed episodes; they are the training signal.

## E1 — live graph watch over unified server

**Source:** #59
**Target:** contract set
**Status:** converged after substrate repair
**Round count:** 2 including repair rerun
**contract_jaccard:** 0.86
**reward:** 1

Ticket #59 is useful but stale. It describes a Rust/axum implementation and a
separate `archmotif mcp serve --http` launch path. Current ArchMotif is Go, and
`archmotif view` now starts one graph server with browser `/` and MCP `/mcp`.

Expected Architect task:

- propose the next smallest live-watch slice over the existing unified graph
  server;
- preserve one graph store and one MCP service;
- add mutation notifications or a documented polling fallback;
- keep the browser framework-free.

Expected Reader checks:

- verify whether mcp-go streamable HTTP can carry notifications in the desired
  shape;
- check existing mutation logging in `internal/mcpserver`;
- check browser code boundaries in `cmd/archmotif/view.go`.

Result:

- `episode-e1-live-watch.md` records the full run.
- Accepted spec: add a small in-process graph update hub, publish after
  successful `mcpserver.Service` mutations, expose browser-facing SSE at
  `/api/events`, and let the framework-free browser debounce events and refetch
  the existing `/api/graph` view.
- Reader correction: keep `/api/events` separate from MCP protocol semantics.
  mcp-go has notification/session support, but the current `/mcp` route is
  configured with stateless streamable HTTP, so explicit browser SSE is the
  smaller first slice.

## E2 — convergence and multi-scale reports

**Source:** #40
**Target:** contract set
**Status:** converged
**Round count:** 1
**contract_jaccard:** 0.78
**reward:** 1

Expected Architect task:

- propose a run-level report schema for repeated optimization batches;
- identify which existing metrics can populate before/after deltas;
- decide whether this belongs in CLI, internal package, or both.

Expected Reader checks:

- inspect `cmd/archmotif/optimize_loop.go`, `internal/catalog`, and
  `internal/metrics`;
- verify whether catalog drift can be reused or whether batch convergence needs
  a separate schema.

Result:

- `episode-e2-convergence-reports.md` records the full run.
- Accepted spec: add a thin run-report schema around the existing
  `optimize-loop` and batch results. Reuse `internal/metrics` for measurement
  and catalog-style metric deltas for vocabulary, but do not move convergence
  semantics into `metrics`, `catalog`, or `graphmlx`.
- Reader correction: `cmd/archmotif/optimize_loop.go` already has run and batch
  result structs plus summary writers, so the first implementation can extend
  that output before introducing a package split.
- Repair rerun: the command-local structs were made graph-visible as
  `OptimizeLoopRunReport`, `OptimizeLoopBatchReport`, and
  `OptimizeLoopConvergenceReport`. The graph now shows `runOptimizeLoopInner`
  returning/using the run report, using the batch report, calling
  `finalizeOptimizeLoopRunReport`, and the finalize path calling
  `buildOptimizeLoopConvergence`.

## E3 — split browser graph server out of cmd/archmotif

**Source:** #65
**Target:** contract set
**Status:** pilot converged

Observed problem:

- `cmd/archmotif/view.go` is `1469` lines.
- It contains CLI flag parsing, graph extraction, graph workspace writes, HTTP
  server setup, browser handlers, view-model construction, GraphML-to-typed
  conversion, static assets, and layout code in one command package file.

Expected Architect task:

- propose a boundary that leaves `cmd/archmotif` as thin CLI orchestration;
- move graph-server/browser model logic into an internal package without
  inventing a generic UI framework;
- keep the `archmotif view` UX unchanged.

Expected Reader checks:

- inspect import cycles and test fallout;
- decide whether the package should be `internal/graphserver`,
  `internal/browser`, or a smaller name;
- verify that `/`, `/api/graph`, `/api/search`, and `/mcp` remain one server.

Pilot result:

- `episode-e3-pilot.md` records the full run.
- `contract_jaccard = 0.80`, `reward = 1`.
- Architect converged on the main boundary in one round: `cmd/archmotif view`
  becomes orchestration, browser/server/viewer logic moves to an internal
  graph-server package, and `internal/mcpserver.Service` remains the shared
  graph store for browser and MCP.
- Reader adjustment: start with one `internal/graphserver` package; only split a
  separate `internal/graphbrowser` if the first extraction remains too large.
- Reader correction: `OPTIONS /mcp` is not a valid smoke; direct POST
  `graph_checkout` to `/mcp` succeeded against `archmotif:actual`.

## E4 — split optimize orchestration out of cmd/archmotif

**Source:** #66
**Target:** contract set
**Status:** converged after substrate repair
**Round count:** 2 including repair rerun
**contract_jaccard:** 0.82
**reward:** 1

Observed problem:

- `cmd/archmotif/optimize_batch.go` is `943` lines.
- `cmd/archmotif/optimize_architecture.go` is `805` lines.
- `cmd/archmotif/optimize_loop.go` is `645` lines.
- The optimizer returned `40845` anomalies but `0` proposals for the oversized
  command package, which is a `proposal_gap`.

Expected Architect task:

- propose the smallest extraction that separates CLI parsing from optimization
  orchestration/reporting;
- identify whether a new proposal rule is needed for oversized command
  packages or whether this should remain a manual pattern.

Expected Reader checks:

- inspect which functions already depend only on internal packages;
- verify that tests can remain near the public CLI surface while business logic
  moves inward.

Result:

- `episode-e4-optimize-split.md` records the full run.
- Accepted spec: keep `cmd/archmotif` as a CLI adapter and extract optimize
  orchestration/report DTOs/helpers into one internal optimization package.
  Existing domain packages keep the actual graph, metric, contract, proposal,
  and shape computations.
- Repair rerun: `internal/propose.CommandPackageSplitRule` is registered in the
  proposer, and `optimize --mode=architecture --pattern ./cmd/archmotif/... .`
  now emits one `command_package_split` contract with `CLIAdapter` and
  `OptimizeOrchestration` package roles. This is an actionable planning
  contract, not an automatic materializer.

## E5 — contract lens retrospective boundary

**Source:** #57
**Target:** boundary assessment
**Status:** converged with semantic gaps
**Round count:** 2 including bootstrap retry
**contract_jaccard:** 0.74
**reward:** 1

Purpose:

- retrospectively assess whether the implemented contract-lens boundary is
  coherent from graph-only evidence;
- check whether contract extraction, contract MCP tools, and graph diff
  validation are visible enough to Architect;
- use this as the fifth Stage 1 episode required by #58.

Bootstrap issue:

- The first Architect attempt failed because the prompt only gave localhost and
  a general "browser/MCP HTTP" instruction. The agent attempted nonexistent
  discovery routes such as `/api`, `/graph/summary`, and
  `/api/graph/summary`.
- Curator fixed the protocol: graph-only Architect now gets an explicit
  allowlist of `/api/layouts`, `/api/search`, `/api/graph`, and `/mcp`
  JSON-RPC calls.

Result:

- `episode-e5-contract-lens-retrospective.md` records the full run.
- Accepted assessment: `internal/contracts` is the extraction/materialization
  boundary, `internal/mcpserver` is the contract MCP exposure boundary,
  `internal/graph` remains substrate, and `internal/parser` feeds extraction.
- MCP tools present: `contracts_tag`, `contracts_list`, `contracts_diff`,
  `contracts_consumers`, `contracts_producers`, `contracts_field_history`, and
  `contracts_export`.
- Validation path works for identical graph self-diff:
  `graph_diff archmotif:actual -> archmotif:actual` and
  `contracts_diff archmotif:actual -> archmotif:actual` return zero deltas.
- Reader-confirmed gaps: observed contract set is too broad and DTO-heavy;
  browser graph stats report `contracts: 0` while MCP `contracts_list` returns
  `227`; dynamic MCP contract tagging and config-driven `internal/contracts`
  extraction are related but not the same lens.

## Template

```markdown
## E<N> — title

**Source:**
**Target:**
**Status:** queued | running | converged | failed
**Round count:**
**contract_jaccard:**
**reward:**
**Disagreements:**

### spec_A

### fb_B

### spec_B or final accepted spec

### Curator notes
```
