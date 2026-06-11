// Package targetcontract turns optimizer target subgraphs into a project-level
// target architecture contract, can scaffold the declared surface, and can
// verify an actual code graph against the contract.
package targetcontract

import (
	"context"
	"encoding/json"
	"fmt"
	"go/format"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
	"github.com/kgatilin/archmotif/internal/parser"
	"github.com/kgatilin/archmotif/internal/proposal"
)

// Version is the current target-contract schema version emitted by this package.
const Version = 1

// OptimizeEnvelope is the wire format produced by `archmotif optimize` and
// consumed by `archmotif target build`: a versioned list of optimize contracts.
type OptimizeEnvelope struct {
	Version   int                `json:"version"`
	Mode      string             `json:"mode"`
	Contracts []OptimizeContract `json:"contracts"`
}

// OptimizeContract is one optimize-stage contract — a candidate refactor with
// its proposal target subgraph and human-facing metadata.
type OptimizeContract struct {
	ID          string            `json:"id"`
	Kind        string            `json:"kind"`
	Rule        string            `json:"rule"`
	ProposalID  string            `json:"proposalId"`
	Description string            `json:"description"`
	Objective   OptimizeObjective `json:"objective"`
	Target      OptimizeTarget    `json:"target"`
	Proposal    *proposal.Proposal `json:"proposal"`
}

// OptimizeObjective captures the package an optimize contract targets.
type OptimizeObjective struct {
	Target string `json:"target"`
}

// OptimizeTarget wraps the proposal target subgraph carried by an optimize contract.
type OptimizeTarget struct {
	Subgraph proposal.TargetSubgraph `json:"subgraph"`
}

// Contract is the project-level target architecture contract produced from an
// optimize envelope. It captures the packages, files, public surface, and
// expected edges the target architecture must satisfy.
type Contract struct {
	Version          int                    `json:"version"`
	ID               string                 `json:"id"`
	SourceContractID string                 `json:"sourceContractId,omitempty"`
	SourceProposalID string                 `json:"sourceProposalId,omitempty"`
	Kind             string                 `json:"kind"`
	Rule             string                 `json:"rule,omitempty"`
	Description      string                 `json:"description,omitempty"`
	Source           SourceSpec             `json:"source"`
	Packages         []PackageSpec          `json:"packages"`
	Files            []FileSpec             `json:"files"`
	PublicInterfaces []InterfaceSpec        `json:"publicInterfaces,omitempty"`
	PublicTypes      []TypeSpec             `json:"publicTypes,omitempty"`
	PublicFunctions  []FunctionSpec         `json:"publicFunctions,omitempty"`
	ExpectedEdges    []EdgeSpec             `json:"expectedEdges,omitempty"`
	ForbiddenEdges   []EdgeSpec             `json:"forbiddenEdges,omitempty"`
	ScaffoldHints    []string               `json:"scaffoldHints,omitempty"`
	TargetSubgraph   proposal.TargetSubgraph `json:"targetSubgraph"`
}

// SourceSpec records the module and command package the contract was derived
// from so downstream tools can resolve import paths consistently.
type SourceSpec struct {
	ModulePath     string `json:"modulePath,omitempty"`
	CommandPackage string `json:"commandPackage,omitempty"`
}

// PackageSpec declares one package in the target architecture (kept or created).
type PackageSpec struct {
	Role        string `json:"role"`
	ImportPath  string `json:"importPath"`
	Dir         string `json:"dir"`
	Name        string `json:"name"`
	Action      string `json:"action"`
	Description string `json:"description,omitempty"`
}

// FileSpec declares one file in the target architecture and the package it
// belongs to.
type FileSpec struct {
	Path        string `json:"path"`
	PackageRole string `json:"packageRole"`
	PackageName string `json:"packageName"`
	Action      string `json:"action"`
	Purpose     string `json:"purpose,omitempty"`
}

// InterfaceSpec declares one public interface in the target architecture.
type InterfaceSpec struct {
	Name        string       `json:"name"`
	PackageRole string       `json:"packageRole"`
	PackagePath string       `json:"packagePath"`
	File        string       `json:"file"`
	Methods     []MethodSpec `json:"methods,omitempty"`
}

// MethodSpec declares one method on an interface in the target architecture.
type MethodSpec struct {
	Name      string `json:"name"`
	Signature string `json:"signature,omitempty"`
}

