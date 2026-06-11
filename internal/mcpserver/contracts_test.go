package mcpserver

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// contractFixture installs a small graph with one public DTO, one private DTO
// (won't tag), one HTTP handler (incoming route_registers edge), one config
// schema, and a few consumer/producer edges. Returned under the given slug.
const contractFixture = `<?xml version="1.0" encoding="UTF-8"?>
<graphml xmlns="http://graphml.graphdrawing.org/xmlns">
  <key id="n_kind" for="node" attr.name="kind" attr.type="string"/>
  <key id="n_name" for="node" attr.name="name" attr.type="string"/>
  <key id="n_package" for="node" attr.name="package" attr.type="string"/>
  <key id="n_qname" for="node" attr.name="qname" attr.type="string"/>
  <key id="n_http_path" for="node" attr.name="http_path" attr.type="string"/>
  <key id="n_http_method" for="node" attr.name="http_method" attr.type="string"/>
  <key id="n_id" for="node" attr.name="archmotif_id" attr.type="string"/>
  <key id="e_kind" for="edge" attr.name="kind" attr.type="string"/>
  <graph id="G" edgedefault="directed">
    <node id="n0">
      <data key="n_id">type:UserDTO</data>
      <data key="n_kind">type</data>
      <data key="n_name">UserDTO</data>
      <data key="n_package">api</data>
      <data key="n_qname">github.com/example/api.UserDTO</data>
    </node>
    <node id="n1">
      <data key="n_id">type:internalKey</data>
      <data key="n_kind">type</data>
      <data key="n_name">internalKey</data>
      <data key="n_package">api</data>
    </node>
    <node id="n2">
      <data key="n_id">func:GetUser</data>
      <data key="n_kind">function</data>
      <data key="n_name">GetUser</data>
      <data key="n_package">api</data>
      <data key="n_http_path">/users/{id}</data>
      <data key="n_http_method">GET</data>
    </node>
    <node id="n3">
      <data key="n_id">router:main</data>
      <data key="n_kind">function</data>
      <data key="n_name">RegisterRoutes</data>
      <data key="n_package">api</data>
    </node>
    <node id="n4">
      <data key="n_id">cfg:Config</data>
      <data key="n_kind">ConfigSchema</data>
      <data key="n_name">Config</data>
      <data key="n_package">cfg</data>
    </node>
    <node id="n5">
      <data key="n_id">func:Handle</data>
      <data key="n_kind">function</data>
      <data key="n_name">Handle</data>
      <data key="n_package">app</data>
    </node>
    <node id="n6">
      <data key="n_id">func:NewUser</data>
      <data key="n_kind">function</data>
      <data key="n_name">NewUser</data>
      <data key="n_package">api</data>
    </node>
    <edge id="e0" source="n3" target="n2">
      <data key="e_kind">route_registers</data>
    </edge>
    <edge id="e1" source="n5" target="n0">
      <data key="e_kind">usesType</data>
    </edge>
    <edge id="e2" source="n6" target="n0">
      <data key="e_kind">returns</data>
    </edge>
    <edge id="e3" source="n5" target="n4">
      <data key="e_kind">references</data>
    </edge>
  </graph>
</graphml>
`

// installContractFixture writes contractFixture to <tmp>/graphs/<slug>/actual.graphml.
func installContractFixture(t *testing.T, slug string) string {
	t.Helper()
	root := t.TempDir()
	writeFixtureGraphML(t, filepath.Join(root, "graphs", slug), "actual.graphml", contractFixture)
	return root
}

