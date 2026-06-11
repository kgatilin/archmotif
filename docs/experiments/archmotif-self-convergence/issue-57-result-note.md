E5 result from #58 self-convergence: converged with semantic gaps.

Graph-only Architect confirmed the implemented boundary:

- `internal/contracts` is the extraction/materialization package.
- `internal/mcpserver` exposes contract operations as MCP tools.
- `internal/graph` remains the shared substrate.
- `internal/parser` feeds extraction/building.

MCP tools observed:

- `contracts_tag`
- `contracts_list`
- `contracts_diff`
- `contracts_consumers`
- `contracts_producers`
- `contracts_field_history`
- `contracts_export`

Validation evidence:

- `graph_list`: `archmotif:actual`, `6026` nodes, `12647` edges.
- `contracts_list`: `227` contracts.
- `contracts_diff archmotif:actual -> archmotif:actual`: zero delta.
- `graph_diff archmotif:actual -> archmotif:actual`: zero delta.

Reader-confirmed gaps:

- observed contracts on ArchMotif are all public DTO-style contracts;
- browser graph stats report `contracts: 0` while MCP `contracts_list` returns
  `227`;
- dynamic MCP contract tagging and config-driven `internal/contracts`
  extraction are related but not the same mechanism;
- external/public library types appear in the contract list and need policy or
  filtering if they are not intended API contracts.

Full record:
`docs/experiments/archmotif-self-convergence/episode-e5-contract-lens-retrospective.md`.
