package diagram

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"strings"
	"testing"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// xmlDecoderFromString returns an xml.Decoder reading s. Used by the
// GraphML test to assert well-formedness without bringing in a heavier
// XML library.
func xmlDecoderFromString(s string) *xml.Decoder {
	return xml.NewDecoder(strings.NewReader(s))
}

// makeFixtureGraph builds a small typed graph that exercises every
// projection kind. Layout:
//
//   - 3 owned packages: domain, app, adapter (roled accordingly)
//   - 1 foreign package: third
//   - 1 contract type Greeter (interface) in domain, roled "port"
//   - 1 implementer Server (struct) in adapter (roled adapter_dto)
//   - methods: Server.Greet, Server.Run; functions: app.Main, app.Hello
//   - DependsOn: app -> domain, adapter -> domain, app -> third
//   - Implements: Server implements Greeter
//   - Calls: Main -> Hello, Main -> Server.Run, Server.Run -> Server.Greet
func makeFixtureGraph(t *testing.T) *mgraph.Graph {
	t.Helper()
	g := mgraph.New()

	addPkg := func(id, name, role string, foreign bool) {
		attrs := map[string]any{"foreign": foreign}
		if role != "" {
			attrs[mgraph.AttrRole] = role
			attrs[mgraph.AttrRoleSource] = "package"
		}
		_, _ = g.AddNode(mgraph.Node{
			ID:    id,
			Kind:  mgraph.NodePackage,
			Name:  name,
			QName: id[len("pkg:"):],
			Attrs: attrs,
		})
	}
	addPkg("pkg:example.com/m/domain", "domain", string(mgraph.RolePackageDomain), false)
	addPkg("pkg:example.com/m/app", "app", string(mgraph.RolePackageApplication), false)
	addPkg("pkg:example.com/m/adapter", "adapter", string(mgraph.RolePackageOutboundAdapter), false)
	addPkg("pkg:third.party/lib", "lib", "", true)

	addType := func(id, name, qname, pkgID, role string, contract bool) {
		attrs := map[string]any{"foreign": false}
		if role != "" {
			attrs[mgraph.AttrRole] = role
			attrs[mgraph.AttrRoleSource] = "type"
		}
		if contract {
			attrs[mgraph.AttrIsContract] = true
			attrs[mgraph.AttrContractKind] = "interface"
			attrs[mgraph.AttrContractSource] = "config"
		}
		_, _ = g.AddNode(mgraph.Node{
			ID:    id,
			Kind:  mgraph.NodeType,
			Name:  name,
			QName: qname,
			Attrs: attrs,
		})
		_, _ = g.AddEdge(mgraph.Edge{From: pkgID, To: id, Kind: mgraph.EdgeContains})
	}
	addType("type:greeter", "Greeter", "example.com/m/domain.Greeter",
		"pkg:example.com/m/domain", string(mgraph.RoleTypePort), true)
	addType("type:server", "Server", "example.com/m/adapter.Server",
		"pkg:example.com/m/adapter", string(mgraph.RoleTypeAdapterDTO), false)

	addFunc := func(id, name, qname, pkgID string, kind mgraph.NodeKind) {
		_, _ = g.AddNode(mgraph.Node{
			ID:    id,
			Kind:  kind,
			Name:  name,
			QName: qname,
			Attrs: map[string]any{"foreign": false},
		})
		_, _ = g.AddEdge(mgraph.Edge{From: pkgID, To: id, Kind: mgraph.EdgeContains})
	}
	addFunc("fn:main", "main", "example.com/m/app.main",
		"pkg:example.com/m/app", mgraph.NodeFunction)
	addFunc("fn:hello", "Hello", "example.com/m/app.Hello",
		"pkg:example.com/m/app", mgraph.NodeFunction)
	addFunc("fn:server-run", "Run", "example.com/m/adapter.(*Server).Run",
		"pkg:example.com/m/adapter", mgraph.NodeMethod)
	addFunc("fn:server-greet", "Greet", "example.com/m/adapter.(*Server).Greet",
		"pkg:example.com/m/adapter", mgraph.NodeMethod)

	// Foreign function (third-party) that main calls — used to test
	// foreign drop in call-flow.
	_, _ = g.AddNode(mgraph.Node{
		ID:    "fn:foreign-helper",
		Kind:  mgraph.NodeFunction,
		Name:  "Helper",
		QName: "third.party/lib.Helper",
		Attrs: map[string]any{"foreign": true},
	})
	_, _ = g.AddEdge(mgraph.Edge{
		From: "pkg:third.party/lib", To: "fn:foreign-helper",
		Kind: mgraph.EdgeContains,
	})

	// DependsOn edges between packages.
	for _, e := range []mgraph.Edge{
		{From: "pkg:example.com/m/app", To: "pkg:example.com/m/domain", Kind: mgraph.EdgeDependsOn},
		{From: "pkg:example.com/m/app", To: "pkg:third.party/lib", Kind: mgraph.EdgeDependsOn},
		{From: "pkg:example.com/m/adapter", To: "pkg:example.com/m/domain", Kind: mgraph.EdgeDependsOn},
	} {
		if _, err := g.AddEdge(e); err != nil {
			t.Fatalf("AddEdge: %v", err)
		}
	}

	// Implements: Server -> Greeter
	if _, err := g.AddEdge(mgraph.Edge{
		From: "type:server", To: "type:greeter", Kind: mgraph.EdgeImplements,
	}); err != nil {
		t.Fatalf("AddEdge implements: %v", err)
	}

	// Calls graph.
	for _, e := range []mgraph.Edge{
		{From: "fn:main", To: "fn:hello", Kind: mgraph.EdgeCalls},
		{From: "fn:main", To: "fn:server-run", Kind: mgraph.EdgeCalls},
		{From: "fn:main", To: "fn:foreign-helper", Kind: mgraph.EdgeCalls},
		{From: "fn:server-run", To: "fn:server-greet", Kind: mgraph.EdgeCalls},
	} {
		if _, err := g.AddEdge(e); err != nil {
			t.Fatalf("AddEdge call: %v", err)
		}
	}
	return g
}

