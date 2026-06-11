package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProposeListFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"propose", "--list"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "extract_interface") {
		t.Fatalf("expected extract_interface in stdout, got %q", stdout.String())
	}
}

func TestProposeUsageWithoutArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"propose"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "Usage") {
		t.Fatalf("expected usage in stderr, got %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "extract_interface") {
		t.Fatalf("expected registered rules listing in stderr, got %q", stderr.String())
	}
}

// TestProposeOnEmptyPathSucceeds asserts the pipeline runs end-to-end
// on a path that produces zero proposals (small fixture). Smoke test
// for ADR-022's CLI wiring: graph → metrics → anomalies → proposals.
func TestProposeOnEmptyPathSucceeds(t *testing.T) {
	dir := writeMinimalGoModule(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"propose", "--format=json", dir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	var env struct {
		Version   int             `json:"version"`
		Proposals json.RawMessage `json:"proposals"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v\nstdout=%s", err, stdout.String())
	}
	if env.Version != 1 {
		t.Fatalf("version = %d, want 1", env.Version)
	}
}

// writeMinimalGoModule creates a temp dir holding a minimal but
// valid Go module. The graph the parser produces from this should
// have too few elements to trigger any motif redundancy.
func writeMinimalGoModule(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/m\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	src := `package m

type T struct{}

func (T) Hello() string { return "hi" }
`
	if err := os.WriteFile(filepath.Join(dir, "m.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write m.go: %v", err)
	}
	return dir
}
