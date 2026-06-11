# ADR-006 — Inter-package handling: opaque foreign types, no stdlib recursion

**Status:** accepted
**Date:** 2026-05-04
**Stage:** 1 — Build the typed graph (level 3.5)
**Supersedes:** —

## Context

ROADMAP Stage 1: "Inter-package vs intra-package — in v1, do
single-module only, follow imports lazily."

Concretely, when we walk a function in package P and it calls
`fmt.Println` or returns an `io.Reader`, we have to decide whether to:

1. Recurse into the foreign package and build full graph nodes for
   `fmt.Println` and its body, or
2. Emit a placeholder node for the foreign symbol and stop there.

(1) is correct in the limit but explodes the graph (a single program
pulls in hundreds of stdlib functions); (2) keeps the graph the size
of the user's code, at the cost of resolving cross-module relationships
shallowly.

## Decision

For Stage 1:

- **Primary scope** is the packages explicitly loaded by `archmotif
  graph <path>`. These are walked fully — every type, function, method,
  field, and control-flow primitive becomes a node.
- **Foreign packages** (anything outside the loaded set, including
  stdlib and third-party deps) are represented by a single `Package`
  node per package, plus on-demand `Type` and `Function`/`Method`
  placeholder nodes when the loaded code references them. Foreign
  placeholders carry `Foreign: true` and have no source position
  (we use the import path as their location surrogate per ADR-005).
- We do **not** walk foreign ASTs even when `go/packages` returns
  them. The loader is configured with `NeedTypes` so we can resolve
  `Implements` and `Calls` semantically, but we don't iterate
  `pkg.Syntax` for foreign packages.

The `Calls` edge from a loaded function to a foreign function therefore
points at a placeholder; the placeholder has no outgoing `Contains` /
`Calls` of its own. This matches the Stage-1 verify expectation
("plausible size, hundreds-to-low-thousands nodes") on archlint.

## Alternatives considered

- **Recurse into all reachable packages.** Closer to a "complete"
  graph but explodes size on any non-trivial program (a hello-world
  pulls in `fmt`, `io`, `unicode`, `syscall`, …). Stage 1 verify caps
  archmotif at "tens to low hundreds" of nodes — recursive walk
  blows past that even for the scaffold. Rejected.
- **Drop foreign references entirely.** Cleaner graph but loses the
  `Calls` / `Returns` edges to stdlib, which Stage 3 modularity and
  Stage 4 anomaly detection will want as signal. Rejected.
- **Recurse into intra-module foreign packages only.** Would be the
  natural extension when `<path>` points at a single subpackage. Stage 1
  side-steps this by having the user point at the module root or a
  package list; the loader expands those. Revisit if multi-module
  workspaces become a real use case.

## Consequences

- Foreign placeholder nodes are flat: they have at most a `Contains`
  edge from their `Package` node and incoming `Calls` / `Returns` /
  `DependsOn` edges from loaded code. They do not contain `Method` or
  `Field` nodes.
- Two loaded calls to the same foreign function reuse the same
  placeholder ID, so degree on the placeholder is the count of
  call sites. Useful for Stage 4 anomaly detection
  ("everyone calls `errors.New`, that's not anomalous").
- Implements edges from loaded concrete types to foreign interfaces
  (e.g. `io.Reader`) are emitted by walking the loaded type's method
  set against `*types.Interface` objects we encounter in the type
  universe. Loaded interface implemented by foreign type is *not*
  detected — we don't iterate foreign type-checked objects.
- When the loaded set spans multiple modules (uncommon at Stage 1),
  each module's packages are still walked fully if the loader returned
  syntax for them.
