# ADR-009 — Contract markers as Node attributes (not a typed field)

**Status:** accepted
**Date:** 2026-05-04
**Stage:** 2 — Contract nodes
**Supersedes:** —

## Context

ROADMAP Stage 2 says "the graph marks declared contract nodes with a
`IsContract: true` attribute." Issue #3 asks where this lives: a
dedicated `IsContract bool` field on `graph.Node`, or a slot inside
the existing `Attrs map[string]any`.

Stage 4+ will layer additional annotations on the same nodes
(motif role, anomaly score, transformation candidacy). Each new
attribute as a struct field would mean a schema bump in
`graph.JSON.Version` and a migration in any consumer. That's the
wrong direction.

## Decision

Reuse the existing `Node.Attrs map[string]any`. Stage 2 writes:

```go
node.Attrs["isContract"] = true
node.Attrs["contractKind"] = "interface" // or "type"
node.Attrs["contractSource"] = "config"  // future: "comment", "inferred"
```

We add a small typed accessor in `internal/graph` so callers don't
type-assert by hand:

```go
func (n Node) IsContract() bool
func (n Node) ContractKind() string
```

The accessors return zero values when the attribute is absent. Writers
go through dedicated helpers in `internal/contracts/mark.go`
(`Mark(g, id, kind, source)`) to keep the key names in one place.

JSON schema does **not** bump. The Stage 1 reader treats unknown
attribute keys as opaque, so existing consumers continue to work.

Embedding propagation (issue #3, "yes — if interface B embeds A and A
is a contract, B's contract-typed members are transitively
contract-relevant") is implemented as a graph traversal at marking
time: after marking the explicitly-declared interfaces, we walk
inbound `Embeds` edges and propagate the contract attribute to the
embedder, recording `contractSource: "embedded"` and
`contractEmbeds: <originID>` for traceability.

## Alternatives considered

- **Dedicated `IsContract bool` field on `Node`.** Simple, type-safe,
  and JSON-stable. But each new annotation kind (motif role,
  anomaly score, …) demands another field, the JSON schema version
  bumps every time, and Stage 1 consumers may break. Rejected.
- **Separate `Annotations` map keyed off `NodeID`, kept outside the
  graph.** Avoids touching `Node`, but every consumer would have to
  thread two parallel data structures. The `Subgraph` helper in
  particular would need to copy annotations alongside nodes. Rejected
  as accidental complexity.
- **A typed enum field `ContractKind` plus `IsContract bool`.** Mid-
  ground; still bumps schema. Rejected for the same reason.

## Consequences

- Schema-stable: Stage 1 JSON consumers see the new keys as opaque
  payload they can ignore.
- Type-unsafe access (`map[string]any`) is bounded by the accessor
  helpers; callers outside `internal/contracts/` should not poke
  attribute keys directly.
- Stage 4+ follows the same pattern: write under a documented key
  (`motifRole`, `anomalyScore`), expose a typed accessor, never bump
  JSON version unless we change semantics.
- A consumer that wants to query "all contract nodes" uses
  `graph.NodesByKind` plus the `IsContract()` accessor, or the helper
  `contracts.AllContracts(g)` which is provided for ergonomics.
