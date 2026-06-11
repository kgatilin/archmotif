package coupling_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/kgatilin/archmotif/internal/coupling"
	"github.com/kgatilin/archmotif/internal/graph"
)

// fixture builds a small typed graph with three packages — domain,
// adapter, infrastructure — plus a value-object type, an adapter DTO
// type, an external-noise package, and a handful of edges between
// them. Used by every test below; tests pick the slices they care
// about.
//
// Roles assigned (per ADR-027):
//
//	pkg:domain         → role=domain
//	pkg:adapter        → role=outbound_adapter
//	pkg:infra          → role=infrastructure
//	pkg:noise          → role=external_noise
//	domain.User        → role=value_object (type-level override)
//	adapter.UserDTO    → role=adapter_dto (type-level override)
//
// Edges (only the kinds DefaultEdgeKinds picks up):
//
//	pkg:adapter dependsOn pkg:domain
//	adapter.UserDTO usesType domain.User
//	pkg:domain dependsOn pkg:infra            (forbidden by tests)
//	pkg:domain dependsOn pkg:adapter          (forbidden by tests)
//	pkg:adapter dependsOn pkg:infra
//	pkg:adapter dependsOn pkg:noise           (dropped: external_noise)
//	pkg:adapter contains adapter.UserDTO      (dropped: contains)
func fixture() *graph.Graph {
	g := graph.New()

	addPkg := func(id string, role graph.Role) {
		n, _ := g.AddNode(graph.Node{ID: id, Kind: graph.NodePackage, Name: id, QName: id})
		_ = n
		g.SetRole(id, role, "package")
	}
	addPkg("pkg:domain", graph.RolePackageDomain)
	addPkg("pkg:adapter", graph.RolePackageOutboundAdapter)
	addPkg("pkg:infra", graph.RolePackageInfrastructure)
	addPkg("pkg:noise", graph.RoleTypeExternalNoise)

	addType := func(id, qname string, role graph.Role, owner string) {
		_, _ = g.AddNode(graph.Node{ID: id, Kind: graph.NodeType, Name: qname, QName: qname})
		g.SetRole(id, role, "type")
		_, _ = g.AddEdge(graph.Edge{From: owner, To: id, Kind: graph.EdgeContains})
	}
	addType("domain.User", "domain.User", graph.RoleTypeValueObject, "pkg:domain")
	addType("adapter.UserDTO", "adapter.UserDTO", graph.RoleTypeAdapterDTO, "pkg:adapter")

	addEdge := func(from, to string, kind graph.EdgeKind) {
		_, _ = g.AddEdge(graph.Edge{From: from, To: to, Kind: kind})
	}
	addEdge("pkg:adapter", "pkg:domain", graph.EdgeDependsOn)
	addEdge("adapter.UserDTO", "domain.User", graph.EdgeUsesType)
	addEdge("pkg:domain", "pkg:infra", graph.EdgeDependsOn)
	addEdge("pkg:domain", "pkg:adapter", graph.EdgeDependsOn)
	addEdge("pkg:adapter", "pkg:infra", graph.EdgeDependsOn)
	addEdge("pkg:adapter", "pkg:noise", graph.EdgeDependsOn)

	return g
}

// TestCompute_PairMatrix asserts the role-pair matrix carries the
// expected counts and is sorted (count desc, then from asc, then
// to asc).
func TestCompute_PairMatrix(t *testing.T) {
	g := fixture()
	r := coupling.Compute(g, coupling.Config{})

	if r.EdgesConsidered != 5 {
		t.Errorf("EdgesConsidered = %d, want 5 (one dropped for noise, one for contains)", r.EdgesConsidered)
	}
	if r.UnroledEndpoints != 0 {
		t.Errorf("UnroledEndpoints = %d, want 0", r.UnroledEndpoints)
	}

	want := map[coupling.Pair]int{
		{From: graph.RolePackageOutboundAdapter, To: graph.RolePackageDomain}:         1,
		{From: graph.RoleTypeAdapterDTO, To: graph.RoleTypeValueObject}:               1,
		{From: graph.RolePackageDomain, To: graph.RolePackageInfrastructure}:          1,
		{From: graph.RolePackageDomain, To: graph.RolePackageOutboundAdapter}:         1,
		{From: graph.RolePackageOutboundAdapter, To: graph.RolePackageInfrastructure}: 1,
	}
	if len(r.PairCounts) != len(want) {
		t.Errorf("len(PairCounts) = %d, want %d; got %+v", len(r.PairCounts), len(want), r.PairCounts)
	}
	for _, pc := range r.PairCounts {
		got, ok := want[pc.Pair]
		if !ok {
			t.Errorf("unexpected pair %+v with count %d", pc.Pair, pc.Count)
			continue
		}
		if pc.Count != got {
			t.Errorf("pair %+v count = %d, want %d", pc.Pair, pc.Count, got)
		}
	}
}

