package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strings"

	"github.com/kgatilin/archmotif/internal/shape"
)

// runOptimizeBatch implements `archmotif optimize-batch`.
//
// It is the pipeline entrypoint for recursive graph cleanup: each invocation
// selects exactly one deterministic next batch and optionally writes a
// materializer-agent prompt for that batch.
func runOptimizeBatch(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("archmotif optimize-batch", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("format", "summary", "output format: summary|json")
	contractOut := fs.String("contract-out", "", "path to write the selected batch contract JSON")
	promptOut := fs.String("prompt-out", "", "path to write a materializer-agent prompt")
	patchOut := fs.String("patch-out", "", "patch path to embed in the generated prompt")
	predicate := fs.String("predicate", "part-of", "structural edge predicate to optimize")
	layer := fs.String("layer", "", "optional edge layer filter")
	parentDirection := fs.String("parent-direction", "in", "structural direction: in/child-to-parent or out/parent-to-child")
	maxDirect := fs.Int("max-direct-children", 12, "target max direct structural children per hub")
	groupMin := fs.Int("group-min-children", 4, "target min children per introduced group")
	groupMax := fs.Int("group-max-children", 12, "target max children per introduced group")
	minLeafRatio := fs.Float64("min-leaf-ratio", 0.70, "minimum direct-child leaf ratio for flat-star detection")
	orphanBatchSize := fs.Int("orphan-batch-size", 64, "hard cap for orphan nodes in one orphan batch")
	contextBudgetBytes := fs.Int("context-budget-bytes", 12000, "approximate max contract context bytes for selected orphan refs; 0 disables")
	orphanAnchorID := fs.String("orphan-anchor-id", "", "existing node id used as parent anchor for orphan batches")
	orphanAnchorLabel := fs.String("orphan-anchor-label", "_unplaced", "existing node label used as parent anchor for orphan batches")
	ignoreOrphans := fs.Bool("ignore-orphans", false, "skip orphan batches and select only structural hub batches")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage:\n  archmotif optimize-batch [flags] <graphml-file>\n\nFlags:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() < 1 {
		fs.Usage()
		return 2
	}
	dir := normalizeDirection(*parentDirection)
	if dir == "" {
		_, _ = fmt.Fprintf(stderr, "archmotif optimize-batch: unsupported parent direction %q (want in|child-to-parent|out|parent-to-child)\n", *parentDirection)
		return 2
	}

	f, err := os.Open(fs.Arg(0))
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif optimize-batch: open %s: %v\n", fs.Arg(0), err)
		return 1
	}
	defer func() { _ = f.Close() }()

	g, err := shape.ReadGraphML(f)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif optimize-batch: %v\n", err)
		return 1
	}

	opts := shape.Options{
		Predicate:         *predicate,
		Layer:             *layer,
		ParentDirection:   dir,
		MaxDirectChildren: *maxDirect,
		GroupMinChildren:  *groupMin,
		GroupMaxChildren:  *groupMax,
		MinLeafRatio:      *minLeafRatio,
	}
	batch := selectOptimizeBatch(g, opts, batchSelectionOptions{
		IgnoreOrphans:      *ignoreOrphans,
		OrphanBatchSize:    *orphanBatchSize,
		ContextBudgetBytes: *contextBudgetBytes,
		OrphanAnchorID:     *orphanAnchorID,
		OrphanAnchorLabel:  *orphanAnchorLabel,
	})
	if isDeterministicDeleteBatch(batch) {
		batch.Materializer.Mode = "deterministic_delete"
		batch.Materializer.Notes = append(batch.Materializer.Notes,
			"Selected orphan batch is WORKING/session scratch; ArchMotif can emit a deterministic delete patch without an LLM.")
	}

	if *contractOut != "" {
		if err := writeJSONFile(*contractOut, batch); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif optimize-batch: write contract: %v\n", err)
			return 1
		}
	}
	if *patchOut != "" && batch.Materializer.Mode == "deterministic_delete" {
		patch := renderDeterministicDeletePatch(batch, *contractOut)
		if err := writeJSONFile(*patchOut, patch); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif optimize-batch: write deterministic patch: %v\n", err)
			return 1
		}
	}
	if *promptOut != "" {
		promptPatchOut := *patchOut
		if promptPatchOut == "" {
			promptPatchOut = strings.TrimSuffix(*promptOut, ".md") + "-patch.json"
		}
		prompt := renderOptimizeBatchPrompt(batch, *contractOut, promptPatchOut)
		if err := os.WriteFile(*promptOut, []byte(prompt), 0o644); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif optimize-batch: write prompt: %v\n", err)
			return 1
		}
	}

	switch *format {
	case "json":
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(batch); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif optimize-batch: write json: %v\n", err)
			return 1
		}
	case "summary", "":
		writeOptimizeBatchSummary(stdout, batch, *contractOut, *promptOut)
	default:
		_, _ = fmt.Fprintf(stderr, "archmotif optimize-batch: unknown format %q (want: summary|json)\n", *format)
		return 2
	}
	return 0
}

