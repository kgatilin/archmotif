# ADR-005 — Node ID format and stability

**Status:** accepted
**Date:** 2026-05-04
**Stage:** 1 — Build the typed graph (level 3.5)
**Supersedes:** —

## Context

ROADMAP Stage 1 calls for "stable node IDs" with a suggested format of
`<file>:<line>:<col>:<kind>`. Stable IDs are needed so that:

- The JSON graph can be diffed across runs (Stage 10 drift).
- Stage 6 skeletons can refer to specific subgraph nodes by ID.
- Stage 8 verification can match generated nodes back to target IDs.

The `Package` node has no source position; control-flow primitives
nest inside functions and need disambiguation when several appear on
the same line.

## Decision

Use the format:

```
<rel-path>:<line>:<col>:<kind>[:<name>][#<ordinal>]
```

- `<rel-path>` is the file path relative to the module root, with
  forward slashes (`/`). For `Package` nodes there is no file; use the
  package import path with a leading `pkg:` sentinel
  (`pkg:github.com/foo/bar`).
- `<line>` and `<col>` come from `token.Position` of the AST node's
  start. They are 1-indexed.
- `<kind>` is the lowercased `NodeKind` (`function`, `method`, `loop`, …).
- `<name>` is included when the node has a stable identifier (function
  name, type name, field name); omitted for anonymous control-flow nodes.
- `#<ordinal>` is appended only when needed to break ties for nodes at
  the same position+kind+name (e.g. two anonymous funcs on one line).
  Ordinal is 0-based assignment order during the AST walk; collisions
  are resolved deterministically by walk order, which itself is
  deterministic given the package's file list (sorted by `go/packages`).

Examples:

- `pkg:github.com/kgatilin/archmotif/internal/graph`
- `internal/graph/graph.go:14:6:type:Graph`
- `internal/graph/graph.go:42:2:method:AddNode`
- `internal/parser/build.go:88:4:loop`
- `internal/parser/build.go:88:4:loop#1` (second loop on same line/col)

## Alternatives considered

- **Hash of (path, line, col, kind, name).** Compact but unreadable in
  diffs and unhelpful in skeleton output. Stability is the same.
  Rejected for v1; revisit if IDs become a payload-size problem.
- **Random UUID per run.** Trivially stable within a run but fails
  cross-run diff (Stage 10) and verification (Stage 8). Rejected.
- **Fully-qualified Go identifier path
  (`github.com/x/y/pkg.Type.Method`).** Clean for symbols but doesn't
  cover control-flow primitives, files, or anonymous functions.
  Rejected as primary key; we *do* expose the Go path as a
  separate `QName` attribute for debugging.

## Consequences

- IDs change when files move or lines shift. That's expected; Stage 10
  drift work will need a structural identity layer on top (e.g. match
  by `kind`+`QName` first, fall back to position).
- The format is plain ASCII with `:` and `#` as separators — chosen so
  IDs are safe in JSON, log lines, and command-line arguments without
  quoting on Linux.
- ID generation logic lives in `internal/graph/id.go` so Stage 2+ can
  call it for synthetic nodes (contracts, motif placeholders) without
  reaching into the parser.
