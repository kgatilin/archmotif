package archai

import (
	"sort"

	"github.com/kgatilin/archmotif/internal/graph"
)

// defaultFacets returns the static facet catalogue used by every
// model export. The names mirror the GraphML view-layer attributes
// produced by internal/graph/graphml.go (see the fc7cc1c commit
// "Add body reference edges and GraphML view layers"), so two
// projections of the same graph stay aligned.
func defaultFacets() []Facet {
	return []Facet{
		{ID: "structure", Description: "structural containment: packages, files, top-level decls"},
		{ID: "model", Description: "type-level shape: types, fields, embeds"},
		{ID: "behavior", Description: "behavior-level: functions and methods"},
		{ID: "control", Description: "control-flow primitives: loops, branches, defers"},
		{ID: "concurrency", Description: "concurrency primitives: goroutines, channel ops, sync prims"},
	}
}

// nodeFacet returns the facet name for a node kind. Mirrors
// graph.graphml.go:nodeLayer so consumers can join the model export
// with GraphML-driven views by facet id.
func nodeFacet(k graph.NodeKind) string {
	switch k {
	case graph.NodePackage, graph.NodeFile:
		return "structure"
	case graph.NodeType, graph.NodeField:
		return "model"
	case graph.NodeFunction, graph.NodeMethod:
		return "behavior"
	case graph.NodeLoop, graph.NodeBranch, graph.NodeDefer:
		return "control"
	case graph.NodeGoroutine, graph.NodeChannelOp, graph.NodeSyncPrim:
		return "concurrency"
	default:
		return ""
	}
}

// edgeRelation translates an archmotif EdgeKind into the snake_case
// archai vocabulary. We collapse "callsFrom" into "calls" because
// downstream Archai treats them as the same relation; the original
// kind is kept verbatim in Dependency.Kind so nothing is lost.
func edgeRelation(k graph.EdgeKind) string {
	switch k {
	case graph.EdgeContains:
		return "contains"
	case graph.EdgeImplements:
		return "implements"
	case graph.EdgeEmbeds:
		return "embeds"
	case graph.EdgeCalls, graph.EdgeCallsFrom:
		return "calls"
	case graph.EdgeReferences:
		return "references"
	case graph.EdgeDependsOn:
		return "depends_on"
	case graph.EdgeReturns:
		return "returns"
	case graph.EdgeUsesType:
		return "uses_type"
	default:
		return string(k)
	}
}

