// Package proposetest builds hand-crafted typed graphs and matching
// motif_redundancy Records used by Stage 5 (propose) tests.
//
// Following the same convention as internal/metrics/metricstest:
// fixtures are programmatic *graph.Graph instances rather than
// JSON files under testdata/. ADR-015 records the rationale (Go's
// toolchain excludes testdata/ from compilation, the rule consumes
// graph.Graph directly).
package proposetest

import (
	"sort"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
	"github.com/kgatilin/archmotif/internal/metrics"
)

// MotifTriple is one motif instance: a Type ("Impl") that contains a
// Method, plus a separate "shared interface candidate" Type ("Iface")
// that the Impl Implements.
type MotifTriple struct {
	Pkg      string
	ImplName string
	IfaceID  string // shared across instances when extracting an interface
}

// Triple builds the canonical extract-interface motif fixture: one
// shared Iface Type, plus n {Impl, Method} pairs each with an
// Implements edge to Iface and a Contains edge to its Method.
//
// markContractIdx, when ≥ 0, marks the Impl at that index as a
// contract (per ADR-009). Used to test the contract-exclusion path.
//
// The motif metric returns one Region record per group of isomorphic
// instances. Triple separately returns a hand-built Record matching
// the shape Stage 3 emits — so tests can drive the rule without
// re-running the (expensive) motif metric.
func Triple(n int, markContractIdx int) (*mgraph.Graph, metrics.Record) {
	g := mgraph.New()
	pkgID := "pkg:fixture/triple"
	g.AddNode(mgraph.Node{ID: pkgID, Kind: mgraph.NodePackage, Name: "triple"})

	ifaceID := "fixture/triple/iface.go:1:1:type:Reader"
	g.AddNode(mgraph.Node{ID: ifaceID, Kind: mgraph.NodeType, Name: "Reader"})
	_, _ = g.AddEdge(mgraph.Edge{From: pkgID, To: ifaceID, Kind: mgraph.EdgeContains})

	instances := make([][]string, 0, n)
	for i := 0; i < n; i++ {
		implID, methodID := addImplWithMethod(g, pkgID, i)
		// Implements: Impl -> Iface
		_, _ = g.AddEdge(mgraph.Edge{From: implID, To: ifaceID, Kind: mgraph.EdgeImplements})
		members := []string{ifaceID, implID, methodID}
		sort.Strings(members)
		instances = append(instances, members)
	}

	if markContractIdx >= 0 && markContractIdx < n {
		implID := implIDForIndex(markContractIdx)
		g.MarkContract(implID, "interface", "config", nil)
	}

	rec := metrics.Record{
		Metric: "motif_redundancy",
		Scope:  metrics.ScopeRegion,
		Target: "motif-0",
		Value:  float64(n),
		Details: map[string]any{
			"canonical": "k=3;nodes=method,type,type;edges=1-contains->0|2-implements->1|",
			"size":      3,
			"instances": toAnySlice(instances),
		},
	}
	return g, rec
}

// AlmostTriple builds a fixture with n motif instances but where the
// Iface candidate is missing — only Impl + Method per instance. Used
// to test that the rule still emits a Proposal (with Iface unfilled
// in samples) when the participants don't include a separate iface
// node, exercising the role-assignment heuristic.
func AlmostTriple(n int) (*mgraph.Graph, metrics.Record) {
	g := mgraph.New()
	pkgID := "pkg:fixture/almost"
	g.AddNode(mgraph.Node{ID: pkgID, Kind: mgraph.NodePackage, Name: "almost"})
	instances := make([][]string, 0, n)
	for i := 0; i < n; i++ {
		implID, methodID := addImplWithMethod(g, pkgID, i)
		members := []string{implID, methodID}
		sort.Strings(members)
		instances = append(instances, members)
	}
	rec := metrics.Record{
		Metric: "motif_redundancy",
		Scope:  metrics.ScopeRegion,
		Target: "motif-0",
		Value:  float64(n),
		Details: map[string]any{
			"canonical": "k=2;nodes=method,type;edges=1-contains->0|",
			"size":      2,
			"instances": toAnySlice(instances),
		},
	}
	return g, rec
}

func addImplWithMethod(g *mgraph.Graph, pkgID string, i int) (implID, methodID string) {
	implID = implIDForIndex(i)
	methodID = methodIDForIndex(i)
	g.AddNode(mgraph.Node{ID: implID, Kind: mgraph.NodeType, Name: implName(i)})
	g.AddNode(mgraph.Node{ID: methodID, Kind: mgraph.NodeMethod, Name: "Read"})
	_, _ = g.AddEdge(mgraph.Edge{From: pkgID, To: implID, Kind: mgraph.EdgeContains})
	_, _ = g.AddEdge(mgraph.Edge{From: implID, To: methodID, Kind: mgraph.EdgeContains})
	return implID, methodID
}

func implIDForIndex(i int) string {
	return "fixture/triple/" + implName(i) + ".go:1:1:type:" + implName(i)
}

func methodIDForIndex(i int) string {
	return "fixture/triple/" + implName(i) + ".go:5:1:method:Read"
}

