package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// graphFilter controls how loadFilteredGraph projects a raw archmotif graph
// before a metric command consumes it. It is the first-class, in-tool
// replacement for hand-massaging the graph outside the binary: scope (foreign
// exclusion) and granularity (symbol vs package) are command flags, not
// preprocessing.
type graphFilter struct {
	// IncludeForeign keeps stdlib / third-party nodes (attrs.foreign==true).
	// Default false: external dependencies are dropped so a metric describes the
	// project's own architecture.
	IncludeForeign bool
	// Granularity selects the node level: granularitySymbol (every node as-is)
	// or granularityPackage (collapse to package nodes joined by dependsOn
	// edges — the bounded-context view).
	Granularity string
}

const (
	granularitySymbol  = "symbol"
	granularityPackage = "package"
)

// validGranularity reports whether g is a recognised granularity value.
func validGranularity(g string) bool {
	return g == granularitySymbol || g == granularityPackage
}

// loadFilteredGraph reads an archmotif graph JSON file and applies the filter:
// foreign-node exclusion and optional package-level projection. Shared entry
// point for graph-consuming metric commands (communities, ppr, …) so scope and
// granularity live in the tool rather than in ad-hoc preprocessing.
func loadFilteredGraph(path string, f graphFilter) (mgraph.JSON, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return mgraph.JSON{}, fmt.Errorf("read %s: %w", path, err)
	}
	var doc mgraph.JSON
	if err := json.Unmarshal(raw, &doc); err != nil {
		return mgraph.JSON{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return filterGraph(doc, f), nil
}

// filterGraph is the pure projection behind loadFilteredGraph (no disk I/O, so
// it is directly unit-testable).
func filterGraph(doc mgraph.JSON, f graphFilter) mgraph.JSON {
	if f.Granularity == granularityPackage {
		return packageLevelGraph(doc, f.IncludeForeign)
	}
	if f.IncludeForeign {
		return doc
	}
	return dropForeignNodes(doc)
}

// dropForeignNodes returns doc without nodes flagged attrs.foreign, dropping any
// edge that referenced a removed node.
func dropForeignNodes(doc mgraph.JSON) mgraph.JSON {
	keep := make(map[string]bool, len(doc.Nodes))
	out := mgraph.JSON{Version: doc.Version}
	for _, n := range doc.Nodes {
		if isForeign(n.Attrs) {
			continue
		}
		keep[n.ID] = true
		out.Nodes = append(out.Nodes, n)
	}
	for _, e := range doc.Edges {
		if keep[e.From] && keep[e.To] {
			out.Edges = append(out.Edges, e)
		}
	}
	return out
}

// packageLevelGraph collapses doc to its package nodes (kind==package) joined by
// dependsOn edges — the same semantics as pkg-graph's buildPackageProjection,
// but emitted as an mgraph.JSON keyed by package QName so metric tools consume
// it directly. Foreign packages are excluded unless includeForeign is set.
func packageLevelGraph(doc mgraph.JSON, includeForeign bool) mgraph.JSON {
	id2name := make(map[string]string)
	out := mgraph.JSON{Version: doc.Version}
	seenNode := make(map[string]bool)
	for _, n := range doc.Nodes {
		if n.Kind != mgraph.NodePackage {
			continue
		}
		if !includeForeign && isForeign(n.Attrs) {
			continue
		}
		name := n.QName
		if name == "" {
			name = n.ID
		}
		id2name[n.ID] = name
		if !seenNode[name] {
			seenNode[name] = true
			out.Nodes = append(out.Nodes, mgraph.Node{ID: name, Kind: mgraph.NodePackage, QName: name, Name: n.Name})
		}
	}
	seenEdge := make(map[[2]string]bool)
	for _, e := range doc.Edges {
		if e.Kind != mgraph.EdgeDependsOn {
			continue
		}
		f, ok1 := id2name[e.From]
		t, ok2 := id2name[e.To]
		if !ok1 || !ok2 || f == t {
			continue
		}
		key := [2]string{f, t}
		if seenEdge[key] {
			continue
		}
		seenEdge[key] = true
		out.Edges = append(out.Edges, mgraph.Edge{From: f, To: t, Kind: mgraph.EdgeDependsOn})
	}
	sort.Slice(out.Nodes, func(i, j int) bool { return out.Nodes[i].ID < out.Nodes[j].ID })
	sort.Slice(out.Edges, func(i, j int) bool {
		if out.Edges[i].From != out.Edges[j].From {
			return out.Edges[i].From < out.Edges[j].From
		}
		return out.Edges[i].To < out.Edges[j].To
	})
	return out
}
