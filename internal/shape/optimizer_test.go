package shape

import "testing"

func TestOptimizeDetectsFlatStarAndEmitsStructuralContract(t *testing.T) {
	g := loadFixture(t)
	res := Optimize(g, Options{
		Predicate:         "part-of",
		Layer:             "SEMANTIC",
		ParentDirection:   "in",
		MaxDirectChildren: 4,
		GroupMinChildren:  2,
		GroupMaxChildren:  4,
		MinLeafRatio:      0.70,
	})

	if len(res.Candidates) != 1 {
		t.Fatalf("candidates = %d, want 1", len(res.Candidates))
	}
	c := res.Candidates[0]
	if c.Pattern != "flat_star_hub" {
		t.Fatalf("pattern = %q", c.Pattern)
	}
	if c.Center.ID != "root" {
		t.Fatalf("center = %q, want root", c.Center.ID)
	}
	if c.Metrics.DirectStructuralChildren != 11 {
		t.Fatalf("direct children = %d, want 11", c.Metrics.DirectStructuralChildren)
	}
	if c.Metrics.LeafChildren != 10 {
		t.Fatalf("leaf children = %d, want 10", c.Metrics.LeafChildren)
	}
	if c.Metrics.TargetGroupCount != 3 {
		t.Fatalf("target group count = %d, want 3", c.Metrics.TargetGroupCount)
	}
	if c.Metrics.TargetRootChildren != 4 {
		t.Fatalf("target root children = %d, want 4", c.Metrics.TargetRootChildren)
	}
	if !c.Metrics.Feasible {
		t.Fatalf("candidate should be feasible: %s", c.Metrics.InfeasibleReason)
	}
	if len(c.EditableSubgraph.ReplaceableEdges) != 10 {
		t.Fatalf("replaceable edges = %d, want 10", len(c.EditableSubgraph.ReplaceableEdges))
	}
	if len(c.BoundaryContext.PreservedDirectChildren) != 1 ||
		c.BoundaryContext.PreservedDirectChildren[0].ID != "existing-area" {
		t.Fatalf("preserved direct children = %+v", c.BoundaryContext.PreservedDirectChildren)
	}
	if len(c.TargetRewrite.NewGroupNodes) != 3 {
		t.Fatalf("new group nodes = %d, want 3", len(c.TargetRewrite.NewGroupNodes))
	}
	for _, e := range c.TargetRewrite.AddStructuralEdges {
		if e.Layer != "SEMANTIC" {
			t.Fatalf("add structural edge layer = %q, want SEMANTIC", e.Layer)
		}
	}
	if len(c.TargetRewrite.AssignmentConstraints.AssignLeaves) != 10 {
		t.Fatalf("assignment leaves = %d, want 10", len(c.TargetRewrite.AssignmentConstraints.AssignLeaves))
	}
	if len(c.TargetRewrite.GroupAssignments) != 3 {
		t.Fatalf("group assignments = %d, want 3", len(c.TargetRewrite.GroupAssignments))
	}
	gotSizes := []int{
		len(c.TargetRewrite.GroupAssignments[0].LeafChildren),
		len(c.TargetRewrite.GroupAssignments[1].LeafChildren),
		len(c.TargetRewrite.GroupAssignments[2].LeafChildren),
	}
	wantSizes := []int{4, 3, 3}
	for i := range wantSizes {
		if gotSizes[i] != wantSizes[i] {
			t.Fatalf("group assignment sizes = %v, want %v", gotSizes, wantSizes)
		}
	}
	if len(c.TargetRewrite.MaterializedStructuralEdges) != 13 {
		t.Fatalf("materialized structural edges = %d, want 13", len(c.TargetRewrite.MaterializedStructuralEdges))
	}
	if !c.MaterializationTask.ChooseSemanticGroupNames || c.MaterializationTask.AssignLeavesBySemantics {
		t.Fatalf("materialization task should leave names to LLM but keep tool-generated assignments")
	}
}
