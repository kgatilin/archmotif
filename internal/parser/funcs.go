package parser

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/packages"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// emitFuncDecl emits a Function or Method node plus signature-level edges.
// Bodies are walked in a later pass so references to declarations that appear
// later in the file resolve to loaded nodes instead of foreign placeholders.
func (b *builder) emitFuncDecl(p *packages.Package, d *ast.FuncDecl, fileID string) string {
	if d.Name == nil {
		return ""
	}
	obj := p.TypesInfo.Defs[d.Name]
	fn, _ := obj.(*types.Func)

	pos := b.relPos(p.Fset, d.Name.Pos())
	kind := mgraph.NodeFunction
	parentID := fileID

	receiverName := ""
	if d.Recv != nil && len(d.Recv.List) > 0 {
		kind = mgraph.NodeMethod
		recvType := exprType(p, d.Recv.List[0].Type)
		// strip pointer
		if pt, ok := recvType.(*types.Pointer); ok {
			recvType = pt.Elem()
		}
		if named, ok := recvType.(*types.Named); ok {
			if rid := b.typeNodeID(named.Obj()); rid != "" {
				parentID = rid // method belongs to the type, not the file
				receiverName = named.Obj().Name()
			}
		}
	}

	qname := ""
	if fn != nil && fn.Pkg() != nil {
		qname = fn.FullName()
	} else {
		qname = d.Name.Name
	}
	id := b.reserveID(kind, pos, d.Name.Name)
	attrs := map[string]any{"foreign": false}
	if receiverName != "" {
		attrs["receiver"] = receiverName
	}
	b.g.AddNode(mgraph.Node{
		ID:    id,
		Kind:  kind,
		Name:  d.Name.Name,
		QName: qname,
		Pos:   pos,
		Attrs: attrs,
	})
	_, _ = b.g.AddEdge(mgraph.Edge{From: parentID, To: id, Kind: mgraph.EdgeContains})
	// Always also link the file to the function/method for navigation.
	if parentID != fileID {
		_, _ = b.g.AddEdge(mgraph.Edge{From: fileID, To: id, Kind: mgraph.EdgeContains})
	}
	if fn != nil {
		b.funcNodes[fn] = id
	}

	// Returns edges: link function to the named types in its result list.
	if fn != nil {
		if sig, ok := fn.Type().(*types.Signature); ok && sig.Results() != nil {
			for i := 0; i < sig.Results().Len(); i++ {
				rt := sig.Results().At(i).Type()
				if tid := b.resolveType(rt); tid != "" {
					_, _ = b.g.AddEdge(mgraph.Edge{From: id, To: tid, Kind: mgraph.EdgeReturns})
				}
			}
		}
	}
	return id
}

// walkFuncDeclBody walks the body for control-flow primitives, calls,
// callback references, and explicit body-level type usage.
func (b *builder) walkFuncDeclBody(p *packages.Package, d *ast.FuncDecl, id string) {
	// Walk the body for control-flow primitives + calls.
	if d.Body == nil || id == "" {
		return
	}
	w := &stmtWalker{b: b, p: p, ownerFunc: id}
	w.walkBlock(d.Body, id)
}
