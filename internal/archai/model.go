// Package archai converts an archmotif typed graph into a stable
// architecture-model document that Archai (and similar domain-oriented
// tools) can ingest.
//
// The exporter is a one-way projection: archmotif is the source of
// truth for the graph, the model document is a derived view. We don't
// import archai directly — we publish a JSON/YAML schema and let the
// consumer adapt. See docs/decisions/034-archai-bridge.md for the
// mapping rationale.
//
// The document is a single ArchitectureModel value with three top-level
// sections: Packages (architecture units, equivalent to Archai
// packages/layers), Symbols (types, functions, methods — equivalent to
// Archai symbols), and Dependencies (typed edges between packages and
// symbols, including implementation links). Every entity preserves its
// archmotif node id and qualified name so downstream consumers can
// trace any model element back to graph evidence.
//
// Determinism: arrays are sorted by stable id at the package boundary.
// Two runs over the same graph produce byte-identical output.
package archai

import (
	"sort"

	"github.com/kgatilin/archmotif/internal/graph"
)

// CurrentSchemaVersion is the version emitted in
// ArchitectureModel.Schema.Version. Bump on breaking changes.
const CurrentSchemaVersion = 1

// SchemaName identifies the document family. Together with Version
// it lets consumers reject unknown shapes.
const SchemaName = "archmotif.archai-model"

// ArchitectureModel is the top-level document. It is shaped to map
// 1:1 onto Archai's package / symbol / dependency / implementation
// concepts while keeping every archmotif identifier visible for
// traceability.
type ArchitectureModel struct {
	Schema       Schema       `json:"schema" yaml:"schema"`
	Source       Source       `json:"source" yaml:"source"`
	Facets       []Facet      `json:"facets" yaml:"facets"`
	Stereotypes  []Stereotype `json:"stereotypes" yaml:"stereotypes"`
	Packages     []Package    `json:"packages" yaml:"packages"`
	Symbols      []Symbol     `json:"symbols" yaml:"symbols"`
	Dependencies []Dependency `json:"dependencies" yaml:"dependencies"`
}

// Schema identifies the document family + version.
type Schema struct {
	Name    string `json:"name" yaml:"name"`
	Version int    `json:"version" yaml:"version"`
}

// Source records what produced the document. Lets a downstream tool
// distinguish multiple bridges feeding the same model registry.
type Source struct {
	Tool   string `json:"tool" yaml:"tool"`
	Format string `json:"format" yaml:"format"`
	// Counts mirrors the input graph at export time. Useful for
	// quick smoke-checks (did we drop everything?).
	Counts Counts `json:"counts" yaml:"counts"`
}

// Counts summarises the input graph.
type Counts struct {
	Packages     int `json:"packages" yaml:"packages"`
	Symbols      int `json:"symbols" yaml:"symbols"`
	Dependencies int `json:"dependencies" yaml:"dependencies"`
}

