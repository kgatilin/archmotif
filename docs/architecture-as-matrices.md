# Architecture validation as matrix algebra

A model for treating architectural rules as **matrices over the code graph**,
and architectural quality as **numbers you can optimize**. The **forward (gate)
layer largely exists** as of 2026-06-06 (see §0); the construction, semantic, and
history half is the roadmap. Companion to `concepts.md` (the graph),
`030-coupling-metrics.md` (role-pair matrix + forbidden edges),
`027-role-metadata.md` (roles), `034-archai-bridge.md` (graph source),
`036-graph-embeddings-research.md` (the semantic axis), and `target-contract.md`
(expected/forbidden edges).

The whole model rests on two lines that must stay bright:

- **descriptive vs normative** — what the code *is* (observed) vs what the owner
  *declared allowed*. Never let the descriptive masquerade as the normative.
- **gate vs construction** — the deterministic CI check vs the heuristic,
  LLM-assisted process of *authoring* the rules. The gate must be dumb and
  reproducible; all the cleverness lives in construction.

---

## 0. Implementation status (2026-06-07)

The **forward gate** was built on 2026-06-06 (`4b6706f`, "matrix-based validator
framework") plus the spectral programme (#75 spectral, #76 Leiden communities,
#77 quotient + curvature). What exists vs what this doc still proposes:

**Exists** (`internal/metrics/matrix_*.go`):

- `MatrixValidator{Encoder, Operation, Interpreter}` — the L1/L2/L3 pipeline,
  deterministic encoders, `EdgeKinds` filter = the typed-slice axis (§1).
- `HadamardOp A⊙R` (§2 residual), `LayerCollapseOp Lᵀ·A·L` (§1 quotient),
  `RowColSumOp` fan-in/out (§4 node rules), `PowerDiagOp` cycles (§6),
  `TransposeDiffOp` asymmetry.
- `layer_mask` validator — forbidden role-pair mask (`A⊙V`), default
  Clean-Architecture table; `instability_matrix` (Martin I); `cycle_matrix`.
- spectral / communities / quotient subcommands — the §6 generative *inputs*.
- Convention note: the existing mask is a **forbidden** mask (deny-list), the
  complement of this doc's allow-mask — equivalent under ¬.

**Missing** (this doc's net-new):

- **family / containment** primitive (§3) — `layer_mask` is flat role pairs; the
  "own subtree OK, sibling forbidden" rule (e.g. a plugin family) is absent.
- **config-driven specs** — `SpecLoader` is a reserved hook; rules are hardcoded
  defaults. The `roles / allow / families` config (§3) is not wired.
- **ratchet / Δc / baseline** (§2) — validators emit absolute records only.
- slices **`C` (co-change)** and **`S` (semantic)**, history-survival fitness
  (§5), the embedding overlay (§7).
- the **construction / backward** half (§5 propose→score→triage, MDL fitness) and
  the **generative proposal** wiring (§6 spectral+communities → candidate `P`).

---

## 1. The graph is a stack of matrices (a tensor)

The typed graph (`concepts.md` §1) is not one matrix. Each **edge kind** is a
slice: `A_imports`, `A_calls`, `A_implements`, `A_references`, … Stack them and
you have a 3rd-order **adjacency tensor** `T[k][i][j]` = "is there an edge of
kind k from i to j". Slicing by k gives a typed matrix.

A matrix is a **projection** of the graph along three independent axes:

1. **edge kind** — which relation (`imports` vs `implements` vs `calls`). Already
   exists as the `kinds map[EdgeKind]bool` filter in `internal/metrics/gonum.go`.
2. **node selection** — diagonal selector matrices `D_kind` ("only interfaces",
   "only adapters"). `D_a · M · D_b` cuts a rectangular block.
3. **partition (collapse)** — `πᵀ M π` aggregates leaf packages into domains
   (the quotient graph). Lets you author rules coarse and check them fine.

Because all slices share one node→index ordering (sorted stable IDs,
`005-node-id-format.md`), they are **composable**: `A_calls · A_implements` =
"calls something that implements X" — a derived relation present in no single
slice. This is what makes cross-type rules (e.g. "concrete bypasses interface")
expressible without leaving linear algebra.

### The four slices we care about

| Slice | Source | Kind | Role |
|-------|--------|------|------|
| `A` (imports/calls/…) | code (AST / `go list`) | structural | **descriptive** — what depends on what |
| `C` (co-change) | git history | evolutionary | **descriptive** — what changes together |
| `S` (semantic similarity) | node embeddings | meaning | **descriptive** — what is *about* the same thing |
| `P` (policy) | declared by owner | normative | **the rule** — what is *allowed* |

`A`, `C`, `S` are observed. `P` is declared. Validation compares them.

---

## 2. Forward: validation as a matrix operation (the gate)

The policy `P` lives in the same index space as the data. `P[i][j]` ∈
{allowed, forbidden, don't-care}. Validation is **element-wise**, not matrix
multiplication:

```
residual  R = A ⊙ ¬P_allowed      # present edges that are not allowed
coefficient c = ‖R‖               # count (or weighted sum) of violations
```

Standard matrix multiplication `A × V` is the wrong operation — it composes
relations / counts paths, and `A × V = A` is solved trivially by the identity
and carries no architectural meaning. The operation that means "is the code
clean" is the Hadamard mask `A ⊙ ¬P` (equivalently, the two-sided projection
`A − V·A·Vᵀ` when `P` is a block-structured projector; equivalently the existing
`coupling.forbidden` membership test, lifted to matrix form).

### The signal is a delta, not an absolute

On a real codebase `c = 0` is neither achievable nor required. Freeze the
current state as baseline and watch the **ratchet**:

```
Δc = c_after − c_before        # for a code change
Δc > 0  → regression → red signal
Δc ≤ 0  → fine (or improving)
```

This is how linters adopt onto legacy code. Existing documented exceptions are
the baseline; only *new* violations fire. `archmotif` emits `c` + the list of
offending edges, stateless; CI/agent owns the before/after diff against a
committed baseline.

### The gate must be deterministic

`A` and `P` are exact and reproducible. The gate runs over them and emits a
trustworthy number — nothing learned, nothing drifting, nothing LLM inside it.
The agent consuming the signal (analyse → fix → escalate to human) is a separate
layer and never reaches into the gate. Detection and remediation are different
concerns.

---

## 3. The minimal policy matrix: allowed edges

Start with one matrix: allowed import edges, authored at **domain/layer**
granularity (not leaf packages). On a measured mid-size Go service: N=215
packages, E=603 internal
edges, 1.3% density — a 215×215 matrix is 98.7% empty, so the leaf level stays
sparse (iterate E edges, O(E)); the *authoring* unit is ~20–44 domains, a
`K×K` matrix small enough to write by hand.

### Config → matrix

A laconic config (default-deny; only the allowed cells listed) compiles to a
`K×K` group matrix plus one structural primitive. From a measured
architecture:

```yaml
module: example.com/org/service
roles:                       # path → role, first match wins
  app:      internal/app           # composition root / wiring
  plugins:  internal/plugins/**    # a family
  internal: internal/**            # a family
  cmd:      cmd/**
  top:      "*"                    # everything else on top = public interfaces
allow:                       # default deny
  "*":  [top]                # everyone may import interfaces
  app:  ["*"]                # wiring may import anything
  cmd:  [app, internal, top]
families: [internal, plugins]  # within a family: own subtree OK, sibling FORBIDDEN
```

Twelve lines describe the whole architecture. It compiles two-level:

- **Level A — coarse `K×K`:** `P[i][j]=1` iff role i may import role j. The
  shape is telling: the `top` column is all-ones (everyone imports interfaces),
  the `app` row is all-ones (wiring imports everything), a couple of `cmd`
  cells. A clean architecture here = a matrix concentrated in one column.
- **Level B — family block (containment):** the `families` line means each
  member may import its own ancestor/descendant chain (prefix containment) but
  not a sibling subtree. Over the family members this is a **block-diagonal
  mask** — generated by one config line, never enumerated.

### Check engine (lift)

For each actual leaf edge `u → v`:

- same family → allowed iff `v` is on `u`'s prefix path; else violation.
- otherwise → allowed iff `P[role(u)][role(v)] = 1`.

`residual` = edges where allowed is false. The `families` primitive is the one
thing a flat `K×K` matrix cannot express — and the empirical reason it is
needed: in the measured codebase the `(plugins,plugins)` cell has 136 edges, but almost all are
*intra-plugin* (parent → own child), legitimate; the rule "plugins are mutually
exclusive" means *different* plugins, which is the off-diagonal block.

This minimal matrix reproduces what `go-arch-lint` already does (allow-lists).
The novelty is the **form**: `deny` is a single cell instead of re-enumerating
everything else; the check yields a **coefficient**, not just pass/fail; and the
matrix **composes** with the other slices below. First step is therefore low
risk: port the existing allowed-edges rule into matrix+coefficient form, get the
ratchet signal for free, build the rest on top.

---

## 4. Node/attribute rules are also matrices (just heterogeneous)

"An interface has ≤3 implementations", "an adapter must reference a real type" —
these are *also* matrix expressions, over different typed slices:

- ≤3 implementations: in-degree on the `implements` slice = `d = Implᵀ · 1`;
  rule is `d[j] ≤ 3`. (matrix·vector → degree vector → threshold)
- adapter references a real type: `D_adapter · References · D_type`, row sums ≥ 1.

The real distinction is **not** "matrix-able vs not" — everything on the graph
is matrix-able (this is the thesis of GraphBLAS: graph algorithms as linear
algebra over semirings). The distinction is **homogeneity**:

- **dependency-flow rules** share *one* matrix `A` and *one* policy `P` of the
  same shape → they fold into a single residual and a single optimizable scalar.
- **node/attribute rules** are each their own operator over a different typed
  slice (+ selectors), producing a per-node count vector + a threshold → each is
  its **own** metric, not a contribution to the global residual.

The only genuinely non-linear step is the final comparison (`≤3`, `≥1`) — a
predicate on a matrix-derived number, trivial. So: dependency flow → one global
residual; everything else → a **library of typed operators**, each emitting a
coefficient. The true boundary is "in the graph vs not in the graph", not
"matrix vs not-matrix".

---

## 5. Backward: constructing the policy (the hard part)

Authoring a good `P` for a real codebase is the actually-hard problem. It is a
**search over the space of policy matrices**, human-in-the-loop:

```
   propose  ──▶  score  ──▶  triage
  (generator)  (gate, det.)  (human/LLM)
      ▲                          │
      └────────── refine ◀───────┘
```

- **propose** — a candidate `P` from graph clustering (communities / Leiden),
  co-change clustering, semantic clustering (§7), or an LLM reading the code +
  stated intent. For small `K` the LLM/human writes `P` directly; ML matters for
  *discovering the partition* `π` on a large flat cluster (e.g. the 99-package
  `internal/plugins`), not for the mask.
- **score** — the deterministic gate computes residual on HEAD + parsimony
  `‖P‖` + the offending-edge list.
- **triage** — human/LLM decides each offending edge: intentional policy change
  (accept → `P` loosens) or slip (forbid → stays). Converges to a tight `P` +
  short explicit exception list.

### Fitness function

A policy is good to the extent it **compresses the real graph** — an MDL /
description-length criterion:

```
fitness(P) = rule complexity (allowed cells)  +  exceptions (residual on HEAD)
```

Both degenerate ends are bad: "allow everything" → residual 0 but says nothing;
"forbid everything" → tiny rules but huge residual. The good `P` sits between:
tight, yet most of the real graph falls inside few allowed regions with a short
exception tail.

### History as the fitness signal — read it as survival, not acceptance

History is a stream of graph deltas. The owner's axiom is **"accepted = OK"** —
there is no external oracle of good architecture; the repo's accepted state *is*
the ground truth, and the metric is downstream of the owner's judgment (a
**consistency mirror**, not a judge: it asks "this deviates from how you've
consistently done it — intentional?", and the owner's answer defines the new
truth).

But "accepted" applies to **removals** too. If an edge was added (OK then) and
later removed (OK then), both are accepted — so the only consistent reading is:
the policy is the owner's **current** revealed preference, and history is a
trajectory toward it. Therefore:

- **baseline = HEAD** (the fully-accepted current state).
- **history = confidence weighting**: edges/separations held stable across many
  commits are load-bearing intent; recently-churned ones may still be in flux.
- **negative examples** = edges the owner *added then removed* (especially in
  `refactor`/`decouple` commits) — the closest thing to a "don't do this" label,
  obtained for free. Not "bad commits" — separations the owner moved away from.

Fitness then becomes a **backtest**: replay structural commits; a good `P`
permits the edges history *kept* (few false alarms) and would have caught the
edges history *reverted* (true catches), minus complexity. History ranks and
surfaces; the human still triages contested edges. Survival is a proxy, not
truth — never let the backtest become an auto-oracle either.

---

## 6. The generative inverse: code → abstractions → text

The mirror of validation. Validation scores code against a declared `P`;
generation *proposes* `P` (and a domain model) from the code. This is
**renormalization** — collapse local detail, see what survives at the next scale
(`concepts.md`, issue #40):

1. **how many abstractions** — spectral: Laplacian eigengap → natural module
   count `k` (`012-spectral-method.md`). Caveat: on real code the gap is often
   soft, so `k` is a candidate range, not a crisp number.
2. **which nodes form each** — collapse by **fused** clustering: spectral
   coordinates + semantic embeddings together (attributed graph clustering).
   Each cluster = an abstraction = a centroid in embedding space. Pick `k` by
   which value yields semantically tight clusters.
3. **what each one is, in words** — you **cannot** invert an embedding (lossy,
   non-invertible; vec2text is fragile and unneeded). Instead decode the
   *cluster's members*: take the node descriptions nearest the centroid, hand
   them to an LLM → "name and describe the common concept". Decode the members,
   not the vector.

Output: an auto-generated **candidate domain model with labels** = candidate `π`
+ candidate `P`. This *is* the strongest `propose` step of §5. It stays
**descriptive and advisory** — it describes the latent structure of the code *as
written* (run it on mess, it finds the mess's natural clusters); it becomes
prescriptive only when the owner ratifies. Score the proposal with the same
fitness (MDL + semantic coherence + history stability) so the generated
abstractions are measurably good, not just plausible LLM prose.

---

## 7. The semantic axis (slice `S`)

Each node carries a text description of what it represents; embed it → a vector
per node → a dense **semantic similarity** matrix `S[i][j] = cos(e_i, e_j)`. The
*meaning* axis, orthogonal to topology, and the thing that makes the other
slices interpretable.

What it unlocks:

- **auto-discovers `π`** with human-readable labels (the hard part of §5).
- **explainable smells** via misalignment of `A` / `C` / `S`: high `S` + no edge
  + high `C` = "belongs together but scattered" (missed abstraction, *named*);
  edge + low `S` = "dependency crosses a meaning boundary". Not a black box.
- **"structure lies" audit**: folder partition vs semantic clustering disagree.
- **(speculative)** policy as *meaning regions* — robust to renaming/moving.

Discipline: `S` drifts (re-embed → changes), is LLM-derived, and similarity ≠
permission. So `S` lives in the descriptive/construction/advisory layer only —
it proposes groups and explains smells; it never judges in the gate.

### What text per node, and from where (measured on a real Go codebase)

The measured codebase carries unusually high human-written semantic text — strong inputs
*without* an LLM:

- **package node** (67% have a package doc; paths strongly domain-encoding):
  assemble `path + package doc comment + README first paragraph (if any) +
  exported interface/type names with their one-line docs`. LLM summarization is
  a fallback only for the ~33% undocumented packages.
- **type/interface node** (95–98% documented): `name + doc comment + method
  names/docs` is sufficient on its own in 95%+ of cases. No LLM needed.

Extraction priority per package: (1) `doc.go`/package comment → (2) sibling
`README.md` first para → (3) always append exported type names + one-line docs →
(4) LLM only as fallback.

### Documentation as a policy (closing the 33% gap)

The ~33% of packages without a doc comment are not an LLM-summarization problem —
make the description a **rule**: "every package node must declare what it
represents" (a non-empty `description`). This is itself a node-attribute
validator (§4) in the existing framework — a `doc_coverage` MatrixValidator whose
residual is the undocumented packages. Consequences:

- slice `S` becomes **deterministic**: descriptions are committed doc comments
  (forced by the gate), embedded reproducibly — not an LLM call at analysis time.
- the LLM only **proposes** the comment (construction); the human commits it (it
  becomes code, ground truth); the gate stays dumb. Same propose/ratify split.

This dogfoods the whole model: the requirement to describe a node is, itself, a
policy matrix.

---

## 8. Graph schema (what to parse, what to attach)

Nodes and edges build on what `concepts.md` and the archai bridge already
define. For *this* model the load-bearing granularity is **package and domain**
(where policy lives); keep type/symbol nodes for richer operators (§4, §6).

### Nodes

| Node | Granularity | Why |
|------|-------------|-----|
| `module` | root | scope |
| `domain` / `group` | derived (π) | where `P` is authored; from config globs *or* §6/§7 discovery |
| `package` | leaf | the unit of `A`, `C`; policy is lifted to/from here |
| `type` (interface/struct) | leaf | `implements`/`references` operators (§4), contract surface |
| `function`/`method` | leaf | `calls` operator, control-flow primitives (archmotif level ~3.5) |

### Edges — parseable from code (the structural slices)

| Edge | From → To | Source |
|------|-----------|--------|
| `imports` | package → package | `go list` / AST — **this is `A`, materialize it explicitly** |
| `contains` | package → type → method | AST |
| `implements` | type → interface | AST + type info |
| `embeds` | type → type | AST |
| `calls` | function → function | call graph |
| `references` / `usesType` | symbol → type | AST + type info |
| `belongs_to_role` / `belongs_to_domain` | node → group | derived (π): config globs or discovery |

Note: archai already declares an `imports` edge kind but **never emits it** —
the single most important gap to close, since `A` is built from it.

### Edges/attributes — NOT from code (the descriptive overlays)

| Data | Attach to | Source |
|------|-----------|--------|
| `co_change` weight | package ↔ package | git history (per-commit import-set diffs) |
| `first_seen` / `survival` / `churn` | edge | git history (§5 confidence weighting) |
| `description` (text) | package, type | doc comments + README + names (§7) |
| `embedding` (vector) | package, type | embed the description (`036-graph-embeddings-research.md`) |
| `role` | package | config or `027-role-metadata.md` |

This attribute set is exactly enough for everything above: `imports` →
`A`/residual/gate; `role`+`belongs_to` → `π`/policy lift; `co_change`+`survival`
→ history fitness; `description`+`embedding` → `S`/discovery/explainable smells.

---

## 9. Division of labour

- **archai** — the read model + authoring + UI. Materialize the `imports` edge;
  author the `K×K` policy over domains/layers; render the DSM grid + residual as
  a projection (the UI already surfaces `policyViolations`). Today the layer-rule
  check is reimplemented three times off-graph — collapse to one graph traversal
  that consumes `P`.
- **archmotif** — the matrix engine. Residual + norm + coefficient; quotient
  `πᵀAπ`; transitive reachability (forbid indirect paths, not the `A²` DIP
  story); cycles via SCC/triangularization; the spectral/communities/quotient
  programme (#74 family, still unbuilt); co-change + embedding slices. Seeds
  already present: `coupling` role-pair matrix and `target-contract`'s
  `ForbiddenEdges` field (declared, currently **unchecked** — the natural place
  to wire the mask).
- **go-arch-lint** — retire; salvage the 3-function core (path→role classifier,
  per-role allow-list, prefix match) into archai's checker.
- **archlint** (mshogin upstream) — **reference only**, do not build on the fork.
  It proves the operator→number machinery works (154 research metrics build real
  adjacency/Laplacian matrices) — and is a cautionary tale: those metrics are
  **disconnected from the rules** (forbidden = string-pattern deny-list), so they
  are numbers without an attached decision. Cherry-pick the interpretable
  operators (rank, condition number, SVD on the dependency matrix), port to
  Go/gonum; leave the rest.

---

## 10. What this explicitly is not

- **Not** a learned validator. The policy is *declared*, never a neural net.
  Nets/clustering/LLMs *propose* candidate policies; the policy itself, once
  chosen, is frozen and human-readable. A net target matrix learns the code's
  decay as "normal" and has no notion of "good", only "frequent".
- **Not** a metric zoo. A metric earns its place only if (a) interpretable and
  (b) it maps to an action when red. Start from *zero* metrics; the only ones
  added are residuals against declared policies. The count is bounded by your
  policies, not by branches of mathematics.
- **Not** an external judge. There is no external oracle of good architecture;
  the owner's accepted state is ground truth. The gate is a consistency mirror.
- **Not** one magic matrix. Dependency flow folds into one global residual;
  everything else is a library of typed operators, each its own coefficient.