// TypeSpec declares one public concrete type in the target architecture.
type TypeSpec struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	PackageRole string `json:"packageRole"`
	PackagePath string `json:"packagePath"`
	File        string `json:"file"`
}

// FunctionSpec declares one public function in the target architecture.
type FunctionSpec struct {
	Name        string `json:"name"`
	PackageRole string `json:"packageRole"`
	PackagePath string `json:"packagePath"`
	File        string `json:"file"`
	Signature   string `json:"signature"`
}

// EdgeSpec declares an expected or forbidden edge between roles/packages in the
// target architecture.
type EdgeSpec struct {
	FromRole string `json:"fromRole"`
	ToRole   string `json:"toRole"`
	From     string `json:"from"`
	To       string `json:"to"`
	Kind     string `json:"kind"`
}

// ScaffoldResult records the files Scaffold created or skipped for a contract.
type ScaffoldResult struct {
	Created []string `json:"created,omitempty"`
	Skipped []string `json:"skipped,omitempty"`
}

// VerifyResult records the diff between a target contract and an actual code
// graph: missing packages, files, public surface, and expected edges.
type VerifyResult struct {
	TargetID                string     `json:"targetId"`
	Match                   bool       `json:"match"`
	MissingPackages         []string   `json:"missingPackages,omitempty"`
	MissingFiles            []string   `json:"missingFiles,omitempty"`
	MissingPublicInterfaces []string   `json:"missingPublicInterfaces,omitempty"`
	MissingPublicTypes      []string   `json:"missingPublicTypes,omitempty"`
	MissingPublicFunctions  []string   `json:"missingPublicFunctions,omitempty"`
	MissingExpectedEdges    []EdgeSpec `json:"missingExpectedEdges,omitempty"`
	LoadErrors              []string   `json:"loadErrors,omitempty"`
}

// LoadOptimizeEnvelopeFile decodes an optimize envelope JSON file from disk.
func LoadOptimizeEnvelopeFile(path string) (OptimizeEnvelope, error) {
	f, err := os.Open(path)
	if err != nil {
		return OptimizeEnvelope{}, err
	}
	defer func() { _ = f.Close() }()
	var env OptimizeEnvelope
	if err := json.NewDecoder(f).Decode(&env); err != nil {
		return OptimizeEnvelope{}, err
	}
	return env, nil
}

// LoadFile decodes a target Contract JSON file from disk.
func LoadFile(path string) (Contract, error) {
	f, err := os.Open(path)
	if err != nil {
		return Contract{}, err
	}
	defer func() { _ = f.Close() }()
	var c Contract
	if err := json.NewDecoder(f).Decode(&c); err != nil {
		return Contract{}, err
	}
	return c, nil
}

