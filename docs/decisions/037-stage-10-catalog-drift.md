# ADR-037 — Stage 10: catalog + drift

**Status:** accepted
**Date:** 2026-05-06
**Stage:** 10 — see [ROADMAP.md](../../ROADMAP.md#stage-10--catalog--drift-later) — issue #11
**Builds on:** ADR-011 (metric registry), ADR-013 (motif isomorphism),
ADR-015 (metric output schema), ADR-026 (pattern reports).

## Context

Earlier stages (1–9) compute structural information about a codebase
*at one moment in time*: graph nodes, metric values, motif groups,
pattern reports. Stage 10 adds the longitudinal view — same project,
across two refs — so an operator can answer "did this commit make the
architecture better or worse?" without re-running the whole pipeline
from scratch and eyeballing two metric dumps.

Roadmap requirement: persist named patterns + motif counts + metric
values from prior runs in a catalog file (`.archmotif/catalog.yaml`),
and ship `archmotif drift` to diff between two refs. Verify by
running between two commits of `archmotif` itself and observing a
meaningful diff.

Three things drive the design:

1. **Reuse, don't re-derive.** The metric runner (ADR-015) and the
   pattern runner (ADR-026) already emit stable, JSON-serialisable
   records with deterministic ordering. The catalog is a thin
   serialisation layer over what they already produce — it does
   *not* re-implement metric computation.

2. **Refs are user-supplied labels, not git invocations.** Stage 10
   should not shell out to `git` or pin itself to git's repo layout.
   The user captures a snapshot at whatever ref they want, hands us
   a label, and `drift` diffs by label. Whether the label happens
   to match a git SHA is the user's problem.

3. **Catalog file is a bag of snapshots.** Snapshots accumulate over
   time. Re-capturing the same label overwrites the prior entry so
   the file does not grow unboundedly during repeated runs at HEAD.
   No history pruning beyond that — the file is small (one snapshot
   ≈ a few KB even on archmotif itself) and an operator can rotate
   it manually.

## Decision

### 1. New `internal/catalog/` package

Public surface:

```go
package catalog

const CatalogVersion = 1

type Catalog struct {
    Version   int        `yaml:"version"`
    Snapshots []Snapshot `yaml:"snapshots"`
}

type Snapshot struct {
    Label      string         `yaml:"label"`
    Ref        string         `yaml:"ref,omitempty"`
    CapturedAt time.Time      `yaml:"captured_at"`
    Path       string         `yaml:"path"`
    Pattern    string         `yaml:"pattern"`
    Metrics    []MetricEntry  `yaml:"metrics"`
    Motifs     MotifSummary   `yaml:"motifs"`
    Patterns   []PatternEntry `yaml:"patterns"`
}

func Capture(g *graph.Graph, opts CaptureOptions) (Snapshot, error)
func Load(path string) (Catalog, error)
func Save(path string, c Catalog) error
func (c *Catalog) Upsert(s Snapshot)
func (c Catalog) Find(label string) (Snapshot, bool)

func Diff(from, to Snapshot) Drift
```

Why a new package: the catalog file is a multi-snapshot record whose
schema is *not* the metrics envelope and *not* the patterns envelope.
Folding it into either package would force one to know about the
other, and would put YAML serialisation in packages that today emit
JSON. A small dedicated package keeps both runners clean.

### 2. Catalog snapshot is a digest, not a full graph dump

Each snapshot stores:

- **Metrics** — only `ScopeGraph` records, by name + value. Node-,
  region-, and edge-scoped records are not persisted (they reference
  graph IDs that mean nothing across refs — a node ID at HEAD~1 is
  not the same node at HEAD). The drift report can still be useful
  with graph-scope-only data: spectral gap, modularity, motif group
  count, cycle count, etc. all live at graph scope.
- **Motif summary** — `total_groups`, `total_instances`, plus a
  per-canonical-form histogram `(canonical, size, count)`. Canonical
  forms come from the motif metric's existing region records
  (ADR-013) and are stable across runs of the same code.
- **Pattern reports** — id, version, status, score, threshold. No
  evidence-node IDs (same reason as above: IDs don't survive refs).

This intentionally drops the per-instance member lists from motifs
and the per-region member lists from metrics. They are recoverable
by re-running the metric on either ref; the catalog answers the
"did the count go up or down" question without them.

### 3. CLI surface

Two subcommands:

- **`archmotif catalog [flags] <path>`** — capture one snapshot.
  Flags:
    - `--label <name>` (required) — the snapshot's identifier in the
      catalog. Subsequent captures with the same label overwrite.
    - `--ref <git-sha-or-tag>` (optional) — recorded verbatim,
      informational only.
    - `--catalog <path>` — file to read/write (default
      `.archmotif/catalog.yaml`).
    - Plus the standard `--pattern` / `--tests` flags shared with
      `metrics` and `patterns`.
  Output: short text confirmation listing the captured metrics /
  motif counts / pattern verdicts. The catalog file is rewritten
  in place with the snapshot upserted.

- **`archmotif drift [flags]`** — diff two snapshots.
  Flags:
    - `--from <label>` (required)
    - `--to <label>` (required)
    - `--catalog <path>` — file to read (default
      `.archmotif/catalog.yaml`)
    - `--format text|json` — text default for terminal use, JSON
      versioned for tooling.
  Exit code: 0 on success regardless of whether anything drifted.
  This is a reporting command, not a CI gate. Gating belongs in a
  later stage (or as a thin shell wrapper).

### 4. Drift output shape

```go
type Drift struct {
    Version int
    From    DriftSnapshotRef
    To      DriftSnapshotRef
    Metrics []MetricDelta
    Motifs  MotifDrift
    Patterns []PatternDelta
}

type MetricDelta struct {
    Name  string
    From  *float64  // nil = absent in from
    To    *float64  // nil = absent in to
    Delta *float64  // nil when either side is nil
}

type MotifDrift struct {
    TotalGroups    Delta[int]
    TotalInstances Delta[int]
    Added   []MotifGroupDelta // new canonical forms
    Removed []MotifGroupDelta // forms gone from to
    Changed []MotifGroupDelta // count moved
}

type PatternDelta struct {
    ID         string
    StatusFrom string
    StatusTo   string
    ScoreFrom  *float64
    ScoreTo    *float64
}
```

Determinism: every slice is sorted by name / canonical form / id so
two diffs of the same input produce byte-identical output (key for
snapshot-style tests on the drift command).

### 5. Catalog file is the user's, not archmotif's

`.archmotif/` is already gitignored (see `.gitignore`). The catalog
is a tool-local artefact a user *can* commit if they want to track
drift in CI — archmotif neither encourages nor blocks that choice.

## Alternatives considered

- **Persist the full typed graph per snapshot.** Rejected: snapshots
  would balloon and the only thing drift could do with the extra
  data is recompute metrics, which is exactly what the existing
  `metrics` command already does. Drift over digests is the
  meaningful operation.

- **Persist node-scope and region-scope records too.** Rejected:
  graph node IDs are file-line-col-kind tuples (ADR-005). They
  shift on every code change. Cross-ref node-level diffs aren't
  meaningful without a node-matching algorithm, which is out of
  scope for Stage 10. Per-canonical-form motif counts are the right
  granularity — they survive code churn because the canonical form
  is the structural shape, not a location.

- **Use git directly: `archmotif drift HEAD~1 HEAD`.** Rejected:
  forces archmotif to know about git, to check out arbitrary refs
  in the user's working tree, and to handle dirty working copies.
  The two-step "capture then diff" workflow keeps archmotif free of
  VCS coupling and lets users diff non-git snapshots (e.g. before
  and after a refactor branch).

- **Append-only snapshot log (no upsert).** Rejected: the typical
  "run on every CI build" usage would add a snapshot per build and
  the file would grow without bound. Upsert-by-label keeps the file
  bounded by the number of distinct labels the user cares about.

- **YAML vs JSON for the catalog.** YAML chosen: the `.archmotif/`
  config conventions in ADR-008 (contracts) and ADR-033
  (optimize-loop) already use YAML, the catalog is human-edited
  often enough (you'll want to delete a stale entry by hand) that
  YAML's diffability and comments matter more than JSON's
  round-trip speed. `gopkg.in/yaml.v3` is already a dependency.

## Consequences

- A new top-level package (`internal/catalog/`) and two new CLI
  commands. No changes to the metric or pattern runners.
- Operators can now answer "did this commit drift the architecture"
  with one command, given two captured snapshots.
- Stage-10's "named patterns" requirement is satisfied by reusing
  the Stage-3 motif `canonical` form as the name. We don't ship a
  separate pattern-naming UI; if an operator wants friendlier names
  they can post-process the YAML.
- Drift reports are *informational*. Gating CI on architectural
  regression (e.g. "fail if motif count grew by >N") is a future
  follow-up — the drift JSON shape gives downstream tooling the
  hooks to do that without further core changes.
- The catalog format is `version: 1`. Schema bumps are envelope-
  level: a future stage that adds, say, role-aggregate stats can
  bump to `version: 2` and ship a one-shot migration.
