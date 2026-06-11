package shape

import (
	"fmt"
	"math"
	"sort"
)

const (
	// CurrentJSONVersion is the optimize JSON envelope version.
	CurrentJSONVersion = 1
)

// Options defines the target self-similar structural envelope.
type Options struct {
	Predicate         string
	Layer             string
	ParentDirection   string
	MaxDirectChildren int
	GroupMinChildren  int
	GroupMaxChildren  int
	MinLeafRatio      float64
	Top               int
}

// Result is the optimize command JSON envelope.
type Result struct {
	Version    int         `json:"version"`
	Input      InputInfo   `json:"input"`
	Target     TargetInfo  `json:"target"`
	Candidates []Candidate `json:"candidates"`
	Errors     []string    `json:"errors,omitempty"`
}

// InputInfo records the graph and detector settings used.
type InputInfo struct {
	Nodes           int    `json:"nodes"`
	Edges           int    `json:"edges"`
	Predicate       string `json:"predicate"`
	Layer           string `json:"layer,omitempty"`
	ParentDirection string `json:"parentDirection"`
}

// TargetInfo records the self-similar shape constraints.
type TargetInfo struct {
	MaxDirectChildren int     `json:"maxDirectChildren"`
	GroupMinChildren  int     `json:"groupMinChildren"`
	GroupMaxChildren  int     `json:"groupMaxChildren"`
	MinLeafRatio      float64 `json:"minLeafRatio"`
}

// Candidate is one structural rewrite opportunity.
type Candidate struct {
	ID                  string              `json:"id"`
	Pattern             string              `json:"pattern"`
	Score               float64             `json:"score"`
	Center              NodeRef             `json:"center"`
	Metrics             CandidateMetrics    `json:"metrics"`
	CurrentGraph        Subgraph            `json:"currentGraph"`
	TargetGraph         Subgraph            `json:"targetGraph"`
	EditableSubgraph    EditableSubgraph    `json:"editableSubgraph"`
	BoundaryContext     BoundaryContext     `json:"boundaryContext"`
	TargetRewrite       TargetRewrite       `json:"targetRewrite"`
	MaterializationTask MaterializationTask `json:"materializationTask"`
}

// Subgraph is the graph shape before or after optimization. It is a shape,
// not an executable patch: callers use it as the target when changing source
// code and then re-run graph generation/metrics to verify movement.
type Subgraph struct {
	Description string    `json:"description,omitempty"`
	Nodes       []NodeRef `json:"nodes"`
	Edges       []EdgeRef `json:"edges"`
}

// CandidateMetrics describes why the current shape violates the target.
type CandidateMetrics struct {
	DirectStructuralChildren int     `json:"directStructuralChildren"`
	LeafChildren             int     `json:"leafChildren"`
	NonLeafDirectChildren    int     `json:"nonLeafDirectChildren"`
	LeafRatio                float64 `json:"leafRatio"`
	MaxDirectChildren        int     `json:"maxDirectChildren"`
	TargetGroupCount         int     `json:"targetGroupCount"`
	TargetRootChildren       int     `json:"targetRootChildren"`
	Feasible                 bool    `json:"feasible"`
	InfeasibleReason         string  `json:"infeasibleReason,omitempty"`
}

// EditableSubgraph is the part an LLM materializer may rewire.
type EditableSubgraph struct {
	Center              NodeRef   `json:"center"`
	LeafChildren        []NodeRef `json:"leafChildren"`
	ReplaceableEdges    []EdgeRef `json:"replaceableEdges"`
	ExistingGroupLike   []NodeRef `json:"existingGroupLike,omitempty"`
	EditableDescription string    `json:"editableDescription"`
}

// BoundaryContext is context that must stay intact.
type BoundaryContext struct {
	PreservedDirectChildren []NodeRef `json:"preservedDirectChildren,omitempty"`
	PreservedEdges          []EdgeRef `json:"preservedEdges,omitempty"`
	Rule                    string    `json:"rule"`
}

