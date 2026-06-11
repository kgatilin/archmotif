package mcpserver

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
	"github.com/kgatilin/archmotif/internal/targetcontract"
)

// TargetRef is a reference to a stored target graph: the source it was derived
// from, the workspace path it lives at, and basic size metadata.
type TargetRef struct {
	TargetID      string `json:"target_id"`
	GraphID       string `json:"graph_id"`
	SourceGraphID string `json:"source_graph_id,omitempty"`
	Path          string `json:"path"`
	Nodes         int    `json:"nodes"`
	Edges         int    `json:"edges"`
	Description   string `json:"description,omitempty"`
}

// TargetShape extends TargetRef with the full node and edge contents of a
// target graph, for callers that want to inspect the target structure.
type TargetShape struct {
	TargetRef
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

// PutTargetGraph writes the supplied target contract as a target graph derived
// from sourceGraphID. If force is false and a target graph with the same id
// already exists, the call fails.
func (s *Service) PutTargetGraph(sourceGraphID, targetID string, contract targetcontract.Contract, force bool) (TargetRef, error) {
	if sourceGraphID == "" {
		return TargetRef{}, fmt.Errorf("target_put: graph_id is required")
	}
	if contract.ID == "" {
		return TargetRef{}, fmt.Errorf("target_put: contract.id is required")
	}
	if targetID == "" {
		targetID = contract.ID
	}
	targetGraphID, err := targetGraphID(sourceGraphID, targetID)
	if err != nil {
		return TargetRef{}, err
	}
	path, err := s.resolvePath(targetGraphID)
	if err != nil {
		return TargetRef{}, err
	}
	if !force {
		if _, err := os.Stat(path); err == nil {
			return TargetRef{}, fmt.Errorf("target graph %q already exists (pass force=true to overwrite)", targetGraphID)
		}
	}
	g := graphFromTargetContract(sourceGraphID, targetGraphID, targetID, contract)
	if err := s.SaveGraph(targetGraphID, g); err != nil {
		return TargetRef{}, err
	}
	_ = s.Logger.Append(MutationRecord{
		Tool:    "target_put",
		GraphID: targetGraphID,
		Args: map[string]any{
			"source_graph_id": sourceGraphID,
			"target_id":       targetID,
			"force":           force,
		},
		Result: map[string]any{"path": path, "nodes": len(g.Nodes), "edges": len(g.Edges)},
	})
	return targetRefFromGraph(targetID, targetGraphID, sourceGraphID, path, g), nil
}

// ListTargets returns all target graphs stored for the given source graph,
// sorted by graph id.
func (s *Service) ListTargets(sourceGraphID string) ([]TargetRef, error) {
	slug, _, err := splitGraphID(sourceGraphID)
	if err != nil {
		return nil, err
	}
	refs, err := s.ListGraphs()
	if err != nil {
		return nil, err
	}
	out := make([]TargetRef, 0)
	for _, ref := range refs {
		if ref.Slug != slug || !strings.HasPrefix(ref.Variant, "target-") {
			continue
		}
		g, err := s.LoadGraph(ref.ID)
		if err != nil {
			continue
		}
		targetID := targetIDFromGraph(g, strings.TrimPrefix(ref.Variant, "target-"))
		out = append(out, targetRefFromGraph(targetID, ref.ID, sourceGraphID, ref.Path, g))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].GraphID < out[j].GraphID })
	return out, nil
}

// ShowTarget returns the full TargetShape (ref plus nodes and edges) for the
// named target graph.
func (s *Service) ShowTarget(sourceGraphID, targetID string) (TargetShape, error) {
	graphID, err := targetGraphID(sourceGraphID, targetID)
	if err != nil {
		return TargetShape{}, err
	}
	path, err := s.resolvePath(graphID)
	if err != nil {
		return TargetShape{}, err
	}
	g, err := s.LoadGraph(graphID)
	if err != nil {
		return TargetShape{}, err
	}
	ref := targetRefFromGraph(targetIDFromGraph(g, targetID), graphID, sourceGraphID, path, g)
	return TargetShape{TargetRef: ref, Nodes: g.Nodes, Edges: g.Edges}, nil
}

