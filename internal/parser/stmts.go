package parser

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/packages"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// stmtWalker walks a function body and emits control-flow primitive
// nodes plus Calls / CallsFrom edges. ownerFunc is the function/method
// node ID; parentID rotates as we descend into nested primitives.
type stmtWalker struct {
	b         *builder
	p         *packages.Package
	ownerFunc string
}

// walkBlock walks the statements in a block, attaching them to parentID
// via Contains edges (statements that produce a node).
func (w *stmtWalker) walkBlock(blk *ast.BlockStmt, parentID string) {
	if blk == nil {
		return
	}
	for _, s := range blk.List {
		w.walkStmt(s, parentID)
	}
}

func (w *stmtWalker) walkStmt(s ast.Stmt, parentID string) {
	switch n := s.(type) {
	case *ast.ForStmt:
		id := w.emitPrimitive(mgraph.NodeLoop, n.For, parentID, map[string]any{"loopKind": "for"})
		w.walkExpr(n.Cond, id)
		w.walkStmt(n.Init, id)
		w.walkStmt(n.Post, id)
		w.walkBlock(n.Body, id)
	case *ast.RangeStmt:
		id := w.emitPrimitive(mgraph.NodeLoop, n.For, parentID, map[string]any{"loopKind": "range"})
		w.walkExpr(n.X, id)
		w.walkBlock(n.Body, id)
	case *ast.IfStmt:
		id := w.emitPrimitive(mgraph.NodeBranch, n.If, parentID, map[string]any{"branchKind": "if"})
		w.walkStmt(n.Init, id)
		w.walkExpr(n.Cond, id)
		w.walkBlock(n.Body, id)
		w.walkStmt(n.Else, id)
	case *ast.SwitchStmt:
		id := w.emitPrimitive(mgraph.NodeBranch, n.Switch, parentID, map[string]any{"branchKind": "switch"})
		w.walkStmt(n.Init, id)
		w.walkExpr(n.Tag, id)
		w.walkBlock(n.Body, id)
	case *ast.TypeSwitchStmt:
		id := w.emitPrimitive(mgraph.NodeBranch, n.Switch, parentID, map[string]any{"branchKind": "typeSwitch"})
		w.walkStmt(n.Init, id)
		w.walkStmt(n.Assign, id)
		w.walkBlock(n.Body, id)
	case *ast.SelectStmt:
		id := w.emitPrimitive(mgraph.NodeBranch, n.Select, parentID, map[string]any{"branchKind": "select"})
		w.walkBlock(n.Body, id)
	case *ast.CaseClause:
		// Case clauses live inside a Branch parent; treat their bodies
		// as belonging to that Branch (no separate node).
		for _, e := range n.List {
			w.walkExpr(e, parentID)
		}
		for _, st := range n.Body {
			w.walkStmt(st, parentID)
		}
	case *ast.CommClause:
		// Communication clause inside a select.
		w.walkStmt(n.Comm, parentID)
		for _, st := range n.Body {
			w.walkStmt(st, parentID)
		}
	case *ast.GoStmt:
		id := w.emitPrimitive(mgraph.NodeGoroutine, n.Go, parentID, nil)
		// The call expression itself becomes a CallsFrom edge.
		w.walkCallExpr(n.Call, id, true)
	case *ast.DeferStmt:
		id := w.emitPrimitive(mgraph.NodeDefer, n.Defer, parentID, nil)
		w.walkCallExpr(n.Call, id, true)
	case *ast.SendStmt:
		w.emitChannelOp(n.Arrow, parentID, "send")
		w.walkExpr(n.Chan, parentID)
		w.walkExpr(n.Value, parentID)
	case *ast.IncDecStmt:
		w.walkExpr(n.X, parentID)
	case *ast.AssignStmt:
		for _, e := range n.Lhs {
			w.walkExpr(e, parentID)
		}
		for _, e := range n.Rhs {
			w.walkExpr(e, parentID)
		}
	case *ast.ExprStmt:
		w.walkExpr(n.X, parentID)
	case *ast.ReturnStmt:
		for _, e := range n.Results {
			w.walkExpr(e, parentID)
		}
	case *ast.BlockStmt:
		w.walkBlock(n, parentID)
	case *ast.LabeledStmt:
		w.walkStmt(n.Stmt, parentID)
	case *ast.DeclStmt:
		w.walkDecl(n.Decl, parentID)
	}
}

