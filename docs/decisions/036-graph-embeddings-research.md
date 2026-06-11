# ADR-036 — Graph embeddings for architecture similarity (research note)

**Status:** accepted (research note — no production wiring)
**Date:** 2026-05-06
**Issue:** #32
**Related:** ADR-013 (motif iso), ADR-020 (anomaly scoring), ADR-026
(pattern reports), ADR-027 (role metadata), ADR-030 (coupling
metrics)
**Recommendation:** **defer.** Do not adopt graph embeddings in v1.
Re-evaluate when (a) a labelled cross-project corpus exists or (b) a
human-in-the-loop "find similar packages" query becomes a real user
need, neither of which is true today.

## Context

Issue #32 asks whether graph embeddings or graph kernels add useful
signal for architecture similarity and anomaly discovery on top of
archmotif's deterministic stack:

- ADR-013 motif isomorphism + ADR-020 robust z-score anomaly scoring
  already detect repeated and outlier subgraphs.
- ADR-026 named pattern reports give stable verdicts (`domain_core`,
  `external_noise_sink`, `forbidden_role_edges`).
- ADR-027 puts role metadata on nodes; ADR-030 turns those roles into
  a deterministic role-pair coupling matrix and `domain_purity` /
  `adapter_isolation` scores.

The deliverable for #32 is a recommendation (use now / defer / do not
use) backed by concrete reasoning, optionally with a minimal
prototype. This is a research note, not a production feature ADR.

## Question summary

1. Can WL kernels, node2vec, metapath2vec, or other heterogeneous
   graph embeddings capture useful code architecture structure?
2. What corpus / labels would be needed to train or evaluate them?
3. Can embeddings find similar adapter / domain / port structures
   across packages?
4. Do they add signal beyond deterministic motif and coupling
   metrics?
5. What failure modes make embeddings unsafe for automated
   architecture judgment?

## Methods evaluated

### Weisfeiler-Lehman (WL) subtree kernel

- No training step. Deterministic. Closed form on the graph.
- Initial labels can be the (NodeKind, Role) pair archmotif already
  produces — so the kernel is label-aware without extra config.
- Computes a multiset embedding; pairwise similarity is cosine on the
  hashed-label histogram.
- Strict generalisation of the kind of subgraph counting ADR-013
  already does. Implementing it on top of the typed graph is a
  ~150-line Go file (see prototype below).
- Failure mode: at depth ≥ 1 on small graphs, every node's neighbour
  signature is unique, so two graphs with the same role mix but
  different wiring score 0 — far below intuitive similarity (see
  prototype output).

### node2vec

- Trainable random-walk embedding. Produces dense vectors per node.
- Requires a corpus to train and a downstream task to evaluate
  against. Archmotif has neither: it analyses one repository at a
  time, and there is no labelled "this package is well-architected"
  signal.
- Treats edges as homogeneous; archmotif's edge kinds (`calls` vs
  `implements` vs `embeds`) carry meaning that node2vec discards
  unless metapath restrictions are layered on.
- Non-deterministic — two runs on the same graph produce different
  vectors unless seed and hyperparameters are fixed and walks are
  exhaustive (which defeats the purpose).

### metapath2vec / heterogeneous graph embeddings

- Designed for heterogeneous graphs: node and edge types are first-
  class. This is the correct fit for archmotif's typed graph in
  principle.
- Requires the user to specify metapath schemas (e.g. `Type
  --implements--> Type --calls--> Type`). Picking schemas is the
  same problem as picking deterministic motifs — except deterministic
  motifs come with a verdict, while metapath embeddings come with a
  vector.
- Same training-corpus requirement as node2vec.
- Same non-determinism + non-explainability problems.

### Graph neural networks (GIN, R-GCN, HAN)

- Out of scope: requires a labelled supervised task and infrastructure
  archmotif does not have. Captured here only to confirm that the
  deferred path "if we ever do this, we use heterogeneous GNN with WL-
  initialised features" is the natural ceiling, not random-walk
  methods.

## Corpus / labels needed for a credible evaluation

For any embedding method to be more than a parlour trick on archmotif,
the following would have to exist:

1. **A labelled corpus.** Tens to hundreds of packages each tagged
   with a coarse-grained verdict (e.g. "follows hex architecture",
   "leaks domain into adapters", "no clear layering"). archmotif
   currently has zero such labels — role metadata (ADR-027) labels
   *nodes*, not *packages*.
2. **A held-out test split.** Otherwise any reported similarity
   number is fitted to the same data the method saw.
3. **A baseline.** The deterministic stack (ADR-026 patterns +
   ADR-030 coupling) is the floor. An embedding that does not beat
   the floor on a held-out split is noise.
4. **A human-explainable failure mode.** ADR-020 anomalies and ADR-
   026 patterns return evidence nodes / edges. An embedding similarity
   of 0.73 returns nothing a reviewer can act on.

archmotif's current scale is *one repository at a time*, run by an
operator who wants a verdict, not a vector. None of (1)–(4) is
achievable in this regime. (1)–(3) require coordinated labelling
work across multiple repositories.

## Comparison vs deterministic baseline

The deterministic stack already answers most of the questions
embeddings would be claimed to answer:

| Question | Deterministic answer today | Embedding gap |
|---|---|---|
| "Does package X look like ports-and-adapters?" | ADR-026 `domain_core` + `forbidden_role_edges` after roles are configured | Embeddings would need a labelled "looks like P&A" corpus to learn the same thing |
| "Are there repeated subgraphs in X?" | ADR-013 motif isomorphism + ADR-020 z-score anomaly | Embeddings re-derive a weaker version of this |
| "Are these two packages structurally similar?" | Deterministic motif overlap + coupling-matrix overlap | This is the only place embeddings have a marginal advantage — and only across a corpus, which we don't have |
| "Why is this anomalous?" | ADR-020 returns evidence + score per metric | Embedding distance is unexplainable |
| "What pattern does this match?" | ADR-026 returns named pattern with stable ID | Embedding has no named output |

The one cell where embeddings have a theoretical edge — cross-project
similarity — is precisely the cell archmotif does not enter today.

## Prototype

A minimal WL-1 prototype lives at
`scripts/research/wlkernel/`. It implements:

- `Compute(g, iterations)` — iterative WL refinement keyed on
  `(NodeKind, Role)`, folding edge kind + direction into neighbour
  signatures.
- `Cosine(a, b)` — normalised cosine similarity on label-hash
  histograms.

The package is intentionally **not** wired into the CLI or any Stage
pipeline; the smoke test (`scripts/research/wlkernel/wlkernel_test.go`)
keeps it building, and the demo binary at
`scripts/research/wlkernel/cmd/wldemo` reproduces the numbers below.

### Sample output (`go run ./scripts/research/wlkernel/cmd/wldemo`)

Three-node fixtures:

- **A**, **B**: ports-and-adapters shape — `domain_entity`, `port`,
  `outbound_adapter`; adapter `implements` port and `calls` entity.
- **C**: same three roles, but adapter `calls` port directly and
  port `calls` entity (the layering is broken, the kind of thing
  ADR-026 `forbidden_role_edges` is meant to flag).

```
iter=0  sim(A,B)=1.000  sim(A,C)=1.000  |labels(A)|=3
iter=1  sim(A,B)=1.000  sim(A,C)=0.000  |labels(A)|=3
iter=2  sim(A,B)=1.000  sim(A,C)=0.000  |labels(A)|=3
```

Reading the table:

- **Iter 0** (role histogram only) cannot tell A from C — the role
  multisets are identical. The deterministic baseline catches this
  via `forbidden_role_edges`; WL-0 is strictly weaker.
- **Iter 1+** flips the broken graph to *zero* similarity, not "low
  but nonzero". WL is brittle on small graphs because every node's
  neighbour signature is unique. A reviewer reading "0.000" cannot
  distinguish "completely different architecture" from "one edge
  flipped". This is the core failure mode (see below).

## Failure modes

1. **Brittleness on small graphs.** WL with depth ≥ 1 collapses to
   binary similarity (1.0 or 0.0) on graphs of a few dozen nodes.
   archmotif's per-package subgraphs are routinely this small.
   `motif_redundancy` already shows the same brittleness; ADR-020
   handles it by falling back to an absolute floor when MAD = 0.
   Embedding similarity has no equivalent fallback.
