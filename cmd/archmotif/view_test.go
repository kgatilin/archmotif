package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
	"github.com/kgatilin/archmotif/internal/mcpserver"
	"github.com/kgatilin/archmotif/internal/targetcontract"
)

func TestBrowserServerServesWorkspaceGraphAndMCPRoute(t *testing.T) {
	root := t.TempDir()
	g := mgraph.New()
	g.AddNode(mgraph.Node{ID: "pkg:a", Kind: mgraph.NodePackage, Name: "a", QName: "example/a", Attrs: map[string]any{"foreign": false}})
	g.AddNode(mgraph.Node{ID: "type:store", Kind: mgraph.NodeType, Name: "Store", QName: "example/a.Store", Attrs: map[string]any{
		"foreign":                 false,
		"typeKind":                "struct",
		mgraph.AttrIsContract:     true,
		mgraph.AttrContractKind:   "interface",
		mgraph.AttrContractSource: "test",
	}})
	_, _ = g.AddEdge(mgraph.Edge{From: "pkg:a", To: "type:store", Kind: mgraph.EdgeContains})

	graphPath, err := writeGraphToWorkspace(root, "demo", g)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "graphs", "demo", "actual.graphml"); graphPath != want {
		t.Fatalf("graph path = %q, want %q", graphPath, want)
	}

	svc := mcpserver.NewService(root)
	browser := newBrowserServer(svc, "demo", "/repo", ".")
	mux := http.NewServeMux()
	registerGraphServer(mux, svc, browser)

	index := httptest.NewRecorder()
	mux.ServeHTTP(index, httptest.NewRequest(http.MethodGet, "/", nil))
	if index.Code != http.StatusOK {
		t.Fatalf("index status = %d, want 200", index.Code)
	}
	if body := index.Body.String(); !strings.Contains(body, "/static/style.css") || !strings.Contains(body, "/static/app.js") {
		t.Fatalf("index did not reference embedded static assets:\n%s", body)
	}
	if body := index.Body.String(); !strings.Contains(body, `data-graph-id="demo"`) || !strings.Contains(body, `id="targetList"`) || !strings.Contains(body, `id="diffFromInput"`) {
		t.Fatalf("index did not expose target graph browser state:\n%s", body)
	}

	app := httptest.NewRecorder()
	mux.ServeHTTP(app, httptest.NewRequest(http.MethodGet, "/static/app.js", nil))
	if app.Code != http.StatusOK {
		t.Fatalf("app.js status = %d, want 200", app.Code)
	}
	if !strings.Contains(app.Body.String(), "layoutFlow") {
		t.Fatalf("app.js does not look like the graph viewer script")
	}

	layouts := httptest.NewRecorder()
	mux.ServeHTTP(layouts, httptest.NewRequest(http.MethodGet, "/api/layouts", nil))
	if layouts.Code != http.StatusOK {
		t.Fatalf("layouts status = %d, want 200", layouts.Code)
	}
	if body := layouts.Body.String(); !strings.Contains(body, `"default": "dot"`) || !strings.Contains(body, `"package": "structure"`) || !strings.Contains(body, `"id": "force"`) {
		t.Fatalf("layouts response does not expose the layout registry:\n%s", body)
	}

	graph := httptest.NewRecorder()
	mux.ServeHTTP(graph, httptest.NewRequest(http.MethodGet, "/api/graph?view=package&id=pkg:a&detail=all&layout=structure", nil))
	if graph.Code != http.StatusOK {
		t.Fatalf("graph status = %d, want 200:\n%s", graph.Code, graph.Body.String())
	}
	if body := graph.Body.String(); !strings.Contains(body, `"contract": true`) || !strings.Contains(body, `"contractKind": "interface"`) || !strings.Contains(body, `"typeKind": "struct"`) {
		t.Fatalf("graph response did not preserve typed attrs from the MCP workspace:\n%s", body)
	}

	_, pattern := mux.Handler(httptest.NewRequest(http.MethodGet, "/mcp", nil))
	if pattern != "/mcp" {
		t.Fatalf("mcp route was not mounted")
	}
}