func implName(i int) string {
	// Stable lexicographic order regardless of n: T01, T02, ..., T99.
	switch {
	case i < 10:
		return "T0" + string(rune('0'+i))
	default:
		// Two-digit fallback; n stays small in tests so this branch
		// is only exercised when callers pass i >= 10.
		hi := i / 10
		lo := i % 10
		return "T" + string(rune('0'+hi)) + string(rune('0'+lo))
	}
}

func toAnySlice(instances [][]string) []any {
	out := make([]any, 0, len(instances))
	for _, ins := range instances {
		cp := make([]string, len(ins))
		copy(cp, ins)
		out = append(out, cp)
	}
	return out
}

// TwoDisjointTriples builds a single graph holding two independent
// motif×n groups with disjoint member sets, plus the matching motif
// records for each. Used by Stage 5 conflict-resolution tests.
func TwoDisjointTriples(n1, n2 int) (*mgraph.Graph, metrics.Record, metrics.Record) {
	g := mgraph.New()

	pkgA := "pkg:fixture/disjointA"
	g.AddNode(mgraph.Node{ID: pkgA, Kind: mgraph.NodePackage, Name: "disjointA"})
	ifaceA := "fixture/disjointA/iface.go:1:1:type:ReaderA"
	g.AddNode(mgraph.Node{ID: ifaceA, Kind: mgraph.NodeType, Name: "ReaderA"})
	_, _ = g.AddEdge(mgraph.Edge{From: pkgA, To: ifaceA, Kind: mgraph.EdgeContains})
	instA := buildInstances(g, pkgA, ifaceA, "A", n1)
	recA := metrics.Record{
		Metric: "motif_redundancy",
		Scope:  metrics.ScopeRegion,
		Target: "motif-disjointA",
		Value:  float64(n1),
		Details: map[string]any{
			"canonical": "k=3;nodes=method,type,type;edges=1-contains->0|2-implements->1|",
			"size":      3,
			"instances": toAnySlice(instA),
		},
	}

	pkgB := "pkg:fixture/disjointB"
	g.AddNode(mgraph.Node{ID: pkgB, Kind: mgraph.NodePackage, Name: "disjointB"})
	ifaceB := "fixture/disjointB/iface.go:1:1:type:ReaderB"
	g.AddNode(mgraph.Node{ID: ifaceB, Kind: mgraph.NodeType, Name: "ReaderB"})
	_, _ = g.AddEdge(mgraph.Edge{From: pkgB, To: ifaceB, Kind: mgraph.EdgeContains})
	instB := buildInstances(g, pkgB, ifaceB, "B", n2)
	recB := metrics.Record{
		Metric: "motif_redundancy",
		Scope:  metrics.ScopeRegion,
		Target: "motif-disjointB",
		Value:  float64(n2),
		Details: map[string]any{
			"canonical": "k=3;nodes=method,type,type;edges=1-contains->0|2-implements->1|",
			"size":      3,
			"instances": toAnySlice(instB),
		},
	}

	return g, recA, recB
}

func buildInstances(g *mgraph.Graph, pkgID, ifaceID, tag string, n int) [][]string {
	out := make([][]string, 0, n)
	for i := 0; i < n; i++ {
		impl := "fixture/disjoint" + tag + "/" + tag + implName(i) + ".go:1:1:type:" + tag + implName(i)
		method := "fixture/disjoint" + tag + "/" + tag + implName(i) + ".go:5:1:method:Read"
		g.AddNode(mgraph.Node{ID: impl, Kind: mgraph.NodeType, Name: tag + implName(i)})
		g.AddNode(mgraph.Node{ID: method, Kind: mgraph.NodeMethod, Name: "Read"})
		_, _ = g.AddEdge(mgraph.Edge{From: pkgID, To: impl, Kind: mgraph.EdgeContains})
		_, _ = g.AddEdge(mgraph.Edge{From: impl, To: method, Kind: mgraph.EdgeContains})
		_, _ = g.AddEdge(mgraph.Edge{From: impl, To: ifaceID, Kind: mgraph.EdgeImplements})
		members := []string{ifaceID, impl, method}
		sort.Strings(members)
		out = append(out, members)
	}
	return out
}

// MembersFromRecord flattens the unique member node IDs from a motif
// metrics.Record's instances detail. Returned sorted, deduped.
func MembersFromRecord(rec metrics.Record) []string {
	if rec.Details == nil {
		return nil
	}
	raw, ok := rec.Details["instances"]
	if !ok {
		return nil
	}
	seen := map[string]struct{}{}
	switch insts := raw.(type) {
	case [][]string:
		for _, ins := range insts {
			for _, id := range ins {
				seen[id] = struct{}{}
			}
		}
	case []any:
		for _, item := range insts {
			switch ins := item.(type) {
			case []string:
				for _, id := range ins {
					seen[id] = struct{}{}
				}
			case []any:
				for _, x := range ins {
					if s, ok := x.(string); ok {
						seen[s] = struct{}{}
					}
				}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