// TargetRewrite is a deterministic structural patch template. It is not an
// already-materialised memory patch: semantic group names are intentionally
// left to the LLM materializer, but the target topology is generated here.
type TargetRewrite struct {
	Operation                   string                `json:"operation"`
	GroupCount                  int                   `json:"groupCount"`
	NewGroupNodes               []GroupNode           `json:"newGroupNodes"`
	AddStructuralEdges          []EdgeRef             `json:"addStructuralEdges"`
	GroupAssignments            []GroupAssignment     `json:"groupAssignments"`
	MaterializedStructuralEdges []EdgeRef             `json:"materializedStructuralEdges"`
	RemoveStructuralEdges       []EdgeRef             `json:"removeStructuralEdges"`
	AssignmentConstraints       AssignmentConstraints `json:"assignmentConstraints"`
	Validation                  RewriteValidation     `json:"validation"`
}

// GroupNode is an abstract intermediate node the materializer must name.
type GroupNode struct {
	TempID          string `json:"tempId"`
	Role            string `json:"role"`
	TargetMinLeaves int    `json:"targetMinLeaves"`
	TargetMaxLeaves int    `json:"targetMaxLeaves"`
}

// GroupAssignment is the tool-generated target topology for one new group.
// The materializer may name the group, but should preserve these assignments.
type GroupAssignment struct {
	GroupTempID        string    `json:"groupTempId"`
	LeafChildren       []NodeRef `json:"leafChildren"`
	AddStructuralEdges []EdgeRef `json:"addStructuralEdges"`
}

// AssignmentConstraints define the semantic work left to the materializer.
type AssignmentConstraints struct {
	AssignLeaves                []NodeRef `json:"assignLeaves"`
	EachLeafAssignedExactlyOnce bool      `json:"eachLeafAssignedExactlyOnce"`
	UseOnlyNewGroupNodes        bool      `json:"useOnlyNewGroupNodes"`
	GroupMinChildren            int       `json:"groupMinChildren"`
	GroupMaxChildren            int       `json:"groupMaxChildren"`
}

// RewriteValidation records expected metric movement after materialization.
type RewriteValidation struct {
	BeforeRootDirectChildren int  `json:"beforeRootDirectChildren"`
	AfterRootDirectChildren  int  `json:"afterRootDirectChildren"`
	BeforeLeafChildren       int  `json:"beforeLeafChildren"`
	AfterDirectLeafChildren  int  `json:"afterDirectLeafChildren"`
	OriginalNodesDeleted     int  `json:"originalNodesDeleted"`
	BoundaryEdgesRemoved     int  `json:"boundaryEdgesRemoved"`
	Feasible                 bool `json:"feasible"`
}

// MaterializationTask describes what an LLM/code agent should do with the
// structural rewrite contract.
type MaterializationTask struct {
	ChooseSemanticGroupNames bool     `json:"chooseSemanticGroupNames"`
	AssignLeavesBySemantics  bool     `json:"assignLeavesBySemantics"`
	DoNotParseForDetection   bool     `json:"doNotParseForDetection"`
	Instructions             []string `json:"instructions"`
}

// Optimize runs every POC structural detector.
func Optimize(g *Graph, opts Options) Result {
	opts = normalizeOptions(opts)
	res := Result{
		Version: CurrentJSONVersion,
		Input: InputInfo{
			Nodes:           len(g.Nodes),
			Edges:           len(g.Edges),
			Predicate:       opts.Predicate,
			Layer:           opts.Layer,
			ParentDirection: opts.ParentDirection,
		},
		Target: TargetInfo{
			MaxDirectChildren: opts.MaxDirectChildren,
			GroupMinChildren:  opts.GroupMinChildren,
			GroupMaxChildren:  opts.GroupMaxChildren,
			MinLeafRatio:      opts.MinLeafRatio,
		},
	}
	res.Candidates = detectFlatStars(g, opts)
	if opts.Top > 0 && len(res.Candidates) > opts.Top {
		res.Candidates = res.Candidates[:opts.Top]
	}
	return res
}