// collectStereotypes walks the graph once and emits a deduplicated,
// sorted list of stereotype descriptors. Each archmotif role becomes
// a stereotype with id == role string, plus a single "contract"
// stereotype if any node carries the contract marker.
func collectStereotypes(g *graph.Graph) []Stereotype {
	seen := make(map[string]Stereotype)
	for _, n := range g.Nodes() {
		if r := string(n.Role()); r != "" {
			id := "role:" + r
			if _, ok := seen[id]; !ok {
				seen[id] = Stereotype{
					ID:          id,
					Source:      "archmotif.role",
					Description: stereotypeDescription(r),
				}
			}
		}
		if n.IsContract() {
			id := "contract"
			if _, ok := seen[id]; !ok {
				seen[id] = Stereotype{
					ID:          id,
					Source:      "archmotif.contract",
					Description: "marked as a stable contract by .archmotif.yaml",
				}
			}
		}
	}
	out := make([]Stereotype, 0, len(seen))
	for _, s := range seen {
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// stereotypeDescription gives a one-line label for known role tags.
// Unknown roles fall through to "" — the consumer still gets the id.
func stereotypeDescription(role string) string {
	switch role {
	case string(graph.RolePackageDomain):
		return "domain layer (ADR-027)"
	case string(graph.RolePackageApplication):
		return "application layer (ADR-027)"
	case string(graph.RolePackageInboundAdapter):
		return "inbound adapter layer (ADR-027)"
	case string(graph.RolePackageOutboundAdapter):
		return "outbound adapter layer (ADR-027)"
	case string(graph.RolePackageInfrastructure):
		return "infrastructure layer (ADR-027)"
	case string(graph.RolePackageShared):
		return "shared layer (ADR-027)"
	case string(graph.RoleTypeDomainEntity):
		return "domain entity (ADR-027)"
	case string(graph.RoleTypeValueObject):
		return "value object (ADR-027)"
	case string(graph.RoleTypePort):
		return "port / interface seam (ADR-027)"
	case string(graph.RoleTypeAdapterDTO):
		return "adapter DTO (ADR-027)"
	case string(graph.RoleTypeConfigContract):
		return "config contract (ADR-027)"
	case string(graph.RoleTypeExternalNoise):
		return "external noise (ADR-027)"
	default:
		return ""
	}
}

// nodeStereotypes returns the stereotype ids attached to a node:
// role-based ids prefixed with "role:" plus "contract" when set.
func nodeStereotypes(n graph.Node) []string {
	out := make([]string, 0, 2)
	if r := string(n.Role()); r != "" {
		out = append(out, "role:"+r)
	}
	if n.IsContract() {
		out = append(out, "contract")
	}
	return out
}

// boolAttr safely reads a boolean Attrs entry.
func boolAttr(attrs map[string]any, key string) bool {
	if attrs == nil {
		return false
	}
	v, ok := attrs[key]
	if !ok {
		return false
	}
	b, _ := v.(bool)
	return b
}

// projectNodes walks every graph node once and produces:
//   - the package list (one entry per Package node, including foreign
//     placeholders so dependsOn edges land on real targets),
//   - the symbol list (one entry per Type/Function/Method/Field node),
//   - a packageOf map (symbol id -> owning package id), populated by
//     projectEdges via "contains" edges.
//
// Control-flow nodes (loop / branch / goroutine / defer / channelop /
// syncprim / file) are intentionally dropped from the model: Archai's
// vocabulary stops at the symbol level, and surfacing every control
// node would dilute the diagrams Archai is built to render. The
// archmotif graph remains the source of truth for control-flow.
func projectNodes(g *graph.Graph) ([]Package, []Symbol, map[string]string) {
	pkgs := make([]Package, 0)
	symbols := make([]Symbol, 0)
	packageOf := make(map[string]string)

	for _, n := range g.Nodes() {
		switch n.Kind {
		case graph.NodePackage:
			p := Package{
				ID:          n.ID,
				ArchmotifID: n.ID,
				Name:        n.Name,
				ImportPath:  n.QName,
				Foreign:     boolAttr(n.Attrs, "foreign"),
				Layer:       string(n.Role()),
				Stereotype:  nodeStereotypes(n),
			}
			pkgs = append(pkgs, p)
		case graph.NodeType, graph.NodeFunction, graph.NodeMethod, graph.NodeField:
			s := Symbol{
				ID:          n.ID,
				ArchmotifID: n.ID,
				Name:        n.Name,
				QName:       n.QName,
				Kind:        string(n.Kind),
				Facet:       nodeFacet(n.Kind),
				Foreign:     boolAttr(n.Attrs, "foreign"),
				IsContract:  n.IsContract(),
				Stereotype:  nodeStereotypes(n),
			}
			if n.Pos.File != "" {
				s.Position = &Pos{File: n.Pos.File, Line: n.Pos.Line, Col: n.Pos.Col}
			}
			symbols = append(symbols, s)
		}
	}
	return pkgs, symbols, packageOf
}

// projectEdges walks the edge list once and produces the Dependencies
// slice. It also feeds Package.Symbols using the "contains" edges, so
// the caller passes a pointer to the slice so we can mutate in place.
//
// We emit a Dependency for every edge except package->file containment
// (filtered out: file nodes are not part of the model) and edges that
// reference a node we already dropped (control-flow primitives).
func projectEdges(g *graph.Graph, packageOf map[string]string, pkgs *[]Package) []Dependency {
	if pkgs == nil {
		return nil
	}

	// Index nodes by id once; cheaper than walking g.Nodes per edge.
	nodes := make(map[string]graph.Node, g.NodeCount())
	for _, n := range g.Nodes() {
		nodes[n.ID] = n
	}

	// Index packages so we can append to their Symbols list during
	// the contains-edge pass.
	pkgIdx := make(map[string]int, len(*pkgs))
	for i, p := range *pkgs {
		pkgIdx[p.ID] = i
	}

	// Predicate: is this node id in the model? File / control-flow
	// nodes never made it into projectNodes, so they're absent from
	// either pkgIdx or the symbol set. Build a symbol-id set lazily.
	included := func(id string, kind graph.NodeKind) bool {
		switch kind {
		case graph.NodePackage,
			graph.NodeType, graph.NodeFunction, graph.NodeMethod, graph.NodeField:
			return true
		default:
			return false
		}
	}

	deps := make([]Dependency, 0, g.EdgeCount())
	for _, e := range g.Edges() {
		from, fromOK := nodes[e.From]
		to, toOK := nodes[e.To]
		if !fromOK || !toOK {
			continue
		}
		if !included(e.From, from.Kind) || !included(e.To, to.Kind) {
			continue
		}

		// "contains" edges from a package -> symbol fold into
		// Package.Symbols *and* still surface as a dependency, so
		// the consumer can pick whichever shape it prefers.
		if e.Kind == graph.EdgeContains && from.Kind == graph.NodePackage {
			if i, ok := pkgIdx[e.From]; ok {
				(*pkgs)[i].Symbols = append((*pkgs)[i].Symbols, e.To)
			}
			packageOf[e.To] = e.From
		}

		dep := Dependency{
			ID:               dependencyID(e),
			From:             e.From,
			To:               e.To,
			Relation:         edgeRelation(e.Kind),
			Kind:             string(e.Kind),
			IsImplementation: e.Kind == graph.EdgeImplements,
			FromKind:         string(from.Kind),
			ToKind:           string(to.Kind),
		}
		deps = append(deps, dep)
	}

	// Backfill Symbol.Package using packageOf. We do it here, after
	// the edge walk, so symbols whose containing package only became
	// known via a "contains" edge still get a populated Package id.
	// The model's Symbols slice was returned by reference into the
	// same backing array, but here we're stuck with a copy — so
	// callers (FromGraph) re-resolve Package on Symbols themselves.
	// This helper instead exposes the map.
	_ = packageOf // visible to FromGraph already

	return deps
}

// dependencyID is a stable id for a Dependency. We embed the kind so
// parallel edges (e.g. EdgeCalls + EdgeCallsFrom between the same
// pair) get distinct ids.
func dependencyID(e graph.Edge) string {
	return e.From + "->" + string(e.Kind) + "->" + e.To
}
