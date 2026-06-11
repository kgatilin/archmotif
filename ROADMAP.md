# archmotif — Roadmap

This is a **result-oriented** roadmap. Each stage describes *what we
want to have at the end* and *how we verify it's there*. It does
**not** prescribe how to achieve the result — that's the implementer's
problem to solve, including making research-style decisions during
implementation without coming back to ask.

Open research questions are listed inline per stage and centrally in
[`docs/research-questions.md`](./docs/research-questions.md). When
encountered, **decide and proceed**; record the decision in
`docs/decisions/` (ADR-style). Don't escalate.

Each stage = one GitHub issue. Each stage is independently shippable.

---

## Stage 0 — Project foundations

**Result:**

- Go module initialised
- `cmd/archmotif/` CLI scaffold (cobra or stdlib `flag`, implementer's
  call)
- `archmotif --version` and `archmotif --help` work
- CI: build + test on push (GitHub Actions)
- `make build`, `make test`, `make lint` work
- `README` + this `ROADMAP` checked in

**Verify:**

- `go build ./...` succeeds
- `go test ./...` passes (even if just placeholder tests)
- CI green on first push
- `archmotif --version` prints something sensible

**Open questions to resolve in-flight:**

- Cobra vs stdlib `flag` — pick one and move on
- Linter choice (golangci-lint config) — pick a sane default

---

## Stage 1 — Build the typed graph (level 3.5)

**Result:**

- Given a Go package or module, `archmotif graph <path>` emits the
  typed graph as JSON (and optionally GraphML for visualization)
- Node types covered: `Package`, `File`, `Type` (struct/interface),
  `Function`, `Method`, `Field`, `Loop`, `Branch`, `Goroutine`,
  `Defer`, `ChannelOp`, `SyncPrim`
- Edge types covered: `Contains`, `Implements`, `Embeds`, `Calls`,
  `CallsFrom` (control-primitive → callee), `DependsOn`, `Returns`,
  `Reads`, `Writes` (latter two only for fields, optional in v1)
- Each node has a stable ID (`<file>:<line>:<col>:<kind>` or similar)
- Graph is queryable in-memory (basic API: nodes by type, neighbours,
  subgraph extraction)

**Verify:**

- Synthetic Go file (one per node type) → expected nodes + edges in
  output (table-driven test)
- Run on `archmotif` itself → graph builds, total nodes is
  reasonable (tens to low hundreds), no crashes
- Run on `kgatilin/archlint` → graph builds, total nodes is
  reasonable (hundreds to low thousands)
- Pretty-print sample shows recognisable structure (a struct, its
  methods, who calls them)

**Open questions to resolve in-flight:**

- Which control-flow primitives are nodes vs edge annotations?
  Default: every primitive listed above is a node, with
  `Contains` edges nesting them. Revisit if graph blows up.
- Graph library: `gonum/graph`, custom, or wrap something? Pick what
  makes the metric stage easier (lookahead to Stage 3).
- Inter-package vs intra-package — in v1, do single-module only,
  follow imports lazily.

---

## Stage 2 — Contract nodes

**Result:**

- User can declare contracts via `.archmotif.yaml`:
  ```yaml
  contracts:
    - interface: pkg/store.UserStore
    - type: pkg/api.Request
  ```
- Optional: `// archmotif:contract` comment annotation
- Graph marks declared contract nodes with a `IsContract: true`
  attribute
- For each contract field, the graph records the set of code locations
  that produce values for that field (basic structural connection —
  who returns this type, who assigns this field). Full data-flow
  tracing is out of scope for v1.
- CLI: `archmotif contracts <path>` lists contracts and their
  materialisations

**Verify:**

- Test fixture with a marked interface and two implementations →
  output shows the contract + both impls + the call sites that
  produce contract-typed values
- Tampering test: rename a contract method, verify subsequent stages
  flag the change as a contract violation (basis for stage 8)

**Open questions to resolve in-flight:**

- Comment annotation syntax — start with config-only; add comments
  later if needed
- Should contracts inherit (interface embedding) — yes, mark
  transitively
