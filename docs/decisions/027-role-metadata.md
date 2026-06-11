# ADR-027 — Architecture role metadata

**Status:** accepted
**Date:** 2026-05-05
**Stage:** 1.5 — Graph annotations
**Supersedes:** —

## Context

ROADMAP and issue #28 ask for architecture role metadata to flow
through the typed graph and its exports. Contracts (Stage 2,
ADR-008/009) answer "is this part of a stable interface?" Roles
answer a different question: "where does this node live in the
architecture?" — domain, application, inbound/outbound adapter,
infrastructure, shared.

Downstream issues (#29 motif templates, #30 layer-aware modularity,
#31 anomaly explanations) consume role metadata. We want the schema
fixed before those land so the producers don't churn.

Two modelling choices to settle:

1. **Where does the role live?** New typed `Role` field on `graph.Node`
   vs. another key in the existing `Node.Attrs` map (the path
   ADR-009 took for contracts) vs. a side-table on `Graph`.
2. **How does it interact with the existing `IsContract` flag?**
   Fold contract into role, or keep them as orthogonal axes.

## Decision

**Roles live in `Node.Attrs` under stable keys**, exposed via typed
`Node.Role()` and `Node.RoleSource()` accessors and a single
`Graph.SetRole(id, role, source)` writer. Same pattern as ADR-009 —
JSON schema does not bump; consumers that don't care about roles
ignore the new keys.

**Roles and contracts are orthogonal.** A node can be a contract *and*
have a role; they answer different questions. We keep the existing
`IsContract` machinery unchanged. We do **not** introduce a synthetic
`role: "contract"`. Tools that want both axes read both attributes.

### Schema additions to `.archmotif.yaml`

```yaml
roles:
  packages:
    - {pattern: "internal/domain/**", role: domain}
    - {pattern: "internal/adapters/**/inbound/*", role: inbound_adapter}
  types:
    - {qualified: "internal/domain.User", role: domain_entity}
    - {pattern: "*Request", role: adapter_dto}
```

- Allowed package roles: `domain`, `application`, `inbound_adapter`,
  `outbound_adapter`, `infrastructure`, `shared`.
- Allowed type/symbol roles: `domain_entity`, `value_object`, `port`,
  `adapter_dto`, `config_contract`, `external_noise`.
- Each selector specifies exactly one of `pattern:` (glob over
  package import path / file path / qualified name) or `qualified:`
  (exact `pkg/path.Name` match).
- The role string is constrained at config-load time. Unknown values
  are rejected before any graph work happens.

### Selector precedence (high to low)

1. Explicit type/symbol selector match (`roles.types`).
2. Explicit package selector match (`roles.packages`).
3. Inferred (reserved; currently no auto-inference).

The resolver in `internal/roles` walks each node, applies package
selectors first, then overlays type-selector matches on top. The
role's source (`type`, `package`, `inferred`) is recorded in
`role_source` so consumers can filter by provenance.

### Exports

- **JSON.** Roles appear under `attrs.role` and `attrs.roleSource`
  per node. No version bump (ADR-009 contract pattern).
- **GraphML.** Two new node `<key>` declarations are emitted upfront:

  ```xml
  <key id="n_role" for="node" attr.name="role" attr.type="string"/>
  <key id="n_role_source" for="node" attr.name="role_source" attr.type="string"/>
  ```

  and per-node `<data key="n_role">domain</data>` is written when
  the role is set.

## Alternatives considered

- **Separate `Roles map[NodeID]Role` side-table on `Graph`.** Cleaner
  type-wise — no `any` round-tripping — but every consumer
  (`Subgraph`, JSON, GraphML, future motif/anomaly stages) would
  need to thread a parallel data structure. ADR-009 already chose
  Attrs for contracts; consistency wins. Rejected.
- **New `Role` field on `Node`.** Type-safe but bumps JSON schema and
  forces every Stage 1 consumer to migrate. Rejected for the same
  reason ADR-009 rejected `IsContract bool`.
- **Fold contract into role (`role: "contract"`).** Loses information
  — a domain entity that is also a contract can't be expressed —
  and breaks every existing Stage 2 consumer. Rejected.

## Consequences

- `internal/contracts.Config` gains a `Roles` field with `Packages`
  and `Types` selector lists, validated at load time against the
  fixed allowed-value sets.
- `internal/roles` is the single resolver/writer. Outside callers
  read roles via `Node.Role()` / `Node.RoleSource()`; they don't
  poke Attrs by hand.
- JSON consumers see new `attrs.role` / `attrs.roleSource` keys as
  opaque payload they can ignore — same forwards-compat story as
  ADR-009.
- GraphML consumers must declare new `<key>` ids for `n_role` and
  `n_role_source`; the exporter does this unconditionally so loading
  graphs without role data still validates against the schema.
- Future stages (#29–#31) read roles via the typed accessor and
  branch on `Node.Role()` without touching Attrs internals.
