// Package metrics implements Stage 3 of archmotif: structural metrics
// computed over the typed graph.
//
// Each metric implements the Metric interface and is registered into a
// package-level registry via init() (per ADR-011). Adding a new metric
// is a single file: declare the type, implement Compute, register in
// init() — no other code changes required.
//
// Metrics emit Records keyed by (metric, scope, target). Scopes are
// graph (one record for the whole graph), region (one per identified
// subgraph), node, or edge. Per ADR-015 the schema is shaped so Stage 4
// (anomaly detection) can rank records without rewriting the runner.
package metrics

import (
	"context"
	"fmt"
	"sort"
	"sync"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// Scope is the granularity at which a metric reports a value.
type Scope string

// Scope constants.
const (
	// ScopeGraph means a single value for the entire graph.
	ScopeGraph Scope = "graph"
	// ScopeRegion means one value per identified subgraph (motif group, SCC, etc.).
	ScopeRegion Scope = "region"
	// ScopeNode means one value per node.
	ScopeNode Scope = "node"
	// ScopeEdge means one value per edge.
	ScopeEdge Scope = "edge"
)

// Record is a single (metric, scope, target, value) emission.
//
// Target is the addressable subject of the value:
//   - For ScopeNode: the node ID.
//   - For ScopeEdge: a synthetic edge identifier (from→kind→to).
//   - For ScopeRegion: a region ID assigned by the metric (stable
//     within a run).
//   - For ScopeGraph: empty.
//
// Details carries optional metric-specific context (member node IDs of
// a motif group, SCC participants, eigenvalue index, etc.). It must be
// JSON-serialisable.
type Record struct {
	Metric  string         `json:"metric"`
	Scope   Scope          `json:"scope"`
	Target  string         `json:"target,omitempty"`
	Value   float64        `json:"value"`
	Details map[string]any `json:"details,omitempty"`
}

// Metric is the contract every built-in and custom metric must satisfy.
//
// Compute is pure: it must not mutate g and it must be deterministic
// for a given graph. Compute should return ([]Record, nil) on success;
// errors are reserved for genuinely-unrecoverable conditions
// (numerical breakdown, malformed input). The runner reports errors
// per-metric and continues with the rest.
//
// Compute accepts a context.Context so long-running metrics can be
// cancelled — the MCP server passes the per-request context through so
// a slow metric does not stall the worker thread. Implementations are
// expected to honour ctx.Done() at coarse-grained checkpoints (per
// node / per region / between phases); short metrics may ignore ctx.
//
// Configurable returns options the metric exposes via flags. Most
// metrics return an empty map; motif size cap is the current
// exception. Keys are short identifiers; values are the current
// defaults. The runner does not yet wire flags through, but the
// surface is reserved so Stage 4 can hand metrics user overrides
// without an interface bump.
type Metric interface {
	Name() string
	Description() string
	Compute(ctx context.Context, g *mgraph.Graph) ([]Record, error)
	Configurable() map[string]any
}

// registry holds the package-level set of registered metrics. Keyed by
// Name(); duplicate registration panics (an init() bug, not user
// input).
var (
	registryMu sync.RWMutex
	registry   = map[string]Metric{}
)

// Register adds m to the package-level registry. Intended to be called
// from init(); see ADR-011. Panics on duplicate names so the bug fails
// loud at process start rather than silently shadowing a metric.
func Register(m Metric) {
	registryMu.Lock()
	defer registryMu.Unlock()
	name := m.Name()
	if name == "" {
		panic("metrics.Register: empty metric name")
	}
	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("metrics.Register: duplicate metric %q", name))
	}
	registry[name] = m
}

// Lookup returns the registered metric with the given name and a
// boolean indicating presence.
func Lookup(name string) (Metric, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	m, ok := registry[name]
	return m, ok
}

// All returns every registered metric, sorted by name. The returned
// slice is a fresh copy: callers may sort or mutate it freely.
func All() []Metric {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]Metric, 0, len(registry))
	for _, m := range registry {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// Names returns the registered metric names, sorted.
func Names() []string {
	ms := All()
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.Name())
	}
	return out
}
