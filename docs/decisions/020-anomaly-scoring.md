# ADR-020 — Anomaly scoring: per-metric detectors with robust z-scores

**Status:** accepted
**Date:** 2026-05-05
**Stage:** 4 — Anomaly detection
**Supersedes:** —

## Context

Issue #5 leaves the scoring approach to the implementer with two
hints: per-metric fixed defaults vs a single global score, and z-score
threshold vs top-N quantile. Stage 3 metrics emit records with very
different distributions:

- `motif_redundancy`: a small set of region records, value = instance
  count (always ≥2 by construction). Distribution is a long thin tail;
  most groups have 2–3 instances, a few have many.
- `cycle_rank`: graph-scope count plus one region per non-trivial SCC.
  Each non-trivial SCC is already an anomaly by definition.
- `local_symmetry`: one record per node, value ≥ 0. Distribution can
  be heavy-tailed but most nodes score 0–1.
- `modularity`: one graph-scope Q plus one region per package
  community. Community sizes are skewed (a few huge packages, many
  small ones).
- `spectral_gap`: one graph-scope value. No distribution to compare
  against — the value itself either signals fragility (≈0) or doesn't.

A single global anomaly score across metrics is wrong because the
metrics live on incomparable scales (a count of 10 motif instances
isn't "anomalous" in the same way as a Newman-Q of 0.1). Per-metric
scoring is what concepts.md §4 calls for: "look here for *this*
reason."

For threshold style, plain mean+std z-scores are fragile on Stage 3
distributions: motif group counts are themselves outliers and corrupt
the mean. The robust replacement is the **modified z-score** based on
median absolute deviation (MAD):

    M_i = 0.6745 * (x_i - median) / MAD

Iglewicz & Hoaglin (1993) recommend |M_i| ≥ 3.5 as the anomaly
threshold; we use 3.5 for node-scope and 3.0 for region-scope (region
records are sparser, so a slightly looser threshold keeps signal).

## Decision

**One detector per metric kind, registered via `init()` (mirroring
ADR-011 for metrics).** Each detector consumes the metric's records
and emits zero or more `Anomaly` values. The detector knows the
metric's scope semantics; it doesn't need to introspect record shapes
beyond what the metric documents.

**Per-metric defaults:**

| Metric           | Scope considered | Score                                                | Flag rule                                         |
|------------------|------------------|------------------------------------------------------|---------------------------------------------------|
| motif_redundancy | region           | modified z-score on instance count, or instances ≥3  | M ≥ 3.0 OR instances ≥ 3 (see note)               |
| cycle_rank       | region           | SCC size                                             | every non-trivial SCC (size ≥ 2) is an anomaly    |
| local_symmetry   | node             | modified z-score on score                            | M ≥ 3.5                                           |
| modularity       | region (sizes)   | modified z-score on community size, ratio fallback   | M ≥ 3.0 OR size/median ≥ 5 when MAD = 0           |
| spectral_gap     | graph            | 1 / (gap + ε)                                        | gap ≤ 0.05 (fragile) OR gap == 0 (disconnected)   |

Note on motif_redundancy: small fixtures often have only a handful of
groups, in which case MAD is 0 and the modified z-score is undefined.
We fall back to an absolute floor — three or more instances of the
same isomorphism class is *always* anomalous — and the higher of
(modified z, instance-count z if MAD > 0) is reported. This means
synthetic graphs with a planted motif × N (the verify case in the
issue) reliably score; real-world runs with large dispersion still
get robust z-based ranking.

**Score is a single non-negative float64.** Cross-metric comparisons
are not supported (different scales); the CLI sorts within a metric
and within an overall ranked list using score as the comparator after
normalising via the per-metric flag rule (anything that "passed the
flag rule" is in the output; ranking inside is by raw score).

**Threshold knobs are exposed via `Detector.Configurable()` for
parity with `Metric.Configurable()`.** No CLI plumbing in v1; defaults
land in source. A user who wants to tune can copy a detector or edit
the constant. Stage 5+ will need overrides; we'll add them when the
need is concrete.

## Alternatives considered

- **Single global anomaly score.** Rejected — the metrics aren't
  commensurable. Compressing to one number forces an arbitrary
  weighting that hides the "why anomalous" signal.
- **Top-N quantile per metric.** Simpler than z-scores; works for
  one-shot ranking but loses the absolute-vs-relative distinction
  (a graph with no anomalies still produces a "top N", which is
  noise). Concepts.md §4 explicitly frames this as flag-or-not, not
  rank-everything. Rejected.
- **Plain mean+std z-score.** Sensitive to the same outliers we're
  trying to detect. Rejected for region-scope; kept available as a
  helper but not the default.
- **Per-metric custom logic without a registry.** The metric layer
  already shipped a registry (ADR-011); reusing the pattern keeps the
  codebase predictable. Adding a detector for a future metric is a
  one-file change.

## Consequences

- Stage 5 (proposals) reads `Anomaly` values; the schema documents
  what `Score` and `Reason` mean per detector kind so a proposal rule
  can switch on `Anomaly.Detector` to choose its rewrite.
- Every Stage 3 metric must have a detector or be explicitly opted
  out (via not registering one). A missing detector for a registered
  metric is logged as a warning, not an error.
- The CLI emits a versioned JSON envelope (`anomalies.version = 1`).
  Schema bumps require an ADR.
- `motif_redundancy` flagging "instances ≥ 3" is a deliberate floor
  for the small-graph case (Issue #5 verify uses a planted-motif
  fixture with explicit repetitions). On the archmotif self-graph
  the distribution is wide enough that the modified z-score
  dominates.
