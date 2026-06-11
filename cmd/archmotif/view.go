package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	mcptransport "github.com/mark3labs/mcp-go/server"

	"github.com/kgatilin/archmotif/internal/contracts"
	mgraph "github.com/kgatilin/archmotif/internal/graph"
	"github.com/kgatilin/archmotif/internal/mcpserver"
	"github.com/kgatilin/archmotif/internal/targetcontract"
)

//go:embed static/index.html static/style.css static/app.js
var viewerStatic embed.FS

var viewerIndexTemplate = template.Must(template.ParseFS(viewerStatic, "static/index.html"))

func runView(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("archmotif view", flag.ContinueOnError)
	fs.SetOutput(stderr)
	tests := fs.Bool("tests", false, "include _test.go files")
	pattern := fs.String("pattern", "./...", "go/packages pattern")
	configPath := fs.String("config", "", "explicit path to .archmotif.yaml (overrides module-root lookup)")
	excludeDirs := fs.String("exclude-dir", "", "comma-separated source directories to skip before package loading, e.g. tests,tmp")
	root := fs.String("root", "", "graph workspace root for MCP/browser GraphML (default: $ARCHMOTIF_HOME or ~/.archmotif)")
	graphIDFlag := fs.String("graph-id", "", "graph id to write and serve (default: source directory name)")
	addr := fs.String("http", "127.0.0.1:7140", "HTTP listen address")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage:\n  archmotif view [flags] <path>\n\nFlags:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() < 1 {
		fs.Usage()
		return 2
	}
	dir := fs.Arg(0)
	res, err := contracts.Build(contracts.BuildOptions{
		Dir:         dir,
		Patterns:    []string{*pattern},
		Tests:       *tests,
		ConfigPath:  *configPath,
		ExcludeDirs: splitCSV(*excludeDirs),
	})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif view: %v\n", err)
		return 1
	}
	printGraphContractDiagnostics(res, stderr)

	workspace, err := resolveMCPRoot(*root)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif view: resolve graph workspace: %v\n", err)
		return 1
	}
	graphID := *graphIDFlag
	if graphID == "" {
		graphID = defaultGraphID(dir)
	}
	graphPath, err := writeGraphToWorkspace(workspace, graphID, res.Graph)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif view: write graph workspace: %v\n", err)
		return 1
	}

	mcpserver.Version = version
	svc := mcpserver.NewService(workspace)
	browser := newBrowserServer(svc, graphID, res.ModuleRoot, dir)
	mux := http.NewServeMux()
	registerGraphServer(mux, svc, browser)

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif view: listen %s: %v\n", *addr, err)
		return 1
	}
	url := "http://" + ln.Addr().String() + "/"
	_, _ = fmt.Fprintf(stdout, "archmotif view: %s\n", url)
	_, _ = fmt.Fprintf(stdout, "mcp endpoint: %smcp\n", url)
	_, _ = fmt.Fprintf(stdout, "graph %s: %d nodes, %d edges (%s)\n", graphID, res.Graph.NodeCount(), res.Graph.EdgeCount(), graphPath)

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		_, _ = fmt.Fprintf(stderr, "archmotif view: serve: %v\n", err)
		return 1
	}
	return 0
}

type browserServer struct {
	svc        *mcpserver.Service
	graphID    string
	moduleRoot string
	sourcePath string
}

func newBrowserServer(svc *mcpserver.Service, graphID, moduleRoot, sourcePath string) *browserServer {
	return &browserServer{svc: svc, graphID: graphID, moduleRoot: moduleRoot, sourcePath: sourcePath}
}

func registerGraphServer(mux *http.ServeMux, svc *mcpserver.Service, browser *browserServer) {
	browser.register(mux)
	mux.Handle("/mcp", mcptransport.NewStreamableHTTPServer(
		mcpserver.New(svc),
		mcptransport.WithEndpointPath("/mcp"),
		mcptransport.WithStateLess(true),
	))
}

func (b *browserServer) register(mux *http.ServeMux) {
	mux.HandleFunc("/", b.handleIndex)
	mux.HandleFunc("/static/style.css", serveEmbeddedFile("static/style.css", "text/css; charset=utf-8"))
	mux.HandleFunc("/static/app.js", serveEmbeddedFile("static/app.js", "text/javascript; charset=utf-8"))
	mux.HandleFunc("/api/layouts", b.handleLayouts)
	mux.HandleFunc("/api/graph", b.handleGraph)
	mux.HandleFunc("/api/search", b.handleSearch)
	mux.HandleFunc("/api/targets", b.handleTargets)
	mux.HandleFunc("/api/target", b.handleTarget)
}

func (b *browserServer) viewerForGraphID(graphID string) (*graphViewer, error) {
	if graphID == "" {
		graphID = b.graphID
	}
	g, err := b.svc.LoadGraph(graphID)
	if err != nil {
		return nil, err
	}
	return newGraphViewerFromStoredGraph(g, b.moduleRoot, b.sourcePath)
}