func TestBrowserServerTargetAPIs(t *testing.T) {
	root := t.TempDir()
	g := mgraph.New()
	g.AddNode(mgraph.Node{ID: "pkg:a", Kind: mgraph.NodePackage, Name: "a", QName: "example.com/app/cmd/app"})
	if _, err := writeGraphToWorkspace(root, "demo", g); err != nil {
		t.Fatal(err)
	}
	svc := mcpserver.NewService(root)
	browser := newBrowserServer(svc, "demo", "/repo", ".")
	mux := http.NewServeMux()
	registerGraphServer(mux, svc, browser)

	body, _ := json.Marshal(map[string]any{
		"target_id": "split-optimize",
		"contract": targetcontract.Contract{
			Version:     1,
			ID:          "target-test",
			Description: "split optimize",
			Packages: []targetcontract.PackageSpec{
				{Role: "OptimizeOrchestration", ImportPath: "example.com/app/internal/optimize", Dir: "internal/optimize", Name: "optimize", Action: "create"},
			},
			Files: []targetcontract.FileSpec{
				{Path: "internal/optimize/run.go", PackageRole: "OptimizeOrchestration", PackageName: "optimize", Action: "create"},
			},
			PublicTypes: []targetcontract.TypeSpec{
				{Name: "Options", Kind: "struct", PackageRole: "OptimizeOrchestration", PackagePath: "example.com/app/internal/optimize", File: "internal/optimize/run.go"},
			},
		},
	})
	put := httptest.NewRecorder()
	mux.ServeHTTP(put, httptest.NewRequest(http.MethodPost, "/api/targets", bytes.NewReader(body)))
	if put.Code != http.StatusOK {
		t.Fatalf("put target status = %d:\n%s", put.Code, put.Body.String())
	}
	if !strings.Contains(put.Body.String(), `"graph_id": "demo:target-split-optimize"`) {
		t.Fatalf("put response missing target graph id:\n%s", put.Body.String())
	}

	list := httptest.NewRecorder()
	mux.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/targets", nil))
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"target_id": "split-optimize"`) {
		t.Fatalf("list target response = %d:\n%s", list.Code, list.Body.String())
	}

	graph := httptest.NewRecorder()
	mux.ServeHTTP(graph, httptest.NewRequest(http.MethodGet, "/api/graph?graph_id=demo:target-split-optimize&view=packages", nil))
	if graph.Code != http.StatusOK || !strings.Contains(graph.Body.String(), "internal/optimize") {
		t.Fatalf("target graph response = %d:\n%s", graph.Code, graph.Body.String())
	}
}

func TestBrowserServerGraphDiffOverlay(t *testing.T) {
	root := t.TempDir()
	base := mgraph.New()
	base.AddNode(mgraph.Node{ID: "pkg:a", Kind: mgraph.NodePackage, Name: "a-old", QName: "example.com/app/a"})
	base.AddNode(mgraph.Node{ID: "pkg:old", Kind: mgraph.NodePackage, Name: "old", QName: "example.com/app/old"})
	_, _ = base.AddEdge(mgraph.Edge{From: "pkg:a", To: "pkg:old", Kind: mgraph.EdgeDependsOn})

	branch := mgraph.New()
	branch.AddNode(mgraph.Node{ID: "pkg:a", Kind: mgraph.NodePackage, Name: "a-new", QName: "example.com/app/a"})
	branch.AddNode(mgraph.Node{ID: "pkg:new", Kind: mgraph.NodePackage, Name: "new", QName: "example.com/app/new"})
	_, _ = branch.AddEdge(mgraph.Edge{From: "pkg:a", To: "pkg:new", Kind: mgraph.EdgeDependsOn})

	if _, err := writeGraphToWorkspace(root, "demo:main", base); err != nil {
		t.Fatal(err)
	}
	if _, err := writeGraphToWorkspace(root, "demo:branch", branch); err != nil {
		t.Fatal(err)
	}
	svc := mcpserver.NewService(root)
	browser := newBrowserServer(svc, "demo:branch", "/repo", ".")
	mux := http.NewServeMux()
	registerGraphServer(mux, svc, browser)

	res := httptest.NewRecorder()
	mux.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/graph?graph_id=demo:branch&diff_from=demo:main&view=packages&layout=structure", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("graph status = %d:\n%s", res.Code, res.Body.String())
	}
	var got graphView
	if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Diff == nil {
		t.Fatalf("diff overlay missing:\n%s", res.Body.String())
	}
	if got.Diff.From != "demo:main" || got.Diff.To != "demo:branch" {
		t.Fatalf("diff route = %s -> %s", got.Diff.From, got.Diff.To)
	}
	if got.Diff.Summary.NodesAdded != 1 || got.Diff.Summary.NodesRemoved != 1 || got.Diff.Summary.NodesChanged != 1 {
		t.Fatalf("node summary = %#v", got.Diff.Summary)
	}
	if got.Diff.Summary.EdgesAdded != 1 || got.Diff.Summary.EdgesRemoved != 1 {
		t.Fatalf("edge summary = %#v", got.Diff.Summary)
	}
	nodes := make(map[string]viewNode, len(got.Nodes))
	for _, n := range got.Nodes {
		nodes[n.ID] = n
	}
	if nodes["pkg:new"].Diff != "added" {
		t.Fatalf("pkg:new diff = %q, want added", nodes["pkg:new"].Diff)
	}
	if nodes["pkg:old"].Diff != "removed" {
		t.Fatalf("pkg:old diff = %q, want removed", nodes["pkg:old"].Diff)
	}
	changed := nodes["pkg:a"]
	if changed.Diff != "changed" {
		t.Fatalf("pkg:a diff = %q, want changed", changed.Diff)
	}
	if diff := changed.AttrsDiff["__name"]; diff != [2]any{"a-old", "a-new"} {
		t.Fatalf("pkg:a name diff = %#v", diff)
	}
	edgeDiffs := make(map[string]string, len(got.Edges))
	for _, e := range got.Edges {
		edgeDiffs[e.From+"->"+e.To] = e.Diff
	}
	if edgeDiffs["pkg:a->pkg:new"] != "added" {
		t.Fatalf("new edge diff = %q, want added", edgeDiffs["pkg:a->pkg:new"])
	}
	if edgeDiffs["pkg:a->pkg:old"] != "removed" {
		t.Fatalf("old edge diff = %q, want removed", edgeDiffs["pkg:a->pkg:old"])
	}
}

func TestBrowserServerStructureDiffOverlayForSymbolGraphs(t *testing.T) {
	root := t.TempDir()
	base := mgraph.New()
	base.AddNode(mgraph.Node{ID: "type:config", Kind: mgraph.NodeType, Name: "Config", QName: "example.com/app.Config"})
	base.AddNode(mgraph.Node{ID: "method:applyDefaults", Kind: mgraph.NodeMethod, Name: "applyDefaults", QName: "example.com/app.Config.applyDefaults"})
	_, _ = base.AddEdge(mgraph.Edge{From: "type:config", To: "method:applyDefaults", Kind: mgraph.EdgeContains})

	target := mgraph.New()
	target.AddNode(mgraph.Node{ID: "target:Iface", Kind: mgraph.NodeType, Name: "Iface", QName: "target.Iface", Attrs: map[string]any{"typeKind": "interface"}})
	target.AddNode(mgraph.Node{ID: "target:Impl", Kind: mgraph.NodeType, Name: "Impl", QName: "target.Impl"})
	target.AddNode(mgraph.Node{ID: "target:Method", Kind: mgraph.NodeMethod, Name: "Method", QName: "target.Method"})
	_, _ = target.AddEdge(mgraph.Edge{From: "target:Impl", To: "target:Iface", Kind: mgraph.EdgeImplements})
	_, _ = target.AddEdge(mgraph.Edge{From: "target:Impl", To: "target:Method", Kind: mgraph.EdgeContains})

	if _, err := writeGraphToWorkspace(root, "demo:current", base); err != nil {
		t.Fatal(err)
	}
	if _, err := writeGraphToWorkspace(root, "demo:target", target); err != nil {
		t.Fatal(err)
	}
	svc := mcpserver.NewService(root)
	browser := newBrowserServer(svc, "demo:target", "/repo", ".")
	mux := http.NewServeMux()
	registerGraphServer(mux, svc, browser)

	res := httptest.NewRecorder()
	mux.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/graph?graph_id=demo:target&diff_from=demo:current&view=structure&layout=structure", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("graph status = %d:\n%s", res.Code, res.Body.String())
	}
	var got graphView
	if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.View != "structure" {
		t.Fatalf("view = %q, want structure", got.View)
	}
	if got.Diff == nil {
		t.Fatalf("diff overlay missing:\n%s", res.Body.String())
	}
	nodes := make(map[string]viewNode, len(got.Nodes))
	for _, n := range got.Nodes {
		nodes[n.ID] = n
	}
	if nodes["target:Iface"].Diff != "added" {
		t.Fatalf("target:Iface diff = %q, want added", nodes["target:Iface"].Diff)
	}
	if nodes["type:config"].Diff != "removed" {
		t.Fatalf("type:config diff = %q, want removed", nodes["type:config"].Diff)
	}
	if got.Diff.Visible.NodesAdded != 3 || got.Diff.Visible.NodesRemoved != 2 {
		t.Fatalf("visible diff = %#v", got.Diff.Visible)
	}
}

func TestBrowserServerDiffPackagesProjectsTouchedCodeToPackages(t *testing.T) {
	root := t.TempDir()
	base := mgraph.New()
	base.AddNode(mgraph.Node{ID: "pkg:a", Kind: mgraph.NodePackage, Name: "a", QName: "example.com/app/a"})
	base.AddNode(mgraph.Node{ID: "pkg:old", Kind: mgraph.NodePackage, Name: "old", QName: "example.com/app/old"})
	base.AddNode(mgraph.Node{ID: "pkg:stable", Kind: mgraph.NodePackage, Name: "stable", QName: "example.com/app/stable"})
	base.AddNode(mgraph.Node{ID: "file:a", Kind: mgraph.NodeFile, Name: "a.go", QName: "example.com/app/a/a.go"})
	base.AddNode(mgraph.Node{ID: "file:old", Kind: mgraph.NodeFile, Name: "old.go", QName: "example.com/app/old/old.go"})
	base.AddNode(mgraph.Node{ID: "type:a", Kind: mgraph.NodeType, Name: "OldType", QName: "example.com/app/a.OldType"})
	base.AddNode(mgraph.Node{ID: "type:old", Kind: mgraph.NodeType, Name: "OldAPI", QName: "example.com/app/old.OldAPI"})
	_, _ = base.AddEdge(mgraph.Edge{From: "pkg:a", To: "file:a", Kind: mgraph.EdgeContains})
	_, _ = base.AddEdge(mgraph.Edge{From: "file:a", To: "type:a", Kind: mgraph.EdgeContains})
	_, _ = base.AddEdge(mgraph.Edge{From: "pkg:old", To: "file:old", Kind: mgraph.EdgeContains})
	_, _ = base.AddEdge(mgraph.Edge{From: "file:old", To: "type:old", Kind: mgraph.EdgeContains})
	_, _ = base.AddEdge(mgraph.Edge{From: "pkg:a", To: "pkg:old", Kind: mgraph.EdgeDependsOn})
	_, _ = base.AddEdge(mgraph.Edge{From: "pkg:a", To: "pkg:stable", Kind: mgraph.EdgeDependsOn})

	branch := mgraph.New()
	branch.AddNode(mgraph.Node{ID: "pkg:a", Kind: mgraph.NodePackage, Name: "a", QName: "example.com/app/a"})
	branch.AddNode(mgraph.Node{ID: "pkg:new", Kind: mgraph.NodePackage, Name: "new", QName: "example.com/app/new"})
	branch.AddNode(mgraph.Node{ID: "pkg:stable", Kind: mgraph.NodePackage, Name: "stable", QName: "example.com/app/stable"})
	branch.AddNode(mgraph.Node{ID: "file:a", Kind: mgraph.NodeFile, Name: "a.go", QName: "example.com/app/a/a.go"})
	branch.AddNode(mgraph.Node{ID: "file:new", Kind: mgraph.NodeFile, Name: "new.go", QName: "example.com/app/new/new.go"})
	branch.AddNode(mgraph.Node{ID: "type:a", Kind: mgraph.NodeType, Name: "NewType", QName: "example.com/app/a.NewType"})
	branch.AddNode(mgraph.Node{ID: "type:new", Kind: mgraph.NodeType, Name: "NewAPI", QName: "example.com/app/new.NewAPI"})
	_, _ = branch.AddEdge(mgraph.Edge{From: "pkg:a", To: "file:a", Kind: mgraph.EdgeContains})
	_, _ = branch.AddEdge(mgraph.Edge{From: "file:a", To: "type:a", Kind: mgraph.EdgeContains})
	_, _ = branch.AddEdge(mgraph.Edge{From: "pkg:new", To: "file:new", Kind: mgraph.EdgeContains})
	_, _ = branch.AddEdge(mgraph.Edge{From: "file:new", To: "type:new", Kind: mgraph.EdgeContains})
	_, _ = branch.AddEdge(mgraph.Edge{From: "pkg:a", To: "pkg:new", Kind: mgraph.EdgeDependsOn})
	_, _ = branch.AddEdge(mgraph.Edge{From: "pkg:a", To: "pkg:stable", Kind: mgraph.EdgeDependsOn})

	if _, err := writeGraphToWorkspace(root, "demo:main", base); err != nil {
		t.Fatal(err)
	}
	if _, err := writeGraphToWorkspace(root, "demo:branch", branch); err != nil {
		t.Fatal(err)
	}
	svc := mcpserver.NewService(root)
	browser := newBrowserServer(svc, "demo:branch", "/repo", ".")
	mux := http.NewServeMux()
	registerGraphServer(mux, svc, browser)

	res := httptest.NewRecorder()
	mux.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/graph?graph_id=demo:branch&diff_from=demo:main&view=diff-packages&id=pkg:a&layout=structure", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("graph status = %d:\n%s", res.Code, res.Body.String())
	}
	var got graphView
	if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.View != "diff-packages" {
		t.Fatalf("view = %q, want diff-packages", got.View)
	}
	nodes := make(map[string]viewNode, len(got.Nodes))
	for _, n := range got.Nodes {
		nodes[n.ID] = n
	}
	if nodes["pkg:a"].Diff != "changed" {
		t.Fatalf("pkg:a diff = %q, want changed", nodes["pkg:a"].Diff)
	}
	if nodes["pkg:new"].Diff != "added" {
		t.Fatalf("pkg:new diff = %q, want added", nodes["pkg:new"].Diff)
	}
	if nodes["pkg:old"].Diff != "removed" {
		t.Fatalf("pkg:old diff = %q, want removed", nodes["pkg:old"].Diff)
	}
	if _, ok := nodes["pkg:stable"]; ok {
		t.Fatalf("stable package should not be in touched package view: %#v", nodes["pkg:stable"])
	}
	if nodes["type:a"].Diff != "changed" {
		t.Fatalf("type:a diff = %q, want changed", nodes["type:a"].Diff)
	}
	if _, ok := nodes["type:new"]; ok {
		t.Fatalf("type:new should not be shown while pkg:a is selected: %#v", nodes["type:new"])
	}
	if _, ok := nodes["type:old"]; ok {
		t.Fatalf("type:old should not be shown while pkg:a is selected: %#v", nodes["type:old"])
	}
	edgeDiffs := make(map[string]string, len(got.Edges))
	for _, e := range got.Edges {
		edgeDiffs[e.From+"->"+e.To] = e.Diff
	}
	if edgeDiffs["pkg:a->pkg:new"] != "added" {
		t.Fatalf("new package edge diff = %q, want added", edgeDiffs["pkg:a->pkg:new"])
	}
	if edgeDiffs["pkg:a->pkg:old"] != "removed" {
		t.Fatalf("old package edge diff = %q, want removed", edgeDiffs["pkg:a->pkg:old"])
	}
	if edgeDiffs["pkg:a->type:a"] != "changed" {
		t.Fatalf("changed surface edge diff = %q, want changed", edgeDiffs["pkg:a->type:a"])
	}
	if got.Diff.Visible.NodesAdded != 1 || got.Diff.Visible.NodesRemoved != 1 || got.Diff.Visible.NodesChanged != 2 {
		t.Fatalf("visible node diff = %#v", got.Diff.Visible)
	}
}

func TestParseDotPlainQuotedIDsAndEdgePoints(t *testing.T) {
	raw := `graph 1 0.88619 1.5
node "pkg:a" 0.44309 1.25 0.87197 0.5 "pkg:a" solid ellipse black lightgrey
node "pkg:b" 0.44309 0.25 0.88619 0.5 "pkg:b" solid ellipse black lightgrey
edge "pkg:a" "pkg:b" 4 0.44309 0.99579 0.44309 0.89454 0.44309 0.77398 0.44309 0.66022 solid black
stop
`

	got, err := parseDotPlain(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Width != 0.88619 || got.Height != 1.5 {
		t.Fatalf("graph size = %v x %v", got.Width, got.Height)
	}
	if got.Nodes["pkg:a"].Y != 1.25 {
		t.Fatalf("pkg:a y = %v, want 1.25", got.Nodes["pkg:a"].Y)
	}
	if len(got.Edges) != 1 || got.Edges[0].From != "pkg:a" || got.Edges[0].To != "pkg:b" {
		t.Fatalf("edge = %#v", got.Edges)
	}
	if len(got.Edges[0].Points) != 4 {
		t.Fatalf("edge point count = %d, want 4", len(got.Edges[0].Points))
	}
}

func TestGraphViewerPackageOverviewHidesExternalByDefault(t *testing.T) {
	g := mgraph.New()
	g.AddNode(mgraph.Node{ID: "pkg:a", Kind: mgraph.NodePackage, Name: "a", QName: "example/a", Attrs: map[string]any{"foreign": false}})
	g.AddNode(mgraph.Node{ID: "pkg:b", Kind: mgraph.NodePackage, Name: "b", QName: "example/b", Attrs: map[string]any{"foreign": false}})
	g.AddNode(mgraph.Node{ID: "pkg:fmt", Kind: mgraph.NodePackage, Name: "fmt", QName: "fmt", Attrs: map[string]any{"foreign": true}})
	_, _ = g.AddEdge(mgraph.Edge{From: "pkg:a", To: "pkg:b", Kind: mgraph.EdgeDependsOn})
	_, _ = g.AddEdge(mgraph.Edge{From: "pkg:a", To: "pkg:fmt", Kind: mgraph.EdgeDependsOn})

	viewer := newGraphViewer(g, ".", ".")
	view := viewer.packageOverview(viewRequest{})

	if len(view.Nodes) != 2 {
		t.Fatalf("nodes = %d, want 2", len(view.Nodes))
	}
	if len(view.Edges) != 1 {
		t.Fatalf("edges = %d, want 1", len(view.Edges))
	}

	view = viewer.packageOverview(viewRequest{ShowExternal: true})
	if len(view.Nodes) != 3 {
		t.Fatalf("nodes with external = %d, want 3", len(view.Nodes))
	}
}

func TestGraphViewerPackageViewPublicSurface(t *testing.T) {
	g := mgraph.New()
	g.AddNode(mgraph.Node{ID: "pkg:a", Kind: mgraph.NodePackage, Name: "a", QName: "example/a", Attrs: map[string]any{"foreign": false}})
	g.AddNode(mgraph.Node{ID: "pkg:fmt", Kind: mgraph.NodePackage, Name: "fmt", QName: "fmt", Attrs: map[string]any{"foreign": true}})
	g.AddNode(mgraph.Node{ID: "type:exported", Kind: mgraph.NodeType, Name: "Store", QName: "example/a.Store"})
	g.AddNode(mgraph.Node{ID: "type:private", Kind: mgraph.NodeType, Name: "helper", QName: "example/a.helper"})
	g.AddNode(mgraph.Node{ID: "method:public", Kind: mgraph.NodeMethod, Name: "Lookup", QName: "example/a.Store.Lookup", Attrs: map[string]any{"receiver": "Store"}})
	g.AddNode(mgraph.Node{ID: "func:private", Kind: mgraph.NodeFunction, Name: "makeStore", QName: "example/a.makeStore"})
	g.AddNode(mgraph.Node{ID: "method:foreign", Kind: mgraph.NodeMethod, Name: "Lookup", QName: "example/a.Store.Lookup", Attrs: map[string]any{"foreign": true, "receiver": "Store"}})
	_, _ = g.AddEdge(mgraph.Edge{From: "pkg:a", To: "type:exported", Kind: mgraph.EdgeContains})
	_, _ = g.AddEdge(mgraph.Edge{From: "pkg:a", To: "type:private", Kind: mgraph.EdgeContains})
	_, _ = g.AddEdge(mgraph.Edge{From: "type:exported", To: "method:public", Kind: mgraph.EdgeContains})
	_, _ = g.AddEdge(mgraph.Edge{From: "pkg:a", To: "func:private", Kind: mgraph.EdgeContains})
	_, _ = g.AddEdge(mgraph.Edge{From: "pkg:a", To: "method:foreign", Kind: mgraph.EdgeContains})
	_, _ = g.AddEdge(mgraph.Edge{From: "pkg:a", To: "pkg:fmt", Kind: mgraph.EdgeDependsOn})

	viewer := newGraphViewer(g, ".", ".")
	view, err := viewer.packageView(viewRequest{ID: "pkg:a", Detail: "public"})
	if err != nil {
		t.Fatal(err)
	}

	seen := map[string]bool{}
	for _, n := range view.Nodes {
		seen[n.ID] = true
	}
	if !seen["type:exported"] || !seen["method:public"] {
		t.Fatalf("public surface missing exported type or method: %#v", seen)
	}
	if seen["type:private"] || seen["func:private"] || seen["method:foreign"] {
		t.Fatalf("public surface included private nodes: %#v", seen)
	}
	if seen["pkg:fmt"] {
		t.Fatalf("public surface included external package by default: %#v", seen)
	}

	view, err = viewer.packageView(viewRequest{ID: "pkg:a", Detail: "public", ShowExternal: true})
	if err != nil {
		t.Fatal(err)
	}
	seen = map[string]bool{}
	for _, n := range view.Nodes {
		seen[n.ID] = true
	}
	if !seen["pkg:fmt"] || !seen["method:foreign"] {
		t.Fatalf("public surface did not include external nodes when enabled: %#v", seen)
	}
}
