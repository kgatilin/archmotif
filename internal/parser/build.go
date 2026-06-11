// Package parser builds an archmotif typed graph from a Go source path.
//
// The parser drives golang.org/x/tools/go/packages with NeedTypes and
// NeedSyntax so we can resolve cross-package types (Implements,
// CallsFrom) without re-implementing module resolution. See
// docs/decisions/004-parser-strategy.md.
package parser

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// Options controls graph construction.
type Options struct {
	// Dir is the working directory for `go/packages`. Patterns are
	// resolved relative to this directory.
	Dir string
	// Patterns are passed to `go/packages.Load`. Defaults to "./...".
	Patterns []string
	// Tests, when true, also loads `_test.go` files.
	Tests bool
	// ExcludeDirs skips matching source directories before package loading.
	// Entries without slashes match any directory segment, for example `tests`.
	ExcludeDirs []string
}

// Result is the output of Build.
type Result struct {
	Graph      *mgraph.Graph
	LoadErrors []string
	// Packages is the typed package set used to build Graph. Callers that
	// need type objects should reuse it instead of running packages.Load again.
	Packages []*packages.Package
	// ModuleRoot is the directory we treat as the source root for
	// computing relative paths in node IDs.
	ModuleRoot string
}

// Build loads the Go packages described by opts and returns the typed
// graph. Errors during package loading are surfaced via Result.LoadErrors
// rather than aborting — partially-broken codebases still get a usable
// graph (per ADR-004).
func Build(opts Options) (*Result, error) {
	if opts.Dir == "" {
		opts.Dir = "."
	}
	if len(opts.Patterns) == 0 {
		opts.Patterns = []string{"./..."}
	}
	patterns, err := packagePatterns(opts.Dir, opts.Patterns, opts.ExcludeDirs, opts.Tests)
	if err != nil {
		return nil, err
	}
	cfg := &packages.Config{
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedCompiledGoFiles |
			packages.NeedImports |
			packages.NeedDeps |
			packages.NeedTypes |
			packages.NeedTypesInfo |
			packages.NeedSyntax |
			packages.NeedTypesSizes |
			packages.NeedModule,
		Dir:   opts.Dir,
		Tests: opts.Tests,
	}
	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, fmt.Errorf("packages.Load: %w", err)
	}

	moduleRoot := opts.Dir
	if abs, err := filepath.Abs(opts.Dir); err == nil {
		moduleRoot = abs
	}
	// Prefer the loaded module's directory if available; gives stable
	// relative paths even when patterns don't sit at module root.
	for _, p := range pkgs {
		if p.Module != nil && p.Module.Dir != "" {
			moduleRoot = p.Module.Dir
			break
		}
	}

	b := newBuilder(moduleRoot)
	// Sort packages by path for deterministic output.
	sort.SliceStable(pkgs, func(i, j int) bool { return pkgs[i].PkgPath < pkgs[j].PkgPath })

	loadErrs := make([]string, 0)
	for _, p := range pkgs {
		for _, e := range p.Errors {
			loadErrs = append(loadErrs, e.Error())
		}
	}

	// Pass 1: emit Package nodes for the loaded set, plus File and decl
	// shells. Keep a set of loaded package paths to distinguish foreign
	// from primary later.
	loaded := make(map[string]bool, len(pkgs))
	for _, p := range pkgs {
		if p.PkgPath == "" {
			continue
		}
		loaded[p.PkgPath] = true
	}
	b.loadedPkgs = loaded

	// Pass 1: emit Package, File, and top-level Type shells for every
	// loaded package. Type members are deliberately deferred until every
	// shell exists, so a field in package P that references a type in
	// package Q resolves to the loaded Type node even when Q is processed
	// later (for example alphabetical order).
	for _, p := range pkgs {
		if p.PkgPath == "" {
			continue
		}
		b.walkPackageDecls(p)
	}

	// Pass 2: fields, embedded types, and interface members. Now every
	// loaded type is in `b.typeNodes`, so DependsOn and Embeds edges
	// resolve to the real loaded node, not a foreign placeholder.
	for _, p := range pkgs {
		if p.PkgPath == "" {
			continue
		}
		b.walkPackageTypeMembers(p)
	}

	// Pass 3: function/method decls + bodies.
	for _, p := range pkgs {
		if p.PkgPath == "" {
			continue
		}
		b.walkPackageFuncs(p)
	}

	// Pass 4: implements edges (need full type universe).
	b.emitImplements(pkgs)

	return &Result{
		Graph:      b.g,
		LoadErrors: loadErrs,
		Packages:   pkgs,
		ModuleRoot: moduleRoot,
	}, nil
}