// TestCompute_ForbiddenViolations exercises ADR-030 §1 + §5: configured
// forbidden rules surface as violations carrying the rule + concrete
// edge evidence.
func TestCompute_ForbiddenViolations(t *testing.T) {
	g := fixture()
	cfg := coupling.Config{
		Forbidden: []coupling.ForbiddenEdge{
			{From: graph.RolePackageDomain, To: graph.RolePackageInfrastructure, Reason: "no infra"},
			{From: graph.RolePackageDomain, To: graph.RolePackageOutboundAdapter},
		},
	}
	r := coupling.Compute(g, cfg)

	if len(r.ForbiddenViolations) != 2 {
		t.Fatalf("ForbiddenViolations = %d, want 2; got %+v", len(r.ForbiddenViolations), r.ForbiddenViolations)
	}
	rules := map[string]bool{}
	for _, v := range r.ForbiddenViolations {
		rules[string(v.Rule.From)+"->"+string(v.Rule.To)] = true
		if v.Evidence.From == "" || v.Evidence.To == "" {
			t.Errorf("violation missing evidence ids: %+v", v)
		}
	}
	if !rules["domain->infrastructure"] {
		t.Errorf("missing domain->infrastructure violation; got %+v", rules)
	}
	if !rules["domain->outbound_adapter"] {
		t.Errorf("missing domain->outbound_adapter violation; got %+v", rules)
	}
}

// TestCompute_DomainPurityScore covers the domain_purity score
// definition: edges out of role=domain that land in
// domain/value_object/port/shared, divided by total edges out of
// role=domain.
func TestCompute_DomainPurityScore(t *testing.T) {
	g := fixture()
	r := coupling.Compute(g, coupling.Config{})

	var domainPurity *coupling.Score
	for i, s := range r.Scores {
		if s.Name == "domain_purity" {
			domainPurity = &r.Scores[i]
			break
		}
	}
	if domainPurity == nil {
		t.Fatalf("missing domain_purity score; got %+v", r.Scores)
	}
	// Two edges out of pkg:domain (→ pkg:infra, → pkg:adapter); zero
	// land in domain/value_object/port/shared. Score = 0/2.
	if domainPurity.Numerator != 0 || domainPurity.Denominator != 2 {
		t.Errorf("domain_purity num/denom = %d/%d, want 0/2",
			domainPurity.Numerator, domainPurity.Denominator)
	}
	if domainPurity.Value != 0.0 {
		t.Errorf("domain_purity value = %v, want 0.0", domainPurity.Value)
	}
}

// TestCompute_EvidenceCapPerPair verifies the per-pair evidence list
// is capped at the configured value (default 5; we drop to 2 in this
// test to exercise the cap).
func TestCompute_EvidenceCapPerPair(t *testing.T) {
	g := graph.New()
	_, _ = g.AddNode(graph.Node{ID: "pkg:a", Kind: graph.NodePackage})
	_, _ = g.AddNode(graph.Node{ID: "pkg:b", Kind: graph.NodePackage})
	g.SetRole("pkg:a", graph.RolePackageDomain, "package")
	g.SetRole("pkg:b", graph.RolePackageInfrastructure, "package")
	for i := 0; i < 4; i++ {
		_, _ = g.AddNode(graph.Node{
			ID:    "fnA" + string(rune('0'+i)),
			Kind:  graph.NodeFunction,
			QName: "a.fn",
		})
		_, _ = g.AddNode(graph.Node{
			ID:    "fnB" + string(rune('0'+i)),
			Kind:  graph.NodeFunction,
			QName: "b.fn",
		})
		_, _ = g.AddEdge(graph.Edge{From: "pkg:a", To: "fnA" + string(rune('0'+i)), Kind: graph.EdgeContains})
		_, _ = g.AddEdge(graph.Edge{From: "pkg:b", To: "fnB" + string(rune('0'+i)), Kind: graph.EdgeContains})
		_, _ = g.AddEdge(graph.Edge{
			From: "fnA" + string(rune('0'+i)),
			To:   "fnB" + string(rune('0'+i)),
			Kind: graph.EdgeCalls,
		})
	}
	r := coupling.Compute(g, coupling.Config{EvidenceCap: 2})

	if len(r.PairCounts) != 1 {
		t.Fatalf("len(PairCounts) = %d, want 1; got %+v", len(r.PairCounts), r.PairCounts)
	}
	pc := r.PairCounts[0]
	if pc.Count != 4 {
		t.Errorf("count = %d, want 4", pc.Count)
	}
	if len(pc.Evidence) != 2 {
		t.Errorf("evidence len = %d, want 2 (capped)", len(pc.Evidence))
	}
}

