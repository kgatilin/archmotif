# ADR-013 — Motif redundancy: exact iso on bounded sizes

**Status:** accepted
**Date:** 2026-05-04
**Stage:** 3 — Metrics infrastructure
**Supersedes:** —

## Context

Stage 3 motif redundancy needs to find subgraphs that recur without an
extracted abstraction. ROADMAP and issue #4 say: exact subgraph
isomorphism for sizes 3–5; consider gSpan or similar approximations
later. The implementation tradeoff is between correctness (exact
finds *every* recurrence) and runtime (enumerating all connected
induced subgraphs of size k explodes combinatorially).

archmotif's self-graph has ~1100 nodes. A naive "enumerate all
size-4 connected subgraphs" can hit millions on graphs that dense.

## Decision

Three layered choices:

1. **Substrate restriction.** Motif enumeration only considers
   `Function | Method | Type` nodes connected by `Calls | CallsFrom |
   Implements | Embeds | Returns`. Loops, branches, channel ops,
   packages, and Contains edges are excluded — they don't participate
   in extract-interface / extract-function rewrites (the use case
   that motivates this metric per docs/concepts.md §5). On
   archmotif's graph this reduces the candidate node set from ~1100
   to a few hundred.

2. **Bounded size.** Default cap `MaxSize = 4`. Sizes 3 and 4 are
   enumerated by default. Size 5 is configurable via
   `--motif-max-size` but off by default because empirically it
   pushes runtime past 30s on graphs with more than a few hundred
   substrate nodes — Stage 3 verify wants "all metrics produce
   numbers without crashing", and exceeding the test runner's
   default budget would defeat the smoke test.

3. **ESU enumeration with hard sample limit.** We use the ESU
   (Enumerate-SUbgraphs) algorithm from Wernicke 2006: each anchor
   only extends to higher-rank neighbours, eliminating duplicate
   enumeration of the same node set. A `SampleLimit = 100_000`
   global cap provides a hard ceiling: when reached, enumeration
   halts and the result records `details.budgetExhausted = true`.
   This keeps the metric bounded on graphs we haven't seen yet
   without panicking the runner.

4. **Canonical form by all-permutation rendering.** With k ≤ 4 (24
   permutations) we can afford to render the induced subgraph under
   every permutation of its slots and keep the lexicographically
   smallest string. The string captures: sorted node-kind list,
   sorted (fromSlot, toSlot, edgeKind) tuples for every directed
   intra-subgraph edge. Two motifs are isomorphic iff their canonical
   strings match. Simple, exact, no AB-test against a sophisticated
   colouring algorithm needed.

5. **Abstraction filter.** Subgraphs whose entire member set is
   contained inside a single `Type` node (a struct + its methods,
   already factored) are skipped. The motif this filter was meant to
   surface — "extract an interface across these N call sites" — is
   only interesting *across* an existing type, not within one.

## Alternatives considered

- **gSpan-style frequent-subgraph mining.** Scales to bigger motifs
  but is approximate and harder to debug ("why didn't it find this
  pattern?"). Stage 3 explicitly wants exact for sizes 3–5; we keep
  gSpan as a deferred follow-up if size-6+ motifs become a research
  priority.
- **VF2 / VF3 subgraph isomorphism.** Better asymptotics for very
  large k. Overkill for k ≤ 4 — the all-permutation canonical form
  is 24 string comparisons per candidate.
- **No abstraction filter (count every recurrence).** Floods the
  output with star patterns from `Type`s with ≥ 3 methods. Filter
  matches issue #4's wording ("without an extracted abstraction").
- **Enumerate from every node, skip dedup.** Quadratic redundancy.
  ESU's "rank > anchor" rule is the standard one-line fix.

## Consequences

- The metric is sound on its substrate (no false isomorphism reports)
  but not necessarily complete on the full graph (excluded edges /
  nodes can hide a motif). Documented in the metric description.
- `--motif-max-size 5` is supported; users who hit budget exhaustion
  can drop the size or raise `--motif-sample-limit`.
- Per-instance details include the member node IDs, so Stage 4
  anomaly detection can rank groups by count and link straight back
  into the graph.
- If the substrate ever needs to grow (e.g. to include channel ops
  for concurrency-pattern motifs), the change is localised to
  `motifEdgeKinds` / `motifNodeKinds` in `internal/metrics/motif.go`.