func packagePatterns(dir string, patterns, excludeDirs []string, tests bool) ([]string, error) {
	excludes := normalizeExcludeDirs(excludeDirs)
	if len(excludes) == 0 {
		return patterns, nil
	}
	out := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		expanded, ok, err := expandRecursivePattern(dir, pattern, excludes, tests)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, expanded...)
			continue
		}
		if patternIsExcluded(pattern, excludes) {
			continue
		}
		out = append(out, pattern)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("all package patterns were excluded")
	}
	sort.Strings(out)
	return compactStrings(out), nil
}

func expandRecursivePattern(dir, pattern string, excludes []string, tests bool) ([]string, bool, error) {
	clean := filepath.ToSlash(strings.TrimSpace(pattern))
	if !strings.HasSuffix(clean, "/...") {
		return nil, false, nil
	}
	base := strings.TrimSuffix(clean, "/...")
	if base == "" {
		base = "."
	}
	if base != "." && !strings.HasPrefix(base, "./") && !strings.HasPrefix(base, "../") {
		return nil, false, nil
	}
	root := filepath.Join(dir, filepath.FromSlash(base))
	out := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			rel = ""
		}
		if rel != "" && shouldSkipPackageDir(d.Name(), rel, excludes) {
			return filepath.SkipDir
		}
		ok, err := hasGoFiles(path, tests)
		if err != nil {
			return err
		}
		if ok {
			out = append(out, packagePatternForRel(rel))
		}
		return nil
	})
	if err != nil {
		return nil, true, err
	}
	if len(out) == 0 {
		return nil, true, fmt.Errorf("package pattern %q matched no packages after excludes", pattern)
	}
	sort.Strings(out)
	return compactStrings(out), true, nil
}

func normalizeExcludeDirs(raw []string) []string {
	out := make([]string, 0, len(raw))
	for _, value := range raw {
		for _, part := range strings.Split(value, ",") {
			part = filepath.ToSlash(strings.TrimSpace(part))
			part = strings.TrimPrefix(part, "./")
			part = strings.TrimSuffix(part, "/...")
			part = strings.Trim(part, "/")
			if part != "" {
				out = append(out, part)
			}
		}
	}
	sort.Strings(out)
	return compactStrings(out)
}

func shouldSkipPackageDir(name, rel string, excludes []string) bool {
	if name == "vendor" || name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
		return true
	}
	return relMatchesExclude(rel, excludes)
}

func patternIsExcluded(pattern string, excludes []string) bool {
	clean := filepath.ToSlash(strings.TrimSpace(pattern))
	clean = strings.TrimPrefix(clean, "./")
	clean = strings.TrimSuffix(clean, "/...")
	clean = strings.Trim(clean, "/")
	return relMatchesExclude(clean, excludes)
}

func relMatchesExclude(rel string, excludes []string) bool {
	rel = filepath.ToSlash(strings.Trim(rel, "/"))
	if rel == "" {
		return false
	}
	for _, ex := range excludes {
		if strings.Contains(ex, "/") {
			if rel == ex || strings.HasPrefix(rel, ex+"/") {
				return true
			}
			continue
		}
		for _, segment := range strings.Split(rel, "/") {
			if segment == ex {
				return true
			}
		}
	}
	return false
}

func hasGoFiles(dir string, tests bool) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		if !tests && strings.HasSuffix(name, "_test.go") {
			continue
		}
		return true, nil
	}
	return false, nil
}

func packagePatternForRel(rel string) string {
	if rel == "" {
		return "."
	}
	return "./" + rel
}

func compactStrings(values []string) []string {
	if len(values) == 0 {
		return values
	}
	out := values[:0]
	var prev string
	for i, value := range values {
		if i > 0 && value == prev {
			continue
		}
		out = append(out, value)
		prev = value
	}
	return out
}

