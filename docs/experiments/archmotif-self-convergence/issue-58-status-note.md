Stage 1 is now complete with five real Architect/Reader episodes and one
substrate repair pass.

It was scoped to ArchMotif itself, not `deskd`, because `deskd` is Rust and the
current ArchMotif extractor is Go-only.

Repo artifacts added under
`docs/experiments/archmotif-self-convergence/`:

- `README.md` — objective, graph server command, baseline metrics, episode set,
  and conclusion.
- `protocol.md` — Architect/Reader/Curator roles, spec schema, scoring, and
  disagreement taxonomy.
- `episodes.md` — Stage 1 episode log.
- `episode-e1-live-watch.md` — #59 result.
- `episode-e2-convergence-reports.md` — #40 result.
- `episode-e3-pilot.md` — #65 result.
- `episode-e4-optimize-split.md` — #66 result.
- `episode-e5-contract-lens-retrospective.md` — #57 retrospective result.
- `results.md` — aggregate verdict.
- `tooling.md` — smoke evidence, tooling corrections, and substrate repair
  notes.

Episode results:

- E1 / #59 live graph watch: converged, `contract_jaccard=0.86`, `reward=1`.
- E2 / #40 convergence reports: converged after substrate repair,
  `contract_jaccard=0.78`, `reward=1`.
- E3 / #65 graph browser split: converged, `contract_jaccard=0.80`,
  `reward=1`.
- E4 / #66 optimize split: converged after substrate repair,
  `contract_jaccard=0.82`, `reward=1`.
- E5 / #57 contract lens retrospective: converged with semantic gaps,
  `contract_jaccard=0.74`, `reward=1`.

Conclusion: the two-agent setup works for bounded ArchMotif feature/refactor
specs when the Reader role is mandatory. The graph-only Architect found useful
boundaries in all episodes. Reader checks were still required for stale issue
text, protocol semantics, Go package granularity, and optimization run-time
semantics. When Reader found substrate gaps in E2 and E4, the correct loop was
to update the graph/tooling structure and rerun, not to leave the gaps as final
caveats.

No unresolved experiment tooling blocker remains. Browser/API and MCP smokes
passed against one `archmotif view` server with browser `/` and MCP `/mcp`
sharing the same `mcpserver.Service`. The invalid `OPTIONS /mcp` smoke from the
pilot was replaced with a real JSON-RPC `tools/call` POST. The failed first E5
bootstrap was fixed by adding the exact graph-only HTTP endpoint allowlist to
the protocol.

Metrics after the repair pass for `cmd/archmotif`:

- `graph --summary --pattern ./cmd/archmotif/... .` => `1574` nodes,
  `3363` edges before repair; `1643` nodes, `3526` edges after the
  target-contract pass.
- `cmd/archmotif` has `43` Go files and `9302` lines including tests.
- `anomalies --pattern ./cmd/archmotif/...` top result is modularity score
  `442.47`: package `cmd/archmotif` has `1317` members versus siblings.
- `optimize --mode=architecture --pattern ./cmd/archmotif/...` now found
  `42095` anomalies, `1` proposal, and `1` contract.

That keeps the `cmd` package as a good self-dogfood candidate: the graph sees
the problem and proposal generation can now emit a command-package split
contract, while the actual optimize orchestration extraction remains future
implementation work.
