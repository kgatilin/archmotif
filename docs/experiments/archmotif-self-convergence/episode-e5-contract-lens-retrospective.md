# E5 - contract lens retrospective boundary

**Date:** 2026-05-18
**Source:** #57
**Target:** boundary assessment
**Status:** converged with semantic gaps
**Round count:** 2 including bootstrap retry
**contract_jaccard:** 0.74
**reward:** 1

## Setup

Graph server:

```bash
go run ./cmd/archmotif view \
  --root /tmp/archmotif-self-convergence \
  --graph-id archmotif \
  --http 127.0.0.1:0 \
  .
```

Server output:

```text
archmotif view: http://127.0.0.1:54601/
mcp endpoint: http://127.0.0.1:54601/mcp
graph archmotif: 6026 nodes, 12647 edges (/tmp/archmotif-self-convergence/graphs/archmotif/actual.graphml)
```

Architect constraint: graph server only. No repository file reads, no grep, no
git, no GitHub.

## Bootstrap failure and fix

The first Architect attempt failed to produce a useful assessment. It had only a
generic localhost browser/MCP instruction and tried nonexistent discovery
routes:

- `/api`
- `/graph/summary`
- `/api/graph/summary`

The server itself was healthy; direct checks against `/api/layouts`,
`/api/search`, `/api/graph`, and `/mcp` succeeded. The experiment protocol was
therefore corrected: graph-only Architects now get an explicit endpoint
allowlist and may use `curl` only against those graph-server endpoints.

## spec_A summary

After the bootstrap fix, Architect found a coherent contract-lens boundary:

- `internal/contracts` is the extraction/materialization package.
- `internal/mcpserver` exposes contract operations as MCP tools.
- `internal/graph` remains the shared substrate.
- `internal/parser` feeds extraction/building.

Graph-only evidence used by Architect:

- `/api/graph?view=packages...` showed package dependencies:
  `internal/contracts -> internal/graph`, `internal/contracts ->
  internal/parser`, `internal/roles -> internal/contracts`,
  `cmd/archmotif -> internal/contracts`, and `internal/mcpserver ->
  internal/graph`.
- The `internal/contracts.Build` neighborhood showed orchestration through
  `ApplyExcludes`, `Mark`, `Producers`, `AllContracts`, and `Resolve`.
- The `internal/mcpserver.ContractsList` and `ContractsDiff` neighborhoods
  showed contract operations hanging off the MCP service boundary.
- MCP `tools/list` exposed `contracts_tag`, `contracts_list`,
  `contracts_diff`, `contracts_consumers`, `contracts_producers`,
  `contracts_field_history`, and `contracts_export`.
- MCP `graph_list` returned one graph, `archmotif:actual`, with `6026` nodes and
  `12647` edges.
- MCP `contracts_list` returned `227` contracts; `visibility=public` returned
  `227`; `kind=dto` returned `227`.
- MCP `graph_diff archmotif:actual -> archmotif:actual` returned zero node/edge
  delta.
- MCP `contracts_diff archmotif:actual -> archmotif:actual` returned zero
  added/removed/changed contracts.

## fb_B

Reader code inspection confirms the structural boundary:

- `internal/contracts.Build` loads the typed graph, reads `.archmotif.yaml`,
  resolves declared contracts, marks the graph, and computes producers.
- `cmd/archmotif contracts` is the CLI adapter over `internal/contracts`.
- `internal/mcpserver/tools_contracts.go` registers seven contract tools:
  one writer (`contracts_tag`) and six read/export/diff tools.
- `internal/mcpserver/contracts.go` implements a dynamic contract lens over
  GraphML graphs. It classifies public types as DTOs plus explicit
  `ConfigSchema`, `EventPublisher`, `CliFlag`, `EnvVar`, `HTTPHandler`, and
  incoming `route_registers` patterns when such nodes/edges exist.

Reader adjustments:

- There are two related contract mechanisms: config-driven
  `internal/contracts` extraction and dynamic MCP-side `TagContracts`. The
  graph-only assessment correctly grouped them as "contract lens", but the
  implementation does not make them one identical mechanism.
- The observed ArchMotif graph has `227` MCP contracts and they are all public
  DTOs. That is too broad to claim a sharp API/config/event contract surface
  for this repo.
- Browser `/api/graph` stats reported `contracts: 0` while MCP
  `contracts_list` returned `227`, so dynamically inferred MCP contracts are
  not surfaced consistently in the browser view.
- External/public library types showing up in the contract list are likely
  boundary leakage unless explicitly intended.

## Accepted assessment

The implemented boundary is coherent as a first contract-lens layer:

- extraction/config materialization lives in `internal/contracts`;
- graph-level MCP contract operations live in `internal/mcpserver`;
- graph storage and traversal stay in `internal/graph`;
- parser output feeds the extraction path.

The boundary is not semantically complete on ArchMotif itself:

- the live contract set is DTO-heavy;
- non-DTO contract kinds are not demonstrated by the current graph;
- dynamic MCP contract tags and browser graph stats disagree;
- external type leakage needs either filtering or explicit policy.

## Curator notes

This episode is the best falsification signal of Stage 1. The graph-only
Architect found the real implemented boundary and the real product gaps after
the protocol fix. The initial failed attempt was tooling/procedure, not a
conceptual graph failure.

Disagreement categories:

- `missing_concept`: contract sharpness/policy is not represented as a graph
  concept.
- `missing_structure`: MCP-inferred contracts are not reflected in browser
  graph stats.
- `excess_scope`: broad public DTO tagging includes candidates that may not be
  intended external contracts.
- `stale_ticket`: #57 acceptance talked about `deskd` and JDUI smoke, while the
  Stage 1 target is ArchMotif.
