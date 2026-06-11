# ADR-004 — Parser strategy: golang.org/x/tools/go/packages

**Status:** accepted
**Date:** 2026-05-04
**Stage:** 1 — Build the typed graph (level 3.5)
**Supersedes:** —

## Context

Stage 1 needs to turn a Go source path into the typed graph. The graph
needs type information (to resolve method receivers, interface
implementations, called functions across package boundaries) and import
relationships. Two realistic options:

1. `go/parser` + `go/types` driven by hand. Lightweight; we choose
   exactly what to load.
2. `golang.org/x/tools/go/packages` with `LoadAllSyntax`. Heavier; it
   resolves modules, imports, and type information end-to-end.

## Decision

Use `golang.org/x/tools/go/packages` with the `LoadAllSyntax` mode.

Stage 1 already needs cross-file resolution within a module
(`Implements`, `Calls` to other files in the same package, `Embeds`,
etc.). Re-implementing module resolution and type-checking on top of
`go/parser`+`go/types` is busywork that the standard tooling already
handles correctly across `go.mod`, vendoring, and build tags.

## Alternatives considered

- **`go/parser` + `go/types` directly.** Less to load, but we re-invent
  module resolution and import handling. Useful only if `go/packages`
  proves too slow on the corpora we care about; archlint (the upper
  bound for Stage 1 verification) is a few hundred files, well within
  `go/packages`'s comfort zone.
- **`golang.org/x/tools/go/callgraph` + a callgraph algorithm
  (`cha`/`vta`).** Strong call-edge resolution, but it returns
  call-edges only; we'd still need the rest of the graph (types,
  fields, control-flow primitives) from the AST. Defer; Stage 3 may
  add it as a refinement signal.

## Consequences

- Direct deps: `golang.org/x/tools` (we use `go/packages` and lean on
  `go/ast`/`go/types` from stdlib via the loaded `Package`).
- Inter-package handling for v1 is opaque — see ADR-006. We follow
  imports via `pkg.Imports`, but for stdlib and external types we only
  emit a `Type` placeholder node in the foreign package; we don't recurse.
- Errors on package load do not abort graph construction — we record
  them as `LoadErrors` on the result and emit nodes/edges from
  successfully parsed files. This makes the tool useful on
  partially-broken codebases (which is most real ones).
- We rely on `go/types.Implements` for the `Implements` edge between
  concrete types and interfaces, not a hand-rolled method-set walk.
