package contracts

import (
	"fmt"
	"go/types"

	"golang.org/x/tools/go/packages"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// Resolved is one config entry that successfully matched a type in the
// loaded package set.
type Resolved struct {
	Entry      Entry
	PkgPath    string
	TypeName   string
	Obj        *types.TypeName
	NodeID     string // ID of the existing graph node
	IsIface    bool
	UnderError string // non-empty when interface kind mismatches the source
}

// Unresolved is one config entry that did not match anything in the
// loaded package set. The string explains why.
type Unresolved struct {
	Entry  Entry
	Reason string
}

// ResolveResult bundles the outcome of running Resolve over a Config.
type ResolveResult struct {
	Resolved   []Resolved
	Unresolved []Unresolved
}

// Resolve maps each Config entry onto the corresponding graph node by
// looking up the named type in the loaded packages. Nodes that exist
// in the graph but not in the loaded type universe (e.g. when the
// config references a foreign-only type) are reported as Unresolved.
//
// Resolution treats `pkg/path.Name` literally: the import path is
// matched against `*packages.Package.PkgPath`, and the name against the
// package scope. Aliases are resolved through to the underlying type
// for the kind check.
func Resolve(cfg Config, pkgs []*packages.Package, g *mgraph.Graph) ResolveResult {
	// Build a quick lookup by import path → package.
	byPath := make(map[string]*packages.Package, len(pkgs))
	for _, p := range pkgs {
		if p != nil && p.PkgPath != "" {
			byPath[p.PkgPath] = p
		}
	}

	out := ResolveResult{}
	for _, e := range cfg.Contracts {
		pkgPath, typeName := SplitIdentifier(e.Identifier())
		if pkgPath == "" {
			out.Unresolved = append(out.Unresolved, Unresolved{
				Entry:  e,
				Reason: fmt.Sprintf("malformed identifier %q", e.Identifier()),
			})
			continue
		}
		p, ok := byPath[pkgPath]
		if !ok {
			out.Unresolved = append(out.Unresolved, Unresolved{
				Entry:  e,
				Reason: fmt.Sprintf("package %q not in the loaded set", pkgPath),
			})
			continue
		}
		if p.Types == nil {
			out.Unresolved = append(out.Unresolved, Unresolved{
				Entry:  e,
				Reason: fmt.Sprintf("package %q has no type info", pkgPath),
			})
			continue
		}
		obj := p.Types.Scope().Lookup(typeName)
		tn, ok := obj.(*types.TypeName)
		if !ok {
			out.Unresolved = append(out.Unresolved, Unresolved{
				Entry:  e,
				Reason: fmt.Sprintf("type %q not found in package %q", typeName, pkgPath),
			})
			continue
		}
		_, isIface := tn.Type().Underlying().(*types.Interface)

		// Kind mismatch is non-fatal but flagged: caller decides how to
		// surface it. Record the expectation gap on the Resolved entry.
		mismatch := ""
		switch e.Kind() {
		case EntryInterface:
			if !isIface {
				mismatch = fmt.Sprintf("declared as `interface:` but %s.%s is not an interface", pkgPath, typeName)
			}
		case EntryType:
			if isIface {
				mismatch = fmt.Sprintf("declared as `type:` but %s.%s is an interface (use `interface:`)", pkgPath, typeName)
			}
		}

		// Find the existing graph node by QName. The parser tags every
		// loaded named type with `<import-path>.<TypeName>` (see
		// internal/parser/decls.go), so a QName scan is enough — we
		// don't need to recompute the position-based ID.
		nodeID := lookupTypeNodeByQName(g, pkgPath+"."+typeName)
		if nodeID == "" {
			out.Unresolved = append(out.Unresolved, Unresolved{
				Entry:  e,
				Reason: fmt.Sprintf("type %s.%s resolved in types but no graph node found", pkgPath, typeName),
			})
			continue
		}

		out.Resolved = append(out.Resolved, Resolved{
			Entry:      e,
			PkgPath:    pkgPath,
			TypeName:   typeName,
			Obj:        tn,
			NodeID:     nodeID,
			IsIface:    isIface,
			UnderError: mismatch,
		})
	}
	return out
}

// lookupTypeNodeByQName scans the graph for a Type node with the given
// QName attribute. The QName is `<import-path>.<TypeName>` per
// internal/parser/decls.go.
func lookupTypeNodeByQName(g *mgraph.Graph, qname string) string {
	for _, n := range g.NodesByKind(mgraph.NodeType) {
		if n.QName == qname {
			// Skip foreign placeholders — they aren't loaded source and
			// can't be marked meaningfully.
			if n.Attrs != nil {
				if foreign, _ := n.Attrs["foreign"].(bool); foreign {
					continue
				}
			}
			return n.ID
		}
	}
	return ""
}
