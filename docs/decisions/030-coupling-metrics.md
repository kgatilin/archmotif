# ADR-030 — Layer-aware coupling metrics, forbidden edges, JSON + Markdown reports

**Status:** accepted
**Date:** 2026-05-05
**Stage:** post-Stage-3 / post-Stage-4 architecture-health pass — issue #29
**Builds on:** ADR-027 (role metadata), ADR-009 (contract attributes), ADR-015 (metric output schema)

## Context

Issue #29 asks for **deterministic** architectural health signals on
top of the role metadata that landed in #28 / ADR-027:

> Once graph nodes have architecture roles, ArchMotif should be able
> to compute deterministic health signals: which roles depend on
> which other roles, where dependency direction is suspicious, and
> which package/type nodes act as architectural bridges.

Concrete deliverables on the issue:

- Role-pair dependency matrix
- Forbidden-edge count + evidence
- Domain-purity / adapter-isolation scores
- Cross-package centrality (excluding configured noise nodes)
- JSON + Markdown output
- GraphML attributes for selected derived scores

ADR-027 already standardised the `role` Attrs key on every node and
the package/type selector vocabulary that drives it. The runner +
metric framework (ADR-011, ADR-015) is shaped for *one number per
record*; coupling output is richer (per-pair counts, evidence lists,
named scores), so a dedicated package + command is the right shape.

Three independent decisions were on the table:

1. **Where the code lives.** New `internal/coupling/` package + new
   `archmotif coupling` command, or fold into `internal/metrics`?
2. **Which edges contribute** to coupling counts? (Containment isn't
   coupling. CallsFrom would double-count Calls.)
3. **Scope cut for v1.** The issue lists six deliverables. What's in
   the first PR, what's deferred?

## Decision

### 1. New package `internal/coupling/` and CLI command `archmotif coupling`

Coupling reports are per-pair (and per-edge for evidence), not
per-node + scalar. The metrics framework (ADR-011 / ADR-015) commits
to "one Record per (metric, scope, target) triple"; squeezing a
role-pair matrix into that shape would either flatten the matrix
into N×N records or stuff the matrix into `Details`, and either
loses the natural shape.

Putting coupling in its own package keeps the metric runner clean
and gives the coupling report room to grow (centrality, GraphML
attributes) without bloating the metric vocabulary.

Public surface:

```go
package coupling

type Config struct {
    Forbidden  []ForbiddenEdge
    EvidenceCap int  // per-pair evidence list cap; default 5
}

type ForbiddenEdge struct {
    From   graph.Role
    To     graph.Role
    Reason string
}

type Compute(g *graph.Graph, cfg Config) Report

type Report struct {
    PairCounts          []PairCount
    ForbiddenViolations []EdgeEvidence
    Scores              []Score
    UnroledEndpoints    int
}
```

Renderers in `format.go`: `RenderJSON`, `RenderMarkdown`. Same shape
as the existing `verify.FormatText` / `FormatJSON` split.

CLI:

```
archmotif coupling [flags] <path>
  --format=json|markdown   default: json
  --evidence-cap=N         override config (default: 5)
  --pattern=...            go/packages pattern
  --tests                  include _test.go in pipeline
```

Exit codes:
| Code | Meaning |
|------|---------|
| 0    | Report rendered (forbidden violations are surfaced, not exit-fatal) |
| 1    | Pipeline / parse error |
| 2    | Argument or load error |

Non-zero exit on forbidden violations was tempting (CI could fail
the build) but is **deferred**: the v1 user is exploring the report,
not gating CI on it. A future flag `--fail-on-forbidden` is the
natural seam.

### 2. Edge-kind filter: structural relations only, no containment, no callsFrom

Coupling counts edges that represent **dependency** relations, not
structural nesting. Concretely the report consumes these edge kinds:

| Kind | In matrix? | Rationale |
|------|------------|-----------|
| `dependsOn`   | yes | package-level imports — the coarse signal |
| `implements`  | yes | concrete → interface dependency |
| `embeds`      | yes | struct/interface composition |
| `calls`       | yes | function-level call graph |
| `references`  | yes | function-as-value usage |
| `returns`     | yes | function returns type from another role |
| `usesType`    | yes | function declares / converts to / asserts a type |
| `contains`    | **no** | structural nesting, not architectural coupling |
| `callsFrom`   | **no** | duplicates `calls` once we attribute by enclosing function |

`callsFrom` is excluded to avoid double-counting: `calls` already
records the function-to-callee edge, and `callsFrom` is the same
relation re-routed through the enclosing control-flow primitive
(loop / branch / goroutine). For a coupling report at function /
package granularity, the primitive isn't part of the picture.

Containment edges are excluded for the same reason: a package
*containing* a struct is not the package *depending on* the struct
in any architectural sense.

The set is exposed as `coupling.DefaultEdgeKinds()` so future passes
that want a different cut (e.g. data-flow only) can reconfigure
without forking the package.

### 3. Scope cut for v1: matrix + forbidden + scores; defer centrality + GraphML

