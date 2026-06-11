# ADR-015 — Metric output schema

**Status:** accepted
**Date:** 2026-05-04
**Stage:** 3 — Metrics infrastructure
**Supersedes:** —

## Context

Issue #4 specifies: "structured JSON, one record per (metric, scope,
target) where scope is `node | edge | region | graph` and target is
the node ID, edge ID, subgraph id, or empty. Each record includes:
`metric`, `scope`, `target`, `value`, optionally `details: {...}`."

The schema needs to support Stage 4 (anomaly detection): rank by
metric, slice by scope, link a record back into the graph by target.
It also needs to be stable enough that downstream consumers (Stage 5
proposals, Stage 8 verification) don't bend on every metric we add.

A second question: do we ship a flat list of records, or group by
metric? Grouping is friendlier to humans but harder to stream and
filter. We aren't streaming for Stage 3; the file is small.

## Decision

Schema:

```json
{
  "version": 1,
  "records": [
    {
      "metric":  "cycle_rank",
      "scope":   "graph",
      "target":  "",
      "value":   2.0,
      "details": {"edgeKinds": ["calls", "dependsOn"]}
    },
    {
      "metric":  "cycle_rank",
      "scope":   "region",
      "target":  "scc-0",
      "value":   3.0,
      "details": {"members": ["pkg:foo:type:Bar", "..."]}
    }
  ],
  "ran":    ["cycle_rank", "spectral_gap", ...],
  "errors": []
}
```

Decisions inside the schema:

- **Flat list.** `records[]` is one flat array. Stage 4 can sort by
  any field without restructuring. The runner sorts by `(metric, scope,
  target)` before serialising for a deterministic on-disk form.
- **Single `value` field is `float64`.** All Stage 3 metrics produce
  a numeric scalar. Where a metric naturally returns multiple values
  (eigenvalue spectrum), put the extras in `details`.
- **`target` semantics.**
  - `node`: the stable node ID (per ADR-005). Plain string.
  - `edge`: synthesised as `<from>--<edgeKind>-->`. No Stage 3
    metric currently emits edge-scope records, but the schema slot
    is reserved.
  - `region`: a metric-assigned ID like `scc-0`, `motif-3`, or the
    package node ID for modularity communities. Stable within a run
    so a follow-up tool can resolve `details.members` back into
    nodes.
  - `graph`: empty string.
- **`details` is `map[string]any`.** Trades type-safety for
  forward-compat. JSON marshals it cleanly; Go consumers do typed
  decode if they care. ADR-009 made the same call for `Node.Attrs`;
  this is the same pattern at the metrics layer.
- **`ran` lists metrics that produced records.** Errored metrics
  appear in `errors[]` instead. Lets Stage 4 know what the run
  *attempted* without inferring from the records list (a metric can
  legitimately produce zero records).
- **Versioned.** `version: 1`. Bump on breaking shape changes.
  Stage 4 must check the version on read.

## Alternatives considered

- **Group by metric: `{"cycle_rank": {...}, "spectral_gap": {...}}`.**
  Friendlier to a human reading the JSON but harder to filter and
  forces consumers to know every metric name in advance. Rejected
  for the JSON form; the `--format=pretty` output groups for the
  human reader.
- **Separate value type per scope.** E.g. a node-scope record's
  value is a per-node float64, region-scope is a struct. Cleaner in
  Go, awful in JSON because the consumer can't decode without
  branching on scope. Reject; one numeric value plus details is
  enough.
- **Structured `target`** (`{"kind": "node", "id": "..."}`). More
  explicit but every consumer pays the destructure cost. Plain string
  works because each (scope) implies a target shape.
- **Drop `details`.** Saves bytes but loses information that Stage 4
  almost certainly needs (motif member lists, eigenvalue spectra,
  SCC participants). Keep.

## Consequences

- Stage 4 will read the schema and emit its own anomaly schema with
  references back to (metric, scope, target). Stage 5 proposals will
  link to anomalies, which link to records, which link to graph
  nodes. The chain is the closed loop documented in concepts.md §7.
- Adding a new scope kind (e.g. `path` for spectral path-bottleneck
  reports) is non-breaking — `Scope` is a string, so old consumers
  see an unrecognised value and skip.
- Stage 3 fixtures live as Go builders in
  `internal/metrics/metricstest/builders.go` rather than under
  `testdata/`: Go's toolchain excludes `testdata/` from compilation,
  and the metrics consume `*graph.Graph` instances rather than JSON.
  Closed-form fixtures (pure path, pure clique) are built ad-hoc in
  the test files via `helpers_test.go`.
