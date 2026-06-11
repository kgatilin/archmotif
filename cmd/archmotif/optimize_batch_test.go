package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestOptimizeBatchSelectsOrphansFirst(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"optimize-batch",
		"--format", "json",
		"--orphan-batch-size", "2",
		filepath.Join("..", "..", "testdata", "shape", "orphans.graphml"),
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	var decoded struct {
		Kind        string `json:"kind"`
		OrphanBatch struct {
			Metrics struct {
				SelectedOrphans      int `json:"selectedOrphans"`
				ExpectedOrphansAfter int `json:"expectedOrphansAfter"`
			} `json:"metrics"`
			Anchor struct {
				Label string `json:"label"`
			} `json:"anchor"`
		} `json:"orphanBatch"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("decode optimize-batch json: %v\n%s", err, stdout.String())
	}
	if decoded.Kind != "orphan_batch" {
		t.Fatalf("kind = %q, want orphan_batch", decoded.Kind)
	}
	if decoded.OrphanBatch.Anchor.Label != "_unplaced" {
		t.Fatalf("anchor = %q, want _unplaced", decoded.OrphanBatch.Anchor.Label)
	}
	if decoded.OrphanBatch.Metrics.SelectedOrphans != 2 {
		t.Fatalf("selected orphans = %d, want 2", decoded.OrphanBatch.Metrics.SelectedOrphans)
	}
	if decoded.OrphanBatch.Metrics.ExpectedOrphansAfter != 2 {
		t.Fatalf("expected orphans after = %d, want 2", decoded.OrphanBatch.Metrics.ExpectedOrphansAfter)
	}
}

func TestOptimizeBatchFallsBackToFlatStar(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"optimize-batch",
		"--format", "json",
		"--max-direct-children", "4",
		"--group-min-children", "2",
		"--group-max-children", "4",
		filepath.Join("..", "..", "testdata", "shape", "flat-star.graphml"),
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	var decoded struct {
		Kind     string `json:"kind"`
		FlatStar struct {
			Center struct {
				Label string `json:"label"`
			} `json:"center"`
			Metrics struct {
				TargetGroupCount int  `json:"targetGroupCount"`
				Feasible         bool `json:"feasible"`
			} `json:"metrics"`
			TargetRewrite struct {
				GroupAssignments []struct {
					GroupTempID        string `json:"groupTempId"`
					LeafChildren       []any  `json:"leafChildren"`
					AddStructuralEdges []any  `json:"addStructuralEdges"`
				} `json:"groupAssignments"`
				MaterializedStructuralEdges []any `json:"materializedStructuralEdges"`
			} `json:"targetRewrite"`
		} `json:"flatStar"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("decode optimize-batch json: %v\n%s", err, stdout.String())
	}
	if decoded.Kind != "flat_star_hub" {
		t.Fatalf("kind = %q, want flat_star_hub", decoded.Kind)
	}
	if decoded.FlatStar.Center.Label != "Root subsystem" {
		t.Fatalf("center = %q, want Root subsystem", decoded.FlatStar.Center.Label)
	}
	if !decoded.FlatStar.Metrics.Feasible {
		t.Fatal("flat star candidate should be feasible")
	}
	if decoded.FlatStar.Metrics.TargetGroupCount != 3 {
		t.Fatalf("target groups = %d, want 3", decoded.FlatStar.Metrics.TargetGroupCount)
	}
	if len(decoded.FlatStar.TargetRewrite.GroupAssignments) != 3 {
		t.Fatalf("group assignments = %d, want 3", len(decoded.FlatStar.TargetRewrite.GroupAssignments))
	}
	if got := len(decoded.FlatStar.TargetRewrite.MaterializedStructuralEdges); got != 13 {
		t.Fatalf("materialized structural edges = %d, want 13", got)
	}
	if got := len(decoded.FlatStar.TargetRewrite.GroupAssignments[0].LeafChildren); got != 4 {
		t.Fatalf("first group leaf count = %d, want 4", got)
	}
}