func normalizeOptions(opts Options) Options {
	if opts.Predicate == "" {
		opts.Predicate = "part-of"
	}
	if opts.ParentDirection == "" {
		opts.ParentDirection = "in"
	}
	if opts.MaxDirectChildren <= 0 {
		opts.MaxDirectChildren = 12
	}
	if opts.GroupMinChildren <= 0 {
		opts.GroupMinChildren = 4
	}
	if opts.GroupMaxChildren <= 0 {
		opts.GroupMaxChildren = 12
	}
	if opts.GroupMaxChildren < opts.GroupMinChildren {
		opts.GroupMaxChildren = opts.GroupMinChildren
	}
	if opts.MinLeafRatio <= 0 {
		opts.MinLeafRatio = 0.70
	}
	if opts.Top < 0 {
		opts.Top = 0
	}
	return opts
}

func detectFlatStars(g *Graph, opts Options) []Candidate {
	structural := structuralEdges(g, opts)
	childrenByParent := map[string][]Edge{}
	for _, e := range structural {
		parent, ok := parentID(e, opts.ParentDirection)
		if !ok {
			continue
		}
		childrenByParent[parent] = append(childrenByParent[parent], e)
	}

	candidates := []Candidate{}
	for centerID, childEdges := range childrenByParent {
		if len(childEdges) <= opts.MaxDirectChildren {
			continue
		}
		leafEdges := []Edge{}
		preservedDirect := []NodeRef{}
		for _, e := range childEdges {
			child := childID(e, opts.ParentDirection)
			if len(childrenByParent[child]) == 0 {
				leafEdges = append(leafEdges, e)
			} else {
				preservedDirect = append(preservedDirect, g.nodeRef(child))
			}
		}
		leafRatio := float64(len(leafEdges)) / float64(len(childEdges))
		if leafRatio < opts.MinLeafRatio {
			continue
		}
		groupCount := int(math.Ceil(float64(len(leafEdges)) / float64(opts.GroupMaxChildren)))
		if groupCount < 1 {
			groupCount = 1
		}
		targetRootChildren := len(preservedDirect) + groupCount
		feasible := targetRootChildren <= opts.MaxDirectChildren
		reason := ""
		if !feasible {
			reason = fmt.Sprintf("target root children %d exceeds max %d; raise maxDirectChildren or groupMaxChildren", targetRootChildren, opts.MaxDirectChildren)
		}
		score := (float64(len(childEdges)) / float64(opts.MaxDirectChildren)) * leafRatio

		sort.SliceStable(leafEdges, func(i, j int) bool {
			return childID(leafEdges[i], opts.ParentDirection) < childID(leafEdges[j], opts.ParentDirection)
		})
		sort.SliceStable(preservedDirect, func(i, j int) bool {
			return preservedDirect[i].ID < preservedDirect[j].ID
		})
		candidates = append(candidates, buildFlatStarCandidate(
			g,
			opts,
			len(candidates)+1,
			centerID,
			childEdges,
			leafEdges,
			preservedDirect,
			groupCount,
			targetRootChildren,
			feasible,
			reason,
			score,
			leafRatio,
		))
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		return candidates[i].Center.ID < candidates[j].Center.ID
	})
	for i := range candidates {
		candidates[i].ID = fmt.Sprintf("flat-star-%03d", i+1)
	}
	return candidates
}