// WriteFile encodes v as indented JSON and writes it to path.
func WriteFile(path string, v any) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// BuildFromOptimizeEnvelope turns an optimize envelope into a target
// architecture Contract. If contractID is empty the first contract in the
// envelope is used; otherwise the matching contract or proposal ID is picked.
func BuildFromOptimizeEnvelope(env OptimizeEnvelope, contractID string) (Contract, error) {
	if len(env.Contracts) == 0 {
		return Contract{}, fmt.Errorf("target contract: optimize envelope has no contracts")
	}
	var src OptimizeContract
	if contractID == "" {
		src = env.Contracts[0]
	} else {
		found := false
		for _, c := range env.Contracts {
			if c.ID == contractID || c.ProposalID == contractID {
				src = c
				found = true
				break
			}
		}
		if !found {
			return Contract{}, fmt.Errorf("target contract: no optimize contract %q", contractID)
		}
	}
	subgraph := src.Target.Subgraph
	if len(subgraph.Roles) == 0 && src.Proposal != nil {
		subgraph = src.Proposal.TargetSubgraph
	}
	if len(subgraph.Roles) == 0 {
		return Contract{}, fmt.Errorf("target contract: optimize contract %q has no target roles", src.ID)
	}

	sourcePkg := sourcePackageName(src)
	module := modulePathFromPackage(sourcePkg)
	out := Contract{
		Version:          Version,
		ID:               "target-" + src.ID,
		SourceContractID: src.ID,
		SourceProposalID: src.ProposalID,
		Kind:             src.Kind,
		Rule:             src.Rule,
		Description:      src.Description,
		Source: SourceSpec{
			ModulePath:     module,
			CommandPackage: sourcePkg,
		},
		TargetSubgraph: subgraph,
	}

	packagesByRole := map[string]PackageSpec{}
	for _, role := range subgraph.Roles {
		if role.Kind != mgraph.NodePackage {
			continue
		}
		pkg := packageSpecFromRole(role, module, sourcePkg)
		out.Packages = append(out.Packages, pkg)
		packagesByRole[role.Name] = pkg
		if pkgRole := attrString(role.Attrs, "packageRole"); pkgRole != "" {
			packagesByRole[pkgRole] = pkg
		}
	}
	for _, role := range subgraph.Roles {
		switch role.Kind {
		case mgraph.NodeType:
			pkg, ok := packagesByRole[attrString(role.Attrs, "packageRole")]
			if !ok {
				continue
			}
			name := attrStringDefault(role.Attrs, "typeName", role.Name)
			file := attrStringDefault(role.Attrs, "file", filepath.Join(pkg.Dir, strings.ToLower(name)+".go"))
			kind := attrStringDefault(role.Attrs, "typeKind", "struct")
			if kind == "interface" || attrString(role.Attrs, mgraph.AttrContractKind) == "interface" {
				out.PublicInterfaces = append(out.PublicInterfaces, InterfaceSpec{
					Name:        name,
					PackageRole: pkg.Role,
					PackagePath: pkg.ImportPath,
					File:        file,
				})
			} else {
				out.PublicTypes = append(out.PublicTypes, TypeSpec{
					Name:        name,
					Kind:        kind,
					PackageRole: pkg.Role,
					PackagePath: pkg.ImportPath,
					File:        file,
				})
			}
			out.Files = appendFileSpec(out.Files, FileSpec{Path: file, PackageRole: pkg.Role, PackageName: pkg.Name, Action: "create", Purpose: "public target type"})
		case mgraph.NodeFunction:
			pkg, ok := packagesByRole[attrString(role.Attrs, "packageRole")]
			if !ok {
				continue
			}
			name := attrStringDefault(role.Attrs, "functionName", role.Name)
			file := attrStringDefault(role.Attrs, "file", filepath.Join(pkg.Dir, strings.ToLower(name)+".go"))
			sig := attrStringDefault(role.Attrs, "signature", "func "+name+"()")
			out.PublicFunctions = append(out.PublicFunctions, FunctionSpec{
				Name:        name,
				PackageRole: pkg.Role,
				PackagePath: pkg.ImportPath,
				File:        file,
				Signature:   sig,
			})
			out.Files = appendFileSpec(out.Files, FileSpec{Path: file, PackageRole: pkg.Role, PackageName: pkg.Name, Action: "create", Purpose: "public target function"})
		}
	}
	for _, pkg := range out.Packages {
		if pkg.Action == "create" {
			out.Files = appendFileSpec(out.Files, FileSpec{
				Path:        filepath.Join(pkg.Dir, "doc.go"),
				PackageRole: pkg.Role,
				PackageName: pkg.Name,
				Action:      "create",
				Purpose:     "package scaffold",
			})
		}
	}
	for _, edge := range subgraph.Edges {
		fromPkg, fromOK := packagesByRole[edge.From]
		toPkg, toOK := packagesByRole[edge.To]
		if fromOK && toOK && edge.Kind == mgraph.EdgeDependsOn {
			out.ExpectedEdges = append(out.ExpectedEdges, EdgeSpec{
				FromRole: edge.From,
				ToRole:   edge.To,
				From:     "pkg:" + fromPkg.ImportPath,
				To:       "pkg:" + toPkg.ImportPath,
				Kind:     string(edge.Kind),
			})
		}
	}
	out.ScaffoldHints = scaffoldHints(out)
	sortContract(&out)
	return out, nil
}