func TestOptimizeBatchPrioritizesWorkingOrphans(t *testing.T) {
	graph := writeOptimizeBatchFixture(t, `<?xml version="1.0" encoding="UTF-8"?>
<graphml xmlns="http://graphml.graphdrawing.org/xmlns">
  <key id="memory_title" for="node" attr.name="memory_title" attr.type="string"/>
  <key id="memory_type" for="node" attr.name="memory_type" attr.type="string"/>
  <key id="entity_type" for="node" attr.name="entity_type" attr.type="string"/>
  <key id="labels" for="node" attr.name="labels" attr.type="string"/>
  <graph id="memory" edgedefault="directed">
    <node id="anchor"><data key="memory_title">_unplaced</data><data key="labels">phantom</data></node>
    <node id="semantic-a"><data key="memory_title">Durable fact</data><data key="memory_type">SEMANTIC</data><data key="entity_type">fact</data><data key="labels">project:demo</data></node>
    <node id="working-a"><data key="memory_title">Session scratch A</data><data key="memory_type">WORKING</data><data key="entity_type">session</data><data key="labels">workspace:main,project:demo</data></node>
    <node id="working-b"><data key="memory_title">Session scratch B</data><data key="memory_type">WORKING</data><data key="entity_type">session</data><data key="labels">workspace:main,project:demo</data></node>
  </graph>
</graphml>`)
	patchPath := filepath.Join(t.TempDir(), "patch.json")

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"optimize-batch",
		"--format", "json",
		"--context-budget-bytes", "0",
		"--patch-out", patchPath,
		graph,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	var decoded struct {
		Selection struct {
			OrphanBucketLabel string `json:"orphanBucketLabel"`
		} `json:"selection"`
		Materializer struct {
			Mode string `json:"mode"`
		} `json:"materializer"`
		OrphanBatch struct {
			Metrics struct {
				SelectedOrphans int    `json:"selectedOrphans"`
				Bucket          string `json:"bucket"`
			} `json:"metrics"`
			EditableSubgraph struct {
				Orphans []struct {
					ID    string            `json:"id"`
					Attrs map[string]string `json:"attrs"`
				} `json:"orphans"`
			} `json:"editableSubgraph"`
		} `json:"orphanBatch"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("decode optimize-batch json: %v\n%s", err, stdout.String())
	}
	if decoded.Selection.OrphanBucketLabel != "memoryType:WORKING" || decoded.OrphanBatch.Metrics.Bucket != "memoryType:WORKING" {
		t.Fatalf("bucket = selection %q metrics %q, want memoryType:WORKING", decoded.Selection.OrphanBucketLabel, decoded.OrphanBatch.Metrics.Bucket)
	}
	if decoded.Materializer.Mode != "deterministic_delete" {
		t.Fatalf("materializer mode = %q, want deterministic_delete", decoded.Materializer.Mode)
	}
	if decoded.OrphanBatch.Metrics.SelectedOrphans != 2 {
		t.Fatalf("selected orphans = %d, want 2", decoded.OrphanBatch.Metrics.SelectedOrphans)
	}
	for _, n := range decoded.OrphanBatch.EditableSubgraph.Orphans {
		if n.Attrs["memory_type"] != "WORKING" {
			t.Fatalf("selected non-working orphan: %s type=%s", n.ID, n.Attrs["memory_type"])
		}
	}
	patchRaw, err := os.ReadFile(patchPath)
	if err != nil {
		t.Fatalf("read deterministic patch: %v", err)
	}
	var patch struct {
		Remove struct {
			MemoryIDs []string `json:"memoryIds"`
		} `json:"remove"`
	}
	if err := json.Unmarshal(patchRaw, &patch); err != nil {
		t.Fatalf("decode deterministic patch: %v\n%s", err, string(patchRaw))
	}
	if got := patch.Remove.MemoryIDs; len(got) != 2 || got[0] != "working-a" || got[1] != "working-b" {
		t.Fatalf("removed memory IDs = %v, want [working-a working-b]", got)
	}
}

func TestOptimizeBatchLimitsOrphansByContextBudget(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"optimize-batch",
		"--format", "json",
		"--orphan-batch-size", "3",
		"--context-budget-bytes", "7500",
		filepath.Join("..", "..", "testdata", "shape", "orphans.graphml"),
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	var decoded struct {
		OrphanBatch struct {
			Metrics struct {
				SelectedOrphans      int `json:"selectedOrphans"`
				EstimatedContextUsed int `json:"estimatedContextUsed"`
			} `json:"metrics"`
		} `json:"orphanBatch"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("decode optimize-batch json: %v\n%s", err, stdout.String())
	}
	if decoded.OrphanBatch.Metrics.SelectedOrphans != 1 {
		t.Fatalf("selected orphans = %d, want 1", decoded.OrphanBatch.Metrics.SelectedOrphans)
	}
	if decoded.OrphanBatch.Metrics.EstimatedContextUsed <= 0 {
		t.Fatal("estimated context should be populated")
	}
}

func writeOptimizeBatchFixture(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "graph.graphml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}
