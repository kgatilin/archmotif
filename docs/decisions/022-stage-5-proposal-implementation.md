# ADR-022 — Stage 5 proposal implementation: anomaly-driven, score-based, member-overlap conflicts

**Status:** accepted
**Date:** 2026-05-05
**Stage:** 5 — Local transformation proposals (implementation)
**Supersedes:** parts of ADR-019 (see "Updates ADR-019" below)

## Context

ADR-019 pinned the v1 spec stub for Stage 5. Stage 4 has now shipped
(`internal/anomalies/`, ADRs 020/021), giving us:

- A real `Score` field per anomaly, so the proposer can choose between
  overlapping candidates rather than first-match.
- A `Region.Members` field, so the proposer can detect overlap on
  graph node IDs (not just file paths).
- One `Anomaly` per motif instance (per ADR-021), which means the
  proposer must deduplicate by group canonical form before applying
  the rule — otherwise N Proposals fire for one logical extract.

The implementation issue (#6) asks us to wire the path
graph → metrics → anomalies → proposals through the CLI, with
score-based conflict resolution and a real `Apply` that produces a
shaped Proposal from a real motif group.

## Decision

### 1. `Proposer.Propose` consumes anomalies, not raw records

Signature change vs ADR-019:

```go
// before (ADR-019 stub):
func (p *Proposer) Propose(g *graph.Graph, recs []metrics.Record) Result

// after (this ADR):
func (p *Proposer) Propose(g *graph.Graph, anomalies []anomalies.Anomaly) Result
```

Reason: Stage 4 ships scores and resolved Regions (members + files);
re-computing them inside the proposer would duplicate logic. The
proposer is now the natural consumer of `anomalies.Anomaly`. A thin
back-compat helper (`ProposeFromRecords`) is kept for tests that
hand-build `metrics.Record`s — it synthesises a zero-score Anomaly
per record so the rule still fires.

`internal/propose` now imports `internal/anomalies`. No cycle: the
Anomaly package does not import propose.

### 2. Group deduplication: one anomaly per motif group

Stage 4 emits one Anomaly per motif instance (ADR-021). The proposer
deduplicates by `(Metric, SourceRecord.Target)` — the group ID — and
keeps the highest-scoring representative. The rule sees one
representative per group and proposes once.

### 3. Conflict resolution = highest-score on overlapping members

Two proposals "overlap" when their member-node-ID sets intersect. The
member set per Proposal is the union of:

- the AnomalyRef Region members (the anomaly's flagged nodes), and
- the IDs referenced by the Proposal's Samples map.

When two proposals overlap, the higher score wins. Ties break by
trigger order (deterministic). Losers move into `Result.Skipped`.

This subsumes ADR-019's first-match interim. ADR-019 noted explicitly
that first-match was a placeholder until a scorer existed; Stage 4
shipped the scorer, so the upgrade lands now.

### 4. `Apply` extracts a real common method signature

The v1 stub used a name-based heuristic. The real `Apply`:

1. Validates the motif group has the canonical extract-interface
   shape: each instance is a (Type, Method, [external Type]) triple
   where the Method is contained by the Type and the Type Implements
   an external Type. (Detected by inspecting the actual graph edges
   among the members — not by regex on names.)
2. Identifies the common Iface candidate: a Type that is the target
   of an Implements edge from every Impl. If multiple candidates,
   prefer the one shared by the most instances; if none, fall back
   to "no Iface yet — Stage 6 invents one" and leave the Iface
   sample empty.
3. Records the Method name and arity (in-edges + out-edges visible
   on the method node) on the Proposal as `Samples[i]["MethodName"]`
   and `Samples[i]["MethodSignature"]` — the signature is a
   structural fingerprint (e.g. `"in:1,out:0"`) since archmotif's
   graph carries kinds, not types. ADR-016's skeleton renderer treats
   it as advisory.
4. Skips the proposal (returns `nil, nil`) when the method-arity set
   has more than one distinct signature across instances — a real
   extract-interface needs methods to align.

The contract-exclusion check from ADR-019 (defence-in-depth) is
re-run inside `Apply`, even though `Trigger` already enforces it.

### 5. CLI: `archmotif propose <path>` runs the full pipeline

Subcommand surface:

```
archmotif propose [flags] <path>
  --format=text|json  (default text)
  --limit=N           (default 10)
  --list              (existing; lists registered rules)
  --tests             (include _test.go)
  --pattern=...       (go/packages pattern)
```

Pipeline: `parser.Build → metrics.Run → anomalies.Run →
propose.Propose → format`. Mirrors `cmd/archmotif/anomalies.go`
exactly, modulo the final stage.

Text output for each proposal:

```
1. extract-interface (score 5.00)
   anomaly: motif_redundancy / motif-3 (5 instances)
   target shape: 1 Iface + 5 Impls + 5 Methods (Implements + Contains)
   affects 5 files:
     - internal/foo/a.go
     - internal/foo/b.go
   samples:
     [0] Impl=Foo, Method=Read
     [1] Impl=Bar, Method=Read
     ...
```

JSON output: a flat list of Proposal objects (one per line of the
top-level JSON array), version-tagged for Stage 5/6/7 consumers. The
schema is the existing `Proposal` struct; the envelope adds
`{"version": 1, "proposals": [...], "skipped": [...]}`.

### 6. Layout

No new files in `internal/propose/`. The existing layout from ADR-019
holds. `extract_interface.go` gets a real `Apply` body; `registry.go`
gets the Anomaly signature and dedup logic; `propose.go` is unchanged
(the type contract is stable — only the producer changes).

`cmd/archmotif/propose.go` swaps its stub body for the full pipeline.
`cmd/archmotif/propose_test.go` extends from the `--list` smoke test
to cover the real pipeline on a tiny code fixture (the existing
`testdata/` graph fixtures from `parser_test.go` are reused).

## Alternatives considered

- **Keep `Propose([]metrics.Record)`, recompute scores inside the
  proposer.** Doubles the scoring logic, and any change to anomaly
  scoring (ADR-020) would need a synchronised propose-side change.
  Rejected.
- **One ADR per change** (signature change + dedup + conflict + Apply
  + CLI). Each change is small; bundling them keeps the design intent
  visible in one place. The reader follows the data flow — anomaly
  → Apply → CLI — exactly once. ADR-022 stays at one decision
  document.
- **Ship two rules.** ADR-019 deferred this for v1; nothing has
  changed. Defer remains the right call until the first rule is in
  user hands.

## Updates ADR-019

ADR-019 noted, in §"Conflict resolution":

> ROADMAP Stage 5's open question suggested "highest score" — that
> requires Stage 4 to emit a score field, which it does not yet.
> First-match keeps the v1 contract honest; Stage 5 implementation
> can swap in a scorer when one exists.

Stage 4 now ships scores (ADR-020). This ADR makes the swap.

## Consequences

- `internal/propose` now depends on `internal/anomalies`. The
  dependency direction matches the data flow: Stage 4 → Stage 5.
- The CLI `archmotif propose <path>` is end-to-end: a user can run
  it on archmotif itself and see proposals.
- Issue #16 (skeleton renderer) and issue #18 (verifier) consume
  the same `Proposal` shape ADR-019 pinned — no contract change.
- Future rules (ADR-019 Alternatives §"Ship two rules in v1")
  inherit the score-based conflict resolution for free.