// Scaffold materializes the create-action files declared in c under outDir.
// Existing files are skipped unless force is true.
func Scaffold(c Contract, outDir string, force bool) (ScaffoldResult, error) {
	if outDir == "" {
		outDir = "."
	}
	files := map[string]FileSpec{}
	for _, f := range c.Files {
		if f.Action == "create" {
			files[f.Path] = f
		}
	}
	var res ScaffoldResult
	for _, path := range sortedFileKeys(files) {
		spec := files[path]
		full := filepath.Join(outDir, filepath.FromSlash(path))
		if _, err := os.Stat(full); err == nil && !force {
			res.Skipped = append(res.Skipped, path)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return res, err
		}
		src, err := renderFile(c, spec)
		if err != nil {
			return res, fmt.Errorf("%s: %w", path, err)
		}
		if err := os.WriteFile(full, src, 0o644); err != nil {
			return res, err
		}
		res.Created = append(res.Created, path)
	}
	return res, nil
}

// Verify checks the actual code graph at codeDir against the target contract c
// and returns a VerifyResult listing any missing packages, files, public
// surface, or expected edges.
func Verify(ctx context.Context, c Contract, codeDir, pattern string, tests bool) (VerifyResult, error) {
	if pattern == "" {
		pattern = "./..."
	}
	build, err := parser.Build(parser.Options{
		Dir:      codeDir,
		Patterns: []string{pattern},
		Tests:    tests,
	})
	if err != nil {
		return VerifyResult{}, err
	}
	_ = ctx
	res := VerifyResult{TargetID: c.ID}
	res.LoadErrors = append(res.LoadErrors, build.LoadErrors...)
	for _, pkg := range c.Packages {
		if pkg.Action == "create" || pkg.Action == "keep" {
			if !hasPackage(build.Graph, pkg.ImportPath) {
				res.MissingPackages = append(res.MissingPackages, pkg.ImportPath)
			}
		}
	}
	for _, f := range c.Files {
		if f.Action != "create" {
			continue
		}
		if _, err := os.Stat(filepath.Join(codeDir, filepath.FromSlash(f.Path))); err != nil {
			res.MissingFiles = append(res.MissingFiles, f.Path)
		}
	}
	for _, iface := range c.PublicInterfaces {
		if !hasNodeQName(build.Graph, mgraph.NodeType, iface.PackagePath+"."+iface.Name) {
			res.MissingPublicInterfaces = append(res.MissingPublicInterfaces, iface.PackagePath+"."+iface.Name)
		}
	}
	for _, typ := range c.PublicTypes {
		if !hasNodeQName(build.Graph, mgraph.NodeType, typ.PackagePath+"."+typ.Name) {
			res.MissingPublicTypes = append(res.MissingPublicTypes, typ.PackagePath+"."+typ.Name)
		}
	}
	for _, fn := range c.PublicFunctions {
		if !hasNodeQName(build.Graph, mgraph.NodeFunction, fn.PackagePath+"."+fn.Name) {
			res.MissingPublicFunctions = append(res.MissingPublicFunctions, fn.PackagePath+"."+fn.Name)
		}
	}
	for _, edge := range c.ExpectedEdges {
		if edge.Kind == "" || edge.From == "" || edge.To == "" {
			continue
		}
		if !hasEdge(build.Graph, strings.TrimPrefix(edge.From, "pkg:"), strings.TrimPrefix(edge.To, "pkg:"), mgraph.EdgeKind(edge.Kind)) {
			res.MissingExpectedEdges = append(res.MissingExpectedEdges, edge)
		}
	}
	res.Match = len(res.MissingPackages) == 0 &&
		len(res.MissingFiles) == 0 &&
		len(res.MissingPublicInterfaces) == 0 &&
		len(res.MissingPublicTypes) == 0 &&
		len(res.MissingPublicFunctions) == 0 &&
		len(res.MissingExpectedEdges) == 0
	return res, nil
}

// FormatVerifyText writes a human-readable rendering of r to w.
func FormatVerifyText(w io.Writer, r VerifyResult) error {
	if r.Match {
		_, err := fmt.Fprintf(w, "Match on target %s\n", r.TargetID)
		return err
	}
	_, _ = fmt.Fprintf(w, "Mismatch on target %s:\n", r.TargetID)
	writeList := func(title string, vals []string) {
		if len(vals) == 0 {
			return
		}
		_, _ = fmt.Fprintf(w, "  %s:\n", title)
		for _, v := range vals {
			_, _ = fmt.Fprintf(w, "    - %s\n", v)
		}
	}
	writeList("missing packages", r.MissingPackages)
	writeList("missing files", r.MissingFiles)
	writeList("missing public interfaces", r.MissingPublicInterfaces)
	writeList("missing public types", r.MissingPublicTypes)
	writeList("missing public functions", r.MissingPublicFunctions)
	if len(r.MissingExpectedEdges) > 0 {
		_, _ = fmt.Fprintln(w, "  missing expected edges:")
		for _, e := range r.MissingExpectedEdges {
			_, _ = fmt.Fprintf(w, "    - %s --%s--> %s\n", e.From, e.Kind, e.To)
		}
	}
	return nil
}

