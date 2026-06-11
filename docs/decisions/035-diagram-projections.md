# ADR-035 — Diagram projections from typed graph layers

**Status:** accepted
**Date:** 2026-05-06
**Stage:** post-Stage-3 visualisation pass — issue #31
**Builds on:** ADR-005 (node IDs), ADR-009 (contract attributes),
ADR-027 (role metadata), ADR-030 (coupling metrics).

## Context

Issue #31 asks ArchMotif to project the typed graph into deterministic
diagram-shaped slices that humans can actually read. A whole-codebase
GraphML export is great for Gephi exploration, but it does not answer
the architectural questions an operator asks day-to-day:

- "Which packages depend on which?"
- "What are my contracts and ports, and who implements them?"
- "What does the call graph rooted at this entrypoint look like?"
- "Where does my domain core live, and what's adjacent to it?"
- "Where do my adapters touch DTOs / requests / responses?"
- "Where is the concurrency surface (goroutines, channel ops, mutexes)?"

Each of these is a filter over the same typed graph, but the filter
rules differ enough that asking users to compose them by hand defeats
the purpose. They also need to render in three different downstream
contexts: D2 source for static diagrams, JSON for the planned Archai
browser, and a GraphML subgraph so existing Gephi templates keep
working.

Two things stay fixed across every projection:

1. **Determinism.** The same input graph plus the same options must
   produce byte-identical output across runs. Snapshot tests assert
   this; downstream callers depend on it.
2. **Evidence preservation.** Every projection node and edge must
   point back to the underlying graph node IDs and edges by stable
   ID (per ADR-005). This is what lets a diagram be a navigation
   surface, not a dead artefact.

The non-goals are equally clear: this work does not replace the full
GraphML export, does not depend on Gephi being installed, and does
not invent new architecture markers — it only filters and shapes
what the graph already carries (roles from ADR-027, contracts from
ADR-009, coupling-relevant edge kinds from ADR-030).

## Decision

### 1. New `internal/diagram/` package, new `archmotif diagram` command

Coupling reports (ADR-030) and pattern reports (ADR-026) each
landed in their own package because their output shape differs from
the metric runner's "one number per record" envelope. Diagram
projections are different again: the natural shape is a small
directed graph (`Diagram` with `DiagNode` / `DiagEdge`) rather than
a number, a record, or a markdown table. They get their own
package.

Public surface:

```go
package diagram

type Kind string         // package-deps, contract-port, call-flow, ...
type Format string       // d2, json, graphml

type Diagram struct {
    Kind  Kind
    Title string
    Nodes []DiagNode
    Edges []DiagEdge
    Notes []string       // operator-facing diagnostics
}

type DiagNode struct {
    ID, Label, Cluster string
    Kind  graph.NodeKind
    Role  graph.Role
    EvidenceIDs []string
}

type DiagEdge struct {
    From, To, Label string
    Kind  graph.EdgeKind
    Count int
    EvidenceIDs []string
}

func Build(g *graph.Graph, kind Kind, opts Options) (*Diagram, error)
func Render(w io.Writer, d *Diagram, f Format) error
```

CLI: `archmotif diagram <kind> [--format d2|json|graphml]
[--seed=<qname>] [--depth=N] [--include-foreign] [--list] <path>`.
Flags can appear before or after the kind positional so neither
muscle-memory direction is wrong.

### 2. Three projections in v1, framework supports the rest

The issue lists six projection kinds. We ship the projection
**framework** plus the three projections that exercise the most
distinct filter rules:

| Kind | Filter rules |
|------|-------------|
| `package-deps` | keep `NodePackage`, drop foreign by default, project `EdgeDependsOn` between kept packages, cluster by `Role()` |
| `contract-port` | keep nodes where `IsContract()` OR role ∈ {port, domain_entity, value_object}; pull in `EdgeImplements` neighbours so concrete adapters render alongside interfaces; cluster by containing package |
| `call-flow` | resolve seeds (exact ID, QName, or QName-suffix match); auto-pick `main` / `Run` / `Serve` when no seeds match; BFS forward along `EdgeCalls` ∪ `EdgeCallsFrom` up to `--depth` (default 3); drop foreign by default |

The remaining three (`domain-core`, `adapter-dto`, `concurrency`)
are deferred. Each is a thin specialisation of the same primitives:
`domain-core` is a role-set filter (domain / value_object / port)
plus connecting edges; `adapter-dto` is a role-set filter
(adapter_dto, inbound/outbound_adapter); `concurrency` keeps
`NodeGoroutine`, `NodeChannelOp`, `NodeSyncPrim` plus their
`EdgeContains` parents. They reuse the existing `Diagram` shape,
the renderers, and the seed/cluster helpers — adding them is a
copy of `package_deps.go` with the predicate swapped.

### 3. Filter-rule contract

Every projection follows the same three-stage shape so new kinds
remain mechanical to add:

