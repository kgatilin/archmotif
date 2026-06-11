# ADR-012 — Spectral gap via gonum/mat EigenSym, undirected lens

**Status:** accepted
**Date:** 2026-05-04
**Stage:** 3 — Metrics infrastructure
**Supersedes:** —

## Context

Stage 3 requires algebraic connectivity (the second-smallest eigenvalue
of the graph Laplacian) as one of the five built-in metrics. Two
choices need to be made:

- The Laplacian is defined for *undirected* graphs. archmotif's typed
  graph is directed (every edge has direction). What's the lens?
- Eigendecomposition tooling: pure-Go via `gonum/mat`, or shell out
  to a Python helper for sparse eigensolvers (per RQ-6 / ROADMAP)?

## Decision

**Lens.** Symmetrise. Build the Laplacian over the undirected
projection: every directed edge of any kind becomes an undirected edge
between the same endpoints; parallel directed edges (e.g. mutual
calls) collapse to a single undirected edge. The `metrics.toUndirected`
helper does this once and feeds the result to `gonum/graph/spectral.NewLaplacian`.

We document the symmetrisation explicitly: spectral gap is a *connectivity*
lens — does the graph break into chunks linked by a thin bridge? That
question is naturally undirected. Direction-sensitive structural
questions (cycle rank, dependency layering) get their own metrics
that build their own directed views.

**Tooling.** Use `gonum/mat.EigenSym`. The Laplacian is symmetric and
positive semi-definite; `EigenSym` is the right factoriser. archmotif's
self-graph is ~1100 nodes; dense eigendecomposition runs in well under
a second. The Python-spillover path described in RQ-6 stays explicitly
deferred for Stage 3 — when archmotif first needs to run on graphs of
~10k+ nodes, we revisit and either swap in a sparse iterative solver
(ARPACK, LOBPCG) or ship JSON to a Python helper.

The metric reports λ₂ (algebraic connectivity) plus the smallest few
eigenvalues in `details.eigenvalues` so Stage 4 anomaly detection has
context if it wants more than the gap alone.

## Alternatives considered

- **Hermitian / asymmetric Laplacian preserving direction.** Possible
  but harder to interpret, and gonum doesn't ship a directed Laplacian
  helper. Most "spectral architecture" literature symmetrises; we
  follow.
- **Random-walk Laplacian (`NewRandomWalkLaplacian`).** Useful for
  PageRank-style centrality, less so for "where's the architectural
  bottleneck." Out of scope for v1; can be added as a separate metric
  later.
- **Shell out to NumPy.** Faster on huge sparse graphs but adds an
  external dep, complicates testing, and isn't needed at Stage 3
  archmotif sizes. Documented as a fallback under RQ-6, not implemented.
- **Custom power-iteration.** Only finds the largest eigenvalue cheaply;
  algebraic connectivity wants the second-*smallest*. Inverse iteration
  works but reimplements something gonum already gives us.

## Consequences

- One new transitive dep used: `gonum.org/v1/gonum/graph/spectral` and
  `gonum.org/v1/gonum/mat`. Both live in the gonum module already
  required by Stage 1, so go.mod doesn't grow new top-level entries.
- Disconnected graphs report λ₂ = 0 (algebraically correct: the kernel
  of the Laplacian has dimension equal to the number of connected
  components). Stage 4 will need to interpret a near-zero gap as
  "this graph is one bridge away from splitting", not as "no signal."
- Numerical FP noise occasionally produces eigenvalues with tiny
  negative real parts. The metric clamps `(-1e-10, 0)` to 0 so
  downstream consumers don't see physically-impossible negative
  connectivity values. Larger negatives (which would indicate a real
  bug) propagate through unchanged.
- Empty / single-node graphs return 0 with `details.note` explaining
  the cause; no spurious NaN or panic.
