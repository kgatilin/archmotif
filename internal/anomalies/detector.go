package anomalies

import (
	"fmt"
	"sort"
	"sync"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
	"github.com/kgatilin/archmotif/internal/metrics"
)

// Detector is the contract every anomaly detector must satisfy.
//
// Detect is pure: it must not mutate the graph and must be
// deterministic for a given (graph, records) pair. The graph is
// supplied so detectors can resolve node positions for Region.Files
// and dereference member IDs; some detectors (spectral_gap, the
// graph-scope cycle count) may not need it.
//
// Detectors should consume only records whose Metric matches their
// Metric() identifier. The runner already filters for this, but
// implementations defensive-checking does no harm.
type Detector interface {
	// Name returns the detector's identifier. Often equal to the
	// metric name; kept distinct so a future metric can have multiple
	// detectors (e.g. "modularity:oversize" + "modularity:underQ").
	Name() string
	// Metric returns the metric name this detector consumes.
	Metric() string
	// Description is a short human-readable string for `--list`.
	Description() string
	// Configurable returns thresholds and knobs the detector exposes.
	// Values are the current defaults; the runner does not yet wire
	// CLI overrides through (Stage 4 v1 fixes thresholds in source —
	// see ADR-020).
	Configurable() map[string]any
	// Detect inspects records (already filtered to this detector's
	// Metric()) and returns zero or more anomalies plus an optional
	// non-fatal error. Errors are surfaced in the CLI but do not
	// abort the run.
	Detect(g *mgraph.Graph, records []metrics.Record) ([]Anomaly, error)
}

// registry holds the package-level set of registered detectors,
// keyed by Name().
var (
	registryMu sync.RWMutex
	registry   = map[string]Detector{}
)

// Register adds d to the package-level registry. Intended to be
// called from init(). Panics on duplicate names so a build-time bug
// fails loud at process start.
func Register(d Detector) {
	registryMu.Lock()
	defer registryMu.Unlock()
	name := d.Name()
	if name == "" {
		panic("anomalies.Register: empty detector name")
	}
	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("anomalies.Register: duplicate detector %q", name))
	}
	registry[name] = d
}

// Lookup returns the detector with the given name and a boolean
// indicating presence.
func Lookup(name string) (Detector, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	d, ok := registry[name]
	return d, ok
}

// All returns every registered detector, sorted by Name().
func All() []Detector {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]Detector, 0, len(registry))
	for _, d := range registry {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// Names returns the registered detector names, sorted.
func Names() []string {
	ds := All()
	out := make([]string, 0, len(ds))
	for _, d := range ds {
		out = append(out, d.Name())
	}
	return out
}

// ByMetric returns the registered detectors whose Metric() == metric.
// Returns an empty slice if none match.
func ByMetric(metric string) []Detector {
	ds := All()
	out := make([]Detector, 0, len(ds))
	for _, d := range ds {
		if d.Metric() == metric {
			out = append(out, d)
		}
	}
	return out
}
