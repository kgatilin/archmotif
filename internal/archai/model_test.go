package archai_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kgatilin/archmotif/internal/archai"
	"github.com/kgatilin/archmotif/internal/graph"
)

// fixtureGraph builds a small typed graph that exercises every
// projection branch in archai.FromGraph: loaded + foreign packages,
// type / function / method symbols, contains / dependsOn / calls /
// implements / usesType / returns edges, role metadata (package and
// type), and a contract marker.
//
// Layout:
//
//	pkg:example/domain     [role: domain, contract] User (type, contract, value_object)
//	pkg:example/adapter    [role: outbound_adapter] UserDTO (type, adapter_dto), FromUser (function), FromUser body calls New
//	pkg:example/domain     New (function), Greeter (interface; User implements)
//	pkg:foreign/io         [foreign]
func fixtureGraph(t *testing.T) *graph.Graph {
	t.Helper()
	g := graph.New()

	addPkg := func(id, name, qname string, foreign bool) {
		_, _ = g.AddNode(graph.Node{
			ID:    id,
			Kind:  graph.NodePackage,
			Name:  name,
			QName: qname,
			Attrs: map[string]any{"foreign": foreign},
		})
	}
	addPkg("pkg:example/domain", "domain", "example/domain", false)
	addPkg("pkg:example/adapter", "adapter", "example/adapter", false)
	addPkg("pkg:foreign/io", "io", "foreign/io", true)

	g.SetRole("pkg:example/domain", graph.RolePackageDomain, "package")
	g.SetRole("pkg:example/adapter", graph.RolePackageOutboundAdapter, "package")

	addType := func(id, name, qname, owner string, role graph.Role) {
		pos := graph.Position{File: "example/" + filepath.Base(filepath.Dir(qname)) + ".go", Line: 1, Col: 1}
		_, _ = g.AddNode(graph.Node{
			ID:    id,
			Kind:  graph.NodeType,
			Name:  name,
			QName: qname,
			Pos:   pos,
		})
		_, _ = g.AddEdge(graph.Edge{From: owner, To: id, Kind: graph.EdgeContains})
		if role != "" {
			g.SetRole(id, role, "type")
		}
	}
	addType("type:User", "User", "example/domain.User", "pkg:example/domain", graph.RoleTypeValueObject)
	addType("type:Greeter", "Greeter", "example/domain.Greeter", "pkg:example/domain", graph.RoleTypePort)
	addType("type:UserDTO", "UserDTO", "example/adapter.UserDTO", "pkg:example/adapter", graph.RoleTypeAdapterDTO)

	// Contract marker on User (mirrors ADR-009 Stage 2 output).
	g.MarkContract("type:User", "type", "config", nil)

	addFunc := func(id, name, qname, owner string, kind graph.NodeKind) {
		pos := graph.Position{File: "example/file.go", Line: 10, Col: 1}
		_, _ = g.AddNode(graph.Node{
			ID:    id,
			Kind:  kind,
			Name:  name,
			QName: qname,
			Pos:   pos,
		})
		_, _ = g.AddEdge(graph.Edge{From: owner, To: id, Kind: graph.EdgeContains})
	}
	addFunc("func:New", "New", "example/domain.New", "pkg:example/domain", graph.NodeFunction)
	addFunc("func:FromUser", "FromUser", "example/adapter.FromUser", "pkg:example/adapter", graph.NodeFunction)
	addFunc("meth:User.Greet", "Greet", "(*example/domain.User).Greet", "pkg:example/domain", graph.NodeMethod)

	// Edges between packages and symbols.
	_, _ = g.AddEdge(graph.Edge{From: "pkg:example/adapter", To: "pkg:example/domain", Kind: graph.EdgeDependsOn})
	_, _ = g.AddEdge(graph.Edge{From: "pkg:example/adapter", To: "pkg:foreign/io", Kind: graph.EdgeDependsOn})
	_, _ = g.AddEdge(graph.Edge{From: "func:FromUser", To: "func:New", Kind: graph.EdgeCalls})
	_, _ = g.AddEdge(graph.Edge{From: "func:FromUser", To: "type:User", Kind: graph.EdgeUsesType})
	_, _ = g.AddEdge(graph.Edge{From: "func:New", To: "type:User", Kind: graph.EdgeReturns})
	_, _ = g.AddEdge(graph.Edge{From: "type:User", To: "type:Greeter", Kind: graph.EdgeImplements})

	return g
}

