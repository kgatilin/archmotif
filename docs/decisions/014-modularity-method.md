# ADR-014 — Modularity over package boundaries via gonum community.Q

**Status:** accepted
**Date:** 2026-05-04
**Stage:** 3 — Metrics infrastructure
**Supersedes:** —

## Context

Stage 3 modularity asks: do declared package boundaries match the
graph's natural community structure? Newman modularity Q answers
that for a *given* partition (high Q means within-community edges
are denser than chance). Two implementation choices:

- Use `gonum/graph/community.Q(g, communities, resolution)` directly,
  feeding the partition we want to score.
- Compute Q ourselves from the formula
  `Q = Σᵢ (eᵢᵢ - aᵢ²)`.

We're not running Louvain — we're scoring a *user-given* partition
(packages). gonum's Louvain helpers (`Modularize`) discover their own
partition, which isn't what we want here.

Plus the directed-vs-undirected lens for modularity (Newman defines
it for both directed and undirected; gonum ships both).

## Decision

Use `gonum/graph/community.Q` against the undirected projection of
the typed graph. The community list is built by walking outbound
`Contains` edges from each `Package` node; nodes that no package
contains (foreign placeholders, orphans) get singleton communities so
Q is well-defined for the full node set.

Resolution defaults to 1.0 (the standard Newman formula). The
`Configurable` knob exposes the parameter for future tuning; we
don't yet wire a CLI flag because no real use case has surfaced.

The metric emits:

- One ScopeGraph record with the Q value and community count.
- One ScopeRegion record per package community (member list in
  details), so Stage 4 can flag packages whose internal connectivity
  is anomalously low (a hint to split).

**Lens choice.** Same as ADR-012: undirected. Modularity *is* defined
for directed graphs (`qDirected` exists in gonum and Q dispatches to
it when the input is `graph.Directed`). For Stage 3, the question
"do packages match the graph's communities?" is naturally undirected
(an A→B call and a B→A call both bind A and B together). Direction-
sensitive tooling like dependency-cycle detection lives in cycle_rank
where direction matters. Documenting the lens in this ADR keeps it
auditable when Stage 4 wants to layer a directed-modularity comparison.

## Alternatives considered

- **Run Louvain (`Modularize`) and compare its partition to packages.**
  Useful as a separate question ("how would the graph naturally
  partition?") but doesn't directly answer "do packages match?".
  Possible follow-up for a `community_drift` metric.
- **Compute Q from scratch using our typed graph.** Avoids one gonum
  helper but reinvents what's already shipped. We would still need
  to symmetrise. Rejected — using `community.Q` is the small, correct
  default.
- **Pass the directed graph to `community.Q`.** It dispatches to
  `qDirected` automatically. Rejected for v1: archmotif's graph is
  edge-typed (Calls, Returns, Implements, …) and treating a single
  directed Calls edge as a real flow direction (vs an undirected
  binding) is debatable. Document undirected as the v1 lens; revisit
  on user request.

## Consequences

- The metric is fast (one community.Q call on a ~1100-node graph
  is sub-millisecond).
- Q is bounded in `[-0.5, 1)` for unweighted graphs. Negative values
  are valid and meaningful: they say "the partition you chose
  separates things that the graph thinks belong together", which is
  exactly the architectural anomaly Stage 4 should care about.
- Foreign-placeholder Package nodes (per ADR-006 / ADR-009) become
  their own communities, so the community count usually exceeds the
  count of *loaded* packages by however many imports the loaded set
  reaches into. Documented in the Q record's `details.communities`.