func (w *stmtWalker) walkDecl(d ast.Decl, parentID string) {
	gd, ok := d.(*ast.GenDecl)
	if !ok {
		return
	}
	for _, spec := range gd.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		if vs.Type != nil {
			w.emitUsesType(exprType(w.p, vs.Type))
		}
		for _, value := range vs.Values {
			w.walkExpr(value, parentID)
		}
	}
}

// emitPrimitive creates a control-flow primitive node and a Contains
// edge from parent.
func (w *stmtWalker) emitPrimitive(kind mgraph.NodeKind, p token.Pos, parentID string, attrs map[string]any) string {
	pos := w.b.relPos(w.p.Fset, p)
	id := w.b.reserveID(kind, pos, "")
	w.b.g.AddNode(mgraph.Node{
		ID:    id,
		Kind:  kind,
		Pos:   pos,
		Attrs: attrs,
	})
	_, _ = w.b.g.AddEdge(mgraph.Edge{From: parentID, To: id, Kind: mgraph.EdgeContains})
	return id
}

// emitChannelOp emits a ChannelOp node for a send expression. Receive
// expressions are handled in walkExpr via UnaryExpr ARROW.
func (w *stmtWalker) emitChannelOp(p token.Pos, parentID, dir string) string {
	pos := w.b.relPos(w.p.Fset, p)
	id := w.b.reserveID(mgraph.NodeChannelOp, pos, "")
	w.b.g.AddNode(mgraph.Node{
		ID:    id,
		Kind:  mgraph.NodeChannelOp,
		Pos:   pos,
		Attrs: map[string]any{"direction": dir},
	})
	_, _ = w.b.g.AddEdge(mgraph.Edge{From: parentID, To: id, Kind: mgraph.EdgeContains})
	return id
}

// walkExpr recurses into an expression and emits Calls / ChannelOp /
// SyncPrim nodes as relevant. fromPrim is the node we credit a
// CallsFrom edge to — the nearest enclosing control-flow primitive,
// or the owning function if we are at function scope.
func (w *stmtWalker) walkExpr(e ast.Expr, fromPrim string) {
	if e == nil {
		return
	}
	switch n := e.(type) {
	case *ast.CallExpr:
		w.emitCallTypeUse(n)
		w.walkCallExpr(n, fromPrim, false)
		// Arguments may themselves contain calls.
		for _, arg := range n.Args {
			w.walkExpr(arg, fromPrim)
		}
		w.walkCallFun(n.Fun, fromPrim)
	case *ast.Ident:
		w.emitReference(n)
	case *ast.UnaryExpr:
		if n.Op == token.ARROW {
			pos := w.b.relPos(w.p.Fset, n.OpPos)
			id := w.b.reserveID(mgraph.NodeChannelOp, pos, "")
			w.b.g.AddNode(mgraph.Node{
				ID:    id,
				Kind:  mgraph.NodeChannelOp,
				Pos:   pos,
				Attrs: map[string]any{"direction": "recv"},
			})
			_, _ = w.b.g.AddEdge(mgraph.Edge{From: fromPrim, To: id, Kind: mgraph.EdgeContains})
		}
		w.walkExpr(n.X, fromPrim)
	case *ast.BinaryExpr:
		w.walkExpr(n.X, fromPrim)
		w.walkExpr(n.Y, fromPrim)
	case *ast.ParenExpr:
		w.walkExpr(n.X, fromPrim)
	case *ast.SelectorExpr:
		w.emitReference(n)
		w.walkExpr(n.X, fromPrim)
	case *ast.IndexExpr:
		w.walkExpr(n.X, fromPrim)
		w.walkExpr(n.Index, fromPrim)
	case *ast.SliceExpr:
		w.walkExpr(n.X, fromPrim)
		w.walkExpr(n.Low, fromPrim)
		w.walkExpr(n.High, fromPrim)
		w.walkExpr(n.Max, fromPrim)
	case *ast.TypeAssertExpr:
		w.emitUsesType(exprType(w.p, n.Type))
		w.walkExpr(n.X, fromPrim)
	case *ast.CompositeLit:
		w.emitUsesType(exprType(w.p, n.Type))
		for _, el := range n.Elts {
			w.walkExpr(el, fromPrim)
		}
	case *ast.KeyValueExpr:
		w.walkExpr(n.Key, fromPrim)
		w.walkExpr(n.Value, fromPrim)
	case *ast.StarExpr:
		w.walkExpr(n.X, fromPrim)
	}
}