1. **Select primaries.** A predicate over `graph.Node`. For
   `package-deps` it is `n.Kind == NodePackage && (!foreign ||
   IncludeForeign)`. For `contract-port` it is `IsContract() ||
   Role() ∈ {port, domain_entity, value_object}`.
2. **Pull adjacency.** Optionally walk one or more edge kinds to
   bring in supporting nodes (e.g. `EdgeImplements` for
   `contract-port`, BFS along `EdgeCalls`/`EdgeCallsFrom` for
   `call-flow`). The walked edges become diagram edges; the visited
   nodes get added to the kept set.
3. **Materialise.** For each kept node emit a `DiagNode` whose
   `EvidenceIDs` = `[n.ID]`; for each kept edge emit a `DiagEdge`
   whose `EvidenceIDs` = `["from>to>kind"]` (matching the edge
   evidence shape used by ADR-030's coupling report so downstream
   tools can join across reports). Sort nodes by `(Label, ID)` and
   edges by `(From, To, Kind)` before returning.

`Cluster` is set to a stable string per node — package role for
`package-deps`, containing package label for everything else — so
the D2 renderer can lay out visual groups without the projection
having to know about D2.

### 4. Renderers

Three renderers, all stable byte-for-byte under fixed input:

- **D2** (`RenderD2`). Default for the CLI because diagrams are the
  point. Quotes every ID so colons / slashes from ADR-005 IDs
  survive D2 lexing. Emits clusters as containers when present;
  unclustered nodes render at the top level.
- **JSON** (`RenderJSON`). Versioned envelope (`{version: 1,
  diagram: ...}`) with two-space indent, no HTML escaping. Suitable
  for the Archai browser and for snapshot diffs.
- **GraphML** (`RenderGraphML`). Mirrors the key-id vocabulary used
  by `internal/graph`'s full GraphML writer where applicable, plus
  a per-node `evidence_ids` data attribute and a per-edge
  `evidence_ids` so the subgraph round-trips through Gephi without
  losing the link back to the source graph.

### 5. Determinism guarantees

- Projections walk the source graph in `g.Nodes()` insertion order
  (already deterministic per `internal/graph`'s sort).
- Final node/edge slices are sorted by stable keys before render.
- D2 cluster groups render in alphabetical order, with the empty
  cluster first so unclustered nodes appear at the top of the file.
- Snapshot tests in `internal/diagram` assert byte-for-byte D2
  output for `package-deps`; `cmd/archmotif/diagram_test.go`
  asserts JSON envelope shape and GraphML well-formedness on a
  real-loaded fixture.

## Alternatives considered

- **Reuse the metric runner (ADR-011 / ADR-015).** Same reason
  ADR-030 declined: per-node scalar shape doesn't fit a graph
  output.
- **Always export full GraphML and rely on Gephi filters.** Already
  the current state. Issue #31 exists because that workflow is too
  manual; users want a one-shot CLI that emits a focused diagram.
- **One mega-projection with config-driven filter rules.** Tempting
  for flexibility but every projection ends up wanting bespoke
  labelling and cluster heuristics (see `[seed]` prefix on
  call-flow nodes, `cluster=role` for package-deps vs.
  `cluster=package` for contract-port). Splitting projections by
  kind keeps each one short.
- **Render straight to SVG.** Deferred. D2 source is the right
  serialisation boundary: it's diff-friendly, version-controllable,
  and `d2` itself can produce SVG / PNG on demand without putting a
  rendering dependency in archmotif.

## Consequences

- New CLI surface is committed: `archmotif diagram <kind>`. We can
  add new kinds without breaking it; renaming an existing kind
  would be a breaking change.
- The JSON envelope shape (`{version, diagram}`) is committed at
  v1. Adding new fields to `DiagNode` / `DiagEdge` is non-breaking
  if existing fields keep their types and JSON tags.
- The GraphML key vocabulary is committed (`n_label`, `n_id`,
  `n_kind`, `n_role`, `n_cluster`, `n_evidence`, `e_label`,
  `e_kind`, `e_evidence`). Adding new keys is non-breaking;
  renaming or retyping existing keys would break Gephi templates.
- Three projections are deferred (`domain-core`, `adapter-dto`,
  `concurrency`). Tracked as TODOs on the issue / PR; each one is
  a self-contained follow-up that reuses the framework.
- Role resolution runs at the CLI call site (same pattern as
  `archmotif coupling`) because `contracts.Build` skips it to
  avoid an import cycle. Future refactor candidate: lift role
  resolution into `contracts.Build` once that cycle is resolved.
- Determinism is now a load-bearing property of three packages
  (`coupling`, `patterns`, `diagram`). When someone introduces map
  iteration or unsorted output anywhere in the projection
  pipeline, the snapshot tests must catch it; if they don't, the
  test fixtures need to grow until they do.
