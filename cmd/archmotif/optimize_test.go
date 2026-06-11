package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOptimizeCommandJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"optimize",
		"--max-direct-children", "4",
		"--group-min-children", "2",
		"--group-max-children", "4",
		"../../testdata/shape/flat-star.graphml",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	var decoded struct {
		Candidates []struct {
			Pattern string `json:"pattern"`
			Metrics struct {
				TargetGroupCount int `json:"targetGroupCount"`
			} `json:"metrics"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("decode optimize json: %v\n%s", err, stdout.String())
	}
	if len(decoded.Candidates) != 1 {
		t.Fatalf("candidates = %d, want 1", len(decoded.Candidates))
	}
	if decoded.Candidates[0].Pattern != "flat_star_hub" {
		t.Fatalf("pattern = %q", decoded.Candidates[0].Pattern)
	}
	if decoded.Candidates[0].Metrics.TargetGroupCount != 3 {
		t.Fatalf("group count = %d, want 3", decoded.Candidates[0].Metrics.TargetGroupCount)
	}
}

func TestOptimizeCommandTable(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"optimize",
		"--format", "table",
		"--max-direct-children", "4",
		"--group-min-children", "2",
		"--group-max-children", "4",
		"../../testdata/shape/flat-star.graphml",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	for _, want := range []string{"flat_star_hub", "Root subsystem", "true"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("table output missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestOptimizeArchitectureCommandEmitsTargetGraphML(t *testing.T) {
	dir := writeMultiImplFixture(t)
	out := filepath.Join(t.TempDir(), "target.graphml")
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"optimize",
		"--mode=architecture",
		"--target-graphml-out", out,
		dir,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}

	var decoded struct {
		Mode      string `json:"mode"`
		Contracts []struct {
			Kind   string `json:"kind"`
			Target struct {
				Graph struct {
					Nodes []struct {
						ID   string `json:"id"`
						Role string `json:"role"`
					} `json:"nodes"`
					Edges []struct {
						Kind string `json:"kind"`
					} `json:"edges"`
				} `json:"graph"`
			} `json:"target"`
			ExpectedMetricMovement []struct {
				Metric    string `json:"metric"`
				Direction string `json:"direction"`
			} `json:"expectedMetricMovement"`
		} `json:"contracts"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("decode optimize architecture json: %v\n%s", err, stdout.String())
	}
	if decoded.Mode != "architecture" {
		t.Fatalf("mode = %q, want architecture", decoded.Mode)
	}
	if len(decoded.Contracts) == 0 {
		t.Fatalf("expected at least one optimization contract:\nstdout=%s\nstderr=%s", stdout.String(), stderr.String())
	}
	top := decoded.Contracts[0]
	if top.Kind != "motif_quotient_extract_interface" {
		t.Fatalf("contract kind = %q, want motif_quotient_extract_interface", top.Kind)
	}
	if len(top.Target.Graph.Nodes) == 0 || len(top.Target.Graph.Edges) == 0 {
		t.Fatalf("target graph is empty: %+v", top.Target.Graph)
	}
	hasImplements := false
	for _, e := range top.Target.Graph.Edges {
		if e.Kind == "implements" {
			hasImplements = true
		}
	}
	if !hasImplements {
		t.Fatalf("target edges missing implements: %+v", top.Target.Graph.Edges)
	}
	if len(top.ExpectedMetricMovement) == 0 || top.ExpectedMetricMovement[0].Metric != "motif_redundancy" || top.ExpectedMetricMovement[0].Direction != "decrease" {
		t.Fatalf("unexpected metric movement: %+v", top.ExpectedMetricMovement)
	}

	graphML, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read target graphml: %v", err)
	}
	text := string(graphML)
	for _, want := range []string{"target:", "implements", "Iface"} {
		if !strings.Contains(text, want) {
			t.Fatalf("target graphml missing %q:\n%s", want, text)
		}
	}
}
