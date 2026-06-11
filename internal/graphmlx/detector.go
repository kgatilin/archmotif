package graphmlx

import (
	"fmt"
	"sort"
	"sync"
)

// Severity is a coarse triage bucket for findings. Detectors decide
// the bucket from their own scoring rule; the optimizer ranks higher
// severity first.
type Severity string

// Severity buckets. Order is significant — see SeverityRank.
const (
	SeverityInfo     Severity = "info"
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

// SeverityRank maps a Severity to a numeric rank where higher = worse.
// Used by the optimizer to sort findings deterministically.
func SeverityRank(s Severity) int {
	switch s {
	case SeverityCritical:
		return 4
	case SeverityHigh:
		return 3
	case SeverityMedium:
		return 2
	case SeverityLow:
		return 1
	default:
		return 0
	}
}

// Finding is one anomaly emitted by a detector.
//
// The shape mirrors `internal/anomalies.Anomaly` but is intentionally
// independent: GraphML detectors don't have access to a `mgraph.Graph`
// so file/line resolution belongs to the consumer. Findings are
// designed to be JSON-stable across runs (sorted members, no maps with
// non-deterministic iteration in flat fields).
type Finding struct {
	// Detector is the detector identifier (matches Detector.Name()).
	Detector string `json:"detector"`
	// Score is a non-negative scalar where higher = more anomalous.
	// Per-detector scale; not comparable across detectors except via
	// Severity.
	Score float64 `json:"score"`
	// Severity is the detector's coarse triage bucket.
	Severity Severity `json:"severity"`
	// PrimaryID is the most-relevant node ID. Empty for graph-scope
	// findings (e.g. a hierarchy_cycle that spans the whole hierarchy
	// has the lowest-ID member as primary).
	PrimaryID string `json:"primaryID,omitempty"`
	// Members lists every node ID participating in the finding,
	// sorted ascending. Optional for node-scope detectors that only
	// flag a single node.
	Members []string `json:"members,omitempty"`
	// Reason carries a stable code, a human sentence, and structured
	// details. Code is detector-defined but stable across runs.
	Reason Reason `json:"reason"`
	// Evidence is structured, per-detector data: counts, ratios,
	// thresholds, parent IDs, etc. Optimizer surfaces this as
	// metricEvidence in the next-batch JSON.
	Evidence map[string]any `json:"evidence,omitempty"`
}

// Reason is the structured rationale for a finding.
type Reason struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// Detector is the contract every GraphML-level detector must satisfy.
//
// Detect is pure: it must not mutate g and must be deterministic for
// a given graph. Detectors are registered via Register() in init()
// and looked up by Name().
type Detector interface {
	Name() string
	Description() string
	Detect(g *Graph) ([]Finding, error)
}

var (
	registryMu sync.RWMutex
	registry   = map[string]Detector{}
)

// Register adds d to the package-level registry. Panics on empty name
// or on duplicate registration so build-time bugs fail loud.
func Register(d Detector) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if d.Name() == "" {
		panic("graphmlx.Register: empty detector name")
	}
	if _, exists := registry[d.Name()]; exists {
		panic(fmt.Sprintf("graphmlx.Register: duplicate detector %q", d.Name()))
	}
	registry[d.Name()] = d
}

// Lookup returns a detector by name plus a presence boolean.
func Lookup(name string) (Detector, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	d, ok := registry[name]
	return d, ok
}

// All returns every registered detector sorted ascending by Name().
func All() []Detector {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]Detector, 0, len(registry))
	for _, d := range registry {
		out = append(out, d)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// Names returns registered detector names, sorted.
func Names() []string {
	ds := All()
	out := make([]string, 0, len(ds))
	for _, d := range ds {
		out = append(out, d.Name())
	}
	return out
}

// RunAll runs every registered detector against g and returns a
// flattened, deterministically-sorted Finding list. Errors from
// individual detectors are aggregated; a non-empty errs slice does
// not abort the run.
func RunAll(g *Graph) (findings []Finding, errs []error) {
	return RunNamed(g, nil)
}

// RunNamed runs only the named detectors (or all when names is nil/empty).
func RunNamed(g *Graph, names []string) (findings []Finding, errs []error) {
	var dets []Detector
	if len(names) == 0 {
		dets = All()
	} else {
		seen := map[string]bool{}
		for _, n := range names {
			if seen[n] {
				continue
			}
			seen[n] = true
			d, ok := Lookup(n)
			if !ok {
				errs = append(errs, fmt.Errorf("detector %q not registered", n))
				continue
			}
			dets = append(dets, d)
		}
	}
	for _, d := range dets {
		f, err := d.Detect(g)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", d.Name(), err))
			continue
		}
		findings = append(findings, f...)
	}
	SortFindings(findings)
	return findings, errs
}

// SortFindings imposes a deterministic order on findings:
// (severity desc, score desc, detector asc, primaryID asc, members asc).
func SortFindings(fs []Finding) {
	sort.SliceStable(fs, func(i, j int) bool {
		ri, rj := SeverityRank(fs[i].Severity), SeverityRank(fs[j].Severity)
		if ri != rj {
			return ri > rj
		}
		if fs[i].Score != fs[j].Score {
			return fs[i].Score > fs[j].Score
		}
		if fs[i].Detector != fs[j].Detector {
			return fs[i].Detector < fs[j].Detector
		}
		if fs[i].PrimaryID != fs[j].PrimaryID {
			return fs[i].PrimaryID < fs[j].PrimaryID
		}
		return joinIDs(fs[i].Members) < joinIDs(fs[j].Members)
	})
}

func joinIDs(xs []string) string {
	switch len(xs) {
	case 0:
		return ""
	case 1:
		return xs[0]
	}
	out := xs[0]
	for _, s := range xs[1:] {
		out += "\x1f" + s
	}
	return out
}
