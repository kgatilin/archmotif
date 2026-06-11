package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/kgatilin/archmotif/internal/contract"
	mgraph "github.com/kgatilin/archmotif/internal/graph"
	"github.com/kgatilin/archmotif/internal/graphmlx"
)

// runPkgGraph implements `archmotif pkg-graph graph.json`: project the typed
// symbol graph onto one node per package for the graph-metrics self-check.
func runPkgGraph(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("archmotif pkg-graph", flag.ContinueOnError)
	fs.SetOutput(stderr)
	outPath := fs.String("out", "", "write package-level GraphML here")
	policyPath := fs.String("policy", "", "write baseline import-flow policy YAML here")
	textPath := fs.String("text", "", "write {package_path: semantic_text} JSON here")
	textKey := fs.String("text-key", "", "also inject semantic text into GraphML nodes under this key")
	vecPath := fs.String("vec", "", "inject embeddings JSON {package_path: [floats]} as node vec attributes")
	module := fs.String("module", "", "Go module path (default: read go.mod in current directory)")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage:\n  archmotif pkg-graph [flags] <graph.json>\n\nFlags:\n")
		fs.PrintDefaults()
	}
	pos, err := parsePermissiveErr(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if len(pos) != 1 {
		fs.Usage()
		return 2
	}
	if *outPath == "" && *policyPath == "" && *textPath == "" {
		_, _ = fmt.Fprintln(stderr, "archmotif pkg-graph: pass at least one of --out, --policy, or --text")
		return 2
	}

	raw, err := os.ReadFile(pos[0])
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif pkg-graph: read %s: %v\n", pos[0], err)
		return 1
	}
	var doc mgraph.JSON
	if err := json.Unmarshal(raw, &doc); err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif pkg-graph: parse %s: %v\n", pos[0], err)
		return 1
	}
	mod := *module
	if mod == "" {
		mod, err = readModulePath("go.mod")
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif pkg-graph: %v\n", err)
			return 1
		}
	}

	proj := buildPackageProjection(doc, mod)
	if *textPath != "" || *textKey != "" {
		proj.Text = buildPackageText(doc, proj)
	}
	if *vecPath != "" {
		if proj.Vecs, err = readVecs(*vecPath); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif pkg-graph: %v\n", err)
			return 1
		}
	}

	if *outPath != "" {
		g := proj.Graph(*textKey)
		if err := writeGraphMLFile(*outPath, g); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif pkg-graph: %v\n", err)
			return 1
		}
		_, _ = fmt.Fprintf(stderr, "pkg-graph: %d packages, %d edges -> %s\n", len(proj.Packages), len(proj.Edges), *outPath)
	}
	if *textPath != "" {
		if err := writePkgJSONFile(*textPath, proj.Text); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif pkg-graph: %v\n", err)
			return 1
		}
		_, _ = fmt.Fprintf(stderr, "pkg-graph: wrote semantic text for %d packages -> %s\n", len(proj.Text), *textPath)
	}
	if *policyPath != "" {
		if err := writePolicyFile(*policyPath, proj); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif pkg-graph: %v\n", err)
			return 1
		}
		_, _ = fmt.Fprintf(stderr, "pkg-graph: wrote baseline policy -> %s\n", *policyPath)
	}
	if *outPath == "" {
		_, _ = fmt.Fprintf(stdout, "pkg-graph: %d packages, %d edges\n", len(proj.Packages), len(proj.Edges))
	}
	return 0
}

type packageProjection struct {
	Packages []packageNode
	Edges    []packageEdge
	Text     map[string]string
	Vecs     map[string][]float64
	ByQName  map[string]packageNode
}

type packageNode struct {
	ID    string
	Path  string
	Name  string
	QName string
	Group string
}

type packageEdge struct {
	From string
	To   string
}

func buildPackageProjection(doc mgraph.JSON, modulePath string) packageProjection {
	byID := map[string]packageNode{}
	proj := packageProjection{ByQName: map[string]packageNode{}}
	for _, n := range doc.Nodes {
		if n.Kind != mgraph.NodePackage || isForeign(n.Attrs) {
			continue
		}
		path := "."
		if strings.HasPrefix(n.QName, modulePath+"/") {
			path = strings.TrimPrefix(n.QName, modulePath+"/")
		}
		p := packageNode{
			ID:    n.ID,
			Path:  path,
			Name:  n.Name,
			QName: n.QName,
			Group: packageGroup(path),
		}
		byID[n.ID] = p
		proj.ByQName[n.QName] = p
		proj.Packages = append(proj.Packages, p)
	}
	sort.Slice(proj.Packages, func(i, j int) bool { return proj.Packages[i].Path < proj.Packages[j].Path })

	seenEdges := map[packageEdge]bool{}
	for _, e := range doc.Edges {
		if e.Kind != mgraph.EdgeDependsOn {
			continue
		}
		from, ok1 := byID[e.From]
		to, ok2 := byID[e.To]
		if !ok1 || !ok2 || from.Path == to.Path {
			continue
		}
		edge := packageEdge{From: from.Path, To: to.Path}
		if seenEdges[edge] {
			continue
		}
		seenEdges[edge] = true
		proj.Edges = append(proj.Edges, edge)
	}
	sort.Slice(proj.Edges, func(i, j int) bool {
		if proj.Edges[i].From != proj.Edges[j].From {
			return proj.Edges[i].From < proj.Edges[j].From
		}
		return proj.Edges[i].To < proj.Edges[j].To
	})
	return proj
}

