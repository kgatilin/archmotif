package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const graphMLScanFixture = `<?xml version="1.0"?>
<graphml xmlns="http://graphml.graphdrawing.org/xmlns">
  <key id="n_id" for="node" attr.name="archmotif_id" attr.type="string"/>
  <key id="n_kind" for="node" attr.name="kind" attr.type="string"/>
  <graph id="G" edgedefault="directed">
    <node id="n0"><data key="n_id">orphan_a</data><data key="n_kind">stub</data></node>
    <node id="n1"><data key="n_id">orphan_b</data><data key="n_kind">stub</data></node>
    <node id="n2"><data key="n_id">orphan_c</data><data key="n_kind">stub</data></node>
    <node id="n3"><data key="n_id">attached_a</data><data key="n_kind">type</data></node>
    <node id="n4"><data key="n_id">attached_b</data><data key="n_kind">type</data></node>
    <edge id="e0" source="n3" target="n4"/>
  </graph>
</graphml>`

func TestGraphMLScanCommand(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.graphml")
	if err := os.WriteFile(path, []byte(graphMLScanFixture), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"graphml-scan", path}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, `"version": 1`) {
		t.Errorf("expected version field, got %q", out)
	}
	if !strings.Contains(out, "metricEvidence") {
		t.Errorf("expected metricEvidence in output, got %q", out)
	}
	if !strings.Contains(out, "beforeMetrics") {
		t.Errorf("expected beforeMetrics in output, got %q", out)
	}
	if !strings.Contains(out, "targetMetrics") {
		t.Errorf("expected targetMetrics in output, got %q", out)
	}
	if !strings.Contains(out, "selectionReason") {
		t.Errorf("expected selectionReason in output, got %q", out)
	}
	if !strings.Contains(out, "orphan_bucket") {
		t.Errorf("expected orphan_bucket finding, got %q", out)
	}
}

func TestGraphMLScanCommandList(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"graphml-scan", "--list"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	for _, name := range []string{"orphan_bucket", "duplicate_titles", "label_entropy_hub", "hierarchy_cycle", "articulation", "community_parent_mismatch"} {
		if !strings.Contains(stdout.String(), name) {
			t.Errorf("expected detector %q in --list output, got %q", name, stdout.String())
		}
	}
}

func TestGraphMLScanCommandMissingArg(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"graphml-scan"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
}
