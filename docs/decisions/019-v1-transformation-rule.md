# ADR-019 — v1 transformation rule = extract-interface from motif redundancy

**Status:** accepted
**Date:** 2026-05-05
**Stage:** 5 — Local transformation proposals (spec + stub)
**Supersedes:** —

## Context

ROADMAP Stage 5 requires per-anomaly transformation rules. The
research-questions doc and Stage 5's "open questions" both note that
**one** rule is sufficient for v1 — adding more rules later is cheap if
the layout is right. Three rules were considered:

| Rule | Trigger metric | Demo strength | Implementation cost | Verifier complexity |
|---|---|---|---|---|
| **Extract interface from repeated motif** | `motif_redundancy` (Stage 3) | high — concepts.md §5 example, archmotif itself likely has 3+ candidates | small — output is a fixed shape (interface + N impls + Implements edges) | low — verifier checks one interface + N impls + edges |
| Dependency inversion for cycle | `cycle_rank` | medium — must choose which edge to invert; design intent matters | medium-high | medium |
| Pass-as-arg for shared mutable state | `spectral_gap` | weak — needs data flow tracking (out of scope per concepts.md §5) | high | high |

The decision had to also pin the Proposal/TargetSubgraph types up front:
issue #16 (skeleton renderer) and issue #18 (verifier) both consume
those types, and aligning the contract once means renderer and verifier
can be wired against a single source of truth.

## Decision

**Pick `extract-interface from repeated motif` as the v1 transformation
rule.** Define the supporting types in `internal/propose/` so that
adding rule #2 (dependency-inversion, pass-as-arg, …) is a single new
file plus an `init()` registration — mirroring `internal/metrics/`
ADR-011.

### Rule mechanics

The rule is triggered by `metrics.Record` rows with:

- `Metric == "motif_redundancy"`
- `Scope == "region"`
- `Value >= MinRedundancy` (default 3 — enough to suggest abstraction
  without false positives)
- `Details["size"]` between `MinMotifSize` (3) and `MaxMotifSize` (5),
  matching the Stage 3 motif enumeration bounds (ADR-013)
- **None** of the participants in `Details["instances"]` carry
  `IsContract: true` (per ADR-009)

When triggered, the rule emits one `Proposal` whose `TargetSubgraph`
contains:

- one `Iface` role (kind: `type`, contract intent: `interface`)
- N `Impl` roles (kind: `type`, one per motif instance)
- N `Method` roles (kind: `method`, one per Impl)
- `Implements` edges from each Impl to Iface
- `Contains` edges from each Impl to its Method

`Samples` carries the existing instance names (role → existing name)
so Stage 6 (skeleton renderer) and Stage 7 (LLM materialization) can
show concrete names alongside the placeholders.

### Contract exclusion

Per ADR-009 a node carries `Attrs[AttrIsContract] = true` after
`archmotif contracts` runs. Adding a method to a contract-marked
interface or substituting one across call sites breaks the contract.
The rule therefore **skips any motif group whose member set intersects
the contract set**. This is enforced inside `Trigger`, not `Apply`, so
the rule never burns CPU building a Proposal it cannot legally emit.

### Threshold defaults

| Threshold | Default | Rationale |
|---|---|---|
| min motif size | 3 | matches Stage 3 motif enumeration lower bound |
| max motif size | 5 | matches Stage 3 motif enumeration upper bound (ADR-013) |
| min redundancy | 3 | empirical floor for "this looks like a real abstraction opportunity"; 2 is too noisy |

Configurable via struct fields on `ExtractInterfaceRule` so the CLI can
expose flags later. Stage 5 (the implementation issue, distinct from
this spec) wires those flags.

### Conflict resolution

When multiple proposals overlap regions (same instance node appearing
in two motif groups) the proposer keeps the first proposal in
trigger order and drops the rest. ROADMAP Stage 5's open question
suggested "highest score" — that requires Stage 4 to emit a score
field, which it does not yet. First-match keeps the v1 contract honest;
Stage 5 implementation can swap in a scorer when one exists.

### Layout

```
internal/propose/
  propose.go              — Proposer; Proposal, AnomalyRef, TargetSubgraph,
                            Role, EdgeConstraint types (single source of truth)
  registry.go             — pluggable rule registry (init() registration,
                            mirroring metrics ADR-011)
  rule.go                 — Rule interface (Name, Trigger, Apply)
  extract_interface.go    — v1 rule
  extract_interface_test.go — table-driven cases
  testdata/               — synthetic graph fixtures (Go builders,
                            following the metricstest pattern)
```

Public surface:

```go
type Rule interface {
    Name() string
    Trigger(rec metrics.Record, g *graph.Graph) bool
    Apply(g *graph.Graph, rec metrics.Record) (*Proposal, error)
}

type Proposer struct { /* unexported */ }

func (p *Proposer) Propose(g *graph.Graph, anomalies []metrics.Record) []*Proposal
```

### Why pin types here, not in `internal/verify/`?

Issue #18 (verifier) lands in parallel with this issue. The acceptance
criteria for #19 explicitly say: **the proposer is the single source of
truth for `Proposal` and `TargetSubgraph`**, and `internal/verify/`
imports them once both PRs land. #18 is allowed to define equivalent
types locally for its own first cut; consolidation is a small mechanical
follow-up after both merge.

## Alternatives considered

- **Ship two rules in v1** (extract-interface + dependency-inversion).
  Doubles the spec surface for the same demo value; dependency
  inversion has design-intent ambiguity (which edge to invert?) that
  is better solved with a real example in hand. Defer.
- **Pin Proposal types in `internal/verify/`.** Verifier-driven types
  bias the design toward what's easiest to check, not what's most
  natural for the rule. Proposer-driven types match the data-flow
  direction (anomaly → proposal → skeleton → verifier).
- **Score-based conflict resolution in v1.** Stage 4 doesn't yet emit
  a score the proposer can read. First-match is the right interim;
  bump when a real scorer exists.
- **Use `Configurable() map[string]any` like metrics.** Metrics need
  flag-tunable knobs at runtime; the proposer's thresholds are stable
  defaults that change once-per-release. Plain struct fields are
  simpler.

## Consequences

- Stage 5 implementation issue (separate, future) is now scoped to:
  wire the real anomaly stream into `Proposer.Propose` and replace the
  stub `Apply` body with full motif-instance enumeration. The type
  shape, the rule, the registry, the CLI subcommand are all in place.
- Adding rule #2 = one new file in `internal/propose/`, one `init()`
  call. No registry changes, no `cmd/archmotif/propose.go` changes.
- Issue #16 (skeleton renderer) and issue #18 (verifier) both import
  `internal/propose` for `Proposal` and `TargetSubgraph`. Once both
  merge, #18 drops its local placeholder types and imports from
  `internal/propose` (a one-commit follow-up).
- Stage 7 (LLM materialization) prompt template can be designed
  against the fixed Iface/Impl/Method role names: it already knows
  what to fill in.
- The `extract_interface.go` file stubs the heavy enumeration step
  (group motif instances, infer common method signature). The type
  shape returned by `Apply` is correct on a hand-built fixture, so
  downstream consumers can wire against it. Stage 5 fills in the body
  once Stage 4 produces real anomaly Records on real graphs.