func buildFlatStarCandidate(
	g *Graph,
	opts Options,
	seq int,
	centerID string,
	childEdges []Edge,
	leafEdges []Edge,
	preservedDirect []NodeRef,
	groupCount int,
	targetRootChildren int,
	feasible bool,
	infeasibleReason string,
	score float64,
	leafRatio float64,
) Candidate {
	leafRefs := make([]NodeRef, 0, len(leafEdges))
	removeEdges := make([]EdgeRef, 0, len(leafEdges))
	editableIDs := map[string]struct{}{centerID: {}}
	for _, e := range leafEdges {
		leaf := childID(e, opts.ParentDirection)
		editableIDs[leaf] = struct{}{}
		leafRefs = append(leafRefs, g.nodeRef(leaf))
		removeEdges = append(removeEdges, edgeRef(e))
	}
	sort.SliceStable(leafRefs, func(i, j int) bool { return leafRefs[i].ID < leafRefs[j].ID })
	sort.SliceStable(removeEdges, func(i, j int) bool {
		if removeEdges[i].Source != removeEdges[j].Source {
			return removeEdges[i].Source < removeEdges[j].Source
		}
		return removeEdges[i].Target < removeEdges[j].Target
	})

	groupNodes := make([]GroupNode, 0, groupCount)
	addEdges := make([]EdgeRef, 0, groupCount)
	rewriteOpts := opts
	if rewriteOpts.Layer == "" {
		rewriteOpts.Layer = commonLayer(leafEdges)
	}
	for i := 1; i <= groupCount; i++ {
		tempID := fmt.Sprintf("$group_%03d", i)
		groupNodes = append(groupNodes, GroupNode{
			TempID:          tempID,
			Role:            "intermediate_semantic_group",
			TargetMinLeaves: opts.GroupMinChildren,
			TargetMaxLeaves: opts.GroupMaxChildren,
		})
		addEdges = append(addEdges, structuralEdgeRef(tempID, centerID, rewriteOpts))
	}
	groupAssignments := deterministicGroupAssignments(leafRefs, groupNodes, rewriteOpts)
	materializedEdges := make([]EdgeRef, 0, len(addEdges)+len(leafRefs))
	materializedEdges = append(materializedEdges, addEdges...)
	for _, assignment := range groupAssignments {
		materializedEdges = append(materializedEdges, assignment.AddStructuralEdges...)
	}
	currentGraph := currentShapeGraph(g, centerID, childEdges)
	targetGraph := targetShapeGraph(g, centerID, preservedDirect, groupNodes, groupAssignments, materializedEdges, childEdges, removeEdges, rewriteOpts.Layer)

	preservedEdges := preservedEdgesFor(g, opts, editableIDs, removeEdges)
	return Candidate{
		ID:      fmt.Sprintf("flat-star-%03d", seq),
		Pattern: "flat_star_hub",
		Score:   round(score),
		Center:  g.nodeRef(centerID),
		Metrics: CandidateMetrics{
			DirectStructuralChildren: len(childEdges),
			LeafChildren:             len(leafEdges),
			NonLeafDirectChildren:    len(preservedDirect),
			LeafRatio:                round(leafRatio),
			MaxDirectChildren:        opts.MaxDirectChildren,
			TargetGroupCount:         groupCount,
			TargetRootChildren:       targetRootChildren,
			Feasible:                 feasible,
			InfeasibleReason:         infeasibleReason,
		},
		CurrentGraph: currentGraph,
		TargetGraph:  targetGraph,
		EditableSubgraph: EditableSubgraph{
			Center:              g.nodeRef(centerID),
			LeafChildren:        leafRefs,
			ReplaceableEdges:    removeEdges,
			ExistingGroupLike:   preservedDirect,
			EditableDescription: "Only replace direct leaf -> center structural edges. Original leaf nodes stay; target topology is generated by ArchMotif; semantic group names are deferred to the materializer.",
		},
		BoundaryContext: BoundaryContext{
			PreservedDirectChildren: preservedDirect,
			PreservedEdges:          preservedEdges,
			Rule:                    "Do not delete boundary nodes or non-replaced edges touching editable nodes. Preserve them exactly unless a later verified proposal explicitly owns them.",
		},
		TargetRewrite: TargetRewrite{
			Operation:                   "introduce_intermediate_groups",
			GroupCount:                  groupCount,
			NewGroupNodes:               groupNodes,
			AddStructuralEdges:          addEdges,
			GroupAssignments:            groupAssignments,
			MaterializedStructuralEdges: materializedEdges,
			RemoveStructuralEdges:       removeEdges,
			AssignmentConstraints: AssignmentConstraints{
				AssignLeaves:                leafRefs,
				EachLeafAssignedExactlyOnce: true,
				UseOnlyNewGroupNodes:        true,
				GroupMinChildren:            opts.GroupMinChildren,
				GroupMaxChildren:            opts.GroupMaxChildren,
			},
			Validation: RewriteValidation{
				BeforeRootDirectChildren: len(childEdges),
				AfterRootDirectChildren:  targetRootChildren,
				BeforeLeafChildren:       len(leafEdges),
				AfterDirectLeafChildren:  0,
				OriginalNodesDeleted:     0,
				BoundaryEdgesRemoved:     0,
				Feasible:                 feasible,
			},
		},
		MaterializationTask: MaterializationTask{
			ChooseSemanticGroupNames: true,
			AssignLeavesBySemantics:  false,
			DoNotParseForDetection:   true,
			Instructions: []string{
				"Use this structural contract as the source of truth; do not choose a different graph shape.",
				"Read leaf labels/content only to name the group nodes and explain each generated group.",
				"Preserve targetRewrite.groupAssignments exactly unless the caller explicitly requests a different structural optimization.",
				"Create group nodes that satisfy the target child-count bounds.",
				"Add the structural relations listed in targetRewrite.materializedStructuralEdges.",
				"Remove only the listed direct leaf -> center structural edges.",
				"Preserve all boundary edges and original memory/code nodes.",
			},
		},
	}
}