type batchSelectionOptions struct {
	IgnoreOrphans      bool
	OrphanBatchSize    int
	ContextBudgetBytes int
	OrphanAnchorID     string
	OrphanAnchorLabel  string
}

type optimizeBatch struct {
	Version       int                `json:"version"`
	Kind          string             `json:"kind"`
	Input         batchInput         `json:"input"`
	Target        shape.TargetInfo   `json:"target"`
	Selection     batchSelection     `json:"selection"`
	FlatStar      *shape.Candidate   `json:"flatStar,omitempty"`
	OrphanBatch   *orphanBatch       `json:"orphanBatch,omitempty"`
	Materializer  materializerPolicy `json:"materializer"`
	NoBatchReason string             `json:"noBatchReason,omitempty"`
}

type batchInput struct {
	Nodes           int    `json:"nodes"`
	Edges           int    `json:"edges"`
	Predicate       string `json:"predicate"`
	Layer           string `json:"layer,omitempty"`
	ParentDirection string `json:"parentDirection"`
	OrphanNodes     int    `json:"orphanNodes"`
}

type batchSelection struct {
	Policy              string   `json:"policy"`
	SelectedReason      string   `json:"selectedReason,omitempty"`
	SkippedCandidates   []string `json:"skippedCandidates,omitempty"`
	OrphanBucketLabel   string   `json:"orphanBucketLabel,omitempty"`
	OrphanAnchorMissing bool     `json:"orphanAnchorMissing,omitempty"`
}

type orphanBatch struct {
	ID                  string                    `json:"id"`
	Pattern             string                    `json:"pattern"`
	Score               float64                   `json:"score"`
	Anchor              shape.NodeRef             `json:"anchor"`
	Metrics             orphanMetrics             `json:"metrics"`
	EditableSubgraph    orphanEditableSubgraph    `json:"editableSubgraph"`
	BoundaryContext     orphanBoundaryContext     `json:"boundaryContext"`
	TargetRewrite       orphanTargetRewrite       `json:"targetRewrite"`
	MaterializationTask orphanMaterializationTask `json:"materializationTask"`
}

type orphanMetrics struct {
	TotalOrphansBefore   int     `json:"totalOrphansBefore"`
	SelectedOrphans      int     `json:"selectedOrphans"`
	Bucket               string  `json:"bucket,omitempty"`
	ContextBudgetBytes   int     `json:"contextBudgetBytes,omitempty"`
	EstimatedContextUsed int     `json:"estimatedContextUsed,omitempty"`
	TargetGroupCount     int     `json:"targetGroupCount"`
	GroupMinChildren     int     `json:"groupMinChildren"`
	GroupMaxChildren     int     `json:"groupMaxChildren"`
	ExpectedOrphansAfter int     `json:"expectedOrphansAfter"`
	OrphanReduction      int     `json:"orphanReduction"`
	Score                float64 `json:"score"`
}

type orphanEditableSubgraph struct {
	Orphans             []shape.NodeRef `json:"orphans"`
	EditableDescription string          `json:"editableDescription"`
}

type orphanBoundaryContext struct {
	Anchor shape.NodeRef `json:"anchor"`
	Rule   string        `json:"rule"`
}

type orphanTargetRewrite struct {
	Operation             string                      `json:"operation"`
	AllowedActions        []string                    `json:"allowedActions,omitempty"`
	GroupCount            int                         `json:"groupCount"`
	MaxNewMemories        int                         `json:"maxNewMemories"`
	MaxNewRelations       int                         `json:"maxNewRelations"`
	MaxRemoveMemories     int                         `json:"maxRemoveMemories"`
	NewGroupNodes         []shape.GroupNode           `json:"newGroupNodes"`
	AddStructuralEdges    []shape.EdgeRef             `json:"addStructuralEdges"`
	AssignmentConstraints orphanAssignmentConstraints `json:"assignmentConstraints"`
	Validation            orphanRewriteValidation     `json:"validation"`
}