func (p packageProjection) Graph(textKey string) *graphmlx.Graph {
	g := &graphmlx.Graph{Directed: true}
	for _, pkg := range p.Packages {
		attrs := map[string]string{"group": pkg.Group}
		if textKey != "" && p.Text[pkg.Path] != "" {
			attrs[textKey] = p.Text[pkg.Path]
		}
		if vec := p.Vecs[pkg.Path]; len(vec) > 0 {
			attrs["vec"] = formatVec64(vec)
		}
		g.Nodes = append(g.Nodes, graphmlx.Node{
			XMLID: pkg.Path,
			ID:    pkg.Path,
			Label: pkg.Name,
			Kind:  "package",
			Attrs: attrs,
		})
	}
	for _, edge := range p.Edges {
		g.Edges = append(g.Edges, graphmlx.Edge{From: edge.From, To: edge.To, Attrs: map[string]string{}})
	}
	return g
}

func buildPackageText(doc mgraph.JSON, proj packageProjection) map[string]string {
	type symbols struct {
		Interfaces []string
		Types      []string
		Functions  []string
	}
	syms := map[string]*symbols{}
	for _, n := range doc.Nodes {
		if isForeign(n.Attrs) || n.QName == "" {
			continue
		}
		dot := strings.LastIndex(n.QName, ".")
		if dot < 0 {
			continue
		}
		pkgQName, name := n.QName[:dot], n.QName[dot+1:]
		if !startsUpper(name) {
			continue
		}
		if _, ok := proj.ByQName[pkgQName]; !ok {
			continue
		}
		s := syms[pkgQName]
		if s == nil {
			s = &symbols{}
			syms[pkgQName] = s
		}
		switch n.Kind {
		case mgraph.NodeType:
			if attrString(n.Attrs, "kind") == "interface" {
				s.Interfaces = append(s.Interfaces, name)
			} else {
				s.Types = append(s.Types, name)
			}
		case mgraph.NodeFunction:
			s.Functions = append(s.Functions, name)
		}
	}
	text := map[string]string{}
	for _, pkg := range proj.Packages {
		parts := []string{"package " + pkg.Name, "path " + pkg.Path}
		if s := syms[pkg.QName]; s != nil {
			if values := uniqueSortedLimit(s.Interfaces, 30); len(values) > 0 {
				parts = append(parts, "interfaces: "+strings.Join(values, ", "))
			}
			if values := uniqueSortedLimit(s.Types, 40); len(values) > 0 {
				parts = append(parts, "types: "+strings.Join(values, ", "))
			}
			if values := uniqueSortedLimit(s.Functions, 40); len(values) > 0 {
				parts = append(parts, "functions: "+strings.Join(values, ", "))
			}
		}
		text[pkg.Path] = strings.Join(parts, ". ")
	}
	return text
}

func readModulePath(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read module path from %s: %w", path, err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "module" {
			return fields[1], nil
		}
	}
	return "", fmt.Errorf("read module path from %s: no module directive", path)
}

func readVecs(path string) (map[string][]float64, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read vectors %s: %w", path, err)
	}
	var vecs map[string][]float64
	if err := json.Unmarshal(raw, &vecs); err != nil {
		return nil, fmt.Errorf("parse vectors %s: %w", path, err)
	}
	return vecs, nil
}

func writeGraphMLFile(path string, g *graphmlx.Graph) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	if err := contract.WriteGraphML(f, g); err != nil {
		_ = f.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	return nil
}

func writePkgJSONFile(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create %s parent: %w", path, err)
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		_ = f.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	return nil
}

func writePolicyFile(path string, proj packageProjection) error {
	type pair struct{ from, to string }
	seen := map[pair]bool{}
	var pairs []pair
	for _, edge := range proj.Edges {
		from, to := packageGroup(edge.From), packageGroup(edge.To)
		if from == to {
			continue
		}
		p := pair{from: from, to: to}
		if seen[p] {
			continue
		}
		seen[p] = true
		pairs = append(pairs, p)
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].from != pairs[j].from {
			return pairs[i].from < pairs[j].from
		}
		return pairs[i].to < pairs[j].to
	})
	var b strings.Builder
	b.WriteString("# Baseline import-flow policy: the current macro dependency graph as an\n")
	b.WriteString("# allow-list. Commit it as arch-policy.yaml to ratchet — any NEW cross-group\n")
	b.WriteString("# edge then fails `archmotif policy`. Regenerate intentionally when the\n")
	b.WriteString("# architecture legitimately changes.\n")
	b.WriteString("partition: group\nallow:\n")
	for _, p := range pairs {
		b.WriteString("  - from: " + strconv.Quote(p.from) + "\n")
		b.WriteString("    to: " + strconv.Quote(p.to) + "\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func isForeign(attrs map[string]any) bool {
	if attrs == nil {
		return false
	}
	v, ok := attrs["foreign"]
	if !ok {
		return false
	}
	b, ok := v.(bool)
	return ok && b
}

func attrString(attrs map[string]any, key string) string {
	if attrs == nil {
		return ""
	}
	v, _ := attrs[key].(string)
	return v
}

func packageGroup(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) > 1 && parts[0] == "internal" {
		if parts[1] == "plugins" {
			return "plugins"
		}
		return "internal/" + parts[1]
	}
	if parts[0] == "" {
		return "(root)"
	}
	return parts[0]
}

func startsUpper(s string) bool {
	for _, r := range s {
		return unicode.IsUpper(r)
	}
	return false
}

func uniqueSortedLimit(values []string, limit int) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range values {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	if len(out) > limit {
		return out[:limit]
	}
	return out
}

func formatVec64(vec []float64) string {
	parts := make([]string, len(vec))
	for i, v := range vec {
		parts[i] = strconv.FormatFloat(v, 'f', 5, 64)
	}
	return strings.Join(parts, " ")
}
