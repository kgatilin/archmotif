# archmotif — contributor guide

archmotif is a **graph-agnostic architecture-metrics engine**: GraphML in, data
out. It never parses source as part of analysis and never refactors code. The
graph-metrics contract (`analyze`, `calculate`, `quotient --partition`, `policy`,
`diff`, `embed`) lives on branch `poc/graph-metrics-contract`; see
`docs/prd/archmotif-graph-metrics-library.md`.

## Build & test
```bash
make build           # -> bin/archmotif
make test            # go test -race ./...
make lint            # golangci-lint if present, else go vet
```

## Self-dogfooding — run when you change archmotif

archmotif must be able to measure itself. **Before committing a change to
archmotif, run the architecture check and read its output** — if your change
regresses the structure, fix it or update the baseline deliberately.

### `make arch-check` — non-LLM (always run this)
Builds archmotif's own package graph and runs the structural suite + macro shape
+ the import-flow ratchet. No API tokens.
```bash
make arch-check
```
What it emits and how to read it (you make the suggestions; the tool emits facts
— follow the `archmotif` skill: interpret → recommend, do NOT auto-refactor):
- **analyze** — `cycles` must be 0; `layering` should stay 1.0; `god-nodes` are
  flagged but many are intended (`cmd/archmotif` is the composition root,
  `internal/graph` is the shared model). Judge, don't auto-act.
- **quotient `--partition group`** — the macro graph must stay **acyclic=true**.
  A non-DAG macro graph = a new layering violation between package groups.
- **policy** — checks the current graph against `arch-policy.yaml`, the
  **import-flow ratchet** (the allowed cross-group dependency directions). A new
  cross-group import fails it. Modularity is degenerate on this hub graph — rely
  on the quotient + policy, not modularity.

If you intentionally add a legitimate new dependency direction, regenerate the
ratchet and commit it:
```bash
make arch-check                                   # writes .archmotif/self/baseline.policy.yaml
cp .archmotif/self/baseline.policy.yaml arch-policy.yaml
```
`group` = first path segment (or second under `internal/`); see
`archmotif pkg-graph`. Outputs land in `.archmotif/` (gitignored).

### `make arch-check-llm` — LLM/embeddings (run when relevant)
Embeds each package's semantic text with Vertex (`gemini-embedding-001`) and
reports the **emergent** clustering — where the code's meaning disagrees with its
declared package layout (boilerplate repetition, features split across layer
dirs, near-duplicate packages). Spends API tokens, so it is separate.
```bash
export ARCHMOTIF_GCP_PROJECT=<vertex-project>     # ARCH_REGION defaults to europe-west4
make arch-check-llm
```
Embedder command: `archmotif embed`. Model/region are configurable via
`ARCHMOTIF_EMBED_MODEL` / `ARCH_REGION`.

### Reviewing a branch? Focus on the delta
To analyse only what a branch adds (not the whole repo), diff two graphs by a
**stable** key (`qname`, not the position-based node id):
```bash
archmotif graph --format=graphml .                       > before.graphml   # main
archmotif graph --format=graphml /path/to/worktree       > after.graphml
archmotif diff before.graphml after.graphml --key qname --named-only --context 1 > focus.graphml
```
`diff` prints `+added / -removed`; **0 removed = a purely additive change**. The
focused GraphML marks nodes/edges `diff=added|context` so `analyze`/`quotient`
and any visualization centre on the delta.

## Acceptance
`acceptance.yaml` is the Signet contract for the CLI surface (`signet run
acceptance.yaml --yes`). Add a case when you add or change a command.

## Conventions
- The metrics path stays domain- and language-agnostic: no Go-specific or
  application-specific logic in `internal/contract`. It operates on GraphML only.
- `archai` produces graphs (parses source, attaches `group`/domain attributes);
  archmotif scores them. Never add a reverse dependency.
- `dogfood/` and `.archmotif/` are gitignored scratch space.
