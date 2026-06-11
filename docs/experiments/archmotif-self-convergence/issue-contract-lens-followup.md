Child of #58 and follow-up to #57.

## Problem

E5 of the self-convergence experiment found that the contract-lens boundary is
structurally coherent, but the current contract surface is not sharp enough on
ArchMotif itself:

- MCP `contracts_list` returns `227` contracts.
- `contracts_list visibility=public` returns the same `227`.
- `contracts_list kind=dto` returns the same `227`.
- Browser `/api/graph` package stats report `contracts: 0` for the same graph.
- External/public dependency types such as `io.Writer`, `time.Time`,
  `net/http.HandlerFunc`, and mcp-go types appear in the contract list.
- Config-driven `internal/contracts` extraction and dynamic MCP-side
  `TagContracts` are related but not clearly documented as the same or
  different lenses.

## Goal

Make the contract lens sharp and consistent enough to support review and
convergence episodes:

- define policy for external/public dependency types;
- align browser-visible contract stats with MCP contract results, or document
  why they intentionally differ;
- clarify the relationship between config-driven extraction and MCP dynamic
  contract tagging;
- ensure non-DTO contract kinds have fixture coverage or clear documented
  preconditions.

## Acceptance criteria

- [ ] Contract list output includes or filters foreign/external types according
      to an explicit policy.
- [ ] Browser `/api/graph` contract stats and labels are consistent with the
      contract lens used by MCP, or the UI/API names make the distinction clear.
- [ ] Tests cover external DTO leakage and the chosen policy.
- [ ] Tests cover at least one non-DTO contract kind, or docs explain why the
      current ArchMotif graph has none.
- [ ] Documentation explains config-driven `internal/contracts` extraction
      versus MCP-side dynamic `TagContracts`.
- [ ] E5 notes in
      `docs/experiments/archmotif-self-convergence/episode-e5-contract-lens-retrospective.md`
      are updated with the resolution.