// TestFromGraph_BasicShape asserts the high-level invariants of the
// projection: schema id and version, every model section is populated,
// and counts in source.counts match the actual slice lengths.
func TestFromGraph_BasicShape(t *testing.T) {
	g := fixtureGraph(t)
	m := archai.FromGraph(g)

	if m.Schema.Name != archai.SchemaName {
		t.Errorf("schema.name = %q, want %q", m.Schema.Name, archai.SchemaName)
	}
	if m.Schema.Version != archai.CurrentSchemaVersion {
		t.Errorf("schema.version = %d, want %d", m.Schema.Version, archai.CurrentSchemaVersion)
	}
	if m.Source.Tool != "archmotif" || m.Source.Format != "archai-model" {
		t.Errorf("source = %+v, want tool=archmotif format=archai-model", m.Source)
	}
	if len(m.Packages) != 3 {
		t.Errorf("packages = %d, want 3", len(m.Packages))
	}
	if len(m.Symbols) != 6 {
		t.Errorf("symbols = %d, want 6 (User, Greeter, UserDTO, New, FromUser, User.Greet)", len(m.Symbols))
	}
	if m.Source.Counts.Packages != len(m.Packages) {
		t.Errorf("counts.packages = %d, want %d", m.Source.Counts.Packages, len(m.Packages))
	}
	if m.Source.Counts.Symbols != len(m.Symbols) {
		t.Errorf("counts.symbols = %d, want %d", m.Source.Counts.Symbols, len(m.Symbols))
	}
	if m.Source.Counts.Dependencies != len(m.Dependencies) {
		t.Errorf("counts.dependencies = %d, want %d", m.Source.Counts.Dependencies, len(m.Dependencies))
	}
}

// TestFromGraph_LayerMapping confirms package-level role metadata
// flows into Package.Layer + the role:* stereotype.
func TestFromGraph_LayerMapping(t *testing.T) {
	g := fixtureGraph(t)
	m := archai.FromGraph(g)

	got := map[string]string{}
	stereos := map[string][]string{}
	for _, p := range m.Packages {
		got[p.ID] = p.Layer
		stereos[p.ID] = append([]string(nil), p.Stereotype...)
	}
	want := map[string]string{
		"pkg:example/domain":  string(graph.RolePackageDomain),
		"pkg:example/adapter": string(graph.RolePackageOutboundAdapter),
		"pkg:foreign/io":      "",
	}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("layer for %s = %q, want %q", id, got[id], w)
		}
	}
	if !contains(stereos["pkg:example/domain"], "role:domain") {
		t.Errorf("pkg:example/domain stereotypes = %v, missing role:domain", stereos["pkg:example/domain"])
	}
}

// TestFromGraph_SymbolStereotypesAndContract checks that contract
// markers and type-level roles propagate.
func TestFromGraph_SymbolStereotypesAndContract(t *testing.T) {
	g := fixtureGraph(t)
	m := archai.FromGraph(g)

	user := findSymbol(t, m, "type:User")
	if !user.IsContract {
		t.Errorf("User.isContract = false, want true")
	}
	if !contains(user.Stereotype, "role:value_object") || !contains(user.Stereotype, "contract") {
		t.Errorf("User.stereotypes = %v, want both role:value_object and contract", user.Stereotype)
	}
	if user.Package != "pkg:example/domain" {
		t.Errorf("User.package = %q, want pkg:example/domain", user.Package)
	}
	if user.Facet != "model" {
		t.Errorf("User.facet = %q, want model (model facet for type kind)", user.Facet)
	}

	greet := findSymbol(t, m, "meth:User.Greet")
	if greet.Facet != "behavior" {
		t.Errorf("User.Greet.facet = %q, want behavior", greet.Facet)
	}
}