2. **No explanation.** ADR-026 commits the codebase to evidence
   nodes, evidence edges, and named violation codes. Embedding
   similarity returns a scalar with no edge or node to point at.
   This is a hard regression for any user-facing surface.
3. **Non-determinism (random-walk methods).** node2vec /
   metapath2vec require seed pinning and exhaustive walks to be
   reproducible — both of which defeat the speed advantage that is
   their main selling point.
4. **Corpus dependency.** Trainable embeddings claim to "find
   patterns" but only ever surface patterns that resemble the
   training distribution. Without a labelled cross-project corpus,
   the patterns surfaced are an artefact of whatever code happened
   to train the model.
5. **Role-leak.** When (NodeKind, Role) drives the initial label,
   most of the embedding's apparent signal is just the role
   histogram — i.e. a deterministic feature ADR-030 already counts.
   The marginal contribution of WL refinement on top of a role
   histogram is small and brittle.
6. **Adversarial drift.** A user adding one synthetic edge can flip
   embedding similarity from 0.95 to 0.0 (see prototype). For
   automated CI gates this is unsafe — a deterministic pattern
   verdict either trips or doesn't, on a rule the operator can
   read.

## Recommendation: defer

For archmotif's current regime — single-repo, operator-driven,
deterministic-verdict-out — graph embeddings:

- Add no signal the deterministic stack does not already produce.
- Trade away ADR-026's evidence-bearing reports for a scalar.
- Cannot be evaluated honestly without a labelled corpus archmotif
  does not have.
- Have failure modes (brittleness, role-leak, adversarial drift)
  that are unsafe for automated judgment, which is the only
  scenario where the speed/marginal-coverage argument would
  matter.

**Defer revisiting until at least one of the following becomes
true:**

1. A labelled cross-repo corpus exists (e.g. archmotif is run on the
   archlint / promptlint / costlint / archai / myhome ecosystem and
   each repo carries an ADR-style architecture-shape tag).
2. A real user need surfaces for "show me packages structurally
   similar to this one" *across repositories*.
3. The deterministic baseline (motif + coupling + patterns) demonstr-
   ably misses a class of architectural smell that an embedding
   catches on a worked example.

If revisited:

- Start with the WL prototype already in `scripts/research/wlkernel/`
  — it is the cheapest, deterministic, and label-aware.
- Pair it with role-histogram features so the embedding never scores
  worse than the deterministic floor.
- Treat output as advisory only (Stage 5+ proposals, never ADR-020
  anomalies or ADR-026 verdicts) until a held-out evaluation exists.
- Explicitly do **not** adopt random-walk methods (node2vec /
  metapath2vec) without solving the determinism, explainability, and
  corpus-dependency problems first.

## Alternatives considered

- **Adopt WL kernel for the v1 anomaly stack.** Rejected — the
  deterministic motif + z-score path (ADR-020) covers the same
  ground with explainable evidence and known calibration.
- **Adopt metapath2vec for cross-package similarity reports.**
  Rejected — non-deterministic, requires a corpus, and the only
  cross-package question on the roadmap (ADR-030 coupling matrix)
  is already deterministic.
- **Build the corpus first, then revisit.** Plausible but out of
  scope for #32. Tracked as a follow-up condition above; would need
  its own issue and cross-repo coordination.
- **"Do not use" outright.** Rejected — the WL prototype is cheap to
  keep around as a research seed, and the corpus-conditional case
  for revisiting is real, not hypothetical. "Defer" with concrete
  re-evaluation triggers is more honest than a permanent ban.

## Consequences

- No production code is added. The WL prototype lives under
  `scripts/research/wlkernel/` and is excluded from the CLI surface.
- ADR-026 / ADR-030 remain the architectural-judgment surface.
- A future ticket can revisit this ADR by extending the prototype
  rather than starting from scratch.
- Sibling research notes (ADR-034 archai bridge, ADR-035 diagram
  projections) are unaffected by this decision.
