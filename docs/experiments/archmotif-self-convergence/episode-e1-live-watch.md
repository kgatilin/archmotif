# E1 - live graph watch over unified server

**Date:** 2026-05-18
**Source:** #59
**Target:** contract set
**Status:** converged
**Round count:** 1
**contract_jaccard:** 0.86
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
archmotif view: http://127.0.0.1:53393/
mcp endpoint: http://127.0.0.1:53393/mcp
graph archmotif: 6026 nodes, 12647 edges (/tmp/archmotif-self-convergence/graphs/archmotif/actual.graphml)
```

Architect constraint: graph server only. No repository file reads, no grep, no
git.

## spec_A summary

Architect proposed the smallest live-watch slice as invalidation events rather
than client-side graph patching:

- keep one `archmotif view` process with browser `/` and MCP `/mcp`;
- keep `internal/mcpserver.Service` as the single mutation and workspace owner;
- publish a monotonic update event after successful mutation and persistence;
- add a small in-process `GraphUpdateHub`;
- add `GET /api/events?graph=archmotif&since=<seq>` as browser-facing SSE;
- add `revision` or `seq` to `/api/graph`;
- let the framework-free browser use `EventSource`, debounce updates, and call
  the existing `load()` path with the same query state.

Graph-only evidence used by Architect:

- `/` served one browser app with `/static/app.js`.
- `/static/app.js` already uses full-view fetch through `/api/graph`, replaces
  `current`, renders, and syncs URL state.
- `/api/graph` showed `6026` total nodes, `12647` total edges, and a default
  package view of `26` nodes and `50` edges.
- `/api/layouts` showed server/browser layout split.
- `/api/search?q=server` surfaced `browserServer`, `newBrowserServer`,
  `registerGraphServer`, `handleIndex`, `handleLayouts`, `handleGraph`, and
  `handleSearch`.
- The `internal/mcpserver` package view surfaced `Service`, `LoadGraph`,
  `SaveGraph`, `AddNode`, `AddEdge`, `UpdateWeight`, `Activate`,
  `MutationLogger`, and `MutationRecord`.
- `/api/search?q=watch` returned no existing watch-specific nodes.

## fb_B

Reader code inspection confirms the main boundary:

- `cmd/archmotif/view.go` mounts `/mcp` using mcp-go streamable HTTP with
  `WithStateLess(true)`.
- `internal/mcpserver.Service` already funnels write tools through one mutation
  path and logs mutation records after graph writes.
- Existing write tools include `graph_activate`, `graph_add_node`,
  `graph_add_edge`, and `graph_update_weight`.
- `MutationLogger` is append-only durable evidence; it is not a transport or
  live fanout mechanism.

Reader adjustments:

- Do not use MCP transport notifications as the first browser live-watch
  mechanism. mcp-go has notification/session support, but the current route is
  stateless and the browser needs a simple framework-free subscription.
- Add `/api/events` as browser API, not as MCP API.
- Publish only after mutation and save succeed.
- Keep the first browser behavior as full refetch plus debounce. Client-side
  incremental graph patches can wait.
- Use bounded event history and an explicit `resync` event when a reconnecting
  browser misses old sequence numbers.

## Accepted spec

First implementation should:

- add `GraphUpdateHub` or equivalent small in-process publisher;
- extend mutation results with a monotonic sequence per graph id;
- publish update events from `mcpserver.Service` after successful write/save;
- keep `MutationLogger` as durable/debug evidence and optionally include the
  sequence in the log line;
- add `browserServer.handleEvents` at `/api/events`;
- include `revision` or `seq` in `/api/graph`;
- update `/static/app.js` to open `EventSource` after first load, debounce
  `graph-update` events, and reuse the current `load()` state.

## Curator notes

Converged in one round. The graph-only Architect identified the correct shared
service invariant and a small browser-facing event slice. The main graph gap is
transport semantics: the graph can expose that MCP and browser share a server,
but it cannot prove whether streamable HTTP sessions are stateful enough for
browser notification fanout.

Disagreement categories:

- `stale_ticket`: #59 was written for Rust/axum and a separate HTTP MCP server.
- `missing_structure`: no existing live-watch/event nodes exist in the graph.
- `wrong_dependency`: avoided by keeping browser SSE separate from MCP protocol.