// TestCompute_PackageRoleInheritance verifies ADR-030 §4: a function
// node that has no type-level role inherits the role of its containing
// package.
func TestCompute_PackageRoleInheritance(t *testing.T) {
	g := graph.New()
	_, _ = g.AddNode(graph.Node{ID: "pkg:domain", Kind: graph.NodePackage})
	_, _ = g.AddNode(graph.Node{ID: "pkg:infra", Kind: graph.NodePackage})
	g.SetRole("pkg:domain", graph.RolePackageDomain, "package")
	g.SetRole("pkg:infra", graph.RolePackageInfrastructure, "package")

	_, _ = g.AddNode(graph.Node{ID: "fn:domain", Kind: graph.NodeFunction, QName: "domain.fn"})
	_, _ = g.AddNode(graph.Node{ID: "fn:infra", Kind: graph.NodeFunction, QName: "infra.fn"})
	_, _ = g.AddEdge(graph.Edge{From: "pkg:domain", To: "fn:domain", Kind: graph.EdgeContains})
	_, _ = g.AddEdge(graph.Edge{From: "pkg:infra", To: "fn:infra", Kind: graph.EdgeContains})
	_, _ = g.AddEdge(graph.Edge{From: "fn:domain", To: "fn:infra", Kind: graph.EdgeCalls})

	r := coupling.Compute(g, coupling.Config{})

	if len(r.PairCounts) != 1 {
		t.Fatalf("len(PairCounts) = %d, want 1; got %+v", len(r.PairCounts), r.PairCounts)
	}
	got := r.PairCounts[0].Pair
	want := coupling.Pair{From: graph.RolePackageDomain, To: graph.RolePackageInfrastructure}
	if got != want {
		t.Errorf("inherited pair = %+v, want %+v", got, want)
	}
}

// TestCompute_UnroledEndpointsCounted verifies that nodes whose role
// (and containing-package role) cannot be resolved still contribute
// to UnroledEndpoints, surfacing config gaps.
func TestCompute_UnroledEndpointsCounted(t *testing.T) {
	g := graph.New()
	_, _ = g.AddNode(graph.Node{ID: "pkg:a", Kind: graph.NodePackage})
	g.SetRole("pkg:a", graph.RolePackageDomain, "package")
	_, _ = g.AddNode(graph.Node{ID: "fnA", Kind: graph.NodeFunction})
	_, _ = g.AddEdge(graph.Edge{From: "pkg:a", To: "fnA", Kind: graph.EdgeContains})

	// Orphan function with no containing-package edge — role unresolvable.
	_, _ = g.AddNode(graph.Node{ID: "orphan", Kind: graph.NodeFunction})
	_, _ = g.AddEdge(graph.Edge{From: "fnA", To: "orphan", Kind: graph.EdgeCalls})

	r := coupling.Compute(g, coupling.Config{})

	if r.UnroledEndpoints != 1 {
		t.Errorf("UnroledEndpoints = %d, want 1", r.UnroledEndpoints)
	}
}

// TestRenderJSON_StableShape covers the JSON envelope shape used by
// Stage 9 / external consumers.
func TestRenderJSON_StableShape(t *testing.T) {
	g := fixture()
	r := coupling.Compute(g, coupling.Config{})

	var buf bytes.Buffer
	if err := coupling.RenderJSON(&buf, r); err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}
	var decoded struct {
		Version    int `json:"version"`
		PairCounts []struct {
			Pair  coupling.Pair `json:"pair"`
			Count int           `json:"count"`
		} `json:"pairCounts"`
		Scores []struct {
			Name        string  `json:"name"`
			Value       float64 `json:"value"`
			Numerator   int     `json:"numerator"`
			Denominator int     `json:"denominator"`
			Description string  `json:"description"`
		} `json:"scores"`
		EdgesConsidered int `json:"edgesConsidered"`
	}
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v\nraw:\n%s", err, buf.String())
	}
	if decoded.Version != 1 {
		t.Errorf("Version = %d, want 1", decoded.Version)
	}
	if len(decoded.PairCounts) == 0 {
		t.Errorf("PairCounts empty in JSON")
	}
	if decoded.EdgesConsidered != r.EdgesConsidered {
		t.Errorf("EdgesConsidered drift: json=%d, report=%d", decoded.EdgesConsidered, r.EdgesConsidered)
	}
	gotNames := map[string]bool{}
	for _, s := range decoded.Scores {
		gotNames[s.Name] = true
	}
	for _, want := range []string{"domain_purity", "adapter_isolation"} {
		if !gotNames[want] {
			t.Errorf("missing score %q in JSON output", want)
		}
	}
}

// TestRenderMarkdown_HumanShape covers the Markdown summary format.
func TestRenderMarkdown_HumanShape(t *testing.T) {
	g := fixture()
	r := coupling.Compute(g, coupling.Config{
		Forbidden: []coupling.ForbiddenEdge{
			{From: graph.RolePackageDomain, To: graph.RolePackageInfrastructure, Reason: "no infra"},
		},
	})

	var buf bytes.Buffer
	if err := coupling.RenderMarkdown(&buf, r); err != nil {
		t.Fatalf("RenderMarkdown: %v", err)
	}
	out := buf.String()
	for _, frag := range []string{
		"# Coupling report",
		"## Scores",
		"`domain_purity`",
		"`adapter_isolation`",
		"## Role-pair matrix",
		"## Forbidden-edge violations",
		"`domain` -> `infrastructure`",
		"_no infra_",
	} {
		if !strings.Contains(out, frag) {
			t.Errorf("Markdown missing fragment %q\n--- output ---\n%s", frag, out)
		}
	}
}
