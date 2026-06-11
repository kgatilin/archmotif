package metrics_test

import (
	"context"
	"testing"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
	"github.com/kgatilin/archmotif/internal/metrics"
)

// buildStarFanout builds a hub node calling 4 leaves (Hub→L1..L4).
// Hub: fanOut=4, fanIn=0 → I=1.0
// Leaves: fanOut=0, fanIn=1 → I=0.0
func buildStarFanout() *mgraph.Graph {
	g := mgraph.New()
	add := func(id string) {
		g.AddNode(mgraph.Node{ID: id, Kind: mgraph.NodeFunction, Name: id})
	}
	add("hub")
	for _, leaf := range []string{"L1", "L2", "L3", "L4"} {
		add(leaf)
		_, _ = g.AddEdge(mgraph.Edge{From: "hub", To: leaf, Kind: mgraph.EdgeCalls})
	}
	return g
}

func TestInstabilityMatrix_StarFanout(t *testing.T) {
	g := buildStarFanout()
	m, ok := metrics.Lookup("instability_matrix")
	if !ok {
		t.Fatal("instability_matrix not registered")
	}
	recs, err := m.Compute(context.Background(), g)
	if err != nil {
		t.Fatal(err)
	}
	nodes := byScope(recs, metrics.ScopeNode)
	if len(nodes) != 5 {
		t.Fatalf("node records = %d, want 5 (hub + 4 leaves)", len(nodes))
	}
	for _, r := range nodes {
		switch r.Target {
		case "hub":
			if r.Value != 1.0 {
				t.Fatalf("hub I = %v, want 1.0 (only fanOut)", r.Value)
			}
			if fanOut, _ := r.Details["fanOut"].(float64); fanOut != 4 {
				t.Fatalf("hub fanOut = %v, want 4", fanOut)
			}
			if fanIn, _ := r.Details["fanIn"].(float64); fanIn != 0 {
				t.Fatalf("hub fanIn = %v, want 0", fanIn)
			}
		case "L1", "L2", "L3", "L4":
			if r.Value != 0.0 {
				t.Fatalf("leaf %s I = %v, want 0.0 (only fanIn)", r.Target, r.Value)
			}
		default:
			t.Fatalf("unexpected node record target: %s", r.Target)
		}
	}
	graphRec := findGraph(recs, "instability_matrix")
	if graphRec == nil {
		t.Fatal("missing graph-scope record")
	}
	// Mean instability = (1.0 + 0 + 0 + 0 + 0) / 5 = 0.2
	if graphRec.Value < 0.19 || graphRec.Value > 0.21 {
		t.Fatalf("mean I = %v, want ≈ 0.2", graphRec.Value)
	}
}

func TestInstabilityMatrix_LayerAggregate(t *testing.T) {
	// Two domain nodes only consume; two infrastructure nodes only produce.
	g := mgraph.New()
	add := func(id string, role mgraph.Role) {
		g.AddNode(mgraph.Node{ID: id, Kind: mgraph.NodePackage, Name: id})
		g.SetRole(id, role, "package")
	}
	add("pkg:d1", mgraph.RolePackageDomain)
	add("pkg:d2", mgraph.RolePackageDomain)
	add("pkg:i1", mgraph.RolePackageInfrastructure)
	add("pkg:i2", mgraph.RolePackageInfrastructure)
	// infra → domain edges (each infra calls each domain).
	for _, from := range []string{"pkg:i1", "pkg:i2"} {
		for _, to := range []string{"pkg:d1", "pkg:d2"} {
			_, _ = g.AddEdge(mgraph.Edge{From: from, To: to, Kind: mgraph.EdgeCalls})
		}
	}
	m, _ := metrics.Lookup("instability_matrix")
	recs, err := m.Compute(context.Background(), g)
	if err != nil {
		t.Fatal(err)
	}
	regions := byScope(recs, metrics.ScopeRegion)
	// We should have two layer rows: domain and infrastructure.
	if len(regions) != 2 {
		t.Fatalf("layer aggregates = %d, want 2", len(regions))
	}
	for _, r := range regions {
		switch r.Target {
		case "layer:domain":
			if r.Value != 0.0 {
				t.Fatalf("domain layer I = %v, want 0 (consume-only)", r.Value)
			}
		case "layer:infrastructure":
			if r.Value != 1.0 {
				t.Fatalf("infrastructure layer I = %v, want 1 (produce-only)", r.Value)
			}
		default:
			t.Fatalf("unexpected layer target: %s", r.Target)
		}
	}
}
