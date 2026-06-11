# ADR-034 — Archai bridge: graph-to-architecture-model export

**Status:** accepted
**Date:** 2026-05-06
**Stage:** post-Stage-9 — cross-tool bridge (issue #30)
**Builds on:** ADR-005 (node id format), ADR-009 (contract attributes),
ADR-016 (skeleton format / layered views), ADR-027 (role metadata)

## Context

Issue #30 asks for a one-way bridge from the archmotif typed graph
(packages, files, types, functions, methods, control-flow primitives,
typed edges) to a domain-oriented architecture model that Archai can
visualize and reason about. The two tools have complementary strengths:

- **archmotif** is graph-first; it owns parsing, type resolution,
  control-flow primitives, role metadata, and contract markers.
- **Archai** is model-first; it owns dashboards, package/symbol
  browser views, diagrams, and overlays oriented around domain
  concepts (kgatilin/archai #55, #59, #60).

We do not want to couple the two projects directly. archmotif should
not import an Archai schema, and Archai should not parse archmotif
graph JSON. Instead we publish a stable intermediate model document
that both sides can produce/consume, with archmotif as the producer
today and (optionally) Archai as a consumer tomorrow.

Three constraints shaped the design:

1. **Stable across runs.** A consumer that fingerprints the document
   must see byte-identical bytes for the same input.
2. **Lossless on identity.** Every model element preserves the
   archmotif node id so a model browser can hop back to the typed
   graph without re-resolving by name.
3. **No new vocabulary.** The mapping uses Archai's existing
   package/symbol/dependency/implementation/stereotype/facet
   vocabulary. We don't invent a new schema dialect for the bridge.

## Decision

### 1. New subcommand `archmotif export --format archai-model`

The export lives in its own subcommand rather than as a flag on
`archmotif graph`. `graph` is the raw typed graph surface; `export`
is the projection-for-other-tools surface. Mixing the two would make
the flag semantics fuzzy, especially as more export formats land
(Mermaid, PlantUML, Archai overlays).

```
archmotif export [--encoding json|yaml] [--pattern PAT] [--tests] <path>
```

### 2. Document structure

A single top-level `ArchitectureModel` value with seven sections:

| Section | Source | Purpose |
|---|---|---|
| `schema` | static | document family + version |
| `source` | command | producer + counts (smoke check) |
| `facets` | static | view-layer catalogue |
| `stereotypes` | derived | annotation catalogue (roles, contracts) |
| `packages` | Package nodes | architecture units |
| `symbols` | Type/Function/Method/Field nodes | members of a package |
| `dependencies` | edges | typed directed links |

Schema id is `archmotif.archai-model`, current version `1`.

### 3. Mapping table

| archmotif | model entity | model field |
|---|---|---|
| Package node | `Package` | one entry; Foreign flag preserved |
| Type node | `Symbol` (kind=`type`) | facet=`model` |
| Function node | `Symbol` (kind=`function`) | facet=`behavior` |
| Method node | `Symbol` (kind=`method`) | facet=`behavior` |
| Field node | `Symbol` (kind=`field`) | facet=`model` |
| File / Loop / Branch / Goroutine / Defer / ChannelOp / SyncPrim | dropped | (Archai vocabulary stops at the symbol level) |
| `contains` (pkg→sym) | `Package.Symbols` + `Dependency` | both surfaces |
| `dependsOn` | `Dependency` | relation=`depends_on` |
| `calls`, `callsFrom` | `Dependency` | relation=`calls` (kind preserved verbatim) |
| `references` | `Dependency` | relation=`references` |
| `usesType` | `Dependency` | relation=`uses_type` |
| `returns` | `Dependency` | relation=`returns` |
| `embeds` | `Dependency` | relation=`embeds` |
| `implements` | `Dependency` | relation=`implements`, `isImplementation=true` |
| Package role (ADR-027) | `Package.Layer` + `role:<name>` stereotype | string + array |
| Type role (ADR-027) | `role:<name>` stereotype | array |
| `IsContract` (ADR-009) | `Symbol.IsContract` + `contract` stereotype | bool + array |
| Node ID (ADR-005) | `Package.ID` / `Symbol.ID` / `*.ArchmotifID` | string |
| QName | `Symbol.QName` | string |
| Position | `Symbol.Position` | optional struct |

#### Facets

Facets mirror the GraphML view-layer attributes shipped in commit
fc7cc1c so two projections of the same graph stay aligned. The
catalogue is static:

- `structure` — packages, files, top-level decls
- `model` — types, fields, embeds
- `behavior` — functions and methods
- `control` — loops, branches, defers
- `concurrency` — goroutines, channel ops, sync primitives

Symbols carry a single `facet` field; dependencies do not (the
producer joins on endpoint facets when needed).

#### Stereotype encoding

We chose `role:<name>` rather than bare role names so the same
namespace can hold:

- role-derived stereotypes (`role:domain`, `role:value_object`, …),
- contract markers (`contract`),
- future inferred stereotypes without colliding with role values.

### 4. Determinism

`archai.FromGraph` sorts:

- `Packages` by `ID`,
- `Symbols` by `ID`,
- `Dependencies` by composite ID (`from + kind + to`),
- per-package `Symbols` and `Stereotype` slices,
- per-symbol `Stereotype` slice.

Stereotype catalogue is built from a map then sorted before emission.
Two `FromGraph` calls on the same graph produce byte-identical JSON
(verified by `TestFromGraph_Determinism` and the CLI integration test).

### 5. Traceability

Every Package and Symbol carries an `archmotifId` field that equals
its ID. We surface it as a separate field — even though it duplicates
ID today — so a future consumer that re-keys the document under a
different scheme can still recover the original archmotif node id.
Dependencies carry no separate id but encode `from`, `to`, and `kind`
explicitly.

## Alternatives considered

- **Reuse `archmotif graph --format=json`.** Cheapest to ship but the
  consumer would have to walk a typed graph and run the projection
  itself. The whole point of #30 is to *publish* the projection.
  Rejected.
- **Emit GraphML with archai-flavoured keys.** GraphML is already our
  visualization-tool format (Gephi, yEd). Overloading it with Archai
  semantics conflates two audiences. Rejected.
- **Embed the full graph (incl. control-flow nodes).** Archai's
  vocabulary stops at the symbol level. Surfacing every loop and
  defer as a model entity would dilute the diagrams it's built to
  render. Control-flow remains accessible via the typed graph.
  Rejected.
- **Pull in `gopkg.in/yaml.v3` for YAML emission.** The model is
  small, with no cycles or polymorphism, so a hand-rolled emitter
  is straightforward. We can swap libraries without touching the
  schema if the document grows. Deferred.

## Consequences

- A new `internal/archai` package owns the schema and projection.
  No other package imports it; the CLI is the single consumer today.
- `archmotif export --format archai-model` is the supported entry
  point. The format flag is required (not defaulted) so future
  formats can join without ambiguity, but `archai-model` is the only
  legal value at v1.
- Snapshot tests pin the projection of a hand-built fixture graph
  (`internal/archai/testdata/fixture.archai-model.json`). Bumping
  `CurrentSchemaVersion` requires updating the snapshot.
- Downstream Archai work (kgatilin/archai #55, #59, #60) can consume
  the document without further coordination on archmotif's side.
  Schema bumps will be announced by versioning the `schema.version`
  field; consumers reject unknown versions.
- Roles, contracts, and view-layer facets are surfaced together in
  one document. Tools that previously had to merge GraphML view
  layers, JSON role attrs, and Stage 2 contract markers now read a
  single source.
