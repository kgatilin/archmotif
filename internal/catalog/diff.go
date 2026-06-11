package catalog

import (
	"math"
	"sort"
)

// MetricEpsilon is the absolute tolerance below which two metric
// values are considered identical. Spectral and modularity solvers
// (gonum/mat eigendecomposition, in particular) produce results that
// agree to ~14 significant figures across runs of the same input,
// not bit-for-bit; without a tolerance the drift report on
// "before/after no-op" snapshots is noisy with ~1e-15 deltas. The
// epsilon is loose enough to absorb numerical jitter and tight
// enough that genuine architectural drift is never rounded away.
const MetricEpsilon = 1e-9

// CurrentDriftVersion is the schema version emitted by Drift's JSON
// envelope. Bump on breaking changes.
const CurrentDriftVersion = 1

// Drift is the structured comparison of two snapshots: from → to.
// It is deterministic for given inputs (every slice is sorted by a
// stable key) so two calls of Diff on the same snapshots produce
// byte-identical JSON.
type Drift struct {
	Version  int            `json:"version"`
	From     SnapshotRef    `json:"from"`
	To       SnapshotRef    `json:"to"`
	Metrics  []MetricDelta  `json:"metrics,omitempty"`
	Motifs   MotifDrift     `json:"motifs"`
	Patterns []PatternDelta `json:"patterns,omitempty"`
}

// SnapshotRef is the header of a snapshot included in a Drift report
// for traceability — caller can tell which captures the diff was
// computed from without re-loading the catalog.
type SnapshotRef struct {
	Label      string `json:"label"`
	Ref        string `json:"ref,omitempty"`
	CapturedAt string `json:"captured_at,omitempty"`
	Path       string `json:"path,omitempty"`
}

// MetricDelta records the change in one graph-scope metric value
// between two snapshots.
//
// From / To / Delta are pointers so absence on either side is
// distinguishable from a zero value: a metric that returned 0 is not
// the same thing as a metric that wasn't computed (e.g. it's not
// registered in the build that ran the from-snapshot).
type MetricDelta struct {
	Name  string   `json:"name"`
	From  *float64 `json:"from,omitempty"`
	To    *float64 `json:"to,omitempty"`
	Delta *float64 `json:"delta,omitempty"`
}

// MotifDrift summarises changes to the motif histogram.
type MotifDrift struct {
	TotalGroupsFrom    int               `json:"total_groups_from"`
	TotalGroupsTo      int               `json:"total_groups_to"`
	TotalInstancesFrom int               `json:"total_instances_from"`
	TotalInstancesTo   int               `json:"total_instances_to"`
	Added              []MotifGroupDelta `json:"added,omitempty"`
	Removed            []MotifGroupDelta `json:"removed,omitempty"`
	Changed            []MotifGroupDelta `json:"changed,omitempty"`
}

// MotifGroupDelta is one row of a motif drift list. CountFrom /
// CountTo are 0 for adds / removes respectively. Size is taken from
// whichever side has the entry (sizes don't change for a given
// canonical form).
type MotifGroupDelta struct {
	Canonical string `json:"canonical"`
	Size      int    `json:"size"`
	CountFrom int    `json:"count_from"`
	CountTo   int    `json:"count_to"`
}

// PatternDelta records a status / score change for one pattern.
//
// StatusFrom / StatusTo are the empty string when the pattern is
// missing on that side. Score pointers follow the same nil-means-
// absent convention as MetricDelta.
type PatternDelta struct {
	ID         string   `json:"id"`
	StatusFrom string   `json:"status_from,omitempty"`
	StatusTo   string   `json:"status_to,omitempty"`
	ScoreFrom  *float64 `json:"score_from,omitempty"`
	ScoreTo    *float64 `json:"score_to,omitempty"`
}

// Diff computes the structured drift from snapshot `from` to snapshot
// `to`. Both slices in the output (Metrics, Patterns) and the three
// motif sub-slices are sorted by a stable key. Only entries that
// actually changed are emitted — unchanged metric values, unchanged
// motif counts, and unchanged pattern statuses are dropped to keep
// the report focused on what moved.
func Diff(from, to Snapshot) Drift {
	out := Drift{
		Version:  CurrentDriftVersion,
		From:     snapshotRef(from),
		To:       snapshotRef(to),
		Metrics:  diffMetrics(from.Metrics, to.Metrics),
		Motifs:   diffMotifs(from.Motifs, to.Motifs),
		Patterns: diffPatterns(from.Patterns, to.Patterns),
	}
	return out
}

