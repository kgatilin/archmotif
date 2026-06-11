# Stage 1 protocol

## Roles

### Architect

Tools: graph MCP and graph browser HTTP API only. No file reads, grep, GitHub
issue browsing, or arbitrary shell.

For local graph-server episodes, Architect may use `curl` only against the
Curator-provided localhost graph server and only for:

- `GET /api/layouts`
- `GET /api/search?q=<term>`
- `GET /api/graph?...`
- `POST /mcp` JSON-RPC `tools/list`
- `POST /mcp` JSON-RPC `tools/call`

Do not assume discovery routes such as `/api`, `/graph/summary`, or
`/api/graph/summary` exist unless the Curator lists them for that episode.

Input:

- feature/refactor request
- graph id, usually `archmotif`
- allowed MCP tools list

Output: `spec_A`, a structured proposal.

### Reader

Tools: full repository, shell, tests, and read-only graph queries.

Input:

- same feature/refactor request
- `spec_A`

Output: `fb_B`, structured feedback with missing, wrong, excess, and confirmed
items. Reader may also emit `spec_B` when the counterproposal is clearer than a
patch to `spec_A`.

### Curator

Stage 1 Curator is manual. The Curator reviews failed convergence episodes and
decides whether the graph needs:

- semantic enrichment: annotations, contract tags, concept nodes, issue links;
- structural enrichment: parser/extractor improvements, new edge kinds;
- proposal enrichment: new optimization rule that maps a detected anomaly to a
  refactor contract.

## Spec format

Architect and Reader use this JSON shape:

```json
{
  "episode": "E1",
  "target": "contract_set",
  "summary": "short proposal",
  "contracts": [
    {
      "name": "GraphServer",
      "kind": "module_boundary",
      "owned_by": "internal/...",
      "responsibilities": ["..."],
      "public_methods": ["..."],
      "consumers": ["cmd/archmotif"]
    }
  ],
  "graph_delta": {
    "nodes": [
      {"id": "concept:...", "kind": "concept", "name": "..."}
    ],
    "edges": [
      {"from": "...", "to": "...", "kind": "dependsOn"}
    ]
  },
  "verification": [
    "command or code check expected after implementation"
  ],
  "risks": [
    "known uncertainty from graph-only view"
  ]
}
```

`contracts` are the primary convergence target. `graph_delta` is diagnostic and
helps identify missing graph semantics.

## Scoring

Run at most `K = 2` Architect retry rounds after Reader feedback.

Primary score:

```text
contract_jaccard = |contracts_A ∩ contracts_B| / |contracts_A ∪ contracts_B|
```

An episode converges when:

- `contract_jaccard >= 0.70`;
- Reader reports no blocker in `wrong` or `missing`;
- all proposed ownership boundaries are implementable without circular imports;
- verification commands are concrete enough to run after implementation.

Record binary reward:

```text
reward = 1 if converged, else 0
```

Record the numeric score even when `reward = 0`.

## Disagreement taxonomy

Every Reader disagreement must use one of these categories:

- `missing_concept`: graph lacks a domain concept needed for the feature.
- `missing_structure`: graph lacks an edge/kind that exists in code.
- `wrong_boundary`: Architect grouped code that should remain separate.
- `wrong_dependency`: Architect inferred an impossible or reversed dependency.
- `excess_scope`: Architect proposed work outside the feature request.
- `stale_ticket`: GitHub issue text is out of date versus repo reality.
- `proposal_gap`: graph/anomaly detected a problem, but ArchMotif lacks a rule
  to turn it into a concrete refactor contract.

## Episode procedure

1. Curator selects one episode from `episodes.md`.
2. Refresh graph server for current `main`.
3. Curator gives Architect the exact graph server URL and allowed endpoint
   list.
4. Architect produces `spec_A` using graph tools only.
5. Reader inspects code and emits `fb_B`.
6. If not converged and rounds remain, Architect retries with `fb_B`.
7. Curator records final score and disagreement categories in `episodes.md`.
8. Curator creates follow-up issues for graph/proposal gaps that recur.
