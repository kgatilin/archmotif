package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kgatilin/archmotif/internal/catalog"
)

// TestCatalogCommandRoundTrip wires the `catalog` and `drift`
// subcommands together end-to-end against a small Go package
// (internal/graph): capture two snapshots into a tempdir, then
// drift between them. Because the inputs are identical, the drift
// must report no metric / motif / pattern changes.
func TestCatalogCommandRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cat := filepath.Join(dir, "catalog.yaml")

	var stdout, stderr bytes.Buffer
	code := run([]string{"catalog", "--label", "before", "--catalog", cat, "../../internal/graph"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("catalog before: code=%d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `snapshot "before" saved`) {
		t.Fatalf("expected confirmation, got %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"catalog", "--label", "after", "--catalog", cat, "../../internal/graph"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("catalog after: code=%d stderr=%q", code, stderr.String())
	}

	// File on disk parses as a valid catalog with two snapshots.
	c, err := catalog.Load(cat)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(c.Snapshots) != 2 {
		t.Fatalf("snapshots: got %d, want 2", len(c.Snapshots))
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"drift", "--from", "before", "--to", "after", "--catalog", cat, "--format", "text"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("drift: code=%d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "no changes") {
		t.Fatalf("expected 'no changes' on identical snapshots, got %q", stdout.String())
	}

	// JSON format parses as a Drift with version 1.
	stdout.Reset()
	stderr.Reset()
	code = run([]string{"drift", "--from", "before", "--to", "after", "--catalog", cat, "--format", "json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("drift json: code=%d stderr=%q", code, stderr.String())
	}
	var d catalog.Drift
	if err := json.Unmarshal(stdout.Bytes(), &d); err != nil {
		t.Fatalf("unmarshal drift json: %v", err)
	}
	if d.Version != catalog.CurrentDriftVersion {
		t.Fatalf("drift version: got %d, want %d", d.Version, catalog.CurrentDriftVersion)
	}
	if d.From.Label != "before" || d.To.Label != "after" {
		t.Fatalf("drift labels: from=%q to=%q", d.From.Label, d.To.Label)
	}
}

// TestCatalogCommandRequiresLabel confirms the CLI rejects a capture
// without --label. Stage 10 keys snapshots by label so a missing
// flag is a CLI usage error.
func TestCatalogCommandRequiresLabel(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"catalog", "../../internal/graph"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit: got %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "--label is required") {
		t.Fatalf("expected --label hint, got %q", stderr.String())
	}
}

// TestDriftCommandUnknownLabel reports available labels when the
// requested snapshot is missing.
func TestDriftCommandUnknownLabel(t *testing.T) {
	dir := t.TempDir()
	cat := filepath.Join(dir, "catalog.yaml")

	var stdout, stderr bytes.Buffer
	code := run([]string{"catalog", "--label", "only", "--catalog", cat, "../../internal/graph"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("catalog only: code=%d stderr=%q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"drift", "--from", "missing", "--to", "only", "--catalog", cat}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("drift unknown: got code %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "not found") {
		t.Fatalf("expected 'not found', got %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "only") {
		t.Fatalf("expected available label hint, got %q", stderr.String())
	}
}

// TestDriftCommandMissingFlag rejects invocation without --from/--to.
func TestDriftCommandMissingFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"drift", "--from", "x"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit: got %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "required") {
		t.Fatalf("expected required-flag message, got %q", stderr.String())
	}
}

// TestDriftCommandShowsActualDrift writes two snapshots with
// hand-edited YAML so we exercise a real, populated Drift output —
// the round-trip test only proves identical-snapshot equality.
func TestDriftCommandShowsActualDrift(t *testing.T) {
	dir := t.TempDir()
	cat := filepath.Join(dir, "catalog.yaml")
	yaml := `version: 1
snapshots:
  - label: before
    captured_at: 2026-05-01T00:00:00Z
    path: .
    metrics:
      - name: cycle_rank
        value: 5
      - name: modularity
        value: 0.8
    motifs:
      total_groups: 1
      total_instances: 3
      groups:
        - canonical: "k=3|abc"
          size: 3
          count: 3
    patterns:
      - id: domain_core
        version: "1.0.0"
        status: match
        score: 0.9
        threshold: 0.7
  - label: after
    captured_at: 2026-05-02T00:00:00Z
    path: .
    metrics:
      - name: cycle_rank
        value: 7
      - name: modularity
        value: 0.8
    motifs:
      total_groups: 2
      total_instances: 6
      groups:
        - canonical: "k=3|abc"
          size: 3
          count: 4
        - canonical: "k=4|new"
          size: 4
          count: 2
    patterns:
      - id: domain_core
        version: "1.0.0"
        status: near_match
        score: 0.6
        threshold: 0.7
`
	if err := os.WriteFile(cat, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write catalog: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"drift", "--from", "before", "--to", "after", "--catalog", cat, "--format", "text"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("drift: code=%d stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"cycle_rank",
		"5 → 7",
		"k=4|new",
		"domain_core",
		"match → near_match",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in drift text, got:\n%s", want, out)
		}
	}
}
