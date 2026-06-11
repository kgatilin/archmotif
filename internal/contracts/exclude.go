package contracts

import (
	"strings"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// ApplyExcludes returns a graph with configured exclude matches removed.
// The input graph is returned unchanged when there are no excludes.
func ApplyExcludes(g *mgraph.Graph, ex Exclude) *mgraph.Graph {
	if g == nil || excludeEmpty(ex) {
		return g
	}
	drop := make(map[string]struct{})
	for _, n := range g.Nodes() {
		if excludeMatches(n, ex) {
			drop[n.ID] = struct{}{}
		}
	}
	if len(drop) == 0 {
		return g
	}

	out := mgraph.New()
	for _, n := range g.Nodes() {
		if _, ok := drop[n.ID]; ok {
			continue
		}
		out.AddNode(n)
	}
	for _, e := range g.Edges() {
		if _, ok := drop[e.From]; ok {
			continue
		}
		if _, ok := drop[e.To]; ok {
			continue
		}
		_, _ = out.AddEdge(e)
	}
	return out
}

func excludeEmpty(ex Exclude) bool {
	return len(ex.QNames) == 0 &&
		len(ex.QNamePrefixes) == 0 &&
		len(ex.Packages) == 0 &&
		len(ex.Kinds) == 0
}

func excludeMatches(n mgraph.Node, ex Exclude) bool {
	kind := string(n.Kind)
	for _, k := range ex.Kinds {
		if kind == strings.TrimSpace(k) {
			return true
		}
	}
	qname := strings.TrimSpace(n.QName)
	if qname == "" {
		return false
	}
	for _, want := range ex.QNames {
		if qname == strings.TrimSpace(want) {
			return true
		}
	}
	for _, prefix := range ex.QNamePrefixes {
		if strings.HasPrefix(qname, strings.TrimSpace(prefix)) {
			return true
		}
	}
	for _, pkg := range ex.Packages {
		if qnameInPackage(qname, strings.TrimSpace(pkg)) {
			return true
		}
	}
	return false
}

func qnameInPackage(qname, pkg string) bool {
	if pkg == "" {
		return false
	}
	return qname == pkg ||
		strings.HasPrefix(qname, pkg+".") ||
		strings.HasPrefix(qname, "("+pkg+".") ||
		strings.HasPrefix(qname, "(*"+pkg+".")
}
