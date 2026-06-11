package parser

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/packages"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// walkGenDeclTypes handles the type-shell part of `type` declarations.
// Fields, embedded types, and interface members are wired in a later pass.
func (b *builder) walkGenDeclTypes(p *packages.Package, d *ast.GenDecl, fileID, pkgID string) {
	for _, spec := range d.Specs {
		ts, ok := spec.(*ast.TypeSpec)
		if !ok {
			continue
		}
		b.walkTypeSpecShell(p, ts, fileID, pkgID)
	}
}

// walkGenDeclMembers handles the member part of `type` declarations after
// all loaded package type shells have been registered.
func (b *builder) walkGenDeclMembers(p *packages.Package, d *ast.GenDecl) {
	for _, spec := range d.Specs {
		ts, ok := spec.(*ast.TypeSpec)
		if !ok {
			continue
		}
		b.walkTypeSpecMembers(p, ts)
	}
}

func (b *builder) walkTypeSpecShell(p *packages.Package, ts *ast.TypeSpec, fileID, pkgID string) {
	obj := p.TypesInfo.Defs[ts.Name]
	if obj == nil {
		return
	}
	tn, ok := obj.(*types.TypeName)
	if !ok {
		return
	}
	pos := b.relPos(p.Fset, ts.Name.Pos())
	typeKind := "alias"
	switch ts.Type.(type) {
	case *ast.StructType:
		typeKind = "struct"
	case *ast.InterfaceType:
		typeKind = "interface"
	}
	qname := ""
	if tn.Pkg() != nil {
		qname = tn.Pkg().Path() + "." + tn.Name()
	} else {
		qname = tn.Name()
	}
	id := b.reserveID(mgraph.NodeType, pos, tn.Name())
	b.g.AddNode(mgraph.Node{
		ID:    id,
		Kind:  mgraph.NodeType,
		Name:  tn.Name(),
		QName: qname,
		Pos:   pos,
		Attrs: map[string]any{"typeKind": typeKind, "foreign": false},
	})
	_, _ = b.g.AddEdge(mgraph.Edge{From: fileID, To: id, Kind: mgraph.EdgeContains})
	b.typeNodes[tn] = id
	if qname != "" {
		b.typeNodesByQName[qname] = id
	}
}

func (b *builder) walkTypeSpecMembers(p *packages.Package, ts *ast.TypeSpec) {
	obj := p.TypesInfo.Defs[ts.Name]
	if obj == nil {
		return
	}
	tn, ok := obj.(*types.TypeName)
	if !ok {
		return
	}
	id := b.typeNodeID(tn)
	if id == "" {
		return
	}
	node, ok := b.g.Node(id)
	if !ok {
		return
	}
	qname := node.QName

	// Embeds and fields.
	switch tt := ts.Type.(type) {
	case *ast.StructType:
		if tt.Fields != nil {
			for _, field := range tt.Fields.List {
				if len(field.Names) == 0 {
					// Embedded field.
					embedded := exprType(p, field.Type)
					if eid := b.embedTarget(embedded); eid != "" {
						_, _ = b.g.AddEdge(mgraph.Edge{From: id, To: eid, Kind: mgraph.EdgeEmbeds})
					}
					continue
				}
				for _, fname := range field.Names {
					fpos := b.relPos(p.Fset, fname.Pos())
					fid := b.reserveID(mgraph.NodeField, fpos, fname.Name)
					ftype := exprType(p, field.Type)
					ftypeName := typeShortName(ftype)
					b.g.AddNode(mgraph.Node{
						ID:    fid,
						Kind:  mgraph.NodeField,
						Name:  fname.Name,
						QName: qname + "." + fname.Name,
						Pos:   fpos,
						Attrs: map[string]any{"type": ftypeName},
					})
					_, _ = b.g.AddEdge(mgraph.Edge{From: id, To: fid, Kind: mgraph.EdgeContains})
					if tid := b.resolveType(ftype); tid != "" {
						_, _ = b.g.AddEdge(mgraph.Edge{From: fid, To: tid, Kind: mgraph.EdgeDependsOn})
					}
				}
			}
		}
	case *ast.InterfaceType:
		if tt.Methods != nil {
			for _, m := range tt.Methods.List {
				if len(m.Names) == 0 {
					// Embedded interface.
					embedded := exprType(p, m.Type)
					if eid := b.embedTarget(embedded); eid != "" {
						_, _ = b.g.AddEdge(mgraph.Edge{From: id, To: eid, Kind: mgraph.EdgeEmbeds})
					}
					continue
				}
				for _, mname := range m.Names {
					mpos := b.relPos(p.Fset, mname.Pos())
					mid := b.reserveID(mgraph.NodeMethod, mpos, mname.Name)
					b.g.AddNode(mgraph.Node{
						ID:    mid,
						Kind:  mgraph.NodeMethod,
						Name:  mname.Name,
						QName: qname + "." + mname.Name,
						Pos:   mpos,
						Attrs: map[string]any{"interfaceMethod": true},
					})
					_, _ = b.g.AddEdge(mgraph.Edge{From: id, To: mid, Kind: mgraph.EdgeContains})
				}
			}
		}
	}
}

// embedTarget resolves an embedded type expression to a node ID. Loaded
// types use their existing node; foreign types get a placeholder.
func (b *builder) embedTarget(t types.Type) string {
	if t == nil {
		return ""
	}
	switch tt := t.(type) {
	case *types.Named:
		if id := b.typeNodeID(tt.Obj()); id != "" {
			return id
		}
		return b.foreignType(tt.Obj())
	case *types.Pointer:
		return b.embedTarget(tt.Elem())
	}
	return ""
}

func exprType(p *packages.Package, e ast.Expr) types.Type {
	if p.TypesInfo == nil {
		return nil
	}
	if tv, ok := p.TypesInfo.Types[e]; ok {
		return tv.Type
	}
	return nil
}

func typeShortName(t types.Type) string {
	if t == nil {
		return ""
	}
	return t.String()
}
