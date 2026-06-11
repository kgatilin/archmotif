package patterns

import (
	"fmt"
	"sort"
	"sync"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// registry mirrors the metric-registry pattern (ADR-011): every pattern
// file ends with `func init() { Register(MyPattern{}) }`. Adding a new
// pattern means adding one Go file — no other wiring required.
var (
	registryMu sync.RWMutex
	registry   = map[string]Pattern{}
)

// Register adds p to the package-level registry. Intended to be called
// from init(). Panics on empty or duplicate IDs so registration bugs
// fail loud at process start.
func Register(p Pattern) {
	registryMu.Lock()
	defer registryMu.Unlock()
	id := p.ID()
	if id == "" {
		panic("patterns.Register: empty pattern id")
	}
	if _, exists := registry[id]; exists {
		panic(fmt.Sprintf("patterns.Register: duplicate pattern %q", id))
	}
	registry[id] = p
}

// Lookup returns the registered pattern with the given id and a
// boolean indicating presence.
func Lookup(id string) (Pattern, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	p, ok := registry[id]
	return p, ok
}

// All returns every registered pattern, sorted by ID.
func All() []Pattern {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]Pattern, 0, len(registry))
	for _, p := range registry {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID() < out[j].ID() })
	return out
}

// IDs returns the registered pattern IDs, sorted.
func IDs() []string {
	ps := All()
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.ID())
	}
	return out
}

// RunResult aggregates the output of running one or more patterns.
type RunResult struct {
	Reports []Report `json:"reports"`
}

// StatusCounts returns a map status -> count over r.Reports.
func (r RunResult) StatusCounts() map[Status]int {
	out := map[Status]int{
		StatusMatch:         0,
		StatusNearMatch:     0,
		StatusMismatch:      0,
		StatusNotApplicable: 0,
	}
	for _, rep := range r.Reports {
		out[rep.Status]++
	}
	return out
}

// Run executes every selected pattern against g and returns the
// aggregated reports. ids selects patterns by ID; pass nil/empty to run
// all registered patterns. Reports are returned sorted by pattern ID
// so output is stable across runs.
func Run(g *mgraph.Graph, ids []string) RunResult {
	var pats []Pattern
	if len(ids) == 0 {
		pats = All()
	} else {
		for _, id := range ids {
			p, ok := Lookup(id)
			if !ok {
				pats = append(pats, missingPattern{id: id})
				continue
			}
			pats = append(pats, p)
		}
	}

	res := RunResult{}
	for _, p := range pats {
		rep := p.Run(g)
		// Pattern.Run is responsible for setting ID/Version, but if the
		// implementation forgets, fill in from the registry to keep
		// output well-formed.
		if rep.ID == "" {
			rep.ID = p.ID()
		}
		if rep.Version == "" {
			rep.Version = p.Version()
		}
		res.Reports = append(res.Reports, rep)
	}
	sort.SliceStable(res.Reports, func(i, j int) bool {
		return res.Reports[i].ID < res.Reports[j].ID
	})
	return res
}

// missingPattern stubs in for an unknown ID at the CLI surface so the
// runner can produce a NotApplicable report rather than crashing.
type missingPattern struct{ id string }

func (m missingPattern) ID() string          { return m.id }
func (m missingPattern) Version() string     { return "0.0.0" }
func (m missingPattern) Description() string { return "" }
func (m missingPattern) Run(_ *mgraph.Graph) Report {
	return Report{
		ID:      m.id,
		Version: "0.0.0",
		Status:  StatusNotApplicable,
		Reason:  fmt.Sprintf("pattern %q is not registered", m.id),
	}
}
