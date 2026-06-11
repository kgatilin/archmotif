package diagram

import (
	"github.com/kgatilin/archmotif/internal/graph"
)

// buildContractPort projects the contract / port boundary view: each
// node flagged as a contract (ADR-009) or roled as a port / domain
// entity (ADR-027) is kept, alongside the implementing concrete types
// (EdgeImplements head -> tail) and the containing package.
//
// Filter rules (ADR-035):
//   - keep Type / Method / Function nodes whose IsContract() is true
//     OR whose Role is RoleTypePort, RoleTypeDomainEntity, or
//     RoleTypeValueObject;
//   - keep their containing Package nodes (for cluster layout) — found
//     by walking EdgeContains parents;
//   - keep EdgeImplements edges where both endpoints are kept; also
//     surface "implementer" Type nodes even when not roled, so the
//     diagram shows what wires into each port.
func buildContractPort(g *graph.Graph, opts Options) *Diagram {
	d := &Diagram{
		Kind:  KindContractPort,
		Title: "Contracts, ports, and implementers",
	}

	containerOf := buildContainerIndex(g)
	keep := make(map[string]struct{})
	addKeep := func(id string) {
		keep[id] = struct{}{}
	}

	// Pass 1: contracts + roled ports / domain entities / value
	// objects. These are the "primary" diagram nodes.
	primary := []graph.Node{}
	for _, n := range g.Nodes() {
		if !opts.IncludeForeign && nodeForeign(n) {
			continue
		}
		if isContractOrPort(n) {
			primary = append(primary, n)
			addKeep(n.ID)
		}
	}

	// Pass 2: implementers — walk EdgeImplements; if either side is a
	// primary, keep the other endpoint too so the diagram shows
	// concrete adapters wired to ports.
	implPairs := []graph.Edge{}
	for _, e := range g.Edges() {
		if e.Kind != graph.EdgeImplements {
			continue
		}
		_, fromKept := keep[e.From]
		_, toKept := keep[e.To]
		if !fromKept && !toKept {
			continue
		}
		from, fromOK := g.Node(e.From)
		to, toOK := g.Node(e.To)
		if !fromOK || !toOK {
			continue
		}
		if !opts.IncludeForeign && (nodeForeign(from) || nodeForeign(to)) {
			continue
		}
		addKeep(e.From)
		addKeep(e.To)
		implPairs = append(implPairs, e)
	}

	// Materialise diagram nodes with cluster = containing package
	// label. Walk in graph insertion order for determinism.
	for _, n := range g.Nodes() {
		if _, ok := keep[n.ID]; !ok {
			continue
		}
		cluster := packageClusterFor(g, containerOf, n)
		d.Nodes = append(d.Nodes, DiagNode{
			ID:          n.ID,
			Label:       contractPortLabel(n),
			Kind:        n.Kind,
			Role:        n.Role(),
			Cluster:     cluster,
			EvidenceIDs: []string{n.ID},
		})
	}

	for _, e := range implPairs {
		d.Edges = append(d.Edges, DiagEdge{
			From:        e.From,
			To:          e.To,
			Kind:        e.Kind,
			Count:       1,
			Label:       string(graph.EdgeImplements),
			EvidenceIDs: []string{edgeEvidenceID(e.From, e.To, e.Kind)},
		})
	}

	if len(primary) == 0 {
		d.Notes = append(d.Notes, "no contracts or roled ports found — run `archmotif contracts` or set roles in .archmotif.yaml")
	}

	sortNodesByLabel(d.Nodes)
	sortEdges(d.Edges)
	return d
}

// isContractOrPort tests whether a node belongs in the contract-port
// projection's primary set.
func isContractOrPort(n graph.Node) bool {
	if n.IsContract() {
		return true
	}
	switch n.Role() {
	case graph.RoleTypePort,
		graph.RoleTypeDomainEntity,
		graph.RoleTypeValueObject:
		return true
	}
	return false
}

// contractPortLabel renders a "Type" or "iface.Method" label for a
// node. Methods get the receiver-qualified shape so the diagram
// distinguishes interface members from interface types themselves.
func contractPortLabel(n graph.Node) string {
	if n.QName != "" {
		return n.QName
	}
	return n.Name
}

// buildContainerIndex returns a node-id -> direct-container-id map by
// walking EdgeContains. Used by projections that need to anchor
// non-Package nodes to their owning package for layout.
func buildContainerIndex(g *graph.Graph) map[string]string {
	out := make(map[string]string)
	for _, e := range g.Edges() {
		if e.Kind != graph.EdgeContains {
			continue
		}
		if _, dup := out[e.To]; !dup {
			out[e.To] = e.From
		}
	}
	return out
}

// packageClusterFor returns the import path of the package that
// transitively contains n, or "" when none can be found within a
// reasonable walk (16 hops).
func packageClusterFor(g *graph.Graph, containerOf map[string]string, n graph.Node) string {
	if n.Kind == graph.NodePackage {
		return packageLabel(n)
	}
	cur := n.ID
	for i := 0; i < 16; i++ {
		parent, ok := containerOf[cur]
		if !ok {
			return ""
		}
		pn, ok := g.Node(parent)
		if !ok {
			return ""
		}
		if pn.Kind == graph.NodePackage {
			return packageLabel(pn)
		}
		cur = parent
	}
	return ""
}