// TestFromGraph_DependencyMapping asserts that every edge kind we
// claim to project actually appears with the expected relation name
// and the implementation flag is set on implements edges only.
func TestFromGraph_DependencyMapping(t *testing.T) {
	g := fixtureGraph(t)
	m := archai.FromGraph(g)

	type sig struct{ from, to, rel string }
	got := map[sig]archai.Dependency{}
	for _, d := range m.Dependencies {
		got[sig{d.From, d.To, d.Relation}] = d
	}

	wantRels := []sig{
		{"pkg:example/adapter", "pkg:example/domain", "depends_on"},
		{"pkg:example/adapter", "pkg:foreign/io", "depends_on"},
		{"func:FromUser", "func:New", "calls"},
		{"func:FromUser", "type:User", "uses_type"},
		{"func:New", "type:User", "returns"},
		{"type:User", "type:Greeter", "implements"},
	}
	for _, w := range wantRels {
		if _, ok := got[w]; !ok {
			t.Errorf("missing dependency %+v", w)
		}
	}
	impl := got[sig{"type:User", "type:Greeter", "implements"}]
	if !impl.IsImplementation {
		t.Errorf("implements dep IsImplementation = false, want true")
	}
	if impl.Kind != "implements" {
		t.Errorf("implements dep kind = %q, want implements", impl.Kind)
	}

	// "contains" edges are emitted but flagged via FromKind/ToKind.
	contains := got[sig{"pkg:example/domain", "type:User", "contains"}]
	if contains.FromKind != string(graph.NodePackage) || contains.ToKind != string(graph.NodeType) {
		t.Errorf("contains dep kinds = %s -> %s, want package -> type", contains.FromKind, contains.ToKind)
	}
}

// TestFromGraph_Determinism verifies that two projections of the same
// graph produce byte-identical JSON. Critical for snapshot tests and
// downstream registries that key on document bytes.
func TestFromGraph_Determinism(t *testing.T) {
	g := fixtureGraph(t)

	var a, b bytes.Buffer
	if err := archai.WriteJSON(&a, archai.FromGraph(g)); err != nil {
		t.Fatalf("write a: %v", err)
	}
	if err := archai.WriteJSON(&b, archai.FromGraph(g)); err != nil {
		t.Fatalf("write b: %v", err)
	}
	if a.String() != b.String() {
		t.Errorf("non-deterministic export — repeated runs produced different bytes")
	}
}

// TestFromGraph_Snapshot pins the projection of a fixed graph against
// a golden file. Run with `go test -update` to refresh.
func TestFromGraph_Snapshot(t *testing.T) {
	g := fixtureGraph(t)
	var buf bytes.Buffer
	if err := archai.WriteJSON(&buf, archai.FromGraph(g)); err != nil {
		t.Fatalf("write json: %v", err)
	}
	got := buf.Bytes()

	goldenPath := filepath.Join("testdata", "fixture.archai-model.json")
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatalf("update golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v\n(run with UPDATE_GOLDEN=1 to create)", goldenPath, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("snapshot mismatch.\n--- got\n%s\n--- want\n%s", got, want)
	}
}

// TestWriteYAML_RoundsTripStructure asserts the YAML emitter produces
// a non-empty document containing every top-level key.
func TestWriteYAML_RoundsTripStructure(t *testing.T) {
	g := fixtureGraph(t)
	var buf bytes.Buffer
	if err := archai.WriteYAML(&buf, archai.FromGraph(g)); err != nil {
		t.Fatalf("yaml: %v", err)
	}
	out := buf.String()
	for _, key := range []string{"schema:", "source:", "facets:", "stereotypes:", "packages:", "symbols:", "dependencies:"} {
		if !strings.Contains(out, key) {
			t.Errorf("yaml missing %q\n%s", key, out)
		}
	}
}

// TestFromGraph_NilSafe ensures FromGraph handles a nil/empty graph
// without panicking and emits a well-formed empty document.
func TestFromGraph_NilSafe(t *testing.T) {
	m := archai.FromGraph(nil)
	if m.Schema.Name == "" {
		t.Fatalf("nil graph: schema name unset")
	}
	var buf bytes.Buffer
	if err := archai.WriteJSON(&buf, m); err != nil {
		t.Fatalf("write nil: %v", err)
	}
	var dec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &dec); err != nil {
		t.Fatalf("decode nil json: %v", err)
	}
}

func findSymbol(t *testing.T, m archai.ArchitectureModel, id string) archai.Symbol {
	t.Helper()
	for _, s := range m.Symbols {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("symbol %s not found in model", id)
	return archai.Symbol{}
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}