func snapshotRef(s Snapshot) SnapshotRef {
	out := SnapshotRef{Label: s.Label, Ref: s.Ref, Path: s.Path}
	if !s.CapturedAt.IsZero() {
		out.CapturedAt = s.CapturedAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	return out
}

// diffMetrics builds a sorted slice of deltas, emitting one entry per
// metric name that appears in either side and whose values differ
// (or whose presence differs).
func diffMetrics(from, to []MetricEntry) []MetricDelta {
	fmap := indexMetrics(from)
	tmap := indexMetrics(to)
	names := unionKeys(fmap, tmap)
	sort.Strings(names)

	out := make([]MetricDelta, 0, len(names))
	for _, name := range names {
		fv, fok := fmap[name]
		tv, tok := tmap[name]
		switch {
		case fok && tok:
			delta := tv - fv
			if math.Abs(delta) <= MetricEpsilon {
				continue
			}
			out = append(out, MetricDelta{
				Name:  name,
				From:  ptrFloat(fv),
				To:    ptrFloat(tv),
				Delta: ptrFloat(delta),
			})
		case fok:
			out = append(out, MetricDelta{Name: name, From: ptrFloat(fv)})
		case tok:
			out = append(out, MetricDelta{Name: name, To: ptrFloat(tv)})
		}
	}
	return out
}

func indexMetrics(es []MetricEntry) map[string]float64 {
	out := make(map[string]float64, len(es))
	for _, e := range es {
		out[e.Name] = e.Value
	}
	return out
}

// diffMotifs partitions canonical forms into added / removed /
// changed buckets. Identical-count groups are skipped. Each bucket is
// sorted: added by descending count, removed by descending prior
// count, changed by absolute delta descending — these match the
// "what's interesting first" heuristic an operator wants in a drift
// report.
func diffMotifs(from, to MotifSummary) MotifDrift {
	out := MotifDrift{
		TotalGroupsFrom:    from.TotalGroups,
		TotalGroupsTo:      to.TotalGroups,
		TotalInstancesFrom: from.TotalInstances,
		TotalInstancesTo:   to.TotalInstances,
	}
	fmap := indexMotifs(from.Groups)
	tmap := indexMotifs(to.Groups)
	canons := unionKeys(fmap, tmap)
	for _, c := range canons {
		fe, fok := fmap[c]
		te, tok := tmap[c]
		switch {
		case fok && tok:
			if fe.Count == te.Count {
				continue
			}
			out.Changed = append(out.Changed, MotifGroupDelta{
				Canonical: c,
				Size:      fe.Size,
				CountFrom: fe.Count,
				CountTo:   te.Count,
			})
		case fok:
			out.Removed = append(out.Removed, MotifGroupDelta{
				Canonical: c,
				Size:      fe.Size,
				CountFrom: fe.Count,
			})
		case tok:
			out.Added = append(out.Added, MotifGroupDelta{
				Canonical: c,
				Size:      te.Size,
				CountTo:   te.Count,
			})
		}
	}
	sort.SliceStable(out.Added, func(i, j int) bool {
		if out.Added[i].CountTo != out.Added[j].CountTo {
			return out.Added[i].CountTo > out.Added[j].CountTo
		}
		return out.Added[i].Canonical < out.Added[j].Canonical
	})
	sort.SliceStable(out.Removed, func(i, j int) bool {
		if out.Removed[i].CountFrom != out.Removed[j].CountFrom {
			return out.Removed[i].CountFrom > out.Removed[j].CountFrom
		}
		return out.Removed[i].Canonical < out.Removed[j].Canonical
	})
	sort.SliceStable(out.Changed, func(i, j int) bool {
		di := abs(out.Changed[i].CountTo - out.Changed[i].CountFrom)
		dj := abs(out.Changed[j].CountTo - out.Changed[j].CountFrom)
		if di != dj {
			return di > dj
		}
		return out.Changed[i].Canonical < out.Changed[j].Canonical
	})
	return out
}

func indexMotifs(es []MotifGroupEntry) map[string]MotifGroupEntry {
	out := make(map[string]MotifGroupEntry, len(es))
	for _, e := range es {
		out[e.Canonical] = e
	}
	return out
}

// diffPatterns produces deltas for patterns whose status or score
// changed, plus pattern adds / removes (a new pattern was registered
// or an old one was retired between snapshots).
func diffPatterns(from, to []PatternEntry) []PatternDelta {
	fmap := indexPatterns(from)
	tmap := indexPatterns(to)
	ids := unionKeys(fmap, tmap)
	sort.Strings(ids)
	out := make([]PatternDelta, 0, len(ids))
	for _, id := range ids {
		fe, fok := fmap[id]
		te, tok := tmap[id]
		switch {
		case fok && tok:
			if fe.Status == te.Status && math.Abs(fe.Score-te.Score) <= MetricEpsilon {
				continue
			}
			out = append(out, PatternDelta{
				ID:         id,
				StatusFrom: fe.Status,
				StatusTo:   te.Status,
				ScoreFrom:  ptrFloat(fe.Score),
				ScoreTo:    ptrFloat(te.Score),
			})
		case fok:
			out = append(out, PatternDelta{
				ID:         id,
				StatusFrom: fe.Status,
				ScoreFrom:  ptrFloat(fe.Score),
			})
		case tok:
			out = append(out, PatternDelta{
				ID:       id,
				StatusTo: te.Status,
				ScoreTo:  ptrFloat(te.Score),
			})
		}
	}
	return out
}

func indexPatterns(es []PatternEntry) map[string]PatternEntry {
	out := make(map[string]PatternEntry, len(es))
	for _, e := range es {
		out[e.ID] = e
	}
	return out
}

// HasChanges reports whether the drift contains any non-trivial
// difference. Useful for short-circuiting CLI output.
func (d Drift) HasChanges() bool {
	if len(d.Metrics) > 0 || len(d.Patterns) > 0 {
		return true
	}
	m := d.Motifs
	if m.TotalGroupsFrom != m.TotalGroupsTo || m.TotalInstancesFrom != m.TotalInstancesTo {
		return true
	}
	if len(m.Added) > 0 || len(m.Removed) > 0 || len(m.Changed) > 0 {
		return true
	}
	return false
}

func ptrFloat(v float64) *float64 { return &v }

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func unionKeys[V any](a, b map[string]V) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	for k := range a {
		seen[k] = struct{}{}
	}
	for k := range b {
		seen[k] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	return out
}
