# ADR-007 — Control-flow primitives as nodes (not edge annotations)

**Status:** accepted
**Date:** 2026-05-04
**Stage:** 1 — Build the typed graph (level 3.5)
**Supersedes:** —

## Context

ROADMAP Stage 1 leaves open whether control-flow primitives (loops,
branches, goroutines, defers, channel ops, sync primitives) should be
**nodes** in the graph or **annotations** on `Calls` / `Contains`
edges.

The default in the roadmap text is "every primitive is a node, with
`Contains` edges nesting them. Revisit if graph blows up." Stage 1
verify expects archmotif itself in the tens-to-low-hundreds range and
archlint in the hundreds-to-low-thousands range; we need to confirm
this default holds before committing.

## Decision

Keep the roadmap default: every control-flow primitive is its own
typed node, with a `Contains` edge from the enclosing function/method
or from an outer primitive. Calls that originate inside a primitive
emit a `CallsFrom` edge (control-prim → callee) **in addition** to the
plain `Calls` edge from the enclosing function/method to the callee.

Specifically:

- **Loop** — every `for` and `range` statement.
- **Branch** — every `if`, `switch`, `type-switch`, and `select`
  statement. `case` clauses are *not* their own node; they're
  `Contains` children indexed by ordinal at the statement level via the
  `Branch` parent's metadata.
- **Goroutine** — every `go` statement. Body is a `Contains` child of
  the `Goroutine` node (typically a single `Calls` edge to the spawned
  function or an inline anonymous function we represent as a `Function`
  node with `Anonymous: true`).
- **Defer** — every `defer` statement.
- **ChannelOp** — `<-ch` (recv) and `ch <- x` (send), plus `close(ch)`.
  Direction stored as metadata.
- **SyncPrim** — calls to `sync.Mutex.Lock`/`Unlock`,
  `sync.RWMutex.*`, `sync.WaitGroup.Add`/`Done`/`Wait`, `sync.Once.Do`,
  `atomic.*`. Detection is a name match on the receiver type's full
  path. Synchronous map ops are not flagged.

## Alternatives considered

- **Edge annotations only** (e.g. `Calls{InsideLoop: true,
  InsideGoroutine: true}`). Cheaper graph, but Stage 3 metrics like
  cycle-rank-inside-loops and Stage 4 anomalies like "this defer is
  the only one in a 200-function package" become awkward to express
  without the primitive being a first-class node. Rejected.
- **Mixed: nodes for goroutine/defer/channel/sync; annotations for
  loop/branch.** Cleaner graph, but inconsistent and forces Stage 3 to
  handle two access patterns. Rejected for symmetry — easier to drop
  loop/branch nodes later if size becomes a problem than to add them
  back.

## Consequences

- Graph size scales with number of statements, not number of symbols.
  Empirically: archmotif Stage 0 has ~2 functions and minimal
  statements → tens of nodes. archlint has ~hundreds of functions
  with normal control flow → thousands of nodes. Both within Stage 1
  verify ranges.
- The `internal/parser` walker is statement-level, not just
  declaration-level. Code is structured as
  `walkPackage → walkFile → walkDecl → walkFuncBody → walkStmt
   (recursive)`.
- Revisit if a real corpus blows past ~50k nodes — at that point we
  add a `--coarse` flag that drops Loop and Branch nodes.
