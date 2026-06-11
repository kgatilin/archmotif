package graphmlx

import (
	"bytes"
	"strings"
	"testing"
)

const combinedFixture = `<?xml version="1.0"?>
<graphml xmlns="http://graphml.graphdrawing.org/xmlns">
  <key id="n_id" for="node" attr.name="archmotif_id" attr.type="string"/>
  <key id="n_kind" for="node" attr.name="kind" attr.type="string"/>
  <key id="n_title" for="node" attr.name="title" attr.type="string"/>
  <key id="e_kind" for="edge" attr.name="kind" attr.type="string"/>
  <graph id="G" edgedefault="directed">
    <node id="n0"><data key="n_id">a</data><data key="n_kind">stub</data><data key="n_title">Same Title</data></node>
    <node id="n1"><data key="n_id">b</data><data key="n_kind">stub</data><data key="n_title">same title!</data></node>
    <node id="n2"><data key="n_id">c</data><data key="n_kind">stub</data></node>
    <node id="n3"><data key="n_id">d</data><data key="n_kind">stub</data></node>
  </graph>
</graphml>`

func TestOptimizeBatch_DeterministicAndComplete(t *testing.T) {
	g, err := Read(strings.NewReader(combinedFixture))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	res1 := OptimizeBatch(g, OptimizeBatchOptions{MaxBatchSize: 5})
	res2 := OptimizeBatch(g, OptimizeBatchOptions{MaxBatchSize: 5})

	var buf1, buf2 bytes.Buffer
	if err := res1.WriteJSON(&buf1); err != nil {
		t.Fatalf("WriteJSON 1: %v", err)
	}
	if err := res2.WriteJSON(&buf2); err != nil {
		t.Fatalf("WriteJSON 2: %v", err)
	}
	if buf1.String() != buf2.String() {
		t.Errorf("non-deterministic output:\n--first:\n%s\n--second:\n%s", buf1.String(), buf2.String())
	}

	// Should at least surface orphan_bucket and duplicate_titles findings.
	have := map[string]bool{}
	for _, b := range res1.Batch {
		have[b.Detector] = true
	}
	if !have["orphan_bucket"] {
		t.Errorf("missing orphan_bucket in batch: %+v", res1.Batch)
	}
	if !have["duplicate_titles"] {
		t.Errorf("missing duplicate_titles in batch: %+v", res1.Batch)
	}
	if res1.Version != CurrentJSONVersion {
		t.Errorf("version: got %d want %d", res1.Version, CurrentJSONVersion)
	}
	if res1.SelectionReason == "" {
		t.Error("selectionReason: empty")
	}
	if res1.GraphSummary.NodeCount != 4 {
		t.Errorf("graph nodes: got %d want 4", res1.GraphSummary.NodeCount)
	}
}

func TestOptimizeBatch_MinSeverityFilter(t *testing.T) {
	g, err := Read(strings.NewReader(combinedFixture))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	// All findings here are info/low severity (small fixture).
	// MinSeverity=critical should drop everything from the batch.
	res := OptimizeBatch(g, OptimizeBatchOptions{
		MaxBatchSize: 10,
		MinSeverity:  SeverityCritical,
	})
	if len(res.Batch) != 0 {
		t.Errorf("expected empty batch under min severity=critical, got %d", len(res.Batch))
	}
	// before-metrics still capture all findings
	if len(res.BeforeMetrics) == 0 {
		t.Error("beforeMetrics should be non-empty even when batch is empty")
	}
}

func TestOptimizeBatch_BatchCapped(t *testing.T) {
	g, err := Read(strings.NewReader(combinedFixture))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	res := OptimizeBatch(g, OptimizeBatchOptions{MaxBatchSize: 1})
	if len(res.Batch) != 1 {
		t.Errorf("expected 1 batched finding, got %d: %+v", len(res.Batch), res.Batch)
	}
}