- Field-origin tracing depth — start one hop (direct producer);
  extend later

---

## Stage 3 — Metrics infrastructure

**Result:**

- A `Metric` interface in `internal/metrics/` such that adding a new
  metric = one new Go file implementing the interface; auto-registered
- Built-in metrics shipped:
  1. **Motif redundancy** — count of repeated isomorphic subgraphs
     of size 3–5 without an extracted abstraction
  2. **Local symmetry** — for each node, score how many nearby nodes
     play an interchangeable role
  3. **Modularity** — Newman modularity over package boundaries
  4. **Spectral gap** — algebraic connectivity (second-smallest
     eigenvalue of the Laplacian)
  5. **Cycle rank** — count + location of cycles in the dependency
     subgraph
- CLI: `archmotif metrics <path>` outputs all metric scores; `--metric
  <name>` runs one
- Output: structured (JSON), one record per (metric, scope) where
  scope is node / edge / region / whole-graph as appropriate

**Verify:**

- Hand-crafted test graphs (in `testdata/graphs/`) with known values
  for each metric → metric returns the known value
- Run on `archmotif` itself → all metrics produce numbers without
  crashing
- Adding a stub metric = one file, one passing test, registers
  automatically

**Open questions to resolve in-flight:**

- Pure-Go vs spill-to-Python for spectral methods — try `gonum/mat`
  first; if it can't handle the size, dump graph as JSON and shell
  out to a Python helper. Document in ADR.
- Motif counting algorithm — exact subgraph isomorphism (small
  sizes) vs gSpan-style approximation. Start exact for sizes 3–5.

---

## Stage 4 — Anomaly detection

**Result:**

- For each metric, compute its distribution across the graph and flag
  *anomalous* regions (z-score above threshold, or top-N quantile —
  implementer chooses; document)
- CLI: `archmotif anomalies <path>` outputs ranked list:
  `(metric, region, score, why-anomalous)`
- Anomalies link to the underlying graph nodes / edges / files

**Verify:**

- Synthetic graph with planted anomaly (e.g. an obvious motif
  repeated 10 times) → detected with high score for the right metric
- On `archmotif` itself → top anomalies are inspectable and
  human-meaningful (qualitative check)

**Open questions to resolve in-flight:**

- Per-metric threshold tuning vs single global score — start
  per-metric, fixed default thresholds
- Region definition — connected subgraph of size ≤ N for some N

---

## Stage 5 — Local transformation proposals

**Result:**

- Per-anomaly transformation rules (one rule per metric type at minimum):
  - Repeated motif → propose `extract-interface` or
    `extract-function`
  - Cycle → propose `dependency-inversion`
  - Bottleneck through mutable state → propose `pass-as-arg`
- CLI: `archmotif propose <path>` outputs textual proposals for top
  anomalies
- Each proposal includes: which anomaly triggered it, target
  subgraph shape, list of affected files / nodes, rough description
  of the rewrite

**Verify:**

- Planted anomaly → expected proposal class (table-driven)
- On `archmotif` itself → at least one sensible proposal that a
  human reviewer would consider valid

**Open questions to resolve in-flight:**

- How many transformation rules to ship in v1? Just one
  (motif compression / extract-interface) is enough. More can come
  later.
- Conflict resolution when multiple proposals apply to overlapping
  regions — pick highest-score, defer the rest

---

## Stage 6 — Structural skeleton rendering

**Result:**

- Given a proposal, render a Go-ish skeleton with placeholder names
  showing the target shape (see [`docs/concepts.md`](./docs/concepts.md)
  §6 for format)
- Skeleton includes: target subgraph, role labels, sample existing
  instances (3–5)
- CLI: `archmotif skeleton <proposal-id>` outputs the skeleton text

**Verify:**

- Hand-checked skeletons for each transformation rule's output are
  reviewable: a human can read the skeleton and know what code change
  is being proposed
- Skeleton round-trips: the structural information needed by Stage 8
  (verification) is recoverable from the skeleton

**Open questions to resolve in-flight:**