// Facet is the model-level grouping mirror of an archmotif "view layer"
// (structure / model / behavior / control / concurrency — see ADR-016
// and the GraphML view-layer commit). Diagrams in Archai filter by
// facet to render a single architectural cut.
type Facet struct {
	ID          string `json:"id" yaml:"id"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// Stereotype is a named annotation usable by Archai to colour or
// classify symbols/packages. Currently sourced from archmotif role
// metadata (ADR-027) and contract markers (ADR-009).
type Stereotype struct {
	ID          string `json:"id" yaml:"id"`
	Source      string `json:"source" yaml:"source"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// Package is the architecture unit. Maps to Archai package/layer.
type Package struct {
	ID         string   `json:"id" yaml:"id"`
	Name       string   `json:"name" yaml:"name"`
	ImportPath string   `json:"importPath" yaml:"importPath"`
	Layer      string   `json:"layer,omitempty" yaml:"layer,omitempty"`
	Foreign    bool     `json:"foreign" yaml:"foreign"`
	Stereotype []string `json:"stereotypes,omitempty" yaml:"stereotypes,omitempty"`
	// Symbols holds the archmotif IDs of the symbols owned by this
	// package via a contains edge. Lets a consumer materialize the
	// package -> symbol relation without re-walking dependencies.
	Symbols []string `json:"symbols,omitempty" yaml:"symbols,omitempty"`
	// ArchmotifID preserves the original graph node id so a model
	// inspector can hop back to the typed graph.
	ArchmotifID string `json:"archmotifId" yaml:"archmotifId"`
}

// Symbol is a type, function, or method. Maps to Archai symbol.
type Symbol struct {
	ID         string   `json:"id" yaml:"id"`
	Name       string   `json:"name" yaml:"name"`
	QName      string   `json:"qname,omitempty" yaml:"qname,omitempty"`
	Kind       string   `json:"kind" yaml:"kind"`
	Package    string   `json:"package,omitempty" yaml:"package,omitempty"`
	Facet      string   `json:"facet,omitempty" yaml:"facet,omitempty"`
	Stereotype []string `json:"stereotypes,omitempty" yaml:"stereotypes,omitempty"`
	Foreign    bool     `json:"foreign" yaml:"foreign"`
	IsContract bool     `json:"isContract,omitempty" yaml:"isContract,omitempty"`
	Position   *Pos     `json:"position,omitempty" yaml:"position,omitempty"`
	// ArchmotifID preserves the original graph node id.
	ArchmotifID string `json:"archmotifId" yaml:"archmotifId"`
}

// Pos is a source position. Optional — symbols without a position
// (foreign placeholders, package nodes) omit it.
type Pos struct {
	File string `json:"file" yaml:"file"`
	Line int    `json:"line" yaml:"line"`
	Col  int    `json:"col" yaml:"col"`
}

// Dependency is a typed directed link between two model entities.
// A dependency may connect packages, symbols, or a mix; the From/To
// fields hold ArchitectureModel ids (i.e. archmotif node ids).
//
// The Relation field carries the archai-flavoured edge name:
//
//	"depends_on"     — package import dependency
//	"calls"          — call edges (calls, callsFrom)
//	"references"     — function used as value
//	"uses_type"      — body-level type use
//	"returns"        — return-signature type use
//	"embeds"         — type embedding
//	"implements"     — interface implementation (also surfaced in implementsOnly)
//	"contains"       — structural containment (package->symbol)
//
// The Kind field carries the original archmotif EdgeKind verbatim so
// no information is lost on the round trip.
type Dependency struct {
	ID       string `json:"id" yaml:"id"`
	From     string `json:"from" yaml:"from"`
	To       string `json:"to" yaml:"to"`
	Relation string `json:"relation" yaml:"relation"`
	Kind     string `json:"kind" yaml:"kind"`
	// IsImplementation is a convenience flag mirroring "implements"
	// edges, since Archai treats implementation links as a separate
	// concept. Always equals Relation == "implements".
	IsImplementation bool `json:"isImplementation,omitempty" yaml:"isImplementation,omitempty"`
	// ArchmotifFromKind / ToKind preserve the original endpoint
	// node kinds, so consumers don't need to cross-index symbols and
	// packages just to know the shape of an edge.
	FromKind string `json:"fromKind" yaml:"fromKind"`
	ToKind   string `json:"toKind" yaml:"toKind"`
}

// FromGraph projects g into an ArchitectureModel. Pure function; it
// does not mutate g.
func FromGraph(g *graph.Graph) ArchitectureModel {
	if g == nil {
		return emptyModel()
	}
	model := ArchitectureModel{
		Schema:      Schema{Name: SchemaName, Version: CurrentSchemaVersion},
		Source:      Source{Tool: "archmotif", Format: "archai-model"},
		Facets:      defaultFacets(),
		Stereotypes: collectStereotypes(g),
	}

	pkgs, symbols, packageOf := projectNodes(g)
	model.Packages = pkgs
	model.Symbols = symbols

	// Pass over edges. Containment fills Package.Symbols; everything
	// else turns into a Dependency. We resolve symbol owners so the
	// consumer sees package <- symbol membership as a first-class
	// list, not just an edge.
	deps := projectEdges(g, packageOf, &model.Packages)
	model.Dependencies = deps

	// Backfill Symbol.Package using the owner map populated during
	// the edge pass (only "contains" edges from package->symbol set
	// entries). Symbols whose owning package wasn't surfaced (rare:
	// foreign placeholders without a containment edge) keep an empty
	// Package field, which the consumer treats as "unknown owner".
	for i := range model.Symbols {
		if owner, ok := packageOf[model.Symbols[i].ID]; ok {
			model.Symbols[i].Package = owner
		}
	}

	model.Source.Counts = Counts{
		Packages:     len(model.Packages),
		Symbols:      len(model.Symbols),
		Dependencies: len(model.Dependencies),
	}

	// Final sort — packages and symbols are already produced sorted,
	// but sorting again is cheap and guarantees the contract regardless
	// of how the helpers below evolve. Dependencies keep their
	// edge-derived order then sort by composite id for stability.
	sort.SliceStable(model.Packages, func(i, j int) bool {
		return model.Packages[i].ID < model.Packages[j].ID
	})
	sort.SliceStable(model.Symbols, func(i, j int) bool {
		return model.Symbols[i].ID < model.Symbols[j].ID
	})
	sort.SliceStable(model.Dependencies, func(i, j int) bool {
		return model.Dependencies[i].ID < model.Dependencies[j].ID
	})
	for i := range model.Packages {
		sort.Strings(model.Packages[i].Symbols)
		sort.Strings(model.Packages[i].Stereotype)
	}
	for i := range model.Symbols {
		sort.Strings(model.Symbols[i].Stereotype)
	}
	return model
}

func emptyModel() ArchitectureModel {
	return ArchitectureModel{
		Schema:       Schema{Name: SchemaName, Version: CurrentSchemaVersion},
		Source:       Source{Tool: "archmotif", Format: "archai-model"},
		Facets:       defaultFacets(),
		Stereotypes:  []Stereotype{},
		Packages:     []Package{},
		Symbols:      []Symbol{},
		Dependencies: []Dependency{},
	}
}