func (w *stmtWalker) walkCallFun(fn ast.Expr, fromPrim string) {
	switch n := fn.(type) {
	case *ast.Ident:
		return
	case *ast.SelectorExpr:
		w.walkExpr(n.X, fromPrim)
	case *ast.IndexExpr:
		w.walkCallFun(n.X, fromPrim)
		for _, idx := range indexExprIndices(n) {
			w.walkExpr(idx, fromPrim)
		}
	default:
		w.walkExpr(n, fromPrim)
	}
}

func indexExprIndices(e *ast.IndexExpr) []ast.Expr {
	if e == nil {
		return nil
	}
	return []ast.Expr{e.Index}
}

func (w *stmtWalker) emitCallTypeUse(call *ast.CallExpr) {
	if call == nil || w.p.TypesInfo == nil {
		return
	}
	if tv, ok := w.p.TypesInfo.Types[call.Fun]; ok && tv.IsType() {
		w.emitUsesType(tv.Type)
	}
}

func (w *stmtWalker) emitUsesType(t types.Type) {
	if tid := w.b.resolveLoadedType(t); tid != "" {
		_, _ = w.b.g.AddEdge(mgraph.Edge{From: w.ownerFunc, To: tid, Kind: mgraph.EdgeUsesType})
	}
}

func (w *stmtWalker) emitReference(e ast.Expr) {
	fn := w.referencedFunc(e)
	if fn == nil {
		return
	}
	refID := w.b.funcNodes[fn]
	if refID == "" {
		refID = w.b.foreignFunc(fn)
	}
	if refID == "" || refID == w.ownerFunc {
		return
	}
	_, _ = w.b.g.AddEdge(mgraph.Edge{From: w.ownerFunc, To: refID, Kind: mgraph.EdgeReferences})
}

func (w *stmtWalker) referencedFunc(e ast.Expr) *types.Func {
	if w.p.TypesInfo == nil {
		return nil
	}
	switch n := e.(type) {
	case *ast.Ident:
		return funcObject(w.p.TypesInfo.Uses[n], w.p.TypesInfo.Defs[n])
	case *ast.SelectorExpr:
		if sel := w.p.TypesInfo.Selections[n]; sel != nil {
			if fn, ok := sel.Obj().(*types.Func); ok {
				return fn
			}
		}
		return funcObject(w.p.TypesInfo.Uses[n.Sel], w.p.TypesInfo.Defs[n.Sel])
	}
	return nil
}

func funcObject(objs ...types.Object) *types.Func {
	for _, obj := range objs {
		if fn, ok := obj.(*types.Func); ok {
			return fn
		}
	}
	return nil
}

