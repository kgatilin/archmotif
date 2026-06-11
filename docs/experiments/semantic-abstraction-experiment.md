# Experiment: semantic abstraction discovery + target-policy validation

2026-06-07. Ran the full forward+backward loop from `architecture-as-matrices.md`
on archmotif's **own** codebase. Goal: from the code graph, discover how many
abstractions there should be, what they are, and a target dependency policy —
then validate that target against the real graph.

Scratch lives in `dogfood/abstraction-exp/` (gitignored): a Go embedder
(`embed/`, `google.golang.org/genai`, Vertex backend), a Go target-design
generator (`propose/`, Gemini via genai), and offline clustering artifacts.

## Pipeline

1. `archmotif graph .` → 7116-node typed graph; projected to the **internal
   package-dependency graph**: N=27 packages, E=55 edges, density 2.04.
2. Semantic text per package = path + go-doc synopsis + exported type names.
   Doc coverage 26/27 (only `package main` lacked a comment — now added).
3. **Embeddings** via Vertex `text-embedding-005` (768-d, a configurable
   GCP project, europe-west4), called through the **genai Go SDK** (not REST).
4. `archmotif spectral` → eigengap, SVD knee. Clustering (structural spectral /
   semantic / fused) over the package-level graph artifacts.
5. **Target design** from Gemini 2.5 Pro (genai Go) over the semantic clusters.
6. **Validation**: residual of the proposed target policy on the real graph.

## Cycle 1 — how many abstractions, and what

- Spectral: algebraic connectivity λ₂ = 0 (graph is **2 components**), eigengap
  suggests k≈8, **SVD knee ≈ 5**, modularity Q at k = **0.028**.
- **Structural clustering is degenerate** (Q≈0.03): it dumps ~21 of 27 packages
  into one blob + singletons. Reason: the dependency graph is a **star around
  `internal/graph`** (fan-in 22). Structure alone cannot reveal abstractions.
- **Semantic clustering (k=6) is coherent** at the same Q, and interpretable.
  This is the experiment's first finding: **where structural modularity is ~0,
  the semantic embedding axis recovers the abstractions the structure can't see.**

Gemini named the six and proposed a layered target:

| Abstraction | Packages |
|---|---|
| **Core Graph Model** | graph, parser, diagram, archai, graphmlx, pkg/archmotifimport |
| **Pipeline** | anomalies, metrics, propose, llm, verify, skeleton, catalog |
| **Declarative Analysis** | contracts, coupling, patterns, roles |
| **Optimizer / Contracts** | mcpserver, memopt, shape, targetcontract |
| **Test scaffolding** | *…test helpers |
| **Research** | wlkernel |

Target policy: everything → Core; Core → nothing; peers (Pipeline / Analysis /
Optimizer) do not depend on each other; cmd wires all. Flagged misplacements:
`verify` (Stage 8 → Pipeline, not Analysis), `skeleton` (production codegen, not
test), `cmd/archmotif` (standalone entrypoint, not a Pipeline library).

## Cycle 2 — validate the target against the graph

Scored the proposed grouping two ways:

- **Modularity Q = 0.0136** — *lower* than structural/semantic; 78% of edges
  cross an abstraction boundary.
- **Residual against the target policy = 3 violations / 55 edges**, and
  **Core → non-core = 0** (the "Core depends on nothing" rule already holds).

The three violations are exactly peer→peer couplings — the actionable refactor list:

```
pipeline:catalog        -> analysis:patterns
optimizer:mcpserver     -> pipeline:metrics
optimizer:targetcontract-> pipeline:propose
```

## Conclusions

1. **Modularity is the wrong metric for a layered / hub architecture.** It
   penalizes a shared core, so a clean "everyone depends on the graph model"
   design scores *low* (0.0136). The right validation is the **residual against
   a declared policy** — which shows the target is excellent: 52/55 edges
   conform, Core is a clean sink, 3 peer-coupling edges to fix. This is exactly
   the descriptive-metric-vs-normative-policy distinction from the design doc.
2. **Eigengap over-counts on a star** (k≈8 reflects the hub, not real modules).
   SVD knee (≈5) and semantic clustering (6) agreed better and were usable.
3. The **semantic axis is load-bearing**, not decorative: it produced the only
   interpretable grouping, and the LLM turned it into a layered policy that the
   graph then validated at 95%.
4. End-to-end the loop works: graph → Go/Vertex embeddings → spectral → semantic
   clusters → LLM target design → residual validation, all on archmotif itself,
   yielding a concrete 3-item refactor backlog and a target dependency policy.

## Update 2026-06-07 — embedding model migration + tool/agent boundary

Two design corrections after the first run:

1. **Embedding model: `text-embedding-005` → `gemini-embedding-001`.**
   text-embedding-005 is a *legacy* Vertex model (Google flags it for deprecation
   "in the coming months"); `gemini-embedding-001` is the GA, recommended model
   (top MTEB, 3072-d default, MRL-truncatable). `gemini-embedding-2` exists but is
   the multimodal frontier (text/image/video/audio, embedding space incompatible
   with -001) — overkill for pure text. The embedder now defaults to
   `gemini-embedding-001` at full 3072-d with `task_type=CLUSTERING`, and **the
   model is config, not hardcoded**: `ARCHMOTIF_EMBED_MODEL` env (also
   `ARCHMOTIF_GCP_PROJECT` / `ARCHMOTIF_VERTEX_LOCATION` / `ARCHMOTIF_EMBED_TASK`),
   overridable by the `-model` flag.

   Re-ran on the same 27-package graph. Spectral structure is unchanged (it's a
   graph property): 2 components, λ₂≈0, eigengap k≈8. Semantic k=6 stayed
   interpretable — **Pipeline** (anomalies, catalog, llm, propose, verify) and
   **Research** (wlkernel) reproduced cleanly, and gemini-001 pulls everything
   graph-touching into a larger 12-package **Core+Analysis** blob (graph, parser,
   diagram, archai, contracts, coupling, patterns, roles, metrics, mcpserver,
   targetcontract, archmotifimport). Modularity stays negative (~−0.02),
   confirming the hub-graph thesis.

2. **`propose` is an agent task, not a tool feature.** The Gemini-2.5-Pro prose
   step (`dogfood/.../propose/`, Vertex call #2) is **dropped** — naming the
   abstractions, declaring the target dependency policy, flagging mis-placed
   packages, and producing the refactor backlog are done by the *agent* reading
   the tool's output, not baked into archmotif. Vertex is therefore used for
   **embeddings only** (`gemini-embedding-001`).

   Revised graduation target: archmotif emits a machine-readable
   `archmotif analyze --json` (packages, edges, embedding clusters, quotient/
   condensation layers, and — if a policy is supplied — the residual). The agent
   consumes that JSON and authors the architecture judgement. No `propose-arch`
   LLM subcommand.

## Follow-ups

- Graduate the Go embedder from `dogfood/` into `archmotif embed`
  (`gemini-embedding-001`; genai enters the main `go.mod`).
- Add `archmotif analyze --json` emitting graph + embedding clusters + quotient
  layering + residual; agent does the interpretation/propose step.
- Apply the three peer-coupling fixes; re-run to confirm residual → 0.
- Replace modularity with a **layering/acyclicity score** as the structural
  fitness for hub-shaped graphs.