type orphanAssignmentConstraints struct {
	AssignOrphans                 []shape.NodeRef `json:"assignOrphans"`
	EachOrphanAssignedExactlyOnce bool            `json:"eachOrphanAssignedExactlyOnce"`
	UseOnlyNewGroupNodes          bool            `json:"useOnlyNewGroupNodes"`
	GroupMinChildren              int             `json:"groupMinChildren"`
	GroupMaxChildren              int             `json:"groupMaxChildren"`
	FinalGroupMayBeSmaller        bool            `json:"finalGroupMayBeSmaller"`
}

type orphanRewriteValidation struct {
	BeforeOrphanCount    int  `json:"beforeOrphanCount"`
	AfterOrphanCount     int  `json:"afterOrphanCount"`
	SelectedOrphansAfter int  `json:"selectedOrphansAfter"`
	OriginalNodesDeleted int  `json:"originalNodesDeleted"`
	NewOrphansCreated    int  `json:"newOrphansCreated"`
	Feasible             bool `json:"feasible"`
}

type orphanMaterializationTask struct {
	FetchAdditionalMemoryContext bool     `json:"fetchAdditionalMemoryContext"`
	ChooseSemanticGroupNames     bool     `json:"chooseSemanticGroupNames"`
	AssignOrphansBySemantics     bool     `json:"assignOrphansBySemantics"`
	DoNotParseForDetection       bool     `json:"doNotParseForDetection"`
	Instructions                 []string `json:"instructions"`
}

type materializerPolicy struct {
	Output string   `json:"output"`
	Mode   string   `json:"mode"`
	Notes  []string `json:"notes"`
}

func selectOptimizeBatch(g *shape.Graph, opts shape.Options, selOpts batchSelectionOptions) optimizeBatch {
	opts = normalizeShapeOptionsForBatch(opts)
	orphanCount := countOrphans(g)
	batch := optimizeBatch{
		Version: 1,
		Input: batchInput{
			Nodes:           len(g.Nodes),
			Edges:           len(g.Edges),
			Predicate:       opts.Predicate,
			Layer:           opts.Layer,
			ParentDirection: opts.ParentDirection,
			OrphanNodes:     orphanCount,
		},
		Target: shape.TargetInfo{
			MaxDirectChildren: opts.MaxDirectChildren,
			GroupMinChildren:  opts.GroupMinChildren,
			GroupMaxChildren:  opts.GroupMaxChildren,
			MinLeafRatio:      opts.MinLeafRatio,
		},
		Selection: batchSelection{
			Policy: "orphans first by cleanup-priority bucket within context budget, then first feasible flat-star hub by score",
		},
		Materializer: materializerPolicy{
			Output: "memory-apply-json-patch",
			Mode:   "llm",
			Notes: []string{
				"Selection and target topology are deterministic; semantic work is limited to naming generated groups and explaining the tool-generated assignment.",
				"Run this command again after applying and exporting a fresh GraphML snapshot.",
			},
		},
	}

	if !selOpts.IgnoreOrphans && orphanCount > 0 {
		if anchorID := findOrphanAnchor(g, selOpts.OrphanAnchorID, selOpts.OrphanAnchorLabel); anchorID != "" {
			batch.Kind = "orphan_batch"
			batch.Selection.SelectedReason = "degree-zero nodes are not traversable; orphan batches have priority"
			batch.OrphanBatch = buildOrphanBatch(g, opts, orphanCount, anchorID, selOpts)
			batch.Selection.OrphanBucketLabel = batch.OrphanBatch.Metrics.Bucket
			return batch
		}
		batch.Selection.OrphanAnchorMissing = true
		batch.Selection.SkippedCandidates = append(batch.Selection.SkippedCandidates,
			fmt.Sprintf("skipped %d orphan nodes: no orphan anchor found by id=%q label=%q", orphanCount, selOpts.OrphanAnchorID, selOpts.OrphanAnchorLabel))
	}

	res := shape.Optimize(g, opts)
	for _, c := range res.Candidates {
		if !c.Metrics.Feasible {
			batch.Selection.SkippedCandidates = append(batch.Selection.SkippedCandidates,
				fmt.Sprintf("%s %q: %s", c.ID, c.Center.Label, c.Metrics.InfeasibleReason))
			continue
		}
		cc := c
		batch.Kind = "flat_star_hub"
		batch.Selection.SelectedReason = "first feasible flat-star hub by deterministic score order"
		batch.FlatStar = &cc
		return batch
	}

	batch.Kind = "none"
	batch.NoBatchReason = "no orphan batch anchor and no feasible flat-star hub candidates"
	return batch
}

