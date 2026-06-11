# ADR-026 — Deterministic pattern reports

**Status:** accepted
**Date:** 2026-05-05
**Issue:** #27

## Context

ArchMotif's deterministic checks (motifs, contracts, anomalies) produce
graph evidence but don't directly answer "does this package look
architecturally healthy?". Stage 4 anomalies flag *outliers*; we also
need *named patterns* with stable IDs that downstream tooling
(CI gates, dashboards, drift trackers) can refer to without
re-interpreting raw graph metrics each time.

LLM-based interpretation is out of scope here — the output must be
machine-readable evidence with stable shape.

## Decision

Add a `Pattern` interface and registry mirroring the metric registry
(ADR-011): each pattern lives in one file under `internal/patterns/`
and self-registers in `init()`. Adding a future pattern = one new file.

### Report schema

Each pattern emits a `Report` with:

| Field | Purpose |
|---|---|
| `pattern_id` | stable ID, kebab-case (`domain_core`, `external_noise_sink`) |
| `version` | pattern version; bump when the rule changes meaning |
| `status` | enum: `match` / `near_match` / `mismatch` / `not_applicable` |
| `score`, `threshold` | numeric evidence behind the verdict |
| `evidence_nodes`, `evidence_edges` | graph refs that drove the verdict |
| `violations` | structured violations (id + reason + locations) |
| `recommendations` | short human-readable next steps |
| `metrics` | underlying metric values used by the decision |

### Status enum: why four states

`not_applicable` is a first-class state, not silence. Patterns that
need role metadata (`domain_core`, `forbidden_role_edges`) report
`not_applicable` until #28's role config is loaded — keeping the CLI
surface stable as the role system arrives.

### V1 catalog

| ID | Status today | When fully usable |
|---|---|---|
| `external_noise_sink` | works | now (graph topology only) |
| `domain_core` | not_applicable | when roles land (#28) |
| `forbidden_role_edges` | not_applicable | when roles land (#28) |

Two pre-registered stubs are *not dead code* — they're a placeholder
for the CLI's pattern surface so callers can already query and filter
by ID, and so that in-flight `not_applicable` reports surface the
prerequisite to the user.

## Alternatives considered

- **Embed pattern logic inside metrics** — rejected: metrics are
  numerical scalars; patterns are named verdicts with multi-field
  evidence. Different output shapes.
- **Three-state enum (no `not_applicable`)** — rejected: collapses two
  meaningful outcomes (silently absent vs. explicitly missing
  prerequisite). Hides the affordance.
- **Pattern bodies in YAML/DSL** — premature; Go code is the right
  abstraction until at least 5 patterns ship and a pattern is the
  copy-paste unit.

## Consequences

- New patterns are one-file additions, low ceremony.
- Downstream tooling can rely on stable `pattern_id` + `version`.
- `not_applicable` reports surface gaps the user can close (e.g. add
  role config). The CLI stays predictable as the rule set evolves.
- Pattern versioning is the implementer's responsibility — bump on
  semantic change.
