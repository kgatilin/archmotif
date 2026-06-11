Child of #58.

## Problem

The Stage 1 self-convergence baseline found `cmd/archmotif` as the largest
structural anomaly in the ArchMotif graph:

- `graph --summary --pattern ./cmd/archmotif/... .` => `1574` nodes,
  `3363` edges.
- `anomalies --pattern ./cmd/archmotif/...` top result: modularity score
  `442.47`; package `cmd/archmotif` has `1317` members versus sibling
  packages.
- `cmd/archmotif/view.go` is `1469` lines and mixes CLI parsing, graph
  extraction, workspace writes, HTTP/MCP server setup, browser handlers,
  view-model construction, GraphML-to-typed conversion, static assets, and
  layout code.

## Goal

Make `archmotif view` a thin CLI entrypoint over a package-local graph server
implementation. Keep the user-visible command and the unified server model:
browser at `/`, streamable HTTP MCP at `/mcp`, both sharing one graph store.

## Scope

- Move browser graph server logic out of `cmd/archmotif`.
- Preserve `archmotif view` flags and output.
- Preserve the embedded static assets flow or move it with the new package.
- Keep the graph workspace write + MCP service integration as one code path.
- Keep tests for `/`, `/api/graph`, `/api/search`, and `/mcp` route mounting.

## Stage 1 experiment angle

Use this as an Architect/Reader episode:

- Architect sees only the graph and proposes the package boundary.
- Reader inspects code and validates whether the proposed boundary avoids
  import cycles and preserves the single-server model.

## Acceptance criteria

- [ ] `cmd/archmotif/view.go` becomes CLI orchestration only.
- [ ] Browser/server/view-model logic lives in an internal package with a narrow
      public API.
- [ ] Existing `archmotif view` smoke still works on ArchMotif itself.
- [ ] `go test ./cmd/archmotif ./internal/...` passes.
- [ ] Stage 1 episode log records Architect spec, Reader feedback, and
      convergence score.

