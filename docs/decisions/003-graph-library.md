# ADR-003 — Graph library: gonum/graph

**Status:** accepted
**Date:** 2026-05-04
**Stage:** 1 — Build the typed graph (level 3.5)
**Supersedes:** —

## Context

Stage 1 needs an in-memory graph data structure supporting typed nodes and
typed edges, with a query API (nodes by type, in/out neighbours by edge
kind, subgraph extraction). Stage 3 (metrics) later needs Newman
modularity, spectral gap (Laplacian eigenvalues), motif counting, and
cycle detection over the same graph.

Two realistic options:

1. Roll a custom graph package (map-of-maps, in-house adjacency).
2. Use `gonum.org/v1/gonum/graph` plus its companion `gonum/mat` for
   spectral operations.

## Decision

Use `gonum.org/v1/gonum/graph` as the substrate from day one.

We wrap gonum behind our own `internal/graph.Graph` type — gonum's
`graph.Node`/`graph.Edge` interfaces don't carry typed roles
(`Function`, `Calls`, etc.) so we layer our `NodeKind` / `EdgeKind`
attributes on top. The wrapper exposes the queries Stage 1 needs
(`NodesByKind`, `Neighbors(id, dir, kind)`, `Subgraph(seeds)`) and lets
Stage 3 reach the underlying gonum DAG/Laplacian helpers without a
rewrite.

## Alternatives considered

- **Custom map-of-maps.** Trivial to write, but Stage 3 forces us to
  re-implement (or re-export) Laplacian construction, eigen-decomposition,
  community detection, and graph algorithms gonum already ships. Net
  cost is migration work later, plus a second graph type living next to
  the custom one during the migration window. Rejected.
- **`dominikbraun/graph`.** Smaller and ergonomic, but lacks the
  numerical/spectral toolchain. Same Stage-3 problem.
- **Build on `golang.org/x/tools/go/callgraph`.** Solves call-graph
  specifically but bakes in Go-callgraph semantics; awkward to attach
  control-flow primitives, contracts (Stage 2), and arbitrary edge
  kinds. Useful as an *input signal* later, not the substrate.

## Consequences

- Direct deps: `gonum.org/v1/gonum`. (Stage 3 will already need
  `gonum/mat` for spectral work; same module.)
- All node/edge identity is owned by our wrapper. Gonum sees opaque
  `int64` IDs; the typed payload (`Node`, `Edge` structs) lives in our
  store. This keeps gonum-specific types out of the public Stage 1 API.
- JSON serialisation operates on our wrapper, not on gonum types.
- Stage 3 spectral work plugs in via `gonum/graph/simple` adjacency
  conversion; no interface refactor expected.