// walkCallExpr resolves the callee and emits Calls/CallsFrom edges.
// alwaysCallsFrom forces a CallsFrom edge even if fromPrim equals the
// enclosing function (used by GoStmt and DeferStmt where the primitive
// *is* the immediate parent of the call).
func (w *stmtWalker) walkCallExpr(call *ast.CallExpr, fromPrim string, alwaysCallsFrom bool) {
	if call == nil {
		return
	}
	// Detect close(ch) — channel op. Done first so the builtin case
	// doesn't get short-circuited by resolveCallee returning "" for
	// builtins.
	if w.isCloseBuiltin(call) {
		pos := w.b.relPos(w.p.Fset, call.Lparen)
		id := w.b.reserveID(mgraph.NodeChannelOp, pos, "")
		w.b.g.AddNode(mgraph.Node{
			ID:   id,
			Kind: mgraph.NodeChannelOp,
			Pos:  pos,
			Attrs: map[string]any{
				"direction": "close",
			},
		})
		_, _ = w.b.g.AddEdge(mgraph.Edge{From: fromPrim, To: id, Kind: mgraph.EdgeContains})
		return
	}
	calleeID := w.resolveCallee(call)
	if calleeID == "" {
		return
	}
	// Always link the enclosing function to the callee.
	_, _ = w.b.g.AddEdge(mgraph.Edge{From: w.ownerFunc, To: calleeID, Kind: mgraph.EdgeCalls})
	if alwaysCallsFrom || fromPrim != w.ownerFunc {
		_, _ = w.b.g.AddEdge(mgraph.Edge{From: fromPrim, To: calleeID, Kind: mgraph.EdgeCallsFrom})
	}
	// Detect SyncPrim by callee receiver type.
	if w.isSyncPrim(call) {
		pos := w.b.relPos(w.p.Fset, call.Lparen)
		id := w.b.reserveID(mgraph.NodeSyncPrim, pos, "")
		w.b.g.AddNode(mgraph.Node{
			ID:   id,
			Kind: mgraph.NodeSyncPrim,
			Pos:  pos,
			Attrs: map[string]any{
				"target": w.calleeName(call),
			},
		})
		_, _ = w.b.g.AddEdge(mgraph.Edge{From: fromPrim, To: id, Kind: mgraph.EdgeContains})
	}
}

// resolveCallee returns the node ID for the function/method invoked by
// call, or "" if it cannot be resolved (function literal call, etc.).
func (w *stmtWalker) resolveCallee(call *ast.CallExpr) string {
	if w.p.TypesInfo == nil {
		return ""
	}
	var ident *ast.Ident
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		ident = fn
	case *ast.SelectorExpr:
		ident = fn.Sel
	}
	if ident == nil {
		return ""
	}
	obj := w.p.TypesInfo.Uses[ident]
	if obj == nil {
		obj = w.p.TypesInfo.Defs[ident]
	}
	fn, ok := obj.(*types.Func)
	if !ok {
		return ""
	}
	if id, ok := w.b.funcNodes[fn]; ok {
		return id
	}
	return w.b.foreignFunc(fn)
}

func (w *stmtWalker) calleeName(call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		if fn.Sel != nil {
			return fn.Sel.Name
		}
	}
	return ""
}

// isSyncPrim heuristically identifies sync.* and atomic.* operations.
// Stage 1 doesn't need to be exhaustive — these names cover the common
// cases (Mutex.Lock/Unlock, RWMutex.*, WaitGroup.*, Once.Do, atomic.*).
func (w *stmtWalker) isSyncPrim(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if w.p.TypesInfo == nil {
		return false
	}
	obj := w.p.TypesInfo.Uses[sel.Sel]
	fn, ok := obj.(*types.Func)
	if !ok {
		return false
	}
	if fn.Pkg() == nil {
		return false
	}
	pkgPath := fn.Pkg().Path()
	if pkgPath == "sync/atomic" {
		return true
	}
	if pkgPath != "sync" {
		return false
	}
	// Receiver-bearing sync primitives: Mutex, RWMutex, WaitGroup, Once.
	sig, _ := fn.Type().(*types.Signature)
	if sig == nil || sig.Recv() == nil {
		return false
	}
	switch fn.Name() {
	case "Lock", "Unlock", "RLock", "RUnlock", "TryLock", "TryRLock",
		"Add", "Done", "Wait", "Do":
		return true
	}
	return false
}

func (w *stmtWalker) isCloseBuiltin(call *ast.CallExpr) bool {
	ident, ok := call.Fun.(*ast.Ident)
	if !ok {
		return false
	}
	if ident.Name != "close" {
		return false
	}
	if w.p.TypesInfo == nil {
		return true
	}
	obj := w.p.TypesInfo.Uses[ident]
	if obj == nil {
		return false
	}
	if _, ok := obj.(*types.Builtin); ok {
		return true
	}
	return false
}