// builder threads parser state across the AST walk.
type builder struct {
	g          *mgraph.Graph
	moduleRoot string
	loadedPkgs map[string]bool

	// Foreign placeholders deduped by import path (Package node).
	foreignPkg map[string]string // import path -> node ID
	// Foreign symbol placeholders deduped by qname.
	foreignSym map[string]string // qname -> node ID
	// Loaded function/method node IDs by *types.Func — used to wire calls.
	funcNodes map[*types.Func]string
	// Loaded type node IDs by *types.TypeName — used to wire receivers,
	// embeds, returns, and implements.
	typeNodes map[*types.TypeName]string
	// Loaded type node IDs by QName. packages.Load can surface equivalent
	// imported TypeName objects that are not pointer-identical to the
	// defining package's TypeName, so QName is the stable fallback.
	typeNodesByQName map[string]string
	// Ordinal counters for ID disambiguation per (path,line,col,kind,name).
	ordinal map[string]int
	// Per-package state shared between pass 1 (decls) and pass 2 (funcs).
	pkgState map[*packages.Package]*pkgWalkState
}

// pkgWalkState carries IDs assigned during pass 1 so pass 2 can wire
// FuncDecls under the same File / Package nodes without re-emitting
// them.
type pkgWalkState struct {
	pkgID   string
	fileIDs []string // parallel to p.Syntax
}

func newBuilder(moduleRoot string) *builder {
	return &builder{
		g:                mgraph.New(),
		moduleRoot:       moduleRoot,
		foreignPkg:       make(map[string]string),
		foreignSym:       make(map[string]string),
		funcNodes:        make(map[*types.Func]string),
		typeNodes:        make(map[*types.TypeName]string),
		typeNodesByQName: make(map[string]string),
		ordinal:          make(map[string]int),
		pkgState:         make(map[*packages.Package]*pkgWalkState),
	}
}

// reserveID returns a stable ID; if a node already exists at the same
// signature we bump the ordinal so the new one gets a fresh ID.
func (b *builder) reserveID(kind mgraph.NodeKind, pos mgraph.Position, name string) string {
	base := mgraph.MakeID(kind, pos, name, 0)
	if !b.g.HasNode(base) {
		return base
	}
	key := base
	for {
		b.ordinal[key]++
		candidate := mgraph.MakeID(kind, pos, name, b.ordinal[key])
		if !b.g.HasNode(candidate) {
			return candidate
		}
	}
}

func (b *builder) relPos(fset *token.FileSet, p token.Pos) mgraph.Position {
	if !p.IsValid() {
		return mgraph.Position{}
	}
	pos := fset.Position(p)
	rel := pos.Filename
	if abs, err := filepath.Abs(pos.Filename); err == nil {
		if r, err := filepath.Rel(b.moduleRoot, abs); err == nil {
			rel = r
		}
	}
	return mgraph.Position{File: filepath.ToSlash(rel), Line: pos.Line, Col: pos.Column}
}

func (b *builder) typeNodeID(tn *types.TypeName) string {
	if tn == nil {
		return ""
	}
	if id, ok := b.typeNodes[tn]; ok {
		return id
	}
	if tn.Pkg() == nil {
		return ""
	}
	return b.typeNodesByQName[tn.Pkg().Path()+"."+tn.Name()]
}

// addPackageNode emits the Package node for a loaded package.
func (b *builder) addPackageNode(p *packages.Package) string {
	id := mgraph.MakeID(mgraph.NodePackage, mgraph.Position{}, p.PkgPath, 0)
	b.g.AddNode(mgraph.Node{
		ID:    id,
		Kind:  mgraph.NodePackage,
		Name:  p.Name,
		QName: p.PkgPath,
		Attrs: map[string]any{"foreign": false},
	})
	return id
}

// foreignPackage returns (and creates if needed) a Package node for an
// import path the user did not load.
func (b *builder) foreignPackage(importPath string) string {
	if id, ok := b.foreignPkg[importPath]; ok {
		return id
	}
	id := mgraph.MakeID(mgraph.NodePackage, mgraph.Position{}, importPath, 0)
	name := importPath
	if i := strings.LastIndex(importPath, "/"); i >= 0 {
		name = importPath[i+1:]
	}
	b.g.AddNode(mgraph.Node{
		ID:    id,
		Kind:  mgraph.NodePackage,
		Name:  name,
		QName: importPath,
		Attrs: map[string]any{"foreign": true},
	})
	b.foreignPkg[importPath] = id
	return id
}