func TestTagContractsIdempotent(t *testing.T) {
	root := installContractFixture(t, "demo")
	g, err := Load(filepath.Join(root, "graphs", "demo", "actual.graphml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	first := TagContracts(g)
	if first[string(ContractKindDTO)] != 1 {
		t.Fatalf("DTO count = %d, want 1 (got hist=%v)", first[string(ContractKindDTO)], first)
	}
	if first[string(ContractKindHTTPHandler)] != 1 {
		t.Fatalf("HTTPHandler count = %d, want 1", first[string(ContractKindHTTPHandler)])
	}
	if first[string(ContractKindConfigSchema)] != 1 {
		t.Fatalf("ConfigSchema count = %d, want 1", first[string(ContractKindConfigSchema)])
	}
	// Re-run: same histogram.
	second := TagContracts(g)
	for k, v := range first {
		if second[k] != v {
			t.Fatalf("hist[%s] flipped: first=%d second=%d", k, v, second[k])
		}
	}
	// The tag must appear exactly once on each tagged node.
	for _, n := range g.Nodes {
		tags := n.Attrs["tags"]
		if hasTag(tags, contractTagName) {
			count := 0
			for _, t := range splitTags(tags) {
				if t == contractTagName {
					count++
				}
			}
			if count != 1 {
				t.Errorf("node %s carries %d `contract` tags (tags=%q)", n.ID, count, tags)
			}
		}
	}
	// Private DTO must NOT be tagged.
	priv, _ := g.Node("type:internalKey")
	if hasTag(priv.Attrs["tags"], contractTagName) {
		t.Errorf("private type internalKey was tagged as contract: %v", priv.Attrs)
	}
}

func TestContractsListFilters(t *testing.T) {
	root := installContractFixture(t, "demo")
	svc := NewService(root)
	all, err := svc.ContractsList("demo", "", "")
	if err != nil {
		t.Fatalf("ContractsList: %v", err)
	}
	// DTO + HTTPHandler + ConfigSchema = 3.
	if len(all) != 3 {
		t.Fatalf("len(all) = %d, want 3 (got %+v)", len(all), all)
	}
	dtos, _ := svc.ContractsList("demo", "dto", "")
	if len(dtos) != 1 || dtos[0].Name != "UserDTO" {
		t.Fatalf("dto filter = %+v", dtos)
	}
	pub, _ := svc.ContractsList("demo", "", "public")
	if len(pub) == 0 {
		t.Fatalf("public filter returned nothing")
	}
}

func TestContractsDiffAddedRemovedChanged(t *testing.T) {
	root := installContractFixture(t, "demo")
	svc := NewService(root)

	// Branch B: add a new contract and tweak UserDTO's package attr.
	if _, err := svc.ForkGraph("demo", "demo:branch", false); err != nil {
		t.Fatalf("fork: %v", err)
	}
	if _, err := svc.AddNode("demo:branch", "type", map[string]string{
		"id":      "type:Order",
		"name":    "Order",
		"package": "api",
	}); err != nil {
		t.Fatalf("add node: %v", err)
	}
	// Tweak UserDTO so the diff registers a field change.
	{
		g, err := svc.LoadGraph("demo:branch")
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		_ = g.UpdateNodeAttr("type:UserDTO", "package", "api/v2")
		if err := svc.SaveGraph("demo:branch", g); err != nil {
			t.Fatalf("save: %v", err)
		}
	}
	diff, err := svc.ContractsDiff("demo", "demo:branch", "")
	if err != nil {
		t.Fatalf("ContractsDiff: %v", err)
	}
	if diff.Summary.Added != 1 || diff.Added[0].ID != "type:Order" {
		t.Fatalf("added = %+v", diff.Added)
	}
	if diff.Summary.Removed != 0 {
		t.Fatalf("removed = %+v", diff.Removed)
	}
	if diff.Summary.Changed == 0 {
		t.Fatalf("expected at least one changed contract, got 0 (diff=%+v)", diff)
	}
	foundUser := false
	for _, c := range diff.Changed {
		if c.ID == "type:UserDTO" {
			foundUser = true
			pkgChanged := false
			for _, f := range c.FieldDiff {
				if f.Field == "package" && f.New == "api/v2" {
					pkgChanged = true
				}
			}
			if !pkgChanged {
				t.Errorf("UserDTO package change not surfaced: %+v", c.FieldDiff)
			}
		}
	}
	if !foundUser {
		t.Fatalf("UserDTO not in Changed list: %+v", diff.Changed)
	}
	// Scope filter: http_handler only — none changed/added.
	httpDiff, err := svc.ContractsDiff("demo", "demo:branch", "http_handler")
	if err != nil {
		t.Fatalf("ContractsDiff scope: %v", err)
	}
	if httpDiff.Summary.Added != 0 || httpDiff.Summary.Removed != 0 || httpDiff.Summary.Changed != 0 {
		t.Fatalf("scoped diff should be empty: %+v", httpDiff)
	}
}

func TestContractsConsumersProducers(t *testing.T) {
	root := installContractFixture(t, "demo")
	svc := NewService(root)
	cons, err := svc.ContractsConsumers("demo", "type:UserDTO")
	if err != nil {
		t.Fatalf("Consumers: %v", err)
	}
	// func:Handle uses UserDTO via usesType.
	foundHandle := false
	for _, c := range cons {
		if c.ID == "func:Handle" && c.Role == "uses" {
			foundHandle = true
		}
	}
	if !foundHandle {
		t.Fatalf("expected func:Handle in consumers, got %+v", cons)
	}

	prods, err := svc.ContractsProducers("demo", "type:UserDTO")
	if err != nil {
		t.Fatalf("Producers: %v", err)
	}
	foundNewUser := false
	for _, p := range prods {
		if p.ID == "func:NewUser" && p.Role == "returns" {
			foundNewUser = true
		}
	}
	if !foundNewUser {
		t.Fatalf("expected func:NewUser in producers, got %+v", prods)
	}

	// HTTP handler producer: router:main produces func:GetUser via route_registers.
	httpProds, err := svc.ContractsProducers("demo", "func:GetUser")
	if err != nil {
		t.Fatalf("Producers handler: %v", err)
	}
	foundRouter := false
	for _, p := range httpProds {
		if p.ID == "router:main" && p.Role == "route_registers" {
			foundRouter = true
		}
	}
	if !foundRouter {
		t.Fatalf("expected router:main producer for handler, got %+v", httpProds)
	}
}

func TestContractsFieldHistory(t *testing.T) {
	root := installContractFixture(t, "demo")
	svc := NewService(root)
	if _, err := svc.ForkGraph("demo", "demo:branch", false); err != nil {
		t.Fatalf("fork: %v", err)
	}
	g, err := svc.LoadGraph("demo:branch")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	_ = g.UpdateNodeAttr("type:UserDTO", "package", "api/v2")
	if err := svc.SaveGraph("demo:branch", g); err != nil {
		t.Fatalf("save: %v", err)
	}
	hist, err := svc.ContractsFieldHistory("demo", "type:UserDTO", "package")
	if err != nil {
		t.Fatalf("FieldHistory: %v", err)
	}
	if len(hist) != 2 {
		t.Fatalf("hist len = %d, want 2 (got %+v)", len(hist), hist)
	}
	values := map[string]string{}
	for _, h := range hist {
		values[h.GraphID] = h.Value
	}
	if values["demo:actual"] != "api" {
		t.Errorf("demo:actual value = %q", values["demo:actual"])
	}
	if values["demo:branch"] != "api/v2" {
		t.Errorf("demo:branch value = %q", values["demo:branch"])
	}
}

func TestContractsExportOpenAPI(t *testing.T) {
	root := installContractFixture(t, "demo")
	svc := NewService(root)
	doc, err := svc.ContractsExport("demo", "openapi")
	if err != nil {
		t.Fatalf("Export openapi: %v", err)
	}
	if doc["openapi"] != "3.0.0" {
		t.Fatalf("openapi version = %v", doc["openapi"])
	}
	paths, ok := doc["paths"].(map[string]map[string]any)
	if !ok {
		t.Fatalf("paths not a map: %T", doc["paths"])
	}
	op, ok := paths["/users/{id}"]
	if !ok {
		t.Fatalf("expected /users/{id} in paths, got %v", paths)
	}
	if _, ok := op["get"]; !ok {
		t.Fatalf("expected GET op, got %v", op)
	}
	// Component schemas for UserDTO must be present.
	comp, ok := doc["components"].(map[string]any)
	if !ok {
		t.Fatalf("components missing: %v", doc)
	}
	schemas, ok := comp["schemas"].(map[string]any)
	if !ok {
		t.Fatalf("schemas missing: %v", comp)
	}
	if _, ok := schemas["UserDTO"]; !ok {
		t.Fatalf("UserDTO schema missing: %v", schemas)
	}
}

func TestContractsTagPersists(t *testing.T) {
	root := installContractFixture(t, "demo")
	svc := NewService(root)
	hist, err := svc.TagAndPersist("demo")
	if err != nil {
		t.Fatalf("TagAndPersist: %v", err)
	}
	if hist["dto"] != 1 || hist["http_handler"] != 1 {
		t.Fatalf("hist = %+v", hist)
	}
	// Reload and confirm the on-disk tags survived.
	g, err := svc.LoadGraph("demo")
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	user, _ := g.Node("type:UserDTO")
	if !hasTag(user.Attrs["tags"], contractTagName) {
		t.Fatalf("UserDTO not tagged on disk: %v", user.Attrs)
	}
	if user.Attrs[attrContractKind] != "dto" {
		t.Fatalf("UserDTO contract_kind = %q", user.Attrs[attrContractKind])
	}
}

func TestContractsExportUnsupportedFormat(t *testing.T) {
	root := installContractFixture(t, "demo")
	svc := NewService(root)
	_, err := svc.ContractsExport("demo", "graphql")
	if err == nil {
		t.Fatalf("expected error for unsupported format")
	}
	if !strings.Contains(err.Error(), "unsupported format") {
		t.Fatalf("error = %v", err)
	}
}

// collisionFixture has two HTTP handlers resolving to the same (path, method)
// tuple AND two public DTOs resolving to the same schema name. Both kinds of
// collision must be surfaced by exportOpenAPI.
const collisionFixture = `<?xml version="1.0" encoding="UTF-8"?>
<graphml xmlns="http://graphml.graphdrawing.org/xmlns">
  <key id="n_kind" for="node" attr.name="kind" attr.type="string"/>
  <key id="n_name" for="node" attr.name="name" attr.type="string"/>
  <key id="n_package" for="node" attr.name="package" attr.type="string"/>
  <key id="n_http_path" for="node" attr.name="http_path" attr.type="string"/>
  <key id="n_http_method" for="node" attr.name="http_method" attr.type="string"/>
  <key id="n_id" for="node" attr.name="archmotif_id" attr.type="string"/>
  <key id="e_kind" for="edge" attr.name="kind" attr.type="string"/>
  <graph id="G" edgedefault="directed">
    <node id="n0">
      <data key="n_id">type:UserDTO_v1</data>
      <data key="n_kind">type</data>
      <data key="n_name">UserDTO</data>
      <data key="n_package">api/v1</data>
    </node>
    <node id="n1">
      <data key="n_id">type:UserDTO_v2</data>
      <data key="n_kind">type</data>
      <data key="n_name">UserDTO</data>
      <data key="n_package">api/v2</data>
    </node>
    <node id="n2">
      <data key="n_id">func:GetUserA</data>
      <data key="n_kind">function</data>
      <data key="n_name">GetUserA</data>
      <data key="n_package">api</data>
      <data key="n_http_path">/users/{id}</data>
      <data key="n_http_method">GET</data>
    </node>
    <node id="n3">
      <data key="n_id">func:GetUserB</data>
      <data key="n_kind">function</data>
      <data key="n_name">GetUserB</data>
      <data key="n_package">api</data>
      <data key="n_http_path">/users/{id}</data>
      <data key="n_http_method">GET</data>
    </node>
    <node id="n4">
      <data key="n_id">router:main</data>
      <data key="n_kind">function</data>
      <data key="n_name">RegisterRoutes</data>
      <data key="n_package">api</data>
    </node>
    <edge id="e0" source="n4" target="n2">
      <data key="e_kind">route_registers</data>
    </edge>
    <edge id="e1" source="n4" target="n3">
      <data key="e_kind">route_registers</data>
    </edge>
  </graph>
</graphml>
`

// TestContractsExportOpenAPIRejectsCollisions confirms exportOpenAPI returns a
// typed ErrOpenAPICollision when two contracts share a (path, method) tuple or
// a schema name, listing every offending node id in the error message.
func TestContractsExportOpenAPIRejectsCollisions(t *testing.T) {
	root := t.TempDir()
	writeFixtureGraphML(t, filepath.Join(root, "graphs", "demo"), "actual.graphml", collisionFixture)
	svc := NewService(root)
	_, err := svc.ContractsExport("demo", "openapi")
	if err == nil {
		t.Fatalf("expected ErrOpenAPICollision, got nil")
	}
	if !errors.Is(err, ErrOpenAPICollision) {
		t.Fatalf("error %v is not ErrOpenAPICollision", err)
	}
	msg := err.Error()
	// Path collision side: both handler ids must appear in the error.
	if !strings.Contains(msg, "func:GetUserA") || !strings.Contains(msg, "func:GetUserB") {
		t.Errorf("path collision message missing handler ids: %s", msg)
	}
	// Schema collision side: both DTO ids must appear.
	if !strings.Contains(msg, "type:UserDTO_v1") || !strings.Contains(msg, "type:UserDTO_v2") {
		t.Errorf("schema collision message missing DTO ids: %s", msg)
	}
}
