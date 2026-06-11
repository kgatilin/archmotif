E1 result from #58 self-convergence: converged.

Current implementation note after `edc75db`: ArchMotif is Go, not Rust/axum, and
the local browser/MCP path is now `archmotif view --http ...`. That command
starts one graph server: browser at `/`, streamable HTTP MCP at `/mcp`, both
backed by the same GraphML workspace and `mcpserver.Service`.

Accepted next slice:

- keep #59 as the live-watch layer on top of the existing unified server, not
  as a new `archmotif mcp serve --http` implementation;
- add a small in-process graph update hub;
- publish a monotonic graph update event from `mcpserver.Service` only after a
  successful mutation/save;
- add `revision` or `seq` to `/api/graph`;
- expose browser-facing SSE at `/api/events?graph=<id>&since=<seq>`;
- update the framework-free browser to use `EventSource`, debounce update
  events, and refetch the existing `/api/graph` view with current query state.

Reader correction: keep `/api/events` separate from MCP protocol semantics.
mcp-go has notification/session support, but the current `/mcp` route is
stateless streamable HTTP, so explicit browser SSE is the smaller first slice.

Full record: `docs/experiments/archmotif-self-convergence/episode-e1-live-watch.md`.