// foreignType returns a placeholder Type node for a type defined in a
// non-loaded package. Reused across calls.
func (b *builder) foreignType(tn *types.TypeName) string {
	if tn == nil || tn.Pkg() == nil {
		// Built-in type (error, int, etc). Skip.
		return ""
	}
	qname := tn.Pkg().Path() + "." + tn.Name()
	if id := b.typeNodeID(tn); id != "" {
		return id
	}
	if id, ok := b.foreignSym[qname]; ok {
		return id
	}
	pkgID := b.foreignPackage(tn.Pkg().Path())
	id := mgraph.MakeID(mgraph.NodeType, mgraph.Position{}, qname, 0)
	kind := "unknown"
	if _, ok := tn.Type().Underlying().(*types.Interface); ok {
		kind = "interface"
	} else if _, ok := tn.Type().Underlying().(*types.Struct); ok {
		kind = "struct"
	}
	b.g.AddNode(mgraph.Node{
		ID:    id,
		Kind:  mgraph.NodeType,
		Name:  tn.Name(),
		QName: qname,
		Attrs: map[string]any{"foreign": true, "typeKind": kind},
	})
	_, _ = b.g.AddEdge(mgraph.Edge{From: pkgID, To: id, Kind: mgraph.EdgeContains})
	b.foreignSym[qname] = id
	return id
}

// foreignFunc returns a placeholder Function (or Method) node for a
// non-loaded function/method.
func (b *builder) foreignFunc(fn *types.Func) string {
	if fn == nil {
		return ""
	}
	pkg := fn.Pkg()
	if pkg == nil {
		// Builtin (panic, append, etc.) — we don't model these.
		return ""
	}
	// fn.FullName() already includes the package path (and receiver
	// for methods), so it serves both as the dedup key and as the
	// QName attribute on the node.
	qname := fn.FullName()
	if id, ok := b.foreignSym[qname]; ok {
		return id
	}
	pkgID := b.foreignPackage(pkg.Path())
	kind := mgraph.NodeFunction
	sig, _ := fn.Type().(*types.Signature)
	if sig != nil && sig.Recv() != nil {
		kind = mgraph.NodeMethod
	}
	id := mgraph.MakeID(kind, mgraph.Position{}, qname, 0)
	b.g.AddNode(mgraph.Node{
		ID:    id,
		Kind:  kind,
		Name:  fn.Name(),
		QName: qname,
		Attrs: map[string]any{"foreign": true},
	})
	_, _ = b.g.AddEdge(mgraph.Edge{From: pkgID, To: id, Kind: mgraph.EdgeContains})
	b.foreignSym[qname] = id
	return id
}

// resolveType returns the node ID for a Go type expressed by t. Loaded
// named types resolve to their existing node; foreign named types get a
// placeholder. Anonymous types (slices, maps) are not represented.
func (b *builder) resolveType(t types.Type) string {
	if t == nil {
		return ""
	}
	switch tt := t.(type) {
	case *types.Named:
		if tn := tt.Obj(); tn != nil {
			if id := b.typeNodeID(tn); id != "" {
				return id
			}
			return b.foreignType(tn)
		}
	case *types.Pointer:
		return b.resolveType(tt.Elem())
	case *types.Slice:
		return b.resolveType(tt.Elem())
	case *types.Array:
		return b.resolveType(tt.Elem())
	case *types.Chan:
		return b.resolveType(tt.Elem())
	case *types.Interface:
		// Anonymous interface — skip for v1.
		return ""
	}
	return ""
}

// resolveLoadedType returns the node ID for a named type that belongs to the
// loaded package set. It deliberately does not create foreign placeholders:
// body-level type-use edges would otherwise add a lot of standard-library and
// primitive adapter noise to architecture views.
func (b *builder) resolveLoadedType(t types.Type) string {
	if t == nil {
		return ""
	}
	switch tt := t.(type) {
	case *types.Named:
		if tn := tt.Obj(); tn != nil {
			return b.typeNodeID(tn)
		}
	case *types.Pointer:
		return b.resolveLoadedType(tt.Elem())
	case *types.Slice:
		return b.resolveLoadedType(tt.Elem())
	case *types.Array:
		return b.resolveLoadedType(tt.Elem())
	case *types.Chan:
		return b.resolveLoadedType(tt.Elem())
	}
	return ""
}