// FormatJSON writes v to w as indented JSON.
func FormatJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func sourcePackageName(c OptimizeContract) string {
	target := strings.TrimPrefix(c.Objective.Target, "pkg:")
	if target != "" {
		return target
	}
	if c.Proposal != nil {
		for _, sample := range c.Proposal.Samples {
			if v := strings.TrimPrefix(sample["CurrentCommandPackage"], "pkg:"); v != "" {
				return v
			}
		}
	}
	return ""
}

func modulePathFromPackage(pkg string) string {
	for _, marker := range []string{"/cmd/", "/internal/", "/pkg/"} {
		if i := strings.Index(pkg, marker); i > 0 {
			return pkg[:i]
		}
	}
	if i := strings.LastIndex(pkg, "/"); i > 0 {
		return pkg[:i]
	}
	return pkg
}

func packageSpecFromRole(role proposal.Role, modulePath, sourcePkg string) PackageSpec {
	action := attrStringDefault(role.Attrs, "packageAction", "create")
	pkgPath := attrString(role.Attrs, "packagePath")
	importPath := ""
	if pkgPath != "" && modulePath != "" {
		importPath = modulePath + "/" + strings.Trim(pkgPath, "/")
	}
	if importPath == "" && action == "keep" && sourcePkg != "" {
		importPath = sourcePkg
		pkgPath = relativeImportDir(modulePath, sourcePkg)
	}
	if pkgPath == "" {
		pkgPath = defaultPackageDir(role.Name)
	}
	if importPath == "" && modulePath != "" {
		importPath = modulePath + "/" + strings.Trim(pkgPath, "/")
	}
	return PackageSpec{
		Role:        role.Name,
		ImportPath:  importPath,
		Dir:         filepath.ToSlash(pkgPath),
		Name:        attrStringDefault(role.Attrs, "packageName", packageNameFromDir(pkgPath)),
		Action:      action,
		Description: attrString(role.Attrs, "extracts"),
	}
}

func relativeImportDir(modulePath, importPath string) string {
	if modulePath != "" && strings.HasPrefix(importPath, modulePath+"/") {
		return strings.TrimPrefix(importPath, modulePath+"/")
	}
	return importPath
}

func defaultPackageDir(role string) string {
	name := strings.TrimSuffix(role, "Orchestration")
	name = strings.TrimSuffix(name, "Package")
	if name == "" {
		name = role
	}
	return "internal/" + lowerIdent(name)
}

func packageNameFromDir(dir string) string {
	base := filepath.Base(filepath.FromSlash(dir))
	return lowerIdent(base)
}