Issue #29 lists six deliverables. The first PR ships:

- Role-pair dependency matrix (every directed pair seen in the
  graph, with count + capped evidence)
- Forbidden-edge config from `.archmotif.yaml` + violation list with
  evidence
- Two named scores:
  - `domain_purity`: fraction of edges *out of* `domain`-roled
    nodes whose target is also `domain` (or a `value_object` /
    `domain_entity` type role). Higher = purer.
  - `adapter_isolation`: fraction of edges *out of* `adapter_dto`
    typed nodes whose target stays inside adapter / infrastructure
    layers. Higher = more isolated.
- JSON output (machine-readable, stable order)
- Markdown output (human summary table)
- CLI command `archmotif coupling`

Deferred to follow-up tickets (and noted as such in the README /
issue update once landed):

- **Cross-package centrality** with configurable exclusion of noise
  nodes. The hard part is picking the centrality definition
  (PageRank vs betweenness vs closeness — see ADR-012 for the
  spectral conversation). v1 already exposes a per-pair count which
  is the lower bound a centrality pass would consume; doing it well
  warrants its own ADR.
- **GraphML attribute output** for derived scores. The current
  GraphML export (ADR-016 + later) is graph-wide and per-node /
  per-edge; coupling scores live at the pair level, so the natural
  surface is a sidecar file or a new `archmotif coupling --format=graphml`
  that emits a derived graph (one node per role, one edge per pair).
  Both options are possible; pick when a real consumer asks.

The two scores chosen for v1 (`domain_purity`, `adapter_isolation`)
are the ones the issue explicitly named; the remaining three
("forbidden-edge count" — that's a violation list, not a score; the
matrix itself; centrality — deferred) all map to deferred work
above.

### 4. Edge attribution rule when an endpoint has no role

Every edge contributes to exactly one matrix cell `(roleFrom, roleTo)`.
The role of an endpoint is resolved by:

1. The endpoint node's own `Role()` if set.
2. Otherwise the role of the endpoint's containing package node, if
   the package has a role.
3. Otherwise `unknown`.

Step 2 inheritance keeps the matrix populated when the user only
declares package-scoped selectors (the common case — most projects
won't enumerate every type). Step 3 surfaces coverage gaps: edges
that land in `(unknown, *)` or `(*, unknown)` are still counted but
roll up to the bottom of the matrix and contribute to the report's
`UnroledEndpoints` total so the operator can tell whether their
config covers the codebase.

Edges whose endpoint has role `external_noise` are **excluded
entirely** from counts and from the scores. ADR-027 declared
`external_noise` as the canonical "this isn't part of the
architecture" marker; the v1 coupling report honours it without
needing additional `--exclude` flags. Other exclusion needs are
covered by the existing `graph.exclude` machinery (ADR-008).

### 5. `.archmotif.yaml` schema extension

```yaml
coupling:
  forbidden:
    - {from: domain, to: outbound_adapter, reason: "domain may not depend on adapters"}
    - {from: domain, to: infrastructure}
  evidence_cap: 5   # optional; default 5
```

Validation rules (in `contracts.Config`):

- `from` and `to` must be one of the allowed package roles
  (per-pair role-kind matching is open: a package role can forbid
  edges that land in a type-roled node, since type-roled nodes still
  inherit a package role).
- `reason` is optional. Empty reasons render as the canonical text
  "edge from <from> to <to> is forbidden" in Markdown / JSON.
- `evidence_cap` defaults to 5; values < 0 are rejected.

## Alternatives considered

- **Fold into `internal/metrics`.** Forces the matrix and scores
  into the per-record schema (ADR-015) and bloats the metric
  vocabulary. Rejected (decision 1).
- **Include `contains` and `callsFrom`.** Inflates counts, dilutes
  signal. Rejected (decision 2).
- **Ship centrality + GraphML in the first PR.** Doubles the surface
  and burns the cycle on choices that warrant their own ADR.
  Rejected (decision 3).
- **Hard-fail on forbidden violations by default.** Useful as a CI
  gate, premature as a default. Rejected for v1 (left as a future
  `--fail-on-forbidden` flag).
- **Exclude unroled endpoints from the report entirely.** Hides
  config gaps. Rejected; we count them and surface a total.

## Consequences

- New package `internal/coupling/` and CLI command `archmotif
  coupling`. No changes to the existing metric vocabulary; the
  metric runner is untouched.
- `.archmotif.yaml` gains a `coupling.forbidden` block with strict
  validation; existing configs without the block continue to load
  with empty Config.Coupling.
- The `domain_purity` and `adapter_isolation` scores commit
  archmotif to specific definitions; revisiting them is a new ADR.
- Future work tracked: cross-package centrality (own ADR), GraphML
  attribute output (own ADR), `--fail-on-forbidden` gate (small
  follow-up).
- The role-attribution inheritance rule (decision 4) means a project
  that only sets package roles still gets a populated matrix. The
  trade-off: type-level overrides via ADR-027's `roles.types` block
  remain authoritative for individual nodes; package inheritance is
  the fallback, never the primary signal.
