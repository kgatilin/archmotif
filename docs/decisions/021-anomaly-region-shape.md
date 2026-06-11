# ADR-021 — Anomaly region shape

**Status:** accepted
**Date:** 2026-05-05
**Stage:** 4 — Anomaly detection
**Supersedes:** —

## Context

Issue #5 says: "Anomalies link to underlying graph nodes / edges /
files." The roadmap leaves the open question of region definition as
"connected subgraph of size ≤ N". The metric layer (ADR-015) already
emits per-record `Details` carrying member node IDs for region-scope
records and `instances` (a list of node-ID lists) for motif groups.
We need a stable region representation that:

1. lets the CLI print "this region = these nodes / these files",
2. lets Stage 5 propose a rewrite scoped to the region,
3. doesn't recompute graph traversals (consume metric output, per
   Stage 4 implementation guidance).

Two questions:

- Does a region carry the *full* member list, or does it cap at N?
- Are file paths and lines required, or optional?

## Decision

An anomaly's region is captured in a single `Region` struct:

```go
type Region struct {
    // Kind echoes the metric's Scope on the record this region was
    // derived from: "graph", "region", "node", or "edge".
    Kind string `json:"kind"`
    // Members is the full list of stable node IDs participating in
    // the region. For ScopeNode this is the single node; for
    // ScopeRegion it's the metric's Details.members or the union of
    // Details.instances (motif). For ScopeGraph it's empty.
    Members []string `json:"members,omitempty"`
    // PrimaryID is the most-relevant single node to highlight (e.g.
    // the centre of a motif instance, or the node itself for
    // ScopeNode). Empty for ScopeGraph.
    PrimaryID string `json:"primaryID,omitempty"`
    // Files lists the source files touched by Members, sorted and
    // deduplicated, with line ranges where available.
    Files []FileRef `json:"files,omitempty"`
}

type FileRef struct {
    Path  string `json:"path"`
    Lines []int  `json:"lines,omitempty"` // sorted, deduplicated
}
```

**No size cap.** The roadmap's "size ≤ N" hint is a guard against the
metrics emitting unbounded region lists; in practice each Stage 3
region is bounded by its metric (motifs k≤4, SCCs by graph topology,
modularity by package size). Capping the region members would lose
information Stage 5 needs ("which methods of this oversized package
are involved"). The CLI's pretty printer truncates display to the
top 10 members for readability; the JSON envelope keeps everything.

**File references resolved from `Node.Pos`.** The graph already
stores `Position{File, Line, Col}`. The detector consults the graph
to map member IDs back to source files and aggregates lines into
`FileRef`s. Files with no Position (Package nodes, foreign placeholder
types) are omitted with a count footnote in the reason text.

**Motif regions cover one instance, not the union.** A
`motif_redundancy` group has multiple isomorphic instances. Treating
them as one region would create an absurd "region spanning 30 nodes"
when the metric is really pointing at 10 disjoint copies of a 3-node
shape. Each instance becomes its own `Anomaly` value with the same
group canonical form in `Reason.GroupCanonical`. The detector reports
N anomalies for an N-instance group, all at the same score (they
co-occur and Stage 5 will deduplicate by group).

**Cycle regions are the SCC node set.** Already a connected subgraph
by SCC definition.

**Local-symmetry node regions are 1-node.** PrimaryID = the node.
Members = `[primaryID]`. No expansion (the *symmetry signature* is
the explanation, not the neighbourhood).

**Modularity community regions are the full community.** PrimaryID
= the package node ID; Members = all contained nodes.

## Alternatives considered

- **Region capped at N=20 members.** Loses Stage 5 information for
  legitimately large regions. Rejected.
- **Region = subgraph object (nodes + edges).** Heavier; we'd be
  re-implementing `graph.Subgraph` in the anomalies package. Members
  + a graph reference is sufficient — Stage 5 can call
  `graph.Subgraph(members, 0)` when it needs the structure.
- **Aggregate motif instances into one region.** Discussed above;
  loses signal. Stage 5 deduplication is cleaner.

## Consequences

- The anomaly JSON gets larger when communities are big. Acceptable
  for an analysis tool; we can add a `--summary` mode later.
- Stage 5 must be able to walk from `Region.Members` back into the
  graph. Since the CLI's `archmotif anomalies` doesn't currently
  emit the graph alongside, Stage 5 will reload the graph or read
  the metrics JSON sidecar; this is consistent with the staged-file
  model already used by `archmotif metrics`.
- File references with no line numbers (foreign placeholders, package
  nodes) appear as `{"path": "<pkg>", "lines": null}` rather than
  being silently dropped. The pretty printer renders these as
  `(no source)` so they remain visible.