func TestParseKind(t *testing.T) {
	for _, k := range AllKinds() {
		got, err := ParseKind(string(k))
		if err != nil {
			t.Fatalf("ParseKind(%q) error: %v", k, err)
		}
		if got != k {
			t.Errorf("ParseKind(%q) = %q, want %q", k, got, k)
		}
	}
	if _, err := ParseKind("nope"); err == nil {
		t.Error("ParseKind: expected error for unknown kind")
	}
}

func TestParseFormat(t *testing.T) {
	for _, f := range AllFormats() {
		got, err := ParseFormat(string(f))
		if err != nil {
			t.Fatalf("ParseFormat(%q) error: %v", f, err)
		}
		if got != f {
			t.Errorf("ParseFormat(%q) = %q, want %q", f, got, f)
		}
	}
	if got, err := ParseFormat(""); err != nil || got != FormatD2 {
		t.Errorf("ParseFormat(\"\") = (%q, %v), want (d2, nil)", got, err)
	}
	if _, err := ParseFormat("svg"); err == nil {
		t.Error("ParseFormat: expected error for unknown format")
	}
}

func TestPackageDeps_DropsForeignByDefault(t *testing.T) {
	g := makeFixtureGraph(t)
	d, err := Build(g, KindPackageDeps, Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got, want := len(d.Nodes), 3; got != want {
		t.Errorf("nodes: got %d, want %d (3 owned packages)", got, want)
	}
	for _, n := range d.Nodes {
		if strings.Contains(n.ID, "third.party") {
			t.Errorf("foreign node leaked: %s", n.ID)
		}
	}
	// 2 DependsOn edges between owned packages.
	if got, want := len(d.Edges), 2; got != want {
		t.Errorf("edges: got %d, want %d", got, want)
	}
	// Evidence IDs preserved.
	for _, n := range d.Nodes {
		if len(n.EvidenceIDs) == 0 || n.EvidenceIDs[0] != n.ID {
			t.Errorf("node %q: missing evidence id", n.ID)
		}
	}
	for _, e := range d.Edges {
		if len(e.EvidenceIDs) != 1 {
			t.Errorf("edge %s->%s: want 1 evidence id, got %d", e.From, e.To, len(e.EvidenceIDs))
		}
	}
}

func TestPackageDeps_IncludeForeign(t *testing.T) {
	g := makeFixtureGraph(t)
	d, err := Build(g, KindPackageDeps, Options{IncludeForeign: true})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got, want := len(d.Nodes), 4; got != want {
		t.Errorf("nodes: got %d, want %d (3 owned + 1 foreign)", got, want)
	}
	if got, want := len(d.Edges), 3; got != want {
		t.Errorf("edges: got %d, want %d", got, want)
	}
}

func TestContractPort_KeepsContractAndImplementer(t *testing.T) {
	g := makeFixtureGraph(t)
	d, err := Build(g, KindContractPort, Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	ids := map[string]bool{}
	for _, n := range d.Nodes {
		ids[n.ID] = true
	}
	for _, want := range []string{"type:greeter", "type:server"} {
		if !ids[want] {
			t.Errorf("missing node %q in contract-port projection", want)
		}
	}
	if got, want := len(d.Edges), 1; got != want {
		t.Fatalf("edges: got %d, want %d (one Implements)", got, want)
	}
	if d.Edges[0].Kind != mgraph.EdgeImplements {
		t.Errorf("edge kind: got %q, want %q", d.Edges[0].Kind, mgraph.EdgeImplements)
	}
	// Cluster should be set to the package label.
	for _, n := range d.Nodes {
		if n.Cluster == "" {
			t.Errorf("node %q: empty cluster (expected package path)", n.ID)
		}
	}
}

func TestCallFlow_AutoSeed_DropsForeign(t *testing.T) {
	g := makeFixtureGraph(t)
	d, err := Build(g, KindCallFlow, Options{Depth: 5})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	ids := map[string]bool{}
	for _, n := range d.Nodes {
		ids[n.ID] = true
	}
	// Auto-seed picks main + Run; transitive callees follow.
	for _, want := range []string{"fn:main", "fn:hello", "fn:server-run", "fn:server-greet"} {
		if !ids[want] {
			t.Errorf("missing node %q in call-flow", want)
		}
	}
	if ids["fn:foreign-helper"] {
		t.Error("foreign callee should be dropped without --include-foreign")
	}
}

func TestCallFlow_ExplicitSeedAndDepth(t *testing.T) {
	g := makeFixtureGraph(t)
	d, err := Build(g, KindCallFlow, Options{
		Seeds: []string{"example.com/m/app.main"},
		Depth: 1,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	ids := map[string]bool{}
	for _, n := range d.Nodes {
		ids[n.ID] = true
	}
	// Depth 1: main + immediate owned callees only.
	for _, want := range []string{"fn:main", "fn:hello", "fn:server-run"} {
		if !ids[want] {
			t.Errorf("missing node %q at depth=1", want)
		}
	}
	if ids["fn:server-greet"] {
		t.Error("transitive callee should be excluded at depth=1")
	}
}

func TestCallFlow_DroppedSeed_ProducesNote(t *testing.T) {
	g := makeFixtureGraph(t)
	d, err := Build(g, KindCallFlow, Options{Seeds: []string{"nope.NoSuch"}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	found := false
	for _, n := range d.Notes {
		if strings.Contains(n, "seed not found") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'seed not found' note, got %v", d.Notes)
	}
}

func TestRenderD2_StableSnapshot(t *testing.T) {
	g := makeFixtureGraph(t)
	d, err := Build(g, KindPackageDeps, Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	var buf bytes.Buffer
	if err := RenderD2(&buf, d); err != nil {
		t.Fatalf("RenderD2: %v", err)
	}
	got := buf.String()
	want := `# Package dependencies
# kind: package-deps
# note: foreign packages dropped (set --include-foreign to keep)

"application": {
  "pkg:example.com/m/app": {label: "example.com/m/app"}
}
"domain": {
  "pkg:example.com/m/domain": {label: "example.com/m/domain"}
}
"outbound_adapter": {
  "pkg:example.com/m/adapter": {label: "example.com/m/adapter"}
}

"pkg:example.com/m/adapter" -> "pkg:example.com/m/domain": "dependsOn"
"pkg:example.com/m/app" -> "pkg:example.com/m/domain": "dependsOn"
`
	if got != want {
		t.Errorf("D2 snapshot mismatch.\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

func TestRenderJSON_RoundTrip(t *testing.T) {
	g := makeFixtureGraph(t)
	d, err := Build(g, KindContractPort, Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	var buf bytes.Buffer
	if err := RenderJSON(&buf, d); err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}
	var env JSONEnvelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("Unmarshal: %v\n%s", err, buf.String())
	}
	if env.Version != CurrentJSONVersion {
		t.Errorf("version: got %d, want %d", env.Version, CurrentJSONVersion)
	}
	if env.Diagram == nil {
		t.Fatal("diagram missing in envelope")
	}
	if env.Diagram.Kind != KindContractPort {
		t.Errorf("kind: got %q, want %q", env.Diagram.Kind, KindContractPort)
	}
	if len(env.Diagram.Nodes) == 0 {
		t.Error("expected nodes")
	}
}

func TestRenderGraphML_ContainsEvidence(t *testing.T) {
	g := makeFixtureGraph(t)
	d, err := Build(g, KindPackageDeps, Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	var buf bytes.Buffer
	if err := RenderGraphML(&buf, d); err != nil {
		t.Fatalf("RenderGraphML: %v", err)
	}
	out := buf.String()
	// Validate XML is well-formed.
	dec := xmlDecoderFromString(out)
	for {
		_, err := dec.Token()
		if err != nil {
			break
		}
	}
	if !strings.Contains(out, `archmotif_id`) {
		t.Error("graphml: missing archmotif_id key")
	}
	if !strings.Contains(out, `evidence_ids`) {
		t.Error("graphml: missing evidence_ids key")
	}
	if !strings.Contains(out, "pkg:example.com/m/domain") {
		t.Error("graphml: missing domain package id in output")
	}
}

func TestSortIsDeterministic(t *testing.T) {
	g := makeFixtureGraph(t)
	first, _ := Build(g, KindPackageDeps, Options{IncludeForeign: true})
	second, _ := Build(g, KindPackageDeps, Options{IncludeForeign: true})
	var b1, b2 bytes.Buffer
	_ = RenderD2(&b1, first)
	_ = RenderD2(&b2, second)
	if b1.String() != b2.String() {
		t.Error("two builds with same options produced different D2 output")
	}
}