func targetGraphID(sourceGraphID, targetID string) (string, error) {
	slug, _, err := splitGraphID(sourceGraphID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(targetID) == "" {
		return "", fmt.Errorf("target_id is required")
	}
	return slug + ":target-" + Slug(targetID), nil
}

func graphFromTargetContract(sourceGraphID, targetGraphID, targetID string, c targetcontract.Contract) *Graph {
	g := NewGraph()
	seenEdges := map[string]bool{}
	addTargetEdge := func(from, to, kind, role string) {
		if from == "" || to == "" || !g.HasNode(from) || !g.HasNode(to) {
			return
		}
		key := from + "\x00" + to + "\x00" + kind
		if seenEdges[key] {
			return
		}
		seenEdges[key] = true
		attrs := map[string]string{"kind": kind, "target_id": targetID}
		if role != "" {
			attrs["edge_role"] = role
		}
		_ = g.AddEdge(Edge{From: from, To: to, Kind: kind, Attrs: attrs})
	}
	metaID := "target:" + Slug(targetID) + ":contract"
	_ = g.AddNode(Node{
		ID:   metaID,
		Kind: "target",
		Name: c.ID,
		Attrs: map[string]string{
			"id":                 metaID,
			"target_id":          targetID,
			"graph_id":           targetGraphID,
			"source_graph_id":    sourceGraphID,
			"source_contract_id": c.SourceContractID,
			"source_proposal_id": c.SourceProposalID,
			"description":        c.Description,
			"kind":               "target",
			"contract_kind":      c.Kind,
			"rule":               c.Rule,
			"tags":               "target,architecture",
		},
	})

	nodeByRole := map[string]string{}
	pkgByRole := map[string]targetcontract.PackageSpec{}
	for _, pkg := range c.Packages {
		id := "pkg:" + pkg.ImportPath
		if pkg.ImportPath == "" {
			id = "target:" + Slug(targetID) + ":package:" + Slug(pkg.Role)
		}
		_ = g.AddNode(Node{
			ID:   id,
			Kind: string(mgraph.NodePackage),
			Name: pkg.Name,
			Attrs: map[string]string{
				"id":          id,
				"target_id":   targetID,
				"role":        pkg.Role,
				"action":      pkg.Action,
				"package":     pkg.ImportPath,
				"qname":       pkg.ImportPath,
				"dir":         pkg.Dir,
				"description": pkg.Description,
				"tags":        "target,package",
			},
		})
		nodeByRole[pkg.Role] = id
		pkgByRole[pkg.Role] = pkg
	}
	fileNodeByPath := map[string]string{}
	for _, f := range c.Files {
		id := "target:" + Slug(targetID) + ":file:" + Slug(f.Path)
		fileNodeByPath[f.Path] = id
		_ = g.AddNode(Node{
			ID:   id,
			Kind: string(mgraph.NodeFile),
			Name: filepath.Base(filepath.FromSlash(f.Path)),
			Attrs: map[string]string{
				"id":         id,
				"target_id":  targetID,
				"path":       f.Path,
				"role":       f.PackageRole,
				"action":     f.Action,
				"package":    packageImport(pkgByRole[f.PackageRole]),
				"package_go": f.PackageName,
				"purpose":    f.Purpose,
				"tags":       "target,file",
			},
		})
		if pkgID := nodeByRole[f.PackageRole]; pkgID != "" {
			addTargetEdge(pkgID, id, string(mgraph.EdgeContains), f.PackageRole+"->"+f.Path)
		}
	}
	for _, t := range c.PublicTypes {
		qname := t.PackagePath + "." + t.Name
		id := "target:" + Slug(targetID) + ":type:" + Slug(qname)
		_ = g.AddNode(Node{
			ID:   id,
			Kind: string(mgraph.NodeType),
			Name: t.Name,
			Attrs: map[string]string{
				"id":        id,
				"target_id": targetID,
				"qname":     qname,
				"package":   t.PackagePath,
				"role":      roleForTargetNode(c, t.Name, mgraph.NodeType),
				"typeKind":  t.Kind,
				"file":      t.File,
				"tags":      "target,public,type",
			},
		})
		nodeByRole[roleForTargetNode(c, t.Name, mgraph.NodeType)] = id
		if fileID := fileNodeByPath[t.File]; fileID != "" {
			addTargetEdge(fileID, id, string(mgraph.EdgeContains), t.File+"->"+t.Name)
		}
	}
	for _, f := range c.PublicFunctions {
		qname := f.PackagePath + "." + f.Name
		id := "target:" + Slug(targetID) + ":function:" + Slug(qname)
		_ = g.AddNode(Node{
			ID:   id,
			Kind: string(mgraph.NodeFunction),
			Name: f.Name,
			Attrs: map[string]string{
				"id":        id,
				"target_id": targetID,
				"qname":     qname,
				"package":   f.PackagePath,
				"role":      roleForTargetNode(c, f.Name, mgraph.NodeFunction),
				"signature": f.Signature,
				"file":      f.File,
				"tags":      "target,public,function",
			},
		})
		nodeByRole[roleForTargetNode(c, f.Name, mgraph.NodeFunction)] = id
		if fileID := fileNodeByPath[f.File]; fileID != "" {
			addTargetEdge(fileID, id, string(mgraph.EdgeContains), f.File+"->"+f.Name)
		}
	}
	for _, edge := range c.ExpectedEdges {
		from := edge.From
		to := edge.To
		if from == "" {
			from = nodeByRole[edge.FromRole]
		}
		if to == "" {
			to = nodeByRole[edge.ToRole]
		}
		addTargetEdge(from, to, edge.Kind, edge.FromRole+"->"+edge.ToRole)
	}
	for _, edge := range c.TargetSubgraph.Edges {
		from := nodeByRole[edge.From]
		to := nodeByRole[edge.To]
		addTargetEdge(from, to, string(edge.Kind), edge.From+"->"+edge.To)
	}
	return g
}

func targetRefFromGraph(targetID, graphID, sourceGraphID, path string, g *Graph) TargetRef {
	return TargetRef{
		TargetID:      targetIDFromGraph(g, targetID),
		GraphID:       graphID,
		SourceGraphID: sourceGraphID,
		Path:          path,
		Nodes:         len(g.Nodes),
		Edges:         len(g.Edges),
		Description:   targetDescription(g),
	}
}

func targetIDFromGraph(g *Graph, fallback string) string {
	for _, n := range g.Nodes {
		if n.Kind == "target" && n.Attrs["target_id"] != "" {
			return n.Attrs["target_id"]
		}
	}
	return fallback
}

func targetDescription(g *Graph) string {
	for _, n := range g.Nodes {
		if n.Kind == "target" {
			return n.Attrs["description"]
		}
	}
	return ""
}

func packageImport(pkg targetcontract.PackageSpec) string {
	return pkg.ImportPath
}

func roleForTargetNode(c targetcontract.Contract, name string, kind mgraph.NodeKind) string {
	for _, role := range c.TargetSubgraph.Roles {
		if role.Kind != kind {
			continue
		}
		for _, key := range []string{"typeName", "functionName"} {
			if v, _ := role.Attrs[key].(string); v == name {
				return role.Name
			}
		}
		if role.Name == name {
			return role.Name
		}
	}
	return name
}
