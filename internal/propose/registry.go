package propose

import (
	"fmt"
	"sort"
	"sync"

	"github.com/kgatilin/archmotif/internal/anomalies"
	mgraph "github.com/kgatilin/archmotif/internal/graph"
	"github.com/kgatilin/archmotif/internal/metrics"
)

// registry holds the package-level set of registered rules. Keyed by
// Name(); duplicate registration panics (an init() bug, not user
// input). Mirrors internal/metrics/ ADR-011.
var (
	registryMu sync.RWMutex
	registry   = map[string]Rule{}
)

// Register adds r to the package-level registry. Intended to be called
// from init(); see ADR-011 for the rationale and ADR-019 for the
// proposer-specific layout. Panics on duplicate names so the bug fails
// loud at process start rather than silently shadowing a rule.
func Register(r Rule) {
	registryMu.Lock()
	defer registryMu.Unlock()
	name := r.Name()
	if name == "" {
		panic("propose.Register: empty rule name")
	}
	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("propose.Register: duplicate rule %q", name))
	}
	registry[name] = r
}

// Lookup returns the registered rule with the given name and a
// boolean indicating presence.
func Lookup(name string) (Rule, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	r, ok := registry[name]
	return r, ok
}

// All returns every registered rule, sorted by name. The returned
// slice is a fresh copy: callers may sort or mutate it freely.
func All() []Rule {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]Rule, 0, len(registry))
	for _, r := range registry {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// Names returns the registered rule names, sorted.
func Names() []string {
	rs := All()
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.Name())
	}
	return out
}

// Proposer wires registered rules to a graph and an anomaly stream.
//
// A Proposer is created with NewProposer (uses every registered rule)
// or NewProposerWith (explicit subset, useful for tests). The zero
// value is not usable.
type Proposer struct {
	rules []Rule
}

// NewProposer returns a Proposer driven by every registered rule.
func NewProposer() *Proposer {
	return &Proposer{rules: All()}
}

// NewProposerWith returns a Proposer driven by the given rules. Used
// by tests that want to isolate one rule from the rest of the
// registry.
func NewProposerWith(rules ...Rule) *Proposer {
	cp := make([]Rule, len(rules))
	copy(cp, rules)
	return &Proposer{rules: cp}
}

// Rules returns the rules this Proposer was constructed with, in
// stable order.
func (p *Proposer) Rules() []Rule {
	out := make([]Rule, len(p.rules))
	copy(out, p.rules)
	return out
}

// Propose evaluates each anomaly against each rule and returns the
// resulting Proposals.
//
// Pipeline (per ADR-022):
//
//  1. Group anomalies by (Metric, SourceRecord.Target) — Stage 4 emits
//     one Anomaly per motif instance (ADR-021), so the proposer keeps
//     the highest-scoring representative per group before applying
//     rules. Without this dedup the rule fires N times for one logical
//     extract.
//  2. For each representative anomaly we walk the rule list in
//     registry order. The first rule whose Trigger returns true gets
//     Apply called.
//  3. Conflict resolution = highest-score on overlapping member-node
//     sets. "Overlap" = any shared NodeID across the two proposals'
//     trigger Region.Members ∪ Samples values. Ties break by trigger
//     order. Losers move into Result.Skipped.
//
// Errors from Apply do not abort: each is captured and surfaced via
// the Errors slice of the returned Result.
func (p *Proposer) Propose(g *mgraph.Graph, anoms []anomalies.Anomaly) Result {
	res := Result{}
	reps := dedupAnomalies(anoms)
	type candidate struct {
		prop    *Proposal
		score   float64
		members map[string]struct{}
	}
	var candidates []candidate
	for _, a := range reps {
		rec := recordFromAnomaly(a)
		for _, rule := range p.rules {
			if !rule.Trigger(rec, g) {
				continue
			}
			prop, err := rule.Apply(g, rec)
			if err != nil {
				res.Errors = append(res.Errors, RuleError{Rule: rule.Name(), Target: rec.Target, Err: err})
				break
			}
			if prop == nil {
				break
			}
			candidates = append(candidates, candidate{
				prop:    prop,
				score:   a.Score,
				members: proposalMemberSet(prop, a),
			})
			break
		}
	}

	// Conflict resolution: greedy by score (desc). Iterate in trigger
	// order to break ties deterministically.
	accepted := []int{} // indices into candidates
	skipped := []int{}
	order := make([]int, len(candidates))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool {
		return candidates[order[i]].score > candidates[order[j]].score
	})
	for _, idx := range order {
		c := candidates[idx]
		conflict := false
		for _, accIdx := range accepted {
			if membersOverlap(c.members, candidates[accIdx].members) {
				conflict = true
				break
			}
		}
		if conflict {
			skipped = append(skipped, idx)
			continue
		}
		accepted = append(accepted, idx)
	}
	// Surface Proposals in trigger order (not score order) so the CLI
	// can present them in the order the anomaly stream arrived; the
	// caller sorts as needed.
	sort.Ints(accepted)
	sort.Ints(skipped)
	for _, idx := range accepted {
		res.Proposals = append(res.Proposals, candidates[idx].prop)
	}
	for _, idx := range skipped {
		res.Skipped = append(res.Skipped, candidates[idx].prop)
	}
	return res
}

