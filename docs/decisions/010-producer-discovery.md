# ADR-010 — Contract producer discovery: one-hop, type-driven

**Status:** accepted
**Date:** 2026-05-04
**Stage:** 2 — Contract nodes
**Supersedes:** —

## Context

ROADMAP Stage 2: "the graph records the set of code locations that
produce values for that field — basic structural connection — who
returns this type, who assigns this field." Issue #3: "one-hop, direct
producers only … producer = a function/method that returns a value of
that contract type, or an assignment site that writes a contract-typed
value to a field."

We need to define "produces" precisely enough to make the test fixture
deterministic.

## Decision

A "producer" of contract `C` is any node in the loaded graph that
already carries an outbound edge whose target is `C`'s type node, in
one of the categories below. Stage 1 already emits the relevant edges,
so producer discovery is a pure traversal — no second AST walk.

1. **Returns producer.** A `Function` or `Method` node `F` such that
   `(F) --Returns--> (C)` exists in the graph. Stage 1
   (`internal/parser/funcs.go`) emits Returns edges for every named
   type in a function's result list; we reuse them.

2. **Implements producer.** A concrete `Type` node `T` such that
   `(T) --Implements--> (C)` exists, where `C` is an interface
   contract. Stage 1 (`internal/parser/implements.go`) computes these.
   The implementing type's *constructors* and *factory methods* (any
   Returns producer of `T`) are second-order producers, but at one-hop
   we record the implementer itself; the constructor surfaces via case
   (1) when the constructor returns `C` directly.

3. **Field-write producer (struct contracts).** When `C` is a struct
   contract, any `Field` node `f` of `C` whose enclosing type carries
   a Returns producer is itself produced by that producer. We do not
   trace assignment statements (`s.f = expr`) in v1 — that requires a
   data-flow walk we deliberately defer.

The Stage 2 producer discovery is implemented as:

```go
producers(g, contractID) =
    Neighbors(contractID, In, Returns)
  ∪ Neighbors(contractID, In, Implements)   // interface contracts
```

with the field/embedding propagation handled separately by walking
`Contains` and `Embeds` edges.

For each producer we record:

- The producer node (function / method / type) and its source position.
- The contract node it produces.
- The relation kind (`returns`, `implements`).

This is enough to make the issue's fixture verify: the contract node,
both impl types (Implements producers), and the constructor call site
(Returns producer) are recoverable from the graph alone.

## Alternatives considered

- **Multi-hop data-flow tracing.** Walk every assignment statement,
  resolve RHS types, follow dataflow into struct fields and channel
  ops. Closer to "complete" but requires a Stage-1-level rebuild
  (assignment statements are currently walked for child expressions
  only — we don't record write sites as edges). Out of scope per
  issue #3 and ROADMAP.
- **Mark the producer with an `isContractProducer: true` Attrs key.**
  Simple but the same producer can be a producer of multiple contracts;
  we'd need either a list-valued attribute or a separate edge to keep
  the relation. We chose to expose producer enumeration via a dedicated
  `contracts.Producers(g, contractID)` helper that walks the existing
  edges; no new edge kinds, no new attributes.
- **Restrict producers to functions named like constructors
  (`NewX`, `MakeX`).** Pattern-matching on names is brittle; we use
  type information instead. Rejected.

## Consequences

- Producer discovery is `O(in-degree of contract node)` — fast.
- No schema change, no new edge kinds. Stage 1 graphs already contain
  everything needed; Stage 2 adds the marker attributes and a
  read-time helper.
- Constructor-with-impl-return shows up twice in the producer list:
  once as the function's Returns producer (the function node), once
  via the implementing type's Implements producer. The CLI dedups by
  producer ID before printing.
- Field-write tracing is deferred; ROADMAP Stage 8 (verification) is
  the natural place to extend depth, since the verifier needs
  structural diffing anyway.