// walkPackageDecls processes pass 1 of a loaded package: emits the
// Package node, its DependsOn edges, the File nodes, and every top-level
// Type shell. Type members and FuncDecls are skipped here and handled in
// later passes after every package's types are known.
func (b *builder) walkPackageDecls(p *packages.Package) {
	pkgID := b.addPackageNode(p)
	state := &pkgWalkState{pkgID: pkgID}
	b.pkgState[p] = state

	// Emit DependsOn edges from this package to imports.
	importPaths := make([]string, 0, len(p.Imports))
	for ip := range p.Imports {
		importPaths = append(importPaths, ip)
	}
	sort.Strings(importPaths)
	for _, ip := range importPaths {
		var depID string
		if b.loadedPkgs[ip] {
			depID = mgraph.MakeID(mgraph.NodePackage, mgraph.Position{}, ip, 0)
			// May not be added yet; ensure node exists.
			if !b.g.HasNode(depID) {
				dep := p.Imports[ip]
				name := ip
				if dep != nil && dep.Name != "" {
					name = dep.Name
				}
				b.g.AddNode(mgraph.Node{
					ID:    depID,
					Kind:  mgraph.NodePackage,
					Name:  name,
					QName: ip,
					Attrs: map[string]any{"foreign": false},
				})
			}
		} else {
			depID = b.foreignPackage(ip)
		}
		_, _ = b.g.AddEdge(mgraph.Edge{From: pkgID, To: depID, Kind: mgraph.EdgeDependsOn})
	}

	// Files come back in a stable order (matches Syntax slice order).
	state.fileIDs = make([]string, len(p.Syntax))
	for i, f := range p.Syntax {
		if f == nil {
			continue
		}
		fileName := fileNameForSyntax(p, i, f)
		fileID := b.emitFileNode(fileName, pkgID)
		state.fileIDs[i] = fileID
		for _, decl := range f.Decls {
			if gd, ok := decl.(*ast.GenDecl); ok {
				b.walkGenDeclTypes(p, gd, fileID, pkgID)
			}
		}
	}
}

// walkPackageTypeMembers processes pass 2: walks type fields, embedded
// types, and interface members, attaching them to the Type nodes assigned
// in pass 1.
func (b *builder) walkPackageTypeMembers(p *packages.Package) {
	state := b.pkgState[p]
	if state == nil {
		return
	}
	for i, f := range p.Syntax {
		if f == nil || i >= len(state.fileIDs) {
			continue
		}
		fileID := state.fileIDs[i]
		if fileID == "" {
			continue
		}
		for _, decl := range f.Decls {
			if gd, ok := decl.(*ast.GenDecl); ok {
				b.walkGenDeclMembers(p, gd)
			}
		}
	}
}

// walkPackageFuncs processes pass 3: walks every FuncDecl in the
// package, attaching it to the File node assigned in pass 1.
func (b *builder) walkPackageFuncs(p *packages.Package) {
	state := b.pkgState[p]
	if state == nil {
		return
	}
	funcs := []struct {
		decl *ast.FuncDecl
		id   string
	}{}
	for i, f := range p.Syntax {
		if f == nil || i >= len(state.fileIDs) {
			continue
		}
		fileID := state.fileIDs[i]
		if fileID == "" {
			continue
		}
		for _, decl := range f.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok {
				id := b.emitFuncDecl(p, fd, fileID)
				if id != "" {
					funcs = append(funcs, struct {
						decl *ast.FuncDecl
						id   string
					}{decl: fd, id: id})
				}
			}
		}
	}
	for _, fn := range funcs {
		b.walkFuncDeclBody(p, fn.decl, fn.id)
	}
}

// emitFileNode emits a File node for filename and a Contains edge from
// pkgID. Returns the new node ID.
func (b *builder) emitFileNode(filename, pkgID string) string {
	pos := mgraph.Position{}
	if filename != "" {
		rel := filename
		if abs, err := filepath.Abs(filename); err == nil {
			if r, err := filepath.Rel(b.moduleRoot, abs); err == nil {
				rel = r
			}
		}
		pos = mgraph.Position{File: filepath.ToSlash(rel), Line: 1, Col: 1}
	}
	name := filepath.Base(filename)
	id := b.reserveID(mgraph.NodeFile, pos, name)
	b.g.AddNode(mgraph.Node{
		ID:   id,
		Kind: mgraph.NodeFile,
		Name: name,
		Pos:  pos,
	})
	_, _ = b.g.AddEdge(mgraph.Edge{From: pkgID, To: id, Kind: mgraph.EdgeContains})
	return id
}

// fileNameForSyntax returns the on-disk filename for the i-th file in
// p.Syntax, falling back through CompiledGoFiles, GoFiles, and the
// AST position.
func fileNameForSyntax(p *packages.Package, i int, f *ast.File) string {
	if i < len(p.CompiledGoFiles) {
		return p.CompiledGoFiles[i]
	}
	if i < len(p.GoFiles) {
		return p.GoFiles[i]
	}
	return p.Fset.Position(f.Pos()).Filename
}
