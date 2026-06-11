package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestMetricsCommand_List(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"metrics", "--list"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	for _, want := range []string{"cycle_rank", "spectral_gap", "modularity", "motif_redundancy", "local_symmetry", "zero"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("missing metric %q in --list output", want)
		}
	}
}

func TestMetricsCommand_RunOnGraphPackage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	// Use a small package so the test stays fast.
	code := run([]string{"metrics", "--metric", "cycle_rank", "--format", "pretty", "../../internal/graph"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "cycle_rank") {
		t.Fatalf("expected cycle_rank in stdout, got %q", out)
	}
}

func TestMetricsCommand_WithoutPath(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"metrics"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "Usage") {
		t.Fatalf("expected Usage in stderr, got %q", stderr.String())
	}
}

func TestMetricsCommand_UnknownMetricExits1(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"metrics", "--metric", "nope", "../../internal/graph"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
}