func deterministicGroupAssignments(leaves []NodeRef, groups []GroupNode, opts Options) []GroupAssignment {
	if len(groups) == 0 {
		return nil
	}
	assignments := make([]GroupAssignment, 0, len(groups))
	base := len(leaves) / len(groups)
	remainder := len(leaves) % len(groups)
	offset := 0
	for i, group := range groups {
		size := base
		if i < remainder {
			size++
		}
		if size < 0 {
			size = 0
		}
		end := offset + size
		if end > len(leaves) {
			end = len(leaves)
		}
		groupLeaves := append([]NodeRef(nil), leaves[offset:end]...)
		edges := make([]EdgeRef, 0, len(groupLeaves))
		for _, leaf := range groupLeaves {
			edges = append(edges, structuralEdgeRef(leaf.ID, group.TempID, opts))
		}
		assignments = append(assignments, GroupAssignment{
			GroupTempID:        group.TempID,
			LeafChildren:       groupLeaves,
			AddStructuralEdges: edges,
		})
		offset = end
	}
	return assignments
}

func currentShapeGraph(g *Graph, centerID string, childEdges []Edge) Subgraph {
	nodes := []NodeRef{g.nodeRef(centerID)}
	seen := map[string]struct{}{centerID: {}}
	edges := make([]EdgeRef, 0, len(childEdges))
	for _, e := range childEdges {
		edges = append(edges, edgeRef(e))
		for _, id := range []string{e.Source, e.Target} {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			nodes = append(nodes, g.nodeRef(id))
		}
	}
	sort.SliceStable(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	sortEdgeRefs(edges)
	return Subgraph{
		Description: "current direct structural shape selected for optimization",
		Nodes:       nodes,
		Edges:       edges,
	}
}

func targetShapeGraph(
	g *Graph,
	centerID string,
	preservedDirect []NodeRef,
	groupNodes []GroupNode,
	groupAssignments []GroupAssignment,
	materializedEdges []EdgeRef,
	childEdges []Edge,
	removeEdges []EdgeRef,
	layer string,
) Subgraph {
	nodes := []NodeRef{g.nodeRef(centerID)}
	seen := map[string]struct{}{centerID: {}}
	for _, preserved := range preservedDirect {
		if _, ok := seen[preserved.ID]; ok {
			continue
		}
		seen[preserved.ID] = struct{}{}
		nodes = append(nodes, preserved)
	}
	groupSizes := map[string]int{}
	for _, assignment := range groupAssignments {
		groupSizes[assignment.GroupTempID] = len(assignment.LeafChildren)
		for _, leaf := range assignment.LeafChildren {
			if _, ok := seen[leaf.ID]; ok {
				continue
			}
			seen[leaf.ID] = struct{}{}
			nodes = append(nodes, leaf)
		}
	}
	for _, group := range groupNodes {
		if _, ok := seen[group.TempID]; ok {
			continue
		}
		seen[group.TempID] = struct{}{}
		nodes = append(nodes, targetGroupNodeRef(group, groupSizes[group.TempID], layer))
	}
	edges := append([]EdgeRef(nil), materializedEdges...)
	edges = append(edges, preservedDirectEdges(childEdges, removeEdges)...)
	sort.SliceStable(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	sortEdgeRefs(edges)
	return Subgraph{
		Description: "target structural shape produced by optimizer",
		Nodes:       nodes,
		Edges:       edges,
	}
}

func targetGroupNodeRef(group GroupNode, childCount int, layer string) NodeRef {
	attrs := map[string]string{
		"kind":   "target_group",
		"role":   group.Role,
		"origin": "optimizer_target",
	}
	if layer != "" {
		attrs["layer"] = layer
	}
	return NodeRef{
		ID:     group.TempID,
		Label:  group.TempID,
		Attrs:  attrs,
		Degree: childCount + 1,
	}
}

func preservedDirectEdges(childEdges []Edge, removeEdges []EdgeRef) []EdgeRef {
	remove := map[string]struct{}{}
	for _, e := range removeEdges {
		remove[edgeKey(e)] = struct{}{}
	}
	out := []EdgeRef{}
	for _, edge := range childEdges {
		ref := edgeRef(edge)
		if _, ok := remove[edgeKey(ref)]; ok {
			continue
		}
		out = append(out, ref)
	}
	return out
}

func sortEdgeRefs(edges []EdgeRef) {
	sort.SliceStable(edges, func(i, j int) bool {
		if edges[i].Source != edges[j].Source {
			return edges[i].Source < edges[j].Source
		}
		if edges[i].Target != edges[j].Target {
			return edges[i].Target < edges[j].Target
		}
		if edges[i].Predicate != edges[j].Predicate {
			return edges[i].Predicate < edges[j].Predicate
		}
		return edges[i].Layer < edges[j].Layer
	})
}

func structuralEdges(g *Graph, opts Options) []Edge {
	out := []Edge{}
	for _, e := range g.Edges {
		if opts.Predicate != "" && e.Predicate != opts.Predicate {
			continue
		}
		if opts.Layer != "" && e.Layer != opts.Layer {
			continue
		}
		out = append(out, e)
	}
	return out
}

func parentID(e Edge, direction string) (string, bool) {
	switch direction {
	case "in", "child-to-parent":
		return e.Target, true
	case "out", "parent-to-child":
		return e.Source, true
	default:
		return "", false
	}
}

func childID(e Edge, direction string) string {
	switch direction {
	case "out", "parent-to-child":
		return e.Target
	default:
		return e.Source
	}
}

func structuralEdgeRef(child, parent string, opts Options) EdgeRef {
	if opts.ParentDirection == "out" || opts.ParentDirection == "parent-to-child" {
		return EdgeRef{Source: parent, Target: child, Predicate: opts.Predicate, Layer: opts.Layer}
	}
	return EdgeRef{Source: child, Target: parent, Predicate: opts.Predicate, Layer: opts.Layer}
}

func commonLayer(edges []Edge) string {
	if len(edges) == 0 {
		return ""
	}
	layer := edges[0].Layer
	if layer == "" {
		return ""
	}
	for _, e := range edges[1:] {
		if e.Layer != layer {
			return ""
		}
	}
	return layer
}

func preservedEdgesFor(g *Graph, opts Options, editableIDs map[string]struct{}, removeEdges []EdgeRef) []EdgeRef {
	remove := map[string]struct{}{}
	for _, e := range removeEdges {
		remove[edgeKey(e)] = struct{}{}
	}
	out := []EdgeRef{}
	for _, e := range g.Edges {
		ref := edgeRef(e)
		if _, ok := remove[edgeKey(ref)]; ok {
			continue
		}
		_, sourceEditable := editableIDs[e.Source]
		_, targetEditable := editableIDs[e.Target]
		if sourceEditable || targetEditable {
			out = append(out, ref)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		if out[i].Target != out[j].Target {
			return out[i].Target < out[j].Target
		}
		return out[i].Predicate < out[j].Predicate
	})
	return out
}

func edgeKey(e EdgeRef) string {
	return e.ID + "\x00" + e.Source + "\x00" + e.Predicate + "\x00" + e.Target + "\x00" + e.Layer
}

func round(v float64) float64 {
	return math.Round(v*10000) / 10000
}
