// Package archmotifimport exposes archmotif's typed-graph construction
// through a small, stable Go API so external tools can build archmotif
// graphs in-process — without forking archmotif and without going through
// file-based GraphML/JSON round-trips.
//
// The package is intentionally language-agnostic: it knows nothing about
// archai, Java, Go, or any other source. It is a ~thin wrapper over
// internal/graph whose method set mirrors the 12 node kinds and 9 edge
// kinds defined in internal/graph/kinds.go (see the mapping table below).
//
// # Mapping to internal/graph/kinds.go
//
//	Builder method                    internal/graph
//	AddPackage(id, layer, aggregate)  Node(NodePackage)
//	AddType(id, pkg, iface, role)     Node(NodeType) + Edge(EdgeContains) from pkg
//	AddFunction(id, pkg)              Node(NodeFunction) + Edge(EdgeContains) from pkg
//	AddMethod(id, parentType)         Node(NodeMethod) + Edge(EdgeContains) from parentType
//	AddField(id, parentStruct, ref)   Node(NodeField) + Edge(EdgeContains) from parentStruct
//	AddDependency(from, to, kind)     Edge(<kind>); see DependencyKind constants
//	AddImplements(struct, iface)      Edge(EdgeImplements)
//	AddContains(parent, child)        Edge(EdgeContains)
//	Build()                           returns the constructed *Graph
//
// # Package path note
//
// The companion ticket suggested `pkg/import`. Because `import` is a Go
// keyword, that import path forces every caller to use an import alias
// (the bare identifier would not parse). We therefore use the slightly
// longer `pkg/archmotifimport` and keep the in-source package name the
// ticket asked for (`archmotifimport`). The PR body records this
// deviation.
package archmotifimport