// ProposeFromRecords is a back-compat helper that wraps each Record in
// a zero-score Anomaly. Used by tests that hand-build Records before
// Stage 4 wiring; production callers should use Propose with real
// anomalies.
func (p *Proposer) ProposeFromRecords(g *mgraph.Graph, recs []metrics.Record) Result {
	wrapped := make([]anomalies.Anomaly, 0, len(recs))
	for _, r := range recs {
		members := membersFromDetails(r.Details)
		a := anomalies.Anomaly{
			Metric:   r.Metric,
			Detector: r.Metric,
			Score:    r.Value,
			Region: anomalies.Region{
				Kind:    string(r.Scope),
				Members: members,
			},
			SourceRecord: anomalies.SourceRecord{
				Scope:   string(r.Scope),
				Target:  r.Target,
				Value:   r.Value,
				Details: r.Details,
			},
		}
		wrapped = append(wrapped, a)
	}
	return p.Propose(g, wrapped)
}

// Result aggregates the output of a Propose call.
type Result struct {
	// Proposals are the accepted proposals, in trigger order.
	Proposals []*Proposal
	// Skipped are proposals that lost a score-based conflict to a
	// higher-scored proposal (ADR-022). Surfaced so a caller can audit
	// the dropped candidates.
	Skipped []*Proposal
	// Errors are per-rule failures during Apply. Each carries the
	// rule name and the offending anomaly target.
	Errors []RuleError
}

// RuleError captures the failure of one rule's Apply so the proposer
// can keep going and the caller can report a non-zero exit.
type RuleError struct {
	Rule   string
	Target string
	Err    error
}

// Error returns the error string for log output.
func (e RuleError) Error() string {
	return fmt.Sprintf("rule %s on %s: %v", e.Rule, e.Target, e.Err)
}

// Unwrap returns the wrapped error.
func (e RuleError) Unwrap() error { return e.Err }

// dedupAnomalies keeps the highest-scoring Anomaly per
// (Metric, SourceRecord.Target). Stage 4 emits one Anomaly per motif
// instance; the proposer wants one per group. Ties keep the first
// encountered for trigger-order determinism.
func dedupAnomalies(anoms []anomalies.Anomaly) []anomalies.Anomaly {
	type key struct{ metric, target string }
	bestIdx := map[key]int{}
	out := make([]anomalies.Anomaly, 0, len(anoms))
	for i, a := range anoms {
		k := key{metric: a.Metric, target: a.SourceRecord.Target}
		if k.target == "" {
			// Graph-scope anomalies have no group — keep them all.
			out = append(out, a)
			continue
		}
		if existing, ok := bestIdx[k]; ok {
			if a.Score > out[existing].Score {
				out[existing] = a
			}
			_ = i
			continue
		}
		bestIdx[k] = len(out)
		out = append(out, a)
	}
	return out
}

// recordFromAnomaly reconstructs the metrics.Record the rule expects
// from an anomaly's SourceRecord. The rule's Trigger is written
// against Record details, so the proposer keeps the contract stable
// even though the public Propose signature now consumes Anomaly.
func recordFromAnomaly(a anomalies.Anomaly) metrics.Record {
	scope := metrics.Scope(a.SourceRecord.Scope)
	if scope == "" {
		scope = metrics.Scope(a.Region.Kind)
	}
	return metrics.Record{
		Metric:  a.Metric,
		Scope:   scope,
		Target:  a.SourceRecord.Target,
		Value:   a.SourceRecord.Value,
		Details: a.SourceRecord.Details,
	}
}

// proposalMemberSet returns the set of node IDs the proposal "owns"
// for conflict-overlap purposes: the Anomaly's Region members ∪ the
// IDs the rule recorded in the proposal's Samples.
func proposalMemberSet(prop *Proposal, a anomalies.Anomaly) map[string]struct{} {
	out := map[string]struct{}{}
	for _, id := range a.Region.Members {
		if id != "" {
			out[id] = struct{}{}
		}
	}
	for _, sample := range prop.Samples {
		for k, v := range sample {
			if k == "" || v == "" {
				continue
			}
			// Skip non-ID metadata keys (signature fingerprint, index).
			if k == "_index" || k == "MethodSignature" || k == "MethodName" {
				continue
			}
			out[v] = struct{}{}
		}
	}
	return out
}

// membersOverlap reports whether the two member sets share any
// element. Iterates the smaller set for speed.
func membersOverlap(a, b map[string]struct{}) bool {
	if len(a) > len(b) {
		a, b = b, a
	}
	for id := range a {
		if _, ok := b[id]; ok {
			return true
		}
	}
	return false
}

// membersFromDetails extracts a flat list of member node IDs from a
// motif_redundancy details map. Returns nil for non-motif records.
// Used only by ProposeFromRecords (back-compat).
func membersFromDetails(details map[string]any) []string {
	if details == nil {
		return nil
	}
	insts, ok := instancesFromDetails(details)
	if !ok {
		return nil
	}
	seen := map[string]struct{}{}
	out := []string{}
	for _, ins := range insts {
		for _, id := range ins {
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}