func (b *browserServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := viewerIndexTemplate.Execute(w, viewerIndexData{
		GraphID:    b.graphID,
		Source:     b.sourcePath,
		ModuleRoot: b.moduleRoot,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (b *browserServer) handleLayouts(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"default": defaultLayoutID,
		"defaults": map[string]string{
			"diff-packages": defaultLayoutForView("diff-packages"),
			"packages":      defaultLayoutForView("packages"),
			"structure":     defaultLayoutForView("structure"),
			"package":       defaultLayoutForView("package"),
			"neighborhood":  defaultLayoutForView("neighborhood"),
		},
		"layouts": layoutOptions(),
	})
}

func (b *browserServer) handleGraph(w http.ResponseWriter, r *http.Request) {
	graphID := r.URL.Query().Get("graph_id")
	if graphID == "" {
		graphID = b.graphID
	}
	viewer, err := b.viewerForGraphID(graphID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	req := viewRequestFromQuery(r)
	if req.View == "diff-packages" {
		diffFrom := strings.TrimSpace(r.URL.Query().Get("diff_from"))
		if diffFrom == "" {
			http.Error(w, "diff_from is required for diff-packages view", http.StatusBadRequest)
			return
		}
		diff, err := b.svc.DiffGraphs(diffFrom, graphID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		baseViewer, err := b.viewerForGraphID(diffFrom)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		out := buildDiffPackageView(baseViewer, viewer, req, diff)
		applyViewLayout(&out, req.Layout)
		writeJSON(w, out)
		return
	}
	out, err := viewer.buildView(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if diffFrom := strings.TrimSpace(r.URL.Query().Get("diff_from")); diffFrom != "" {
		diff, err := b.svc.DiffGraphs(diffFrom, graphID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var base *graphView
		if baseViewer, err := b.viewerForGraphID(diffFrom); err == nil {
			if baseOut, err := baseViewer.buildView(req); err == nil {
				base = &baseOut
			}
		}
		applyDiffOverlay(&out, base, diff)
	}
	writeJSON(w, out)
}

func (b *browserServer) handleSearch(w http.ResponseWriter, r *http.Request) {
	viewer, err := b.viewerForGraphID(r.URL.Query().Get("graph_id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	viewer.handleSearch(w, r)
}

func (b *browserServer) handleTargets(w http.ResponseWriter, r *http.Request) {
	graphID := r.URL.Query().Get("graph_id")
	if graphID == "" {
		graphID = b.graphID
	}
	switch r.Method {
	case http.MethodGet:
		refs, err := b.svc.ListTargets(graphID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]any{"targets": refs, "count": len(refs)})
	case http.MethodPost:
		var req struct {
			GraphID  string                  `json:"graph_id"`
			TargetID string                  `json:"target_id"`
			Force    bool                    `json:"force"`
			Contract targetcontract.Contract `json:"contract"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.GraphID != "" {
			graphID = req.GraphID
		}
		ref, err := b.svc.PutTargetGraph(graphID, req.TargetID, req.Contract, req.Force)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, ref)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (b *browserServer) handleTarget(w http.ResponseWriter, r *http.Request) {
	graphID := r.URL.Query().Get("graph_id")
	if graphID == "" {
		graphID = b.graphID
	}
	targetID := r.URL.Query().Get("target_id")
	if targetID == "" {
		targetID = r.URL.Query().Get("id")
	}
	if targetID == "" {
		http.Error(w, "target_id is required", http.StatusBadRequest)
		return
	}
	shape, err := b.svc.ShowTarget(graphID, targetID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, shape)
}

type graphViewer struct {
	graph           *mgraph.Graph
	moduleRoot      string
	sourcePath      string
	nodes           []mgraph.Node
	edges           []mgraph.Edge
	byID            map[string]mgraph.Node
	containsParents map[string][]string
	packageOf       map[string]string
	packageLabels   map[string]string
	degree          map[string]int
}

func newGraphViewer(g *mgraph.Graph, moduleRoot, sourcePath string) *graphViewer {
	v := &graphViewer{
		graph:           g,
		moduleRoot:      moduleRoot,
		sourcePath:      sourcePath,
		nodes:           g.Nodes(),
		edges:           g.Edges(),
		byID:            make(map[string]mgraph.Node),
		containsParents: make(map[string][]string),
		packageOf:       make(map[string]string),
		degree:          make(map[string]int),
	}
	for _, n := range v.nodes {
		v.byID[n.ID] = n
		if n.Kind == mgraph.NodePackage {
			v.packageOf[n.ID] = n.ID
		}
	}
	for _, e := range v.edges {
		v.degree[e.From]++
		v.degree[e.To]++
		if e.Kind == mgraph.EdgeContains {
			v.containsParents[e.To] = append(v.containsParents[e.To], e.From)
		}
	}
	for _, n := range v.nodes {
		_ = v.packageID(n.ID, map[string]bool{})
	}
	v.packageLabels = v.buildPackageLabels()
	return v
}

func newGraphViewerFromStoredGraph(g *mcpserver.Graph, moduleRoot, sourcePath string) (*graphViewer, error) {
	typed := mgraph.New()
	for _, n := range g.Nodes {
		typed.AddNode(storedNodeToTyped(n))
	}
	for _, e := range g.Edges {
		if _, err := typed.AddEdge(mgraph.Edge{
			From:  e.From,
			To:    e.To,
			Kind:  mgraph.EdgeKind(e.Kind),
			Attrs: storedAttrs(e.Attrs),
		}); err != nil {
			return nil, err
		}
	}
	return newGraphViewer(typed, moduleRoot, sourcePath), nil
}

func defaultGraphID(sourcePath string) string {
	if abs, err := filepath.Abs(sourcePath); err == nil {
		sourcePath = abs
	}
	base := filepath.Base(filepath.Clean(sourcePath))
	if base == "" || base == "." || base == string(os.PathSeparator) {
		return "graph"
	}
	return mcpserver.Slug(base)
}

func writeGraphToWorkspace(root, graphID string, g *mgraph.Graph) (string, error) {
	path, err := graphPathForID(root, graphID)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	defer func() {
		if tmp != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()
	if err := g.WriteGraphML(tmp); err != nil {
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	tmp = nil
	if err := os.Rename(tmpName, path); err != nil {
		return "", err
	}
	return path, nil
}

func graphPathForID(root, graphID string) (string, error) {
	slug, variant, err := splitGraphPathID(graphID)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "graphs", slug, variant+".graphml"), nil
}

func splitGraphPathID(graphID string) (string, string, error) {
	graphID = strings.TrimSpace(graphID)
	if graphID == "" {
		return "", "", fmt.Errorf("graph_id is required")
	}
	slug := graphID
	variant := "actual"
	if idx := strings.LastIndex(graphID, ":"); idx >= 0 {
		slug = graphID[:idx]
		variant = graphID[idx+1:]
	}
	if slug == "" || variant == "" {
		return "", "", fmt.Errorf("graph_id %q must include non-empty slug and variant", graphID)
	}
	for _, part := range []string{slug, variant} {
		fields := strings.FieldsFunc(part, func(r rune) bool { return r == '/' || r == '\\' })
		for _, seg := range fields {
			if seg == ".." {
				return "", "", fmt.Errorf("graph_id %q contains path-traversal segment %q", graphID, seg)
			}
		}
	}
	return mcpserver.Slug(slug), mcpserver.Slug(variant), nil
}

func storedNodeToTyped(n mcpserver.Node) mgraph.Node {
	attrs := storedAttrs(n.Attrs)
	name := n.Name
	if name == "" {
		name = firstStoredAttr(n.Attrs, "name", "label")
	}
	kind := n.Kind
	if kind == "" {
		kind = n.Attrs["kind"]
	}
	return mgraph.Node{
		ID:    n.ID,
		Kind:  mgraph.NodeKind(kind),
		Name:  name,
		QName: n.Attrs["qname"],
		Pos: mgraph.Position{
			File: n.Attrs["file"],
			Line: parseStoredInt(n.Attrs["line"]),
			Col:  parseStoredInt(n.Attrs["col"]),
		},
		Attrs: attrs,
	}
}

func storedAttrs(attrs map[string]string) map[string]any {
	if len(attrs) == 0 {
		return nil
	}
	out := make(map[string]any, len(attrs)+6)
	for k, v := range attrs {
		out[k] = v
	}
	if v, ok := parseStoredBool(firstStoredAttr(attrs, "foreign")); ok {
		out["foreign"] = v
	}
	if v, ok := parseStoredBool(firstStoredAttr(attrs, "is_contract", mgraph.AttrIsContract)); ok {
		out[mgraph.AttrIsContract] = v
	}
	copyStoredAlias(out, attrs, "contract_kind", mgraph.AttrContractKind)
	copyStoredAlias(out, attrs, "contract_source", mgraph.AttrContractSource)
	copyStoredAlias(out, attrs, "type_kind", "typeKind")
	copyStoredAlias(out, attrs, "role_source", mgraph.AttrRoleSource)
	return out
}

func copyStoredAlias(out map[string]any, attrs map[string]string, from, to string) {
	if v := firstStoredAttr(attrs, from, to); v != "" {
		out[to] = v
	}
}

func firstStoredAttr(attrs map[string]string, keys ...string) string {
	for _, key := range keys {
		if v, ok := attrs[key]; ok && v != "" {
			return v
		}
	}
	return ""
}

func parseStoredBool(raw string) (bool, bool) {
	if raw == "" {
		return false, false
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return false, false
	}
	return v, true
}

func parseStoredInt(raw string) int {
	if raw == "" {
		return 0
	}
	n, _ := strconv.Atoi(raw)
	return n
}

type viewerIndexData struct {
	GraphID    string
	Source     string
	ModuleRoot string
}

func serveEmbeddedFile(path, contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := viewerStatic.ReadFile(path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(data)
	}
}

func (v *graphViewer) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("q")))
	limit := intQuery(r, "limit", 30)
	if limit <= 0 || limit > 80 {
		limit = 30
	}
	out := make([]searchResult, 0)
	if q != "" {
		for _, n := range v.nodes {
			if n.Kind == mgraph.NodeFile || isPrimitiveKind(n.Kind) {
				continue
			}
			label := v.nodeLabel(n)
			if n.Kind == mgraph.NodePackage && isSyntheticPackage(n) {
				continue
			}
			if !showExternalSearch(r) && isExternalNode(n) {
				continue
			}
			hay := strings.ToLower(label + " " + n.Name + " " + n.QName + " " + n.ID)
			if !strings.Contains(hay, q) {
				continue
			}
			view := "neighborhood"
			if n.Kind == mgraph.NodePackage {
				view = "package"
			}
			out = append(out, searchResult{
				ID:       n.ID,
				Label:    label,
				Kind:     string(n.Kind),
				QName:    n.QName,
				View:     view,
				Contract: n.IsContract(),
				Foreign:  viewBoolAttr(n.Attrs, "foreign"),
			})
			if len(out) >= limit {
				break
			}
		}
	}
	writeJSON(w, map[string]any{"results": out})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

type viewRequest struct {
	View         string
	ID           string
	Detail       string
	Layout       string
	Depth        int
	Limit        int
	ShowExternal bool
}

func viewRequestFromQuery(r *http.Request) viewRequest {
	q := r.URL.Query()
	view := q.Get("view")
	if view == "" {
		view = "packages"
	}
	detail := q.Get("detail")
	if detail == "" {
		detail = "public"
	}
	layout := q.Get("layout")
	if layout == "" {
		layout = defaultLayoutForView(view)
	}
	showExternal, _ := strconv.ParseBool(q.Get("external"))
	return viewRequest{
		View:         view,
		ID:           q.Get("id"),
		Detail:       detail,
		Layout:       layout,
		Depth:        intQuery(r, "depth", 1),
		Limit:        intQuery(r, "limit", 220),
		ShowExternal: showExternal,
	}
}

func intQuery(r *http.Request, key string, fallback int) int {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return n
}

func splitCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	out := make([]string, 0)
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

type graphView struct {
	Title     string       `json:"title"`
	Subtitle  string       `json:"subtitle"`
	View      string       `json:"view"`
	Selected  string       `json:"selected,omitempty"`
	Layout    *viewLayout  `json:"layout,omitempty"`
	Diff      *viewDiff    `json:"diff,omitempty"`
	Stats     viewStats    `json:"stats"`
	Nodes     []viewNode   `json:"nodes"`
	Edges     []viewEdge   `json:"edges"`
	Truncated bool         `json:"truncated,omitempty"`
	Context   []viewAction `json:"context,omitempty"`
}

type viewDiff struct {
	From    string          `json:"from"`
	To      string          `json:"to"`
	Summary viewDiffSummary `json:"summary"`
	Visible viewDiffVisible `json:"visible"`
}

type viewDiffSummary struct {
	NodesAdded   int `json:"nodes_added"`
	NodesRemoved int `json:"nodes_removed"`
	NodesChanged int `json:"nodes_changed"`
	EdgesAdded   int `json:"edges_added"`
	EdgesRemoved int `json:"edges_removed"`
}

type viewDiffVisible struct {
	NodesAdded    int `json:"nodesAdded"`
	NodesRemoved  int `json:"nodesRemoved"`
	NodesChanged  int `json:"nodesChanged"`
	EdgesAdded    int `json:"edgesAdded"`
	EdgesRemoved  int `json:"edgesRemoved"`
	RemovedHidden int `json:"removedHidden,omitempty"`
}

type viewStats struct {
	TotalNodes int `json:"totalNodes"`
	TotalEdges int `json:"totalEdges"`
	ViewNodes  int `json:"viewNodes"`
	ViewEdges  int `json:"viewEdges"`
	Contracts  int `json:"contracts"`
	Packages   int `json:"packages"`
}

type viewLayout struct {
	ID      string  `json:"id"`
	Engine  string  `json:"engine"`
	Width   float64 `json:"width,omitempty"`
	Height  float64 `json:"height,omitempty"`
	Warning string  `json:"warning,omitempty"`
}

type viewLayoutOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Engine      string `json:"engine"`
	Placement   string `json:"placement"`
	Description string `json:"description,omitempty"`
}

type viewLayoutEngine struct {
	viewLayoutOption
	Apply func(*graphView) error
}

const defaultLayoutID = "dot"

var viewLayoutEngines = []viewLayoutEngine{
	{
		viewLayoutOption: viewLayoutOption{
			ID:          "dot",
			Label:       "Hierarchy",
			Engine:      "dot",
			Placement:   "server",
			Description: "Graphviz dot layered layout for directed dependency graphs.",
		},
		Apply: applyDotLayout,
	},
	{
		viewLayoutOption: viewLayoutOption{
			ID:          "structure",
			Label:       "Structure",
			Engine:      "browser-structure",
			Placement:   "browser",
			Description: "ArchMotif semantic layout by node kind and containment.",
		},
	},
	{
		viewLayoutOption: viewLayoutOption{
			ID:          "force",
			Label:       "Force",
			Engine:      "browser-force",
			Placement:   "browser",
			Description: "Interactive force layout for exploratory untangling.",
		},
	},
	{
		viewLayoutOption: viewLayoutOption{
			ID:          "radial",
			Label:       "Radial",
			Engine:      "browser-radial",
			Placement:   "browser",
			Description: "Radial focus layout around the selected node.",
		},
	},
}

func layoutOptions() []viewLayoutOption {
	out := make([]viewLayoutOption, 0, len(viewLayoutEngines))
	for _, engine := range viewLayoutEngines {
		out = append(out, engine.viewLayoutOption)
	}
	return out
}

func layoutEngineByID(id string) viewLayoutEngine {
	if id == "hierarchy" || id == "flow" {
		id = defaultLayoutID
	}
	for _, engine := range viewLayoutEngines {
		if engine.ID == id {
			return engine
		}
	}
	return viewLayoutEngines[0]
}

func defaultLayoutForView(view string) string {
	switch view {
	case "diff-packages", "package", "structure":
		return "structure"
	case "", "packages", "neighborhood":
		return "dot"
	default:
		return defaultLayoutID
	}
}

type viewPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type viewNode struct {
	ID           string            `json:"id"`
	Label        string            `json:"label"`
	Kind         string            `json:"kind"`
	Diff         string            `json:"diff,omitempty"`
	AttrsDiff    map[string][2]any `json:"attrsDiff,omitempty"`
	QName        string            `json:"qname,omitempty"`
	Package      string            `json:"package,omitempty"`
	File         string            `json:"file,omitempty"`
	Line         int               `json:"line,omitempty"`
	Foreign      bool              `json:"foreign,omitempty"`
	Contract     bool              `json:"contract,omitempty"`
	ContractKind string            `json:"contractKind,omitempty"`
	TypeKind     string            `json:"typeKind,omitempty"`
	Role         string            `json:"role,omitempty"`
	Exported     bool              `json:"exported,omitempty"`
	Degree       int               `json:"degree"`
	X            float64           `json:"x,omitempty"`
	Y            float64           `json:"y,omitempty"`
}

type viewEdge struct {
	ID     string      `json:"id"`
	From   string      `json:"from"`
	To     string      `json:"to"`
	Kind   string      `json:"kind"`
	Diff   string      `json:"diff,omitempty"`
	Label  string      `json:"label"`
	Weight int         `json:"weight,omitempty"`
	Points []viewPoint `json:"points,omitempty"`
}

type viewAction struct {
	Label string `json:"label"`
	View  string `json:"view"`
	ID    string `json:"id,omitempty"`
}

type searchResult struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Kind     string `json:"kind"`
	QName    string `json:"qname,omitempty"`
	View     string `json:"view"`
	Contract bool   `json:"contract,omitempty"`
	Foreign  bool   `json:"foreign,omitempty"`
}

func (v *graphViewer) buildView(req viewRequest) (graphView, error) {
	if req.Limit <= 0 {
		req.Limit = 220
	}
	if req.Limit > 1000 {
		req.Limit = 1000
	}
	var (
		gv  graphView
		err error
	)
	switch req.View {
	case "", "packages":
		gv = v.packageOverview(req)
	case "structure":
		gv = v.structureView(req)
	case "package":
		gv, err = v.packageView(req)
	case "neighborhood":
		gv, err = v.neighborhoodView(req)
	default:
		return graphView{}, fmt.Errorf("unknown view %q", req.View)
	}
	if err != nil {
		return graphView{}, err
	}
	applyViewLayout(&gv, req.Layout)
	return gv, nil
}

func applyViewLayout(gv *graphView, layoutID string) {
	layout := layoutEngineByID(layoutID)
	gv.Layout = &viewLayout{ID: layout.ID, Engine: layout.Engine}
	if layout.Apply != nil {
		if err := layout.Apply(gv); err != nil {
			gv.Layout = &viewLayout{ID: layout.ID, Engine: layout.Engine, Warning: err.Error()}
		}
	}
}

func (v *graphViewer) packageOverview(req viewRequest) graphView {
	keep := make(map[string]bool)
	for _, n := range v.nodes {
		if n.Kind != mgraph.NodePackage {
			continue
		}
		if isSyntheticPackage(n) {
			continue
		}
		if !req.ShowExternal && isExternalNode(n) {
			continue
		}
		keep[n.ID] = true
	}
	gv := v.renderKeepSet("Packages", "Loaded package dependency view", "packages", "", keep, req.Limit)
	gv.Context = []viewAction{{Label: "Package overview", View: "packages"}}
	return gv
}

func (v *graphViewer) structureView(req viewRequest) graphView {
	keep := make(map[string]bool)
	publicOnly := req.Detail != "all"
	for _, n := range v.nodes {
		if !req.ShowExternal && isExternalNode(n) {
			continue
		}
		if publicOnly && !isStructuralOverviewNode(n) && !n.IsContract() {
			continue
		}
		keep[n.ID] = true
		if len(keep) >= req.Limit {
			break
		}
	}
	title := "Structure"
	subtitle := "Structural graph view"
	if publicOnly {
		subtitle = "Packages, types, functions, methods, fields, and contracts"
	}
	gv := v.renderKeepSet(title, subtitle, "structure", "", keep, req.Limit)
	gv.Context = []viewAction{{Label: "Package overview", View: "packages"}}
	return gv
}

func buildDiffPackageView(base, target *graphViewer, req viewRequest, d mcpserver.GraphDiff) graphView {
	if req.Limit < 600 {
		req.Limit = 600
	}
	if req.Limit > 1500 {
		req.Limit = 1500
	}
	touched := make(map[string]bool)
	status := make(map[string]string)
	nodeDiffStatus := make(map[string]string)
	nodeAttrsDiff := make(map[string]map[string][2]any)
	mark := func(pkgID, next string) {
		if pkgID == "" {
			return
		}
		if !req.ShowExternal {
			if n, ok := target.byID[pkgID]; ok && isExternalNode(n) {
				return
			}
			if n, ok := base.byID[pkgID]; ok && isExternalNode(n) {
				return
			}
		}
		touched[pkgID] = true
		status[pkgID] = mergePackageDiffStatus(status[pkgID], next)
	}
	markAddedNode := func(id, kind string) {
		nodeDiffStatus[id] = "added"
		pkgID := target.packageID(id, map[string]bool{})
		if pkgID == "" && kind == string(mgraph.NodePackage) {
			pkgID = id
		}
		if pkgID == "" {
			return
		}
		if kind == string(mgraph.NodePackage) {
			mark(pkgID, "added")
			return
		}
		if _, existed := base.byID[pkgID]; existed {
			mark(pkgID, "changed")
		} else {
			mark(pkgID, "added")
		}
	}
	markRemovedNode := func(id, kind string) {
		nodeDiffStatus[id] = "removed"
		pkgID := base.packageID(id, map[string]bool{})
		if pkgID == "" && kind == string(mgraph.NodePackage) {
			pkgID = id
		}
		if pkgID == "" {
			return
		}
		if kind == string(mgraph.NodePackage) {
			mark(pkgID, "removed")
			return
		}
		if _, stillExists := target.byID[pkgID]; stillExists {
			mark(pkgID, "changed")
		} else {
			mark(pkgID, "removed")
		}
	}
	for _, n := range d.Nodes.Added {
		markAddedNode(n.ID, n.Kind)
	}
	for _, n := range d.Nodes.Removed {
		markRemovedNode(n.ID, n.Kind)
	}
	for _, n := range d.Nodes.Changed {
		nodeDiffStatus[n.ID] = "changed"
		nodeAttrsDiff[n.ID] = n.AttrsDiff
		if pkgID := target.packageID(n.ID, map[string]bool{}); pkgID != "" {
			mark(pkgID, "changed")
		} else if pkgID := base.packageID(n.ID, map[string]bool{}); pkgID != "" {
			mark(pkgID, "changed")
		}
	}
	for _, e := range d.Edges.Added {
		for _, pkgID := range target.packagesForEdge(e.From, e.To) {
			mark(pkgID, "changed")
		}
	}
	for _, e := range d.Edges.Removed {
		for _, pkgID := range base.packagesForEdge(e.From, e.To) {
			mark(pkgID, "changed")
		}
	}

	nodes := make([]viewNode, 0, len(touched))
	nodeIDs := make(map[string]bool, len(touched))
	packageNodeIDs := make(map[string]bool, len(touched))
	visible := viewDiffVisible{}
	for _, n := range target.nodes {
		if n.Kind != mgraph.NodePackage || !touched[n.ID] || isSyntheticPackage(n) {
			continue
		}
		vn := target.toViewNode(n)
		vn.Diff = status[n.ID]
		countPackageNodeDiff(&visible, vn.Diff)
		nodes = append(nodes, vn)
		nodeIDs[n.ID] = true
		packageNodeIDs[n.ID] = true
		if len(nodes) >= req.Limit {
			break
		}
	}
	if len(nodes) < req.Limit {
		for _, n := range base.nodes {
			if n.Kind != mgraph.NodePackage || !touched[n.ID] || nodeIDs[n.ID] || isSyntheticPackage(n) {
				continue
			}
			vn := base.toViewNode(n)
			vn.Diff = mergePackageDiffStatus(status[n.ID], "removed")
			countPackageNodeDiff(&visible, vn.Diff)
			nodes = append(nodes, vn)
			nodeIDs[n.ID] = true
			packageNodeIDs[n.ID] = true
			if len(nodes) >= req.Limit {
				break
			}
		}
	}

	targetEdges := target.packageEdges(touched, req.ShowExternal)
	baseEdges := base.packageEdges(touched, req.ShowExternal)
	edges := make([]viewEdge, 0)
	edgeKeys := make(map[string]bool)
	for _, e := range targetEdges {
		if !nodeIDs[e.From] || !nodeIDs[e.To] {
			continue
		}
		key := viewEdgeKey(e.From, e.To, e.Kind)
		if _, existed := baseEdges[key]; !existed {
			e.Diff = "added"
			visible.EdgesAdded++
		}
		edges = append(edges, e)
		edgeKeys[key] = true
	}
	for key, e := range baseEdges {
		if edgeKeys[key] || !nodeIDs[e.From] || !nodeIDs[e.To] {
			continue
		}
		e.Diff = "removed"
		e.ID = "diff-removed-" + strconv.Itoa(len(edges))
		edges = append(edges, e)
		visible.EdgesRemoved++
	}

	surfacePackageID := req.ID
	appendSurface := func(v *graphViewer, id, diff string, attrs map[string][2]any) {
		if len(nodes) >= req.Limit || nodeIDs[id] {
			return
		}
		n, ok := v.byID[id]
		if !ok || !isPackageSurfaceKind(n.Kind) {
			return
		}
		if req.Detail != "all" && !v.isSurfaceNode(n) {
			return
		}
		if !req.ShowExternal && isExternalNode(n) {
			return
		}
		pkgID := v.packageID(id, map[string]bool{})
		if pkgID == "" || !packageNodeIDs[pkgID] {
			return
		}
		if surfacePackageID == "" || pkgID != surfacePackageID {
			return
		}
		vn := v.toViewNode(n)
		vn.Diff = diff
		vn.AttrsDiff = attrs
		nodes = append(nodes, vn)
		nodeIDs[id] = true
		countPackageNodeDiff(&visible, diff)
		edge := viewEdge{
			ID:     "diff-surface-" + strconv.Itoa(len(edges)),
			From:   pkgID,
			To:     id,
			Kind:   string(mgraph.EdgeContains),
			Diff:   diff,
			Label:  string(mgraph.EdgeContains),
			Weight: edgeWeight(mgraph.EdgeContains),
		}
		edges = append(edges, edge)
	}
	for _, n := range d.Nodes.Added {
		appendSurface(target, n.ID, nodeDiffStatus[n.ID], nil)
	}
	for _, n := range d.Nodes.Changed {
		appendSurface(target, n.ID, nodeDiffStatus[n.ID], nodeAttrsDiff[n.ID])
	}
	for _, n := range d.Nodes.Removed {
		appendSurface(base, n.ID, nodeDiffStatus[n.ID], nil)
	}

	return graphView{
		Title:    "Changed Packages",
		Subtitle: diffPackageSubtitle(surfacePackageID),
		View:     "diff-packages",
		Selected: surfacePackageID,
		Layout:   nil,
		Diff: &viewDiff{
			From: d.A,
			To:   d.B,
			Summary: viewDiffSummary{
				NodesAdded:   visible.NodesAdded,
				NodesRemoved: visible.NodesRemoved,
				NodesChanged: visible.NodesChanged,
				EdgesAdded:   visible.EdgesAdded,
				EdgesRemoved: visible.EdgesRemoved,
			},
			Visible: visible,
		},
		Stats: viewStats{
			TotalNodes: len(target.nodes),
			TotalEdges: len(target.edges),
			ViewNodes:  len(nodes),
			ViewEdges:  len(edges),
			Packages:   len(packageNodeIDs),
		},
		Nodes:     nodes,
		Edges:     edges,
		Truncated: len(touched) > len(nodes),
		Context:   []viewAction{{Label: "Package overview", View: "packages"}},
	}
}

func (v *graphViewer) packageView(req viewRequest) (graphView, error) {
	pkg, ok := v.byID[req.ID]
	if !ok || pkg.Kind != mgraph.NodePackage {
		return graphView{}, fmt.Errorf("unknown package %q", req.ID)
	}
	publicOnly := req.Detail != "all"
	keep := map[string]bool{pkg.ID: true}
	for _, n := range v.nodes {
		if v.packageOf[n.ID] != pkg.ID {
			continue
		}
		if !req.ShowExternal && isExternalNode(n) {
			continue
		}
		if !isPackageSurfaceKind(n.Kind) {
			continue
		}
		if publicOnly && !v.isSurfaceNode(n) {
			continue
		}
		keep[n.ID] = true
		v.addSymbolParents(keep, n.ID)
	}
	for _, e := range v.edges {
		if e.Kind != mgraph.EdgeDependsOn {
			continue
		}
		if e.From == pkg.ID {
			if !req.ShowExternal && isExternalNode(v.byID[e.To]) {
				continue
			}
			keep[e.To] = true
		}
	}
	if !req.ShowExternal {
		v.dropExternal(keep, pkg.ID)
	}
	title := "Package " + v.nodeLabel(pkg)
	subtitle := "Public surface, contracts, and direct package dependencies"
	if !publicOnly {
		subtitle = "All types, functions, methods, fields, and direct package dependencies"
	}
	gv := v.renderKeepSet(title, subtitle, "package", pkg.ID, keep, req.Limit)
	gv.Context = []viewAction{
		{Label: "Package overview", View: "packages"},
		{Label: "Neighborhood", View: "neighborhood", ID: pkg.ID},
	}
	return gv, nil
}

func (v *graphViewer) neighborhoodView(req viewRequest) (graphView, error) {
	seed, ok := v.byID[req.ID]
	if !ok {
		return graphView{}, fmt.Errorf("unknown node %q", req.ID)
	}
	depth := req.Depth
	if depth <= 0 {
		depth = 1
	}
	if depth > 4 {
		depth = 4
	}
	keep := map[string]bool{seed.ID: true}
	frontier := []string{seed.ID}
	for i := 0; i < depth; i++ {
		next := make([]string, 0)
		for _, id := range frontier {
			for _, e := range v.edges {
				other := ""
				if e.From == id {
					other = e.To
				} else if e.To == id {
					other = e.From
				}
				if other == "" || keep[other] {
					continue
				}
				n := v.byID[other]
				if !req.ShowExternal && isExternalNode(n) {
					continue
				}
				if req.Detail != "all" && !v.isNeighborhoodNode(n, seed.ID) {
					continue
				}
				keep[other] = true
				next = append(next, other)
				if len(keep) >= req.Limit {
					break
				}
			}
			if len(keep) >= req.Limit {
				break
			}
		}
		frontier = next
		if len(frontier) == 0 || len(keep) >= req.Limit {
			break
		}
	}
	v.addSymbolParents(keep, seed.ID)
	if pkgID := v.packageID(seed.ID, map[string]bool{}); pkgID != "" {
		keep[pkgID] = true
	}
	if !req.ShowExternal {
		v.dropExternal(keep, seed.ID)
	}
	title := "Focus " + v.nodeLabel(seed)
	subtitle := fmt.Sprintf("Neighborhood depth %d", depth)
	gv := v.renderKeepSet(title, subtitle, "neighborhood", seed.ID, keep, req.Limit)
	gv.Context = []viewAction{{Label: "Package overview", View: "packages"}}
	if pkgID := v.packageID(seed.ID, map[string]bool{}); pkgID != "" {
		gv.Context = append(gv.Context, viewAction{Label: "Open package", View: "package", ID: pkgID})
	}
	return gv, nil
}

func (v *graphViewer) renderKeepSet(title, subtitle, view, selected string, keep map[string]bool, limit int) graphView {
	if limit <= 0 {
		limit = 220
	}
	ids := make([]string, 0, len(keep))
	for _, n := range v.nodes {
		if keep[n.ID] {
			ids = append(ids, n.ID)
		}
	}
	truncated := false
	if len(ids) > limit {
		truncated = true
		ids = ids[:limit]
		keep = make(map[string]bool, len(ids))
		for _, id := range ids {
			keep[id] = true
		}
	}
	nodes := make([]viewNode, 0, len(ids))
	contractsCount := 0
	packagesCount := 0
	for _, id := range ids {
		n := v.byID[id]
		if n.IsContract() {
			contractsCount++
		}
		if n.Kind == mgraph.NodePackage {
			packagesCount++
		}
		nodes = append(nodes, v.toViewNode(n))
	}
	edges := make([]viewEdge, 0)
	for i, e := range v.edges {
		if !keep[e.From] || !keep[e.To] {
			continue
		}
		edges = append(edges, viewEdge{
			ID:     "e" + strconv.Itoa(i),
			From:   e.From,
			To:     e.To,
			Kind:   string(e.Kind),
			Label:  string(e.Kind),
			Weight: edgeWeight(e.Kind),
		})
	}
	return graphView{
		Title:     title,
		Subtitle:  subtitle,
		View:      view,
		Selected:  selected,
		Stats:     viewStats{TotalNodes: len(v.nodes), TotalEdges: len(v.edges), ViewNodes: len(nodes), ViewEdges: len(edges), Contracts: contractsCount, Packages: packagesCount},
		Nodes:     nodes,
		Edges:     edges,
		Truncated: truncated,
	}
}

func applyDiffOverlay(gv *graphView, base *graphView, d mcpserver.GraphDiff) {
	nodeStatus := make(map[string]string)
	nodeAttrs := make(map[string]map[string][2]any)
	for _, n := range d.Nodes.Added {
		nodeStatus[n.ID] = "added"
	}
	for _, n := range d.Nodes.Changed {
		nodeStatus[n.ID] = "changed"
		nodeAttrs[n.ID] = n.AttrsDiff
	}
	for _, n := range d.Nodes.Removed {
		nodeStatus[n.ID] = "removed"
	}
	visible := viewDiffVisible{}
	nodeIDs := make(map[string]bool, len(gv.Nodes))
	for i := range gv.Nodes {
		nodeIDs[gv.Nodes[i].ID] = true
		switch nodeStatus[gv.Nodes[i].ID] {
		case "added":
			gv.Nodes[i].Diff = "added"
			visible.NodesAdded++
		case "changed":
			gv.Nodes[i].Diff = "changed"
			gv.Nodes[i].AttrsDiff = nodeAttrs[gv.Nodes[i].ID]
			visible.NodesChanged++
		}
	}
	if base != nil {
		for _, n := range base.Nodes {
			if nodeStatus[n.ID] != "removed" || nodeIDs[n.ID] {
				continue
			}
			n.Diff = "removed"
			n.AttrsDiff = nil
			gv.Nodes = append(gv.Nodes, n)
			nodeIDs[n.ID] = true
			visible.NodesRemoved++
		}
	}

	addedEdges := make(map[string]bool, len(d.Edges.Added))
	for _, e := range d.Edges.Added {
		addedEdges[viewEdgeKey(e.From, e.To, e.Kind)] = true
	}
	removedEdges := make(map[string]bool, len(d.Edges.Removed))
	for _, e := range d.Edges.Removed {
		removedEdges[viewEdgeKey(e.From, e.To, e.Kind)] = true
	}
	edgeKeys := make(map[string]bool, len(gv.Edges))
	for i := range gv.Edges {
		key := viewEdgeKey(gv.Edges[i].From, gv.Edges[i].To, gv.Edges[i].Kind)
		edgeKeys[key] = true
		if addedEdges[key] {
			gv.Edges[i].Diff = "added"
			visible.EdgesAdded++
		}
	}
	if base != nil {
		for _, e := range base.Edges {
			key := viewEdgeKey(e.From, e.To, e.Kind)
			if !removedEdges[key] || edgeKeys[key] || !nodeIDs[e.From] || !nodeIDs[e.To] {
				continue
			}
			e.Diff = "removed"
			e.ID = "diff-removed-" + strconv.Itoa(len(gv.Edges))
			gv.Edges = append(gv.Edges, e)
			edgeKeys[key] = true
			visible.EdgesRemoved++
		}
	}
	visible.RemovedHidden = (len(d.Nodes.Removed) - visible.NodesRemoved) + (len(d.Edges.Removed) - visible.EdgesRemoved)
	if visible.RemovedHidden < 0 {
		visible.RemovedHidden = 0
	}
	gv.Diff = &viewDiff{
		From: d.A,
		To:   d.B,
		Summary: viewDiffSummary{
			NodesAdded:   d.Summary.NodesAdded,
			NodesRemoved: d.Summary.NodesRemoved,
			NodesChanged: d.Summary.NodesChanged,
			EdgesAdded:   d.Summary.EdgesAdded,
			EdgesRemoved: d.Summary.EdgesRemoved,
		},
		Visible: visible,
	}
	gv.Stats.ViewNodes = len(gv.Nodes)
	gv.Stats.ViewEdges = len(gv.Edges)
	gv.Stats.Packages = 0
	gv.Stats.Contracts = 0
	for _, n := range gv.Nodes {
		if n.Kind == string(mgraph.NodePackage) {
			gv.Stats.Packages++
		}
		if n.Contract {
			gv.Stats.Contracts++
		}
	}
	if gv.Subtitle == "" {
		gv.Subtitle = "Diff from " + d.A
	} else {
		gv.Subtitle += " · diff from " + d.A
	}
}

func viewEdgeKey(from, to, kind string) string {
	return from + "\x00" + to + "\x00" + kind
}

func applyDotLayout(gv *graphView) error {
	if len(gv.Nodes) == 0 {
		return nil
	}
	src := dotSource(*gv)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "dot", "-Tplain")
	cmd.Stdin = strings.NewReader(src)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return fmt.Errorf("dot layout timed out")
	}
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("dot layout failed: %s", msg)
	}
	plain, err := parseDotPlain(string(out))
	if err != nil {
		return err
	}
	for i := range gv.Nodes {
		if p, ok := plain.Nodes[gv.Nodes[i].ID]; ok {
			gv.Nodes[i].X = p.X
			gv.Nodes[i].Y = p.Y
		}
	}
	edgeRoutes := make(map[string][][]viewPoint)
	for _, e := range plain.Edges {
		key := e.From + "\x00" + e.To
		edgeRoutes[key] = append(edgeRoutes[key], e.Points)
	}
	for i := range gv.Edges {
		key := gv.Edges[i].From + "\x00" + gv.Edges[i].To
		routes := edgeRoutes[key]
		if len(routes) == 0 {
			continue
		}
		gv.Edges[i].Points = routes[0]
		edgeRoutes[key] = routes[1:]
	}
	gv.Layout = &viewLayout{
		ID:     "dot",
		Engine: "dot",
		Width:  plain.Width,
		Height: plain.Height,
	}
	return nil
}

func dotSource(gv graphView) string {
	var b strings.Builder
	b.WriteString("digraph archmotif {\n")
	b.WriteString("  graph [rankdir=TB, newrank=true, concentrate=true, splines=polyline, outputorder=edgesfirst, nodesep=0.28, ranksep=\"0.62 equally\"];\n")
	b.WriteString("  node [shape=circle, fixedsize=true, label=\"\", style=filled, fillcolor=\"#2563eb\", color=\"#1f2933\"];\n")
	b.WriteString("  edge [arrowsize=0.55, color=\"#9ca3af\"];\n")
	for _, n := range gv.Nodes {
		size := dotNodeSizeInches(n)
		fill := dotNodeFill(n)
		color := "#1f2933"
		penwidth := "1"
		if n.Contract {
			color = "#d946ef"
			penwidth = "3"
		}
		_, _ = fmt.Fprintf(&b, "  %s [width=%.3f, height=%.3f, fillcolor=%s, color=%s, penwidth=%s];\n",
			dotID(n.ID), size, size, strconv.Quote(fill), strconv.Quote(color), penwidth)
	}
	for _, e := range gv.Edges {
		minLen := 1
		weight := e.Weight
		if weight <= 0 {
			weight = 1
		}
		_, _ = fmt.Fprintf(&b, "  %s -> %s [weight=%d, minlen=%d];\n", dotID(e.From), dotID(e.To), weight, minLen)
	}
	b.WriteString("}\n")
	return b.String()
}

func dotID(id string) string {
	return strconv.Quote(id)
}

func dotNodeSizeInches(n viewNode) float64 {
	base := 0.22
	switch n.Kind {
	case string(mgraph.NodePackage):
		base = 0.42
	case string(mgraph.NodeType):
		base = 0.34
	case string(mgraph.NodeMethod), string(mgraph.NodeFunction):
		base = 0.27
	}
	base += math.Min(0.28, math.Log2(float64(max(1, n.Degree))+1)*0.06)
	if n.Contract {
		base += 0.09
	}
	return base
}

func dotNodeFill(n viewNode) string {
	switch n.Kind {
	case string(mgraph.NodePackage):
		return "#2563eb"
	case string(mgraph.NodeType):
		return "#0f766e"
	case string(mgraph.NodeFunction):
		return "#d97706"
	case string(mgraph.NodeMethod):
		return "#ea580c"
	case string(mgraph.NodeField):
		return "#64748b"
	default:
		return "#7c3aed"
	}
}

type dotPlainGraph struct {
	Width  float64
	Height float64
	Nodes  map[string]viewPoint
	Edges  []dotPlainEdge
}

type dotPlainEdge struct {
	From   string
	To     string
	Points []viewPoint
}

func parseDotPlain(raw string) (dotPlainGraph, error) {
	out := dotPlainGraph{Nodes: make(map[string]viewPoint)}
	for _, line := range strings.Split(raw, "\n") {
		fields := plainFields(line)
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "graph":
			if len(fields) < 4 {
				return out, fmt.Errorf("bad dot graph line: %q", line)
			}
			width, err := strconv.ParseFloat(fields[2], 64)
			if err != nil {
				return out, fmt.Errorf("bad dot graph width: %w", err)
			}
			height, err := strconv.ParseFloat(fields[3], 64)
			if err != nil {
				return out, fmt.Errorf("bad dot graph height: %w", err)
			}
			out.Width = width
			out.Height = height
		case "node":
			if len(fields) < 4 {
				return out, fmt.Errorf("bad dot node line: %q", line)
			}
			x, err := strconv.ParseFloat(fields[2], 64)
			if err != nil {
				return out, fmt.Errorf("bad dot node x: %w", err)
			}
			y, err := strconv.ParseFloat(fields[3], 64)
			if err != nil {
				return out, fmt.Errorf("bad dot node y: %w", err)
			}
			out.Nodes[fields[1]] = viewPoint{X: x, Y: y}
		case "edge":
			if len(fields) < 5 {
				return out, fmt.Errorf("bad dot edge line: %q", line)
			}
			count, err := strconv.Atoi(fields[3])
			if err != nil {
				return out, fmt.Errorf("bad dot edge point count: %w", err)
			}
			if len(fields) < 4+count*2 {
				return out, fmt.Errorf("bad dot edge point list: %q", line)
			}
			points := make([]viewPoint, 0, count)
			for i := 0; i < count; i++ {
				x, err := strconv.ParseFloat(fields[4+i*2], 64)
				if err != nil {
					return out, fmt.Errorf("bad dot edge x: %w", err)
				}
				y, err := strconv.ParseFloat(fields[5+i*2], 64)
				if err != nil {
					return out, fmt.Errorf("bad dot edge y: %w", err)
				}
				points = append(points, viewPoint{X: x, Y: y})
			}
			out.Edges = append(out.Edges, dotPlainEdge{From: fields[1], To: fields[2], Points: points})
		case "stop":
			return out, nil
		}
	}
	return out, nil
}

func plainFields(line string) []string {
	fields := make([]string, 0)
	for i := 0; i < len(line); {
		for i < len(line) && (line[i] == ' ' || line[i] == '\t' || line[i] == '\r') {
			i++
		}
		if i >= len(line) {
			break
		}
		if line[i] != '"' {
			start := i
			for i < len(line) && line[i] != ' ' && line[i] != '\t' && line[i] != '\r' {
				i++
			}
			fields = append(fields, line[start:i])
			continue
		}
		i++
		var b strings.Builder
		for i < len(line) {
			if line[i] == '\\' && i+1 < len(line) {
				b.WriteByte(line[i+1])
				i += 2
				continue
			}
			if line[i] == '"' {
				i++
				break
			}
			b.WriteByte(line[i])
			i++
		}
		fields = append(fields, b.String())
	}
	return fields
}

func (v *graphViewer) toViewNode(n mgraph.Node) viewNode {
	pkgID := v.packageID(n.ID, map[string]bool{})
	out := viewNode{
		ID:           n.ID,
		Label:        v.nodeLabel(n),
		Kind:         string(n.Kind),
		QName:        n.QName,
		Package:      pkgID,
		File:         n.Pos.File,
		Line:         n.Pos.Line,
		Foreign:      viewBoolAttr(n.Attrs, "foreign"),
		Contract:     n.IsContract(),
		ContractKind: n.ContractKind(),
		TypeKind:     viewStringAttr(n.Attrs, "typeKind"),
		Role:         string(n.Role()),
		Exported:     isExportedName(n.Name),
		Degree:       v.degree[n.ID],
	}
	return out
}

func (v *graphViewer) nodeLabel(n mgraph.Node) string {
	if n.Kind == mgraph.NodePackage {
		if label := v.packageLabels[n.ID]; label != "" {
			return label
		}
		if n.QName != "" {
			return shortImportPath(n.QName, 2)
		}
	}
	if n.Kind == mgraph.NodeMethod {
		if recv := viewStringAttr(n.Attrs, "receiver"); recv != "" && n.Name != "" {
			return recv + "." + n.Name
		}
	}
	if n.Name != "" {
		return n.Name
	}
	if n.QName != "" {
		return shortImportPath(n.QName, 2)
	}
	return n.ID
}

func (v *graphViewer) isSurfaceNode(n mgraph.Node) bool {
	if n.IsContract() || isExportedName(n.Name) {
		return true
	}
	for _, parent := range v.containsParents[n.ID] {
		p := v.byID[parent]
		if p.IsContract() && n.Kind == mgraph.NodeMethod {
			return true
		}
	}
	return false
}

func (v *graphViewer) isNeighborhoodNode(n mgraph.Node, seedID string) bool {
	if n.ID == seedID {
		return true
	}
	if n.Kind == mgraph.NodePackage || n.Kind == mgraph.NodeType || n.Kind == mgraph.NodeFunction || n.Kind == mgraph.NodeMethod {
		return true
	}
	return n.IsContract()
}

func (v *graphViewer) addSymbolParents(keep map[string]bool, id string) {
	for _, parentID := range v.containsParents[id] {
		parent := v.byID[parentID]
		if parent.Kind == mgraph.NodeType || parent.Kind == mgraph.NodePackage {
			keep[parentID] = true
			v.addSymbolParents(keep, parentID)
		}
	}
}

func (v *graphViewer) dropExternal(keep map[string]bool, preserve string) {
	for id := range keep {
		if id == preserve {
			continue
		}
		if isExternalNode(v.byID[id]) {
			delete(keep, id)
		}
	}
}

func (v *graphViewer) packagesForEdge(from, to string) []string {
	out := make([]string, 0, 2)
	seen := make(map[string]bool, 2)
	for _, id := range []string{from, to} {
		pkgID := v.packageID(id, map[string]bool{})
		if pkgID == "" || seen[pkgID] {
			continue
		}
		seen[pkgID] = true
		out = append(out, pkgID)
	}
	return out
}

func (v *graphViewer) packageEdges(keep map[string]bool, showExternal bool) map[string]viewEdge {
	out := make(map[string]viewEdge)
	for i, e := range v.edges {
		from, fromOK := v.byID[e.From]
		to, toOK := v.byID[e.To]
		if !fromOK || !toOK {
			continue
		}
		if from.Kind != mgraph.NodePackage || to.Kind != mgraph.NodePackage {
			continue
		}
		if e.Kind != mgraph.EdgeDependsOn {
			continue
		}
		if !keep[e.From] || !keep[e.To] {
			continue
		}
		if !showExternal && (isExternalNode(from) || isExternalNode(to)) {
			continue
		}
		if isSyntheticPackage(from) || isSyntheticPackage(to) {
			continue
		}
		ve := viewEdge{
			ID:     "e" + strconv.Itoa(i),
			From:   e.From,
			To:     e.To,
			Kind:   string(e.Kind),
			Label:  string(e.Kind),
			Weight: edgeWeight(e.Kind),
		}
		out[viewEdgeKey(ve.From, ve.To, ve.Kind)] = ve
	}
	return out
}

func (v *graphViewer) packageID(id string, visiting map[string]bool) string {
	if pkg, ok := v.packageOf[id]; ok {
		return pkg
	}
	if visiting[id] {
		return ""
	}
	visiting[id] = true
	n, ok := v.byID[id]
	if !ok {
		return ""
	}
	if n.Kind == mgraph.NodePackage {
		v.packageOf[id] = id
		return id
	}
	for _, parentID := range v.containsParents[id] {
		if pkg := v.packageID(parentID, visiting); pkg != "" {
			v.packageOf[id] = pkg
			return pkg
		}
	}
	return ""
}

func (v *graphViewer) buildPackageLabels() map[string]string {
	packages := make([]mgraph.Node, 0)
	for _, n := range v.nodes {
		if n.Kind == mgraph.NodePackage {
			packages = append(packages, n)
		}
	}
	labels := make(map[string]string, len(packages))
	for depth := 1; depth <= 8; depth++ {
		counts := make(map[string]int)
		for _, p := range packages {
			label := shortImportPath(p.QName, depth)
			if label == "" {
				label = p.Name
			}
			counts[label]++
		}
		for _, p := range packages {
			if labels[p.ID] != "" {
				continue
			}
			label := shortImportPath(p.QName, depth)
			if label == "" {
				label = p.Name
			}
			if counts[label] == 1 {
				labels[p.ID] = label
			}
		}
	}
	for _, p := range packages {
		if labels[p.ID] == "" {
			labels[p.ID] = p.QName
		}
	}
	return labels
}

func isPackageSurfaceKind(kind mgraph.NodeKind) bool {
	switch kind {
	case mgraph.NodeType, mgraph.NodeFunction, mgraph.NodeMethod, mgraph.NodeField:
		return true
	default:
		return false
	}
}

func isStructuralOverviewNode(n mgraph.Node) bool {
	if n.Kind == mgraph.NodePackage || n.Kind == mgraph.NodeFile {
		return true
	}
	return isPackageSurfaceKind(n.Kind)
}

func mergePackageDiffStatus(current, next string) string {
	if current == "" {
		return next
	}
	if current == next {
		return current
	}
	if current == "added" || current == "removed" {
		return current
	}
	if next == "added" || next == "removed" {
		return next
	}
	return "changed"
}

func countPackageNodeDiff(visible *viewDiffVisible, status string) {
	switch status {
	case "added":
		visible.NodesAdded++
	case "removed":
		visible.NodesRemoved++
	case "changed":
		visible.NodesChanged++
	}
}

func diffPackageSubtitle(surfacePackageID string) string {
	if surfacePackageID == "" {
		return "Package dependency view filtered to packages touched by the diff"
	}
	return "Package dependency view with public diff surface for the selected package"
}

func isPrimitiveKind(kind mgraph.NodeKind) bool {
	switch kind {
	case mgraph.NodeLoop, mgraph.NodeBranch, mgraph.NodeGoroutine, mgraph.NodeDefer, mgraph.NodeChannelOp, mgraph.NodeSyncPrim:
		return true
	default:
		return false
	}
}

func isSyntheticPackage(n mgraph.Node) bool {
	return n.Kind == mgraph.NodePackage && (n.QName == "" || strings.HasPrefix(n.QName, "."))
}

func edgeWeight(kind mgraph.EdgeKind) int {
	switch kind {
	case mgraph.EdgeContains:
		return 3
	case mgraph.EdgeDependsOn:
		return 2
	case mgraph.EdgeImplements, mgraph.EdgeReturns, mgraph.EdgeUsesType:
		return 2
	default:
		return 1
	}
}

func viewBoolAttr(attrs map[string]any, key string) bool {
	if attrs == nil {
		return false
	}
	switch v := attrs[key].(type) {
	case bool:
		return v
	case string:
		b, _ := strconv.ParseBool(v)
		return b
	default:
		return false
	}
}

func isExternalNode(n mgraph.Node) bool {
	return viewBoolAttr(n.Attrs, "foreign")
}

func showExternalSearch(r *http.Request) bool {
	show, _ := strconv.ParseBool(r.URL.Query().Get("external"))
	return show
}

func viewStringAttr(attrs map[string]any, key string) string {
	if attrs == nil {
		return ""
	}
	switch v := attrs[key].(type) {
	case string:
		return v
	default:
		return ""
	}
}

func isExportedName(name string) bool {
	if name == "" {
		return false
	}
	r := rune(name[0])
	return r >= 'A' && r <= 'Z'
}

func shortImportPath(path string, depth int) string {
	if path == "" {
		return ""
	}
	parts := strings.Split(path, "/")
	if depth <= 0 || depth >= len(parts) {
		return path
	}
	return strings.Join(parts[len(parts)-depth:], "/")
}
