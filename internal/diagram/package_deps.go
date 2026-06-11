package diagram

import (
	"fmt"

	"github.com/kgatilin/archmotif/internal/graph"
)

// buildPackageDeps projects the typed graph onto its package-level
// dependency graph: nodes are Package nodes (foreign optionally
// included), edges are EdgeDependsOn between them.
//
// Filter rules (ADR-035):
//   - keep only Package nodes
//   - drop foreign packages unless opts.IncludeForeign
//   - keep EdgeDependsOn edges where both endpoints are kept
//   - cluster owned packages by their package role (when set), so
//     domain / application / adapter layers render as visual groups
//     in D2.
func buildPackageDeps(g *graph.Graph, opts Options) *Diagram {
	d := &Diagram{
		Kind:  KindPackageDeps,
		Title: "Package dependencies",
	}

	keep := make(map[string]struct{})
	for _, n := range stableNodes(g, graph.NodePackage) {
		if nodeForeign(n) && !opts.IncludeForeign {
			continue
		}
		keep[n.ID] = struct{}{}
		d.Nodes = append(d.Nodes, DiagNode{
			ID:          n.ID,
			Label:       packageLabel(n),
			Kind:        n.Kind,
			Role:        n.Role(),
			Cluster:     packageCluster(n),
			EvidenceIDs: []string{n.ID},
		})
	}

	if len(d.Nodes) == 0 {
		d.Notes = append(d.Notes, "no package nodes in scope")
		return d
	}

	for _, e := range g.Edges() {
		if e.Kind != graph.EdgeDependsOn {
			continue
		}
		if _, ok := keep[e.From]; !ok {
			continue
		}
		if _, ok := keep[e.To]; !ok {
			continue
		}
		d.Edges = append(d.Edges, DiagEdge{
			From:        e.From,
			To:          e.To,
			Kind:        e.Kind,
			Count:       1,
			EvidenceIDs: []string{edgeEvidenceID(e.From, e.To, e.Kind)},
		})
	}

	if !opts.IncludeForeign {
		d.Notes = append(d.Notes, fmt.Sprintf("foreign packages dropped (set --include-foreign to keep)"))
	}

	sortNodesByLabel(d.Nodes)
	sortEdges(d.Edges)
	return d
}

// packageLabel returns the human-friendly label for a Package node.
// Falls back to the QName (import path) when Name is empty, then to ID.
func packageLabel(n graph.Node) string {
	if n.QName != "" {
		return n.QName
	}
	if n.Name != "" {
		return n.Name
	}
	return n.ID
}

// packageCluster picks the D2 cluster id for a package node. Uses the
// package role when set so domain/adapter/infrastructure render as
// visual layers; otherwise "" (no cluster).
func packageCluster(n graph.Node) string {
	if r := n.Role(); r != "" {
		return string(r)
	}
	return ""
}
