package metrics_test

import (
	"context"
	"testing"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
	"github.com/kgatilin/archmotif/internal/metrics"
)

// buildLayeredTriad builds a 3-node graph:
//
//	inbound_adapter --Calls--> application  (allowed)
//	application     --Calls--> domain       (allowed)
//	domain          --Calls--> infrastructure (FORBIDDEN by default rules)
//
// Used to assert exactly one layer_mask violation on the forbidden edge.
func buildLayeredTriad() *mgraph.Graph {
	g := mgraph.New()
	add := func(id string, role mgraph.Role) {
		g.AddNode(mgraph.Node{ID: id, Kind: mgraph.NodePackage, Name: id})
		g.SetRole(id, role, "package")
	}
	add("pkg:in", mgraph.RolePackageInboundAdapter)
	add("pkg:app", mgraph.RolePackageApplication)
	add("pkg:dom", mgraph.RolePackageDomain)
	add("pkg:infra", mgraph.RolePackageInfrastructure)
	link := func(from, to string) {
		_, _ = g.AddEdge(mgraph.Edge{From: from, To: to, Kind: mgraph.EdgeCalls})
	}
	link("pkg:in", "pkg:app")
	link("pkg:app", "pkg:dom")
	link("pkg:dom", "pkg:infra") // forbidden
	return g
}

func TestLayerMask_ForbiddenEdgeIsFlagged(t *testing.T) {
	g := buildLayeredTriad()
	m, ok := metrics.Lookup("layer_mask")
	if !ok {
		t.Fatal("layer_mask metric not registered")
	}
	recs, err := m.Compute(context.Background(), g)
	if err != nil {
		t.Fatal(err)
	}
	graphRec := findGraph(recs, "layer_mask")
	if graphRec == nil {
		t.Fatal("missing graph-scope record")
	}
	if graphRec.Value != 1 {
		t.Fatalf("graph value = %v, want 1 violation", graphRec.Value)
	}
	edges := byScope(recs, metrics.ScopeEdge)
	if len(edges) != 1 {
		t.Fatalf("edge records = %d, want 1", len(edges))
	}
	got := edges[0].Details
	if got["from"] != "pkg:dom" || got["to"] != "pkg:infra" {
		t.Fatalf("violation edge = %+v, want pkg:dom→pkg:infra", got)
	}
	if got["fromRole"] != string(mgraph.RolePackageDomain) || got["toRole"] != string(mgraph.RolePackageInfrastructure) {
		t.Fatalf("violation roles = %+v, want domain→infrastructure", got)
	}
}

func TestLayerMask_AllowedEdgesNotFlagged(t *testing.T) {
	// Subset of buildLayeredTriad without the forbidden edge.
	g := mgraph.New()
	add := func(id string, role mgraph.Role) {
		g.AddNode(mgraph.Node{ID: id, Kind: mgraph.NodePackage, Name: id})
		g.SetRole(id, role, "package")
	}
	add("pkg:in", mgraph.RolePackageInboundAdapter)
	add("pkg:app", mgraph.RolePackageApplication)
	add("pkg:dom", mgraph.RolePackageDomain)
	_, _ = g.AddEdge(mgraph.Edge{From: "pkg:in", To: "pkg:app", Kind: mgraph.EdgeCalls})
	_, _ = g.AddEdge(mgraph.Edge{From: "pkg:app", To: "pkg:dom", Kind: mgraph.EdgeCalls})

	m, _ := metrics.Lookup("layer_mask")
	recs, err := m.Compute(context.Background(), g)
	if err != nil {
		t.Fatal(err)
	}
	graphRec := findGraph(recs, "layer_mask")
	if graphRec.Value != 0 {
		t.Fatalf("graph value = %v, want 0", graphRec.Value)
	}
	if edges := byScope(recs, metrics.ScopeEdge); len(edges) != 0 {
		t.Fatalf("edge records = %d, want 0", len(edges))
	}
}