- Format — annotated Go syntax (`<Placeholder>`) vs structured YAML
  vs both. Default: annotated Go syntax for the LLM; YAML companion
  file for the verifier (Stage 8) to consume
- How much surrounding context to include — just the changed shape,
  or also untouched neighbours

---

## Stage 7 — LLM materialization

**Result:**

- `archmotif refactor <proposal-id>` calls an LLM with: the proposal,
  the skeleton, the original code region, and sample instances
- Output: a git branch with the refactor applied
- Prompt is parameterised; the LLM call is wrapped behind an interface
  so we can swap providers (Anthropic, OpenAI, local) without changing
  the orchestrator

**Verify:**

- End-to-end on `archmotif` itself: produce a branch with one
  refactor; manual inspection passes
- End-to-end on `kgatilin/archlint`: same
- The branch builds (`go build ./...`) and tests pass

**Open questions to resolve in-flight:**

- LLM provider for v1 — pick one (probably Anthropic via Claude API);
  document
- Prompt template — version it under `internal/prompts/`; iterate
- What to do when LLM produces unusable output — fail loud, don't
  retry blindly; surface diff for human

---

## Stage 8 — Verification linter

**Result:**

- Given (target subgraph from Stage 6, new code from Stage 7) →
  build graph from new code, verify it contains a subgraph isomorphic
  to the target with the same role mapping
- CLI: `archmotif verify <proposal-id>` exits 0 on match, non-zero
  with diff on mismatch
- Also runs as part of `archmotif refactor` after the LLM call

**Verify:**

- Hand-crafted target + matching code → passes
- Hand-crafted target + mismatched code (wrong shape) → fails with
  diagnostic
- Hand-crafted target + matching shape but contract-broken (e.g.
  contract method renamed) → fails

**Open questions to resolve in-flight:**

- Strict subgraph isomorphism vs structural similarity — start
  strict; loosen if too brittle
- How to report mismatches — show graph diff (added / removed /
  retyped nodes)

---

## Stage 9 — End-to-end refactor demo

**Result:**

- `archmotif refactor <pkg>` orchestrates Stages 1–8: build graph,
  compute metrics, detect top anomaly, propose transformation,
  render skeleton, call LLM, verify result, output branch
- Single-command demo on `archmotif` itself produces a usable branch

**Verify:**

- Demoable end-to-end: run on `archmotif`, produce branch, branch
  builds, branch passes tests, the refactor is recognisable as the
  proposed change
- Same on `kgatilin/archlint` (or a chosen subset)

**Open questions to resolve in-flight:**

- What to do when no anomalies above threshold are found — exit
  gracefully with "nothing to propose"

---

## Stage 10 — Catalog + drift (later)

**Result:**

- Persist named patterns from prior runs in a catalog file
  (`.archmotif/catalog.yaml`)
- Track motif counts and metric values across commits
- CLI: `archmotif drift` shows changes between two refs

**Verify:**

- Run between two commits of `archmotif` itself → diff is meaningful

**Open questions:** defer until Stage 9 is solid.

---

## Build order rule

**Each stage independently shippable.**

- After Stage 1: archmotif emits useful graphs. Visualise them, sanity-
  check, ship.
- After Stage 3: useful metric outputs even without anomaly detection.
- After Stage 4: useful anomaly reporter even without proposals.
- After Stage 6: useful skeletons even without LLM in the loop (a
  human can read and apply).
- After Stage 8: full closed loop.

Don't gate early stages on later ones being designed.

---

## Test corpus

- `archmotif` itself (recursive)
- `github.com/kgatilin/archlint` (small real-world Go project)
- Larger open-source Go projects (post Stage 9)

---

## Notes for implementer

- This roadmap is **what**, not **how**. Implement, document
  decisions in `docs/decisions/NNN-title.md` (ADR style), open issue
  comments referencing decisions when relevant.
- When you hit an open question, **decide and proceed**. Don't ask
  the user. Document the decision.
- Each stage closes its issue with: brief summary of what was built,
  link to the ADRs created during the stage, link to the demo /
  test output that proves the result.
- Commit early and often. PRs are optional for an exploratory project
  but encouraged for stages 7+ where reviewability matters.