func normalizeShapeOptionsForBatch(opts shape.Options) shape.Options {
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
	return opts
}

func countOrphans(g *shape.Graph) int {
	n := 0
	for _, node := range g.Nodes {
		if node.Degree == 0 {
			n++
		}
	}
	return n
}

func findOrphanAnchor(g *shape.Graph, id, label string) string {
	if id != "" {
		if _, ok := g.Nodes[id]; ok {
			return id
		}
	}
	if label == "" {
		return ""
	}
	matches := []string{}
	for id, n := range g.Nodes {
		if n.Label == label || n.Attrs["memory_title"] == label || n.Attrs["label"] == label || n.Attrs["name"] == label {
			matches = append(matches, id)
		}
	}
	sort.Strings(matches)
	if len(matches) == 0 {
		return ""
	}
	return matches[0]
}

func buildOrphanBatch(g *shape.Graph, opts shape.Options, orphanCount int, anchorID string, selOpts batchSelectionOptions) *orphanBatch {
	selected, bucket, estimatedContext := selectOrphans(g, selOpts.OrphanBatchSize, selOpts.ContextBudgetBytes)
	groupCount := int(math.Ceil(float64(len(selected)) / float64(opts.GroupMaxChildren)))
	if groupCount < 1 {
		groupCount = 1
	}
	groupNodes := make([]shape.GroupNode, 0, groupCount)
	addEdges := make([]shape.EdgeRef, 0, groupCount)
	rewriteOpts := opts
	if rewriteOpts.Layer == "" {
		rewriteOpts.Layer = "SEMANTIC"
	}
	for i := 1; i <= groupCount; i++ {
		tempID := fmt.Sprintf("$orphan_group_%03d", i)
		groupNodes = append(groupNodes, shape.GroupNode{
			TempID:          tempID,
			Role:            "intermediate_semantic_orphan_group",
			TargetMinLeaves: opts.GroupMinChildren,
			TargetMaxLeaves: opts.GroupMaxChildren,
		})
		addEdges = append(addEdges, batchStructuralEdgeRef(tempID, anchorID, rewriteOpts))
	}
	score := float64(len(selected))
	after := orphanCount - len(selected)
	maxNewMemories := groupCount
	maxNewRelations := len(selected) + maxNewMemories
	return &orphanBatch{
		ID:      "orphan-batch-001",
		Pattern: "orphan_batch",
		Score:   score,
		Anchor:  g.NodeRefFor(anchorID),
		Metrics: orphanMetrics{
			TotalOrphansBefore:   orphanCount,
			SelectedOrphans:      len(selected),
			Bucket:               bucket,
			ContextBudgetBytes:   selOpts.ContextBudgetBytes,
			EstimatedContextUsed: estimatedContext,
			TargetGroupCount:     groupCount,
			GroupMinChildren:     opts.GroupMinChildren,
			GroupMaxChildren:     opts.GroupMaxChildren,
			ExpectedOrphansAfter: after,
			OrphanReduction:      len(selected),
			Score:                score,
		},
		EditableSubgraph: orphanEditableSubgraph{
			Orphans:             selected,
			EditableDescription: "Resolve only the selected orphan nodes. Each selected node may be deleted as noise, consolidated into a new connected memory, or kept and attached through a connected relation. Nodes outside this selected batch are immutable.",
		},
		BoundaryContext: orphanBoundaryContext{
			Anchor: g.NodeRefFor(anchorID),
			Rule:   "Do not touch nodes outside the selected orphan batch. Any added memory must be connected to the anchor. Relation removal is not allowed for orphan batches.",
		},
		TargetRewrite: orphanTargetRewrite{
			Operation: "resolve_orphans_with_cleanup",
			AllowedActions: []string{
				"delete_selected_noise_memory",
				"consolidate_selected_memories_into_connected_summary",
				"attach_selected_meaningful_memory_to_connected_group",
			},
			GroupCount:         groupCount,
			MaxNewMemories:     maxNewMemories,
			MaxNewRelations:    maxNewRelations,
			MaxRemoveMemories:  len(selected),
			NewGroupNodes:      groupNodes,
			AddStructuralEdges: addEdges,
			AssignmentConstraints: orphanAssignmentConstraints{
				AssignOrphans:                 selected,
				EachOrphanAssignedExactlyOnce: true,
				UseOnlyNewGroupNodes:          true,
				GroupMinChildren:              opts.GroupMinChildren,
				GroupMaxChildren:              opts.GroupMaxChildren,
				FinalGroupMayBeSmaller:        true,
			},
			Validation: orphanRewriteValidation{
				BeforeOrphanCount:    orphanCount,
				AfterOrphanCount:     after,
				SelectedOrphansAfter: 0,
				OriginalNodesDeleted: len(selected),
				NewOrphansCreated:    0,
				Feasible:             true,
			},
		},
		MaterializationTask: orphanMaterializationTask{
			FetchAdditionalMemoryContext: true,
			ChooseSemanticGroupNames:     true,
			AssignOrphansBySemantics:     true,
			DoNotParseForDetection:       true,
			Instructions: []string{
				"Use this orphan batch contract as the source of truth; do not choose a different selected set.",
				"Fetch additional memory context for selected orphan IDs/titles only if available and only within the context budget.",
				"Delete selected orphan memories that are clearly transient, duplicated, or non-retrievable noise.",
				"For meaningful clusters, create at most the requested number of connected summary/group memories.",
				"Every added memory must be connected to the anchor with a structural relation.",
				"Every selected orphan must be resolved: either removed, or connected to the anchor/a connected new group.",
				"Do not remove relations in orphan batches.",
				"Do not include orphan nodes outside this selected batch.",
				fmt.Sprintf("Selection bucket: %s.", bucket),
			},
		},
	}
}

