# Tooling status

**Date:** 2026-05-18
**Status:** no unresolved experiment tooling blocker

## Graph server under test

```text
archmotif view: http://127.0.0.1:61657/
mcp endpoint: http://127.0.0.1:61657/mcp
graph archmotif: 6097 nodes, 12779 edges (/tmp/archmotif-self-convergence/graphs/archmotif/actual.graphml)
```

The experiment used one Go graph server with two interfaces:

- browser HTTP UI and APIs at `/`, `/api/layouts`, `/api/graph`, and
  `/api/search`;
- streamable HTTP MCP at `/mcp`.

Both interfaces operate on the same GraphML workspace through the same
`internal/mcpserver.Service` boundary.

## Smoke checks

Browser/API checks used during the run:

```bash
curl -sSf http://127.0.0.1:61657/
curl -sSf http://127.0.0.1:61657/api/layouts
curl -sSf 'http://127.0.0.1:61657/api/graph?view=packages&layout=structure&limit=5'
curl -sSf 'http://127.0.0.1:61657/api/search?q=server'
```

MCP check:

```bash
curl -sSf -X POST http://127.0.0.1:61657/mcp \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  --data '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"graph_checkout","arguments":{"graph_id":"archmotif"}}}'
```

The MCP call returns `archmotif:actual` with `6097` nodes and `12779` edges from
`/tmp/archmotif-self-convergence/graphs/archmotif/actual.graphml`.

## Corrections made during the experiment

- Do not use `OPTIONS /mcp` as a mount smoke. It is not a valid MCP call for
  this server and can return 404 even when `/mcp` works.
- Use a real JSON-RPC `tools/call` POST for MCP smoke.
- Treat mcp-go notifications as implementation detail for #59. For the first
  browser live-watch slice, explicit `/api/events` SSE is smaller and avoids
  relying on MCP session state from the browser.
- Give graph-only Architects an explicit endpoint allowlist. A prompt that only
  says "browser/MCP HTTP" led one E5 attempt to probe nonexistent routes:
  `/api`, `/graph/summary`, and `/api/graph/summary`.
- The allowed graph-only HTTP surface for Stage 1 is `/api/layouts`,
  `/api/search`, `/api/graph`, and `/mcp` JSON-RPC `tools/list` /
  `tools/call`.
- Reader-discovered graph blind spots should feed back into substrate changes
  and a rerun. E2 did this by making optimize-loop run/convergence reports
  graph-visible. E4 did this by adding a command-package split proposal rule.

## Remaining product gaps

These are feature tickets, not experiment-tooling blockers:

- #59: browser live graph watch does not exist yet.
- #40: full multi-scale convergence reports are not complete yet, but a
  command-local run/batch/convergence report schema now exists and is graph
  visible.
- #66: optimize orchestration has not been extracted from `cmd/archmotif` yet,
  but the optimizer now emits a `command_package_split` planning contract for
  the command-package anomaly.
- Contract-lens results are not yet sharp enough on ArchMotif itself: MCP
  contract listing currently observes broad public DTOs, and browser graph
  stats do not show the same dynamically inferred contracts.