func lowerIdent(s string) string {
	if s == "" {
		return "target"
	}
	var b strings.Builder
	for i, r := range s {
		if r == '-' || r == '_' || r == ' ' {
			continue
		}
		if i == 0 && r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	out := b.String()
	if out == "" {
		return "target"
	}
	return out
}

func attrString(attrs map[string]any, key string) string {
	if attrs == nil {
		return ""
	}
	switch v := attrs[key].(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return ""
	}
}

func attrStringDefault(attrs map[string]any, key, fallback string) string {
	if v := attrString(attrs, key); v != "" {
		return v
	}
	return fallback
}

func appendFileSpec(files []FileSpec, spec FileSpec) []FileSpec {
	if spec.Path == "" {
		return files
	}
	for i := range files {
		if files[i].Path == spec.Path {
			if files[i].Purpose == "" {
				files[i].Purpose = spec.Purpose
			}
			return files
		}
	}
	return append(files, spec)
}

func scaffoldHints(c Contract) []string {
	out := []string{}
	for _, pkg := range c.Packages {
		switch pkg.Action {
		case "create":
			out = append(out, "create package "+pkg.Dir+" ("+pkg.ImportPath+")")
		case "keep":
			out = append(out, "keep package "+pkg.Dir+" as adapter boundary")
		}
	}
	for _, edge := range c.ExpectedEdges {
		out = append(out, "preserve expected edge "+edge.FromRole+" --"+edge.Kind+"--> "+edge.ToRole)
	}
	return out
}

func sortContract(c *Contract) {
	sort.SliceStable(c.Packages, func(i, j int) bool { return c.Packages[i].Role < c.Packages[j].Role })
	sort.SliceStable(c.Files, func(i, j int) bool { return c.Files[i].Path < c.Files[j].Path })
	sort.SliceStable(c.PublicInterfaces, func(i, j int) bool { return c.PublicInterfaces[i].Name < c.PublicInterfaces[j].Name })
	sort.SliceStable(c.PublicTypes, func(i, j int) bool { return c.PublicTypes[i].Name < c.PublicTypes[j].Name })
	sort.SliceStable(c.PublicFunctions, func(i, j int) bool { return c.PublicFunctions[i].Name < c.PublicFunctions[j].Name })
}

func sortedFileKeys(files map[string]FileSpec) []string {
	out := make([]string, 0, len(files))
	for path := range files {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

func renderFile(c Contract, spec FileSpec) ([]byte, error) {
	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "package %s\n\n", spec.PackageName)
	importContext := false
	for _, fn := range c.PublicFunctions {
		if fn.File == spec.Path && strings.Contains(fn.Signature, "context.") {
			importContext = true
			break
		}
	}
	if importContext {
		b.WriteString("import \"context\"\n\n")
	}
	for _, iface := range c.PublicInterfaces {
		if iface.File != spec.Path {
			continue
		}
		_, _ = fmt.Fprintf(&b, "type %s interface {\n", iface.Name)
		for _, method := range iface.Methods {
			if method.Signature != "" {
				_, _ = fmt.Fprintf(&b, "\t%s\n", method.Signature)
			} else {
				_, _ = fmt.Fprintf(&b, "\t%s()\n", method.Name)
			}
		}
		b.WriteString("}\n\n")
	}
	for _, typ := range c.PublicTypes {
		if typ.File != spec.Path {
			continue
		}
		if typ.Kind == "interface" {
			_, _ = fmt.Fprintf(&b, "type %s interface{}\n\n", typ.Name)
		} else {
			_, _ = fmt.Fprintf(&b, "type %s struct{}\n\n", typ.Name)
		}
	}
	for _, fn := range c.PublicFunctions {
		if fn.File != spec.Path {
			continue
		}
		_, _ = fmt.Fprintf(&b, "%s {\n", strings.TrimSpace(fn.Signature))
		b.WriteString(renderFunctionBody(fn.Signature))
		b.WriteString("}\n\n")
	}
	if strings.TrimSpace(b.String()) == "package "+spec.PackageName {
		_, _ = fmt.Fprintf(&b, "// Package %s is generated from ArchMotif target contract %s.\n", spec.PackageName, c.ID)
	}
	src, err := format.Source([]byte(b.String()))
	if err != nil {
		return nil, err
	}
	return src, nil
}

func renderFunctionBody(signature string) string {
	switch {
	case strings.Contains(signature, "(Result, error)"):
		return "\t_ = ctx\n\t_ = opts\n\treturn Result{}, nil\n"
	case strings.HasSuffix(strings.TrimSpace(signature), " error"):
		return "\treturn nil\n"
	default:
		return "\tpanic(\"TODO: generated target scaffold\")\n"
	}
}

func hasPackage(g *mgraph.Graph, importPath string) bool {
	for _, n := range g.NodesByKind(mgraph.NodePackage) {
		if n.QName == importPath || n.ID == "pkg:"+importPath {
			return true
		}
	}
	return false
}

func hasNodeQName(g *mgraph.Graph, kind mgraph.NodeKind, qname string) bool {
	for _, n := range g.NodesByKind(kind) {
		if n.QName == qname {
			return true
		}
	}
	return false
}

func hasEdge(g *mgraph.Graph, fromImport, toImport string, kind mgraph.EdgeKind) bool {
	fromID := "pkg:" + fromImport
	toID := "pkg:" + toImport
	for _, e := range g.Edges() {
		if e.Kind == kind && e.From == fromID && e.To == toID {
			return true
		}
	}
	return false
}