func batchStructuralEdgeRef(child, parent string, opts shape.Options) shape.EdgeRef {
	if opts.ParentDirection == "out" || opts.ParentDirection == "parent-to-child" {
		return shape.EdgeRef{Source: parent, Target: child, Predicate: opts.Predicate, Layer: opts.Layer}
	}
	return shape.EdgeRef{Source: child, Target: parent, Predicate: opts.Predicate, Layer: opts.Layer}
}

func (b *orphanBatch) MetricsBucketLabel() string {
	if b == nil || len(b.EditableSubgraph.Orphans) == 0 {
		return ""
	}
	counts := map[string]int{}
	for _, n := range b.EditableSubgraph.Orphans {
		for _, label := range splitLabels(n.Attrs["labels"]) {
			counts[label]++
		}
	}
	return bestLabel(counts)
}

func selectOrphans(g *shape.Graph, max, contextBudgetBytes int) ([]shape.NodeRef, string, int) {
	if max <= 0 {
		max = 64
	}
	orphans := []shape.NodeRef{}
	labelCounts := map[string]int{}
	for id, n := range g.Nodes {
		if n.Degree != 0 {
			continue
		}
		ref := g.NodeRefFor(id)
		orphans = append(orphans, ref)
		for _, label := range splitLabels(ref.Attrs["labels"]) {
			labelCounts[label]++
		}
	}
	sort.SliceStable(orphans, func(i, j int) bool {
		if orphans[i].Label != orphans[j].Label {
			return orphans[i].Label < orphans[j].Label
		}
		return orphans[i].ID < orphans[j].ID
	})
	bucket := chooseOrphanBucket(orphans, labelCounts)
	filtered := []shape.NodeRef{}
	switch {
	case strings.HasPrefix(bucket, "memoryType:"):
		want := strings.TrimPrefix(bucket, "memoryType:")
		for _, n := range orphans {
			if strings.EqualFold(n.Attrs["memory_type"], want) {
				filtered = append(filtered, n)
			}
		}
	case strings.HasPrefix(bucket, "entityType:"):
		want := strings.TrimPrefix(bucket, "entityType:")
		for _, n := range orphans {
			if strings.EqualFold(n.Attrs["entity_type"], want) {
				filtered = append(filtered, n)
			}
		}
	case bucket != "":
		for _, n := range orphans {
			if hasLabel(n.Attrs["labels"], bucket) {
				filtered = append(filtered, n)
			}
		}
	default:
		filtered = orphans
	}
	if len(filtered) > max {
		filtered = filtered[:max]
	}
	estimated := estimateOrphanBatchContext(filtered)
	if contextBudgetBytes > 0 {
		filtered, estimated = limitOrphansByContextBudget(filtered, contextBudgetBytes)
	}
	return filtered, bucket, estimated
}

func chooseOrphanBucket(orphans []shape.NodeRef, labelCounts map[string]int) string {
	for _, memoryType := range []string{"WORKING", "EPISODIC"} {
		for _, n := range orphans {
			if strings.EqualFold(n.Attrs["memory_type"], memoryType) {
				return "memoryType:" + memoryType
			}
		}
	}
	for _, entityType := range []string{"session", "event", "issue"} {
		for _, n := range orphans {
			if strings.EqualFold(n.Attrs["entity_type"], entityType) {
				return "entityType:" + entityType
			}
		}
	}
	return bestLabel(labelCounts)
}