import (
	"errors"
	"fmt"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// Graph is the constructed typed graph. It is a type alias of
// internal/graph.Graph so external callers can pass the value back into
// any future public helper that takes *Graph without an extra cast.
type Graph = mgraph.Graph

// DependencyKind tags a generic edge created via AddDependency. The
// constants cover the seven non-structural edge kinds defined in
// internal/graph/kinds.go (EdgeContains and EdgeImplements have their
// own dedicated methods).
type DependencyKind string

// DependencyKind constants — one per edge kind in internal/graph/kinds.go
// other than EdgeContains and EdgeImplements (which have dedicated
// builder methods).
const (
	DependencyDependsOn  DependencyKind = "dependsOn"
	DependencyCalls      DependencyKind = "calls"
	DependencyCallsFrom  DependencyKind = "callsFrom"
	DependencyReferences DependencyKind = "references"
	DependencyEmbeds     DependencyKind = "embeds"
	DependencyReturns    DependencyKind = "returns"
	DependencyUsesType   DependencyKind = "usesType"
)

// edgeKind translates a DependencyKind to the underlying graph.EdgeKind.
// Returns ("", false) for unknown kinds; the caller surfaces a clear error.
func (k DependencyKind) edgeKind() (mgraph.EdgeKind, bool) {
	switch k {
	case DependencyDependsOn:
		return mgraph.EdgeDependsOn, true
	case DependencyCalls:
		return mgraph.EdgeCalls, true
	case DependencyCallsFrom:
		return mgraph.EdgeCallsFrom, true
	case DependencyReferences:
		return mgraph.EdgeReferences, true
	case DependencyEmbeds:
		return mgraph.EdgeEmbeds, true
	case DependencyReturns:
		return mgraph.EdgeReturns, true
	case DependencyUsesType:
		return mgraph.EdgeUsesType, true
	}
	return "", false
}

// Builder constructs a typed archmotif graph from external inputs.
// Methods validate that ids are non-empty and that referenced parents
// exist; on any violation they return an error and the graph is left
// unchanged.
type Builder struct {
	g *mgraph.Graph
}

// NewBuilder returns a fresh Builder backed by an empty graph.
func NewBuilder() *Builder {
	return &Builder{g: mgraph.New()}
}

// AddPackage inserts a NodePackage with the given stable id. The
// optional layer and aggregate strings are recorded as Attrs so callers
// can carry architectural metadata through without touching internal/graph.
func (b *Builder) AddPackage(id, layer, aggregate string) error {
	if id == "" {
		return errors.New("archmotifimport: AddPackage: id must be non-empty")
	}
	attrs := map[string]any{}
	if layer != "" {
		attrs["layer"] = layer
	}
	if aggregate != "" {
		attrs["aggregate"] = aggregate
	}
	if len(attrs) == 0 {
		attrs = nil
	}
	_, inserted := b.g.AddNode(mgraph.Node{ID: id, Kind: mgraph.NodePackage, Name: id, Attrs: attrs})
	if !inserted {
		return fmt.Errorf("archmotifimport: AddPackage: id %q already exists", id)
	}
	return nil
}

// AddType inserts a NodeType nested under packageID via EdgeContains.
// isInterface flags the type form (struct vs interface) and role records
// an architectural role (e.g. "port", "domain_entity") as a free-form
// string in Attrs.
func (b *Builder) AddType(id, packageID string, isInterface bool, role string) error {
	if err := requireID("AddType", "id", id); err != nil {
		return err
	}
	if err := requireParent(b.g, "AddType", "packageID", packageID, mgraph.NodePackage); err != nil {
		return err
	}
	attrs := map[string]any{"isInterface": isInterface}
	if role != "" {
		attrs["role"] = role
	}
	if _, inserted := b.g.AddNode(mgraph.Node{ID: id, Kind: mgraph.NodeType, Name: id, Attrs: attrs}); !inserted {
		return fmt.Errorf("archmotifimport: AddType: id %q already exists", id)
	}
	if _, err := b.g.AddEdge(mgraph.Edge{From: packageID, To: id, Kind: mgraph.EdgeContains}); err != nil {
		return fmt.Errorf("archmotifimport: AddType: link to package: %w", err)
	}
	return nil
}

// AddFunction inserts a NodeFunction nested under packageID via EdgeContains.
func (b *Builder) AddFunction(id, packageID string) error {
	if err := requireID("AddFunction", "id", id); err != nil {
		return err
	}
	if err := requireParent(b.g, "AddFunction", "packageID", packageID, mgraph.NodePackage); err != nil {
		return err
	}
	if _, inserted := b.g.AddNode(mgraph.Node{ID: id, Kind: mgraph.NodeFunction, Name: id}); !inserted {
		return fmt.Errorf("archmotifimport: AddFunction: id %q already exists", id)
	}
	if _, err := b.g.AddEdge(mgraph.Edge{From: packageID, To: id, Kind: mgraph.EdgeContains}); err != nil {
		return fmt.Errorf("archmotifimport: AddFunction: link to package: %w", err)
	}
	return nil
}

// AddMethod inserts a NodeMethod nested under parentTypeID via EdgeContains.
func (b *Builder) AddMethod(id, parentTypeID string) error {
	if err := requireID("AddMethod", "id", id); err != nil {
		return err
	}
	if err := requireParent(b.g, "AddMethod", "parentTypeID", parentTypeID, mgraph.NodeType); err != nil {
		return err
	}
	if _, inserted := b.g.AddNode(mgraph.Node{ID: id, Kind: mgraph.NodeMethod, Name: id}); !inserted {
		return fmt.Errorf("archmotifimport: AddMethod: id %q already exists", id)
	}
	if _, err := b.g.AddEdge(mgraph.Edge{From: parentTypeID, To: id, Kind: mgraph.EdgeContains}); err != nil {
		return fmt.Errorf("archmotifimport: AddMethod: link to type: %w", err)
	}
	return nil
}

// AddField inserts a NodeField nested under parentStructID via EdgeContains.
// typeRef records the field's declared type as a free-form string in Attrs.
func (b *Builder) AddField(id, parentStructID string, typeRef string) error {
	if err := requireID("AddField", "id", id); err != nil {
		return err
	}
	if err := requireParent(b.g, "AddField", "parentStructID", parentStructID, mgraph.NodeType); err != nil {
		return err
	}
	attrs := map[string]any{}
	if typeRef != "" {
		attrs["typeRef"] = typeRef
	}
	if len(attrs) == 0 {
		attrs = nil
	}
	if _, inserted := b.g.AddNode(mgraph.Node{ID: id, Kind: mgraph.NodeField, Name: id, Attrs: attrs}); !inserted {
		return fmt.Errorf("archmotifimport: AddField: id %q already exists", id)
	}
	if _, err := b.g.AddEdge(mgraph.Edge{From: parentStructID, To: id, Kind: mgraph.EdgeContains}); err != nil {
		return fmt.Errorf("archmotifimport: AddField: link to parent type: %w", err)
	}
	return nil
}

// AddDependency creates a generic edge of the given kind between two
// existing nodes. Use AddImplements or AddContains for those specific
// edge kinds.
func (b *Builder) AddDependency(fromID, toID string, kind DependencyKind) error {
	if fromID == "" || toID == "" {
		return errors.New("archmotifimport: AddDependency: fromID and toID must be non-empty")
	}
	ek, ok := kind.edgeKind()
	if !ok {
		return fmt.Errorf("archmotifimport: AddDependency: unknown kind %q", string(kind))
	}
	if !b.g.HasNode(fromID) {
		return fmt.Errorf("archmotifimport: AddDependency: unknown from-node %q", fromID)
	}
	if !b.g.HasNode(toID) {
		return fmt.Errorf("archmotifimport: AddDependency: unknown to-node %q", toID)
	}
	if _, err := b.g.AddEdge(mgraph.Edge{From: fromID, To: toID, Kind: ek}); err != nil {
		return fmt.Errorf("archmotifimport: AddDependency: %w", err)
	}
	return nil
}

// AddImplements inserts an EdgeImplements edge from a concrete type
// (structID) to an interface (interfaceID). Both endpoints must already
// exist as NodeType.
func (b *Builder) AddImplements(structID, interfaceID string) error {
	if structID == "" || interfaceID == "" {
		return errors.New("archmotifimport: AddImplements: structID and interfaceID must be non-empty")
	}
	if !b.g.HasNode(structID) {
		return fmt.Errorf("archmotifimport: AddImplements: unknown struct %q", structID)
	}
	if !b.g.HasNode(interfaceID) {
		return fmt.Errorf("archmotifimport: AddImplements: unknown interface %q", interfaceID)
	}
	if _, err := b.g.AddEdge(mgraph.Edge{From: structID, To: interfaceID, Kind: mgraph.EdgeImplements}); err != nil {
		return fmt.Errorf("archmotifimport: AddImplements: %w", err)
	}
	return nil
}

// AddContains inserts an explicit EdgeContains edge between two existing
// nodes. The dedicated Add* methods (AddType, AddFunction, ...) already
// create the natural parent→child contains edge; this method is for
// callers that want to record additional containment relationships.
func (b *Builder) AddContains(parentID, childID string) error {
	if parentID == "" || childID == "" {
		return errors.New("archmotifimport: AddContains: parentID and childID must be non-empty")
	}
	if !b.g.HasNode(parentID) {
		return fmt.Errorf("archmotifimport: AddContains: unknown parent %q", parentID)
	}
	if !b.g.HasNode(childID) {
		return fmt.Errorf("archmotifimport: AddContains: unknown child %q", childID)
	}
	if _, err := b.g.AddEdge(mgraph.Edge{From: parentID, To: childID, Kind: mgraph.EdgeContains}); err != nil {
		return fmt.Errorf("archmotifimport: AddContains: %w", err)
	}
	return nil
}

// Build returns the constructed typed graph. After calling Build the
// caller may continue to add nodes/edges via further Builder calls;
// Build does not freeze the underlying graph. Returns a non-nil *Graph
// and a nil error in the current implementation; the error return is
// reserved for future validation passes (e.g. orphaned-child checks).
func (b *Builder) Build() (*Graph, error) {
	return b.g, nil
}

// requireID validates an id parameter is non-empty.
func requireID(method, name, id string) error {
	if id == "" {
		return fmt.Errorf("archmotifimport: %s: %s must be non-empty", method, name)
	}
	return nil
}

// requireParent validates a parent id exists and has the expected kind.
func requireParent(g *mgraph.Graph, method, name, id string, want mgraph.NodeKind) error {
	if id == "" {
		return fmt.Errorf("archmotifimport: %s: %s must be non-empty", method, name)
	}
	n, ok := g.Node(id)
	if !ok {
		return fmt.Errorf("archmotifimport: %s: unknown %s %q", method, name, id)
	}
	if n.Kind != want {
		return fmt.Errorf("archmotifimport: %s: %s %q is %s, want %s", method, name, id, n.Kind, want)
	}
	return nil
}
