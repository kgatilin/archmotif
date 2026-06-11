package parser

import (
	"go/types"

	"golang.org/x/tools/go/packages"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// emitImplements walks every loaded named type and emits an Implements
// edge for each interface in the loaded universe that the type
// satisfies. Foreign interfaces (e.g. io.Reader) are handled via
// b.foreignType placeholders; foreign concrete types are skipped because
// we don't load their method sets.
func (b *builder) emitImplements(pkgs []*packages.Package) {
	// Collect all interfaces in scope.
	type ifaceEntry struct {
		tn    *types.TypeName
		iface *types.Interface
	}
	var ifaces []ifaceEntry
	for _, p := range pkgs {
		if p.TypesInfo == nil || p.Types == nil {
			continue
		}
		scope := p.Types.Scope()
		for _, name := range scope.Names() {
			obj := scope.Lookup(name)
			tn, ok := obj.(*types.TypeName)
			if !ok {
				continue
			}
			if iface, ok := tn.Type().Underlying().(*types.Interface); ok && !iface.Empty() {
				ifaces = append(ifaces, ifaceEntry{tn: tn, iface: iface})
			}
		}
	}

	// For each loaded concrete (named, non-interface) type, check
	// implementation against each interface we found.
	for tn, concreteID := range b.typeNodes {
		concrete := tn.Type()
		if _, isIface := concrete.Underlying().(*types.Interface); isIface {
			continue
		}
		for _, ie := range ifaces {
			if ie.tn == tn {
				continue
			}
			ifaceID := b.typeNodeID(ie.tn)
			if ifaceID == "" {
				ifaceID = b.foreignType(ie.tn)
			}
			if ifaceID == "" {
				continue
			}
			// Check both value and pointer receivers — mirrors what
			// Go does at call sites.
			ptr := types.NewPointer(concrete)
			if types.Implements(concrete, ie.iface) || types.Implements(ptr, ie.iface) {
				_, _ = b.g.AddEdge(mgraph.Edge{
					From: concreteID,
					To:   ifaceID,
					Kind: mgraph.EdgeImplements,
				})
			}
		}
	}
}
