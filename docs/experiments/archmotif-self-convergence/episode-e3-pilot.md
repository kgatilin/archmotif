# E3 pilot — split browser graph server out of cmd/archmotif

**Date:** 2026-05-18
**Source:** #65
**Target:** contract set
**Status:** converged
**Round count:** 1
**contract_jaccard:** 0.80
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
archmotif view: http://127.0.0.1:49596/
mcp endpoint: http://127.0.0.1:49596/mcp
graph archmotif: 6026 nodes, 12647 edges (/tmp/archmotif-self-convergence/graphs/archmotif/actual.graphml)
```

Architect constraint: graph server only. No repository file reads, no grep, no
git.

## spec_A summary

Architect proposed four boundaries:

- `cmd/archmotif view`: CLI orchestration only; preserve flags, graph id
  defaulting, workspace materialization, listener setup, diagnostics, and user
  output.
- `internal/graphserver`: unified HTTP composition; one handler tree with
  browser `/`, browser APIs, static assets, and streamable HTTP MCP `/mcp`,
  sharing one `*mcpserver.Service`.
- `internal/graphbrowser`: browser UI/API and view-model construction for
  `/api/layouts`, `/api/graph`, `/api/search`, index, and static files.
- `internal/graphbrowser.Viewer`: projection layer for package overview,
  package detail, neighborhood, search, labels, filters, and layout metadata.

Architect also kept `internal/mcpserver` as the existing graph service/store and
MCP protocol boundary.

Graph-only evidence used by Architect:

- `/api/search?q=browser` surfaced `browserServer`, `newBrowserServer`,
  `registerGraphServer`, and handlers in `cmd/archmotif`.
- Browser neighborhood queries showed `browserServer` owns `register`,
  `viewer`, `handleIndex`, `handleLayouts`, `handleGraph`, and `handleSearch`.
- `graphViewer` queries showed browser projection and search concentrated in
  the command package.
- Package graph showed `cmd/archmotif` depending on `internal/mcpserver` and
  many internal packages.

## fb_B

Reader code inspection confirms the main boundary:

- `runView` currently handles CLI flags, graph extraction, workspace resolution,
  graph id defaulting, GraphML workspace write, service construction, browser
  construction, mux registration, listener setup, and output.
- `browserServer` and `graphViewer` are browser/server logic and should move out
  of `cmd/archmotif`.
- `internal/mcpserver.Service` is already the correct shared graph-store
  boundary.

Reader adjustments:

- Start with one new internal package, preferably `internal/graphserver`, and
  split a nested browser/viewer package only if the first extraction remains too
  large. Architect's `internal/graphbrowser` is directionally right but may be
  premature as a first cut.
- Keep graph extraction and GraphML workspace write in `cmd/archmotif` for the
  first refactor unless a second caller appears. The immediate issue is browser
  server/view projection, not source extraction.
- Static assets must move with the package that owns `go:embed`; Go embed paths
  are package-local.
- `OPTIONS /mcp` returning 404 is not a valid MCP smoke signal. A direct POST
  tool call to `/mcp` succeeded, so `/mcp` is mounted.

Reader smoke evidence:

```bash
curl -sSf http://127.0.0.1:49596/
curl -sSf http://127.0.0.1:49596/api/layouts
curl -sSf 'http://127.0.0.1:49596/api/graph?view=packages&layout=structure&limit=5'
curl -sSf -X POST http://127.0.0.1:49596/mcp \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  --data '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"graph_checkout","arguments":{"graph_id":"archmotif"}}}'
```

The MCP call returned `archmotif:actual` with `6026` nodes and `12647` edges
from `/tmp/archmotif-self-convergence/graphs/archmotif/actual.graphml`.

## Accepted spec

First implementation should:

- add `internal/graphserver`;
- move browser static assets, browser HTTP handlers, `browserServer`,
  `graphViewer`, view DTOs, layout code, and stored-graph conversion helpers
  into that package;
- expose a narrow API such as `graphserver.New(Config).Handler()` or
  `graphserver.Register(mux, Config)`;
- keep `cmd/archmotif/runView` responsible for flags, extraction, workspace
  write, service construction, listener setup, and stdout/stderr UX;
- preserve browser `/`, `/api/layouts`, `/api/graph`, `/api/search`, and MCP
  `/mcp` over the same `mcpserver.Service`.

## Curator notes

Converged in one round. The graph-only Architect found the right main boundary
and the right shared-service invariant. The only disagreements were granularity
(`internal/graphbrowser` can wait) and one weak verification probe (`OPTIONS
/mcp`).

Disagreement categories:

- `wrong_dependency`: none.
- `wrong_boundary`: minor; proposed split may be one package too granular for
  the first refactor.
- `missing_structure`: none found for this episode.
- `proposal_gap`: applies to the broader `cmd/archmotif` anomaly, not this
  specific browser split once the issue exists.