func limitOrphansByContextBudget(nodes []shape.NodeRef, budget int) ([]shape.NodeRef, int) {
	if len(nodes) <= 1 || budget <= 0 {
		return nodes, estimateOrphanBatchContext(nodes)
	}
	selected := []shape.NodeRef{}
	for _, n := range nodes {
		candidate := append(selected, n)
		estimated := estimateOrphanBatchContext(candidate)
		if len(selected) > 0 && estimated > budget {
			return selected, estimateOrphanBatchContext(selected)
		}
		selected = candidate
	}
	return selected, estimateOrphanBatchContext(selected)
}

func estimateOrphanBatchContext(nodes []shape.NodeRef) int {
	const baseContractBytes = 7000
	total := baseContractBytes
	for _, n := range nodes {
		raw, err := json.Marshal(n)
		if err != nil {
			total += 512
			continue
		}
		// Node refs appear more than once in the contract and prompt. The
		// multiplier keeps the selector conservative without making it depend
		// on exact JSON layout.
		total += len(raw)*3 + 160
	}
	return total
}

func splitLabels(raw string) []string {
	parts := strings.Split(raw, ",")
	out := []string{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func hasLabel(raw, want string) bool {
	for _, label := range splitLabels(raw) {
		if label == want {
			return true
		}
	}
	return false
}

func bestLabel(counts map[string]int) string {
	best := ""
	bestCount := 0
	for label, count := range counts {
		if count > bestCount || (count == bestCount && (best == "" || label < best)) {
			best = label
			bestCount = count
		}
	}
	return best
}

func writeJSONFile(path string, v any) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

type memoryApplyPatch struct {
	Metadata map[string]any    `json:"metadata"`
	Remove   memoryPatchRemove `json:"remove,omitempty"`
}

type memoryPatchRemove struct {
	MemoryIDs   []string `json:"memoryIds,omitempty"`
	RelationIDs []string `json:"relationIds,omitempty"`
}

func isDeterministicDeleteBatch(batch optimizeBatch) bool {
	if batch.Kind != "orphan_batch" || batch.OrphanBatch == nil {
		return false
	}
	orphans := batch.OrphanBatch.EditableSubgraph.Orphans
	if len(orphans) == 0 {
		return false
	}
	for _, n := range orphans {
		memoryType := firstAttr(n.Attrs, "memory_type", "memoryType")
		entityType := firstAttr(n.Attrs, "entity_type", "entityType")
		if !strings.EqualFold(memoryType, "WORKING") {
			return false
		}
		if !strings.EqualFold(entityType, "session") && !strings.HasPrefix(strings.ToLower(n.Label), "session ") {
			return false
		}
	}
	return true
}

func renderDeterministicDeletePatch(batch optimizeBatch, contractPath string) memoryApplyPatch {
	ids := []string{}
	if batch.OrphanBatch != nil {
		for _, n := range batch.OrphanBatch.EditableSubgraph.Orphans {
			ids = append(ids, n.ID)
		}
	}
	return memoryApplyPatch{
		Metadata: map[string]any{
			"draft":          true,
			"sourceContract": contractPath,
			"kind":           "orphan_batch",
			"materializer":   "archmotif-deterministic-delete",
			"reason":         "All selected orphan nodes are WORKING/session scratch memories; deleting them resolves degree-zero noise without adding replacement nodes.",
			"assignmentValidation": map[string]any{
				"orphanCount":                   len(ids),
				"removedSelectedMemories":       ids,
				"keptSelectedMemories":          []string{},
				"newMemoryCount":                0,
				"selectedOrphansResolved":       true,
				"newOrphansCreated":             false,
				"allRemovedIDsFromSelection":    true,
				"removedRelationsIntentionally": 0,
			},
			"note": "Generated by ArchMotif without LLM materialization. Review before applying with your memory store's apply command.",
		},
		Remove: memoryPatchRemove{
			MemoryIDs: ids,
		},
	}
}

func firstAttr(attrs map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(attrs[key]); value != "" {
			return value
		}
	}
	return ""
}

func writeOptimizeBatchSummary(w io.Writer, batch optimizeBatch, contractOut, promptOut string) {
	_, _ = fmt.Fprintf(w, "optimize-batch: kind=%s graph=%d nodes/%d edges orphans=%d\n",
		batch.Kind, batch.Input.Nodes, batch.Input.Edges, batch.Input.OrphanNodes)
	if batch.NoBatchReason != "" {
		_, _ = fmt.Fprintf(w, "reason: %s\n", batch.NoBatchReason)
	}
	switch batch.Kind {
	case "orphan_batch":
		b := batch.OrphanBatch
		_, _ = fmt.Fprintf(w, "selected: %s anchor=%s selected_orphans=%d/%d groups=%d expected_orphans_after=%d\n",
			b.Pattern, b.Anchor.Label, b.Metrics.SelectedOrphans, b.Metrics.TotalOrphansBefore, b.Metrics.TargetGroupCount, b.Metrics.ExpectedOrphansAfter)
	case "flat_star_hub":
		c := batch.FlatStar
		_, _ = fmt.Fprintf(w, "selected: %s center=%s children=%d leaves=%d groups=%d feasible=%t\n",
			c.Pattern, c.Center.Label, c.Metrics.DirectStructuralChildren, c.Metrics.LeafChildren, c.Metrics.TargetGroupCount, c.Metrics.Feasible)
	}
	if batch.Materializer.Mode != "" {
		_, _ = fmt.Fprintf(w, "materializer: %s\n", batch.Materializer.Mode)
	}
	if contractOut != "" {
		_, _ = fmt.Fprintf(w, "contract: %s\n", contractOut)
	}
	if promptOut != "" {
		_, _ = fmt.Fprintf(w, "prompt: %s\n", promptOut)
	}
}

func renderOptimizeBatchPrompt(batch optimizeBatch, contractPath, patchPath string) string {
	if contractPath == "" {
		contractPath = "<contract-json-path>"
	}
	var b strings.Builder
	b.WriteString("You are materializing one deterministic memory-graph optimization batch.\n\n")
	b.WriteString("Input contract:\n\n")
	fmt.Fprintf(&b, "`%s`\n\n", contractPath)
	b.WriteString("Output file to create:\n\n")
	fmt.Fprintf(&b, "`%s`\n\n", patchPath)
	b.WriteString("Do not apply the patch. Only write the patch file and report a concise validation summary.\n\n")
	b.WriteString("Use only the selected subgraph from the contract plus small, targeted context lookups if tools are available. Do not broaden the batch.\n\n")
	b.WriteString("Hard rule: the contract is authoritative for the editable set and validation targets. Do not edit original memory nodes outside the selected set, and do not remove edges unless the contract explicitly allows it.\n\n")
	if batch.Kind == "orphan_batch" {
		b.WriteString(renderOrphanPromptBody())
	} else {
		b.WriteString(renderFlatStarPromptBody())
	}
	b.WriteString("\nFinal response:\n\n")
	b.WriteString("- Path of the patch file.\n")
	b.WriteString("- Group names with child counts.\n")
	b.WriteString("- Counts: added memories, added relations, removed relations.\n")
	b.WriteString("- Any validation failures, or say validation passed.\n")
	return b.String()
}

func renderOrphanPromptBody() string {
	return `Task for kind=orphan_batch:

1. Read the input contract JSON.
2. Resolve every orphanBatch.editableSubgraph.orphans[] node using one allowed action:
   - delete_selected_noise_memory: add the selected memory ID to remove.memoryIds when it is transient WORKING/session noise, duplicated, or not useful for retrieval;
   - consolidate_selected_memories_into_connected_summary: create a concise summary/group memory, connect it to orphanBatch.anchor, and remove source memories that are fully superseded;
   - attach_selected_meaningful_memory_to_connected_group: keep a useful selected memory and add a relation from it to orphanBatch.anchor or to a new connected group memory.
3. Structural constraints:
   - only selected orphan memory IDs may be removed;
   - remove.relationIds must be absent or empty;
   - add.memories.length must be <= orphanBatch.targetRewrite.maxNewMemories;
   - add.relations.length must be <= orphanBatch.targetRewrite.maxNewRelations;
   - every added memory must have exactly one structural relation to orphanBatch.anchor;
   - every selected orphan must be resolved: either removed, or connected to the anchor/a connected new group;
   - nodes outside orphanBatch.editableSubgraph.orphans[] are immutable.
4. Semantic work:
   - prefer deletion for low-value WORKING/session scratch;
   - prefer consolidation when several selected memories say the same thing or form one retrieval concept;
   - prefer attach when a selected memory is already a good durable fact/procedure/decision.

Patch schema:

` + "```json" + `
{
  "metadata": {
    "draft": true,
    "sourceContract": "<contract path>",
    "kind": "orphan_batch",
    "anchor": {},
    "assignmentValidation": {
      "orphanCount": 0,
      "removedSelectedMemories": [],
      "keptSelectedMemories": [],
      "newMemoryCount": 0,
      "selectedOrphansResolved": true,
      "newOrphansCreated": false
    },
    "note": "Draft only. Review before applying with your memory store's apply command."
  },
  "add": {
    "memories": [
      {
        "tempId": "$orphan_group_001_semantic_name",
        "title": "Short semantic group title",
        "content": "Why these orphan memories belong together. Include assigned orphan IDs.",
        "memoryType": "SEMANTIC",
        "entityType": "topic",
        "labels": ["graph-optimization", "generated:draft"]
      }
    ],
    "relations": [
      {
        "source": "$orphan_group_001_semantic_name",
        "target": "<anchor_id>",
        "layer": "SEMANTIC",
        "predicate": "part-of",
        "weight": 1.0
      },
      {
        "source": "<orphan_id>",
        "target": "$orphan_group_001_semantic_name",
        "layer": "SEMANTIC",
        "predicate": "part-of",
        "weight": 1.0
      }
    ]
  },
  "remove": {
    "memoryIds": [
      "<selected_orphan_id_to_delete_or_consolidate>"
    ]
  }
}
` + "```" + `

Validation before final response:

- remove.memoryIds is a subset of selected orphan IDs
- remove.relationIds is absent or empty
- add.memories.length <= orphanBatch.targetRewrite.maxNewMemories
- add.relations.length <= orphanBatch.targetRewrite.maxNewRelations
- every added memory tempId has exactly one relation to orphanBatch.anchor
- every selected orphan ID is either in remove.memoryIds or appears in an added relation to orphanBatch.anchor/a connected group
- no memory outside the selected orphan IDs is removed
`
}

func renderFlatStarPromptBody() string {
	return `Task for kind=flat_star_hub:

1. Read the input contract JSON.
2. Use flatStar.targetRewrite exactly:
   - center node must stay unchanged;
   - all original memory nodes must stay unchanged;
   - boundary/preserved edges must stay unchanged;
   - only listed direct leaf -> center structural edges may be removed;
   - every listed leaf child must stay assigned according to flatStar.targetRewrite.groupAssignments;
   - create exactly the requested number of intermediate group nodes;
   - add the structural relations listed in flatStar.targetRewrite.materializedStructuralEdges;
   - remove only listed direct leaf -> center relation IDs.
3. Semantic work:
   - use memory context to name groups and explain the generated groups;
   - do not invent a different graph shape or different leaf assignment.

Patch schema:

` + "```json" + `
{
  "metadata": {
    "draft": true,
    "sourceContract": "<contract path>",
    "kind": "flat_star_hub",
    "center": {},
    "assignmentValidation": {
      "leafCount": 0,
      "groupCount": 0,
      "groupSizes": {},
      "allLeavesAssignedExactlyOnce": true
    },
    "note": "Draft only. Review before applying with your memory store's apply command."
  },
  "add": {
    "memories": [
      {
        "tempId": "$semantic_group_id",
        "title": "Short semantic group title",
        "content": "Why these leaves belong together. Include assigned leaf IDs.",
        "memoryType": "SEMANTIC",
        "entityType": "topic",
        "labels": ["graph-optimization", "generated:draft"]
      }
    ],
    "relations": [
      {
        "source": "$semantic_group_id",
        "target": "<center_id>",
        "layer": "SEMANTIC",
        "predicate": "part-of",
        "weight": 1.0
      },
      {
        "source": "<leaf_id>",
        "target": "$semantic_group_id",
        "layer": "SEMANTIC",
        "predicate": "part-of",
        "weight": 1.0
      }
    ]
  },
  "remove": {
    "relationIds": ["<direct_leaf_to_center_relation_id>"]
  }
}
` + "```" + `

Validation before final response:

- add.memories.length == flatStar.targetRewrite.groupCount
- add.relations matches flatStar.targetRewrite.materializedStructuralEdges after replacing group tempIds with the semantic group tempIds you chose
- remove.relationIds.length == flatStar.editableSubgraph.replaceableEdges.length
- every flatStar.editableSubgraph.leafChildren[].id appears exactly once as a source in a leaf -> group relation
- every removed relation ID is from flatStar.editableSubgraph.replaceableEdges
- no preserved edge ID is removed
- every group size is between groupMinChildren and groupMaxChildren
`
}
