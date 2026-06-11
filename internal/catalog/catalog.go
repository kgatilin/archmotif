// Package catalog implements Stage 10 of archmotif: persistent
// snapshots of metric / motif / pattern values per code ref, plus a
// drift report that diffs two snapshots.
//
// A Catalog is a YAML file (default `.archmotif/catalog.yaml`)
// containing zero or more Snapshots, each labelled by the user
// (typically with a git ref or branch name). Snapshots are upserted
// by label so re-running `archmotif catalog --label main` against
// the working copy overwrites the previous main snapshot rather than
// appending.
//
// Per ADR-037, snapshots persist a *digest* of the current state:
// graph-scope metric values, a per-canonical-form motif histogram,
// and pattern report headers. Per-instance node IDs are intentionally
// not stored — they don't survive across refs and the drift report
// works fine without them.
package catalog

import "time"

// CatalogVersion is the schema version emitted in the YAML envelope.
// Bump on breaking schema changes; readers must reject unknown
// versions rather than silently mis-parse.
const CatalogVersion = 1

// Catalog is the on-disk file: a versioned envelope around a slice
// of Snapshots.
type Catalog struct {
	Version   int        `yaml:"version"`
	Snapshots []Snapshot `yaml:"snapshots"`
}

// Snapshot is one named capture of metric / motif / pattern values at
// a moment in time.
//
// Label is the user-supplied identifier (typically a branch or tag
// name); it is the catalog's primary key. Ref is a free-form string
// recorded verbatim — usually a git SHA — and is informational only.
type Snapshot struct {
	Label      string         `yaml:"label"`
	Ref        string         `yaml:"ref,omitempty"`
	CapturedAt time.Time      `yaml:"captured_at"`
	Path       string         `yaml:"path"`
	Pattern    string         `yaml:"pattern,omitempty"`
	Metrics    []MetricEntry  `yaml:"metrics,omitempty"`
	Motifs     MotifSummary   `yaml:"motifs"`
	Patterns   []PatternEntry `yaml:"patterns,omitempty"`
}

// MetricEntry persists one graph-scope metric value. Node-, region-,
// and edge-scope records are intentionally dropped: their target IDs
// are file-line-col-kind tuples (per ADR-005) that don't survive
// across refs, so cross-ref diffing them is meaningless.
type MetricEntry struct {
	Name  string  `yaml:"name"`
	Value float64 `yaml:"value"`
}

// MotifSummary aggregates the motif-redundancy regions emitted by the
// metric runner.
//
// TotalGroups counts canonical forms with ≥ 2 instances (matching the
// graph-scope record's value). TotalInstances counts the total number
// of motif instances across all repeating groups.
//
// Groups is the histogram, sorted by (count desc, canonical asc) for
// stable output. The catalog file truncates Groups at MaxStoredGroups
// to keep the file readable; drift-relevant comparisons remain
// sound because the truncation order is stable.
type MotifSummary struct {
	TotalGroups    int               `yaml:"total_groups"`
	TotalInstances int               `yaml:"total_instances"`
	Groups         []MotifGroupEntry `yaml:"groups,omitempty"`
}

// MaxStoredGroups caps the per-snapshot motif histogram length so the
// catalog file stays human-readable on graphs with hundreds of
// groups. The cap kicks in only on very motif-heavy codebases; tests
// exercise the boundary with a smaller cap.
const MaxStoredGroups = 200

// MotifGroupEntry is one row of the motif histogram: a canonical form
// with its instance count. Size is parsed out of the canonical form
// for convenience (motif canonical strings start with `k=N|...`).
type MotifGroupEntry struct {
	Canonical string `yaml:"canonical"`
	Size      int    `yaml:"size"`
	Count     int    `yaml:"count"`
}

// PatternEntry persists one Stage-7 pattern report header. Evidence
// nodes / edges are not stored — same reason as MetricEntry: their
// IDs don't survive across refs.
type PatternEntry struct {
	ID        string  `yaml:"id"`
	Version   string  `yaml:"version"`
	Status    string  `yaml:"status"`
	Score     float64 `yaml:"score"`
	Threshold float64 `yaml:"threshold"`
}
