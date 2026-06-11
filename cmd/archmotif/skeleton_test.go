package main

import (
	"bytes"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSkeletonUsageWithoutArgs exercises the missing-path branch.
func TestSkeletonUsageWithoutArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"skeleton"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "Usage") {
		t.Fatalf("expected usage in stderr, got %q", stderr.String())
	}
}

// TestSkeletonUnknownIDFails — when --id is supplied but no proposal
// has that ID, the command must fail with code 2 (argument error).
func TestSkeletonUnknownIDFails(t *testing.T) {
	dir := writeMultiImplFixture(t)
	out := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"skeleton",
		"--id=does-not-exist",
		"--out=" + out,
		dir,
	}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "no proposal with id") {
		t.Fatalf("expected 'no proposal with id' diagnostic, got %q", stderr.String())
	}
}

// TestSkeletonWritesAllProposals runs the CLI on a multi-impl fixture
// that triggers extract-interface, asserts at least one .skeleton.go
// + .skeleton.yaml pair is written, and re-parses the rendered Go to
// confirm it survives go/parser. End-to-end smoke for ADR-023.
func TestSkeletonWritesAllProposals(t *testing.T) {
	dir := writeMultiImplFixture(t)
	out := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"skeleton",
		"--out=" + out,
		dir,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	matches, err := filepath.Glob(filepath.Join(out, "*.skeleton.go"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) == 0 {
		t.Fatalf("expected at least one .skeleton.go file under %s; stdout=%q", out, stdout.String())
	}
	for _, gp := range matches {
		yp := strings.TrimSuffix(gp, ".go") + ".yaml"
		if _, err := os.Stat(yp); err != nil {
			t.Errorf("missing companion YAML for %s: %v", gp, err)
		}
		src, err := os.ReadFile(gp)
		if err != nil {
			t.Fatalf("read %s: %v", gp, err)
		}
		fset := token.NewFileSet()
		if _, err := parser.ParseFile(fset, gp, src, parser.ParseComments); err != nil {
			t.Errorf("rendered Go does not parse (%s): %v", gp, err)
		}
		// Format pin: header markers must survive the round-trip.
		text := string(src)
		for _, want := range []string{"// PROPOSAL:", "// AFFECTED:", "// SAMPLES:", "// ROLE "} {
			if !strings.Contains(text, want) {
				t.Errorf("rendered %s missing required marker %q", gp, want)
			}
		}
	}
}

// writeMultiImplFixture writes a minimal Go module with three structs
// that all implement the same interface and share the same method
// signature. This is the canonical motif-redundancy shape that
// triggers Stage 5's extract_interface rule (ADR-019).
func writeMultiImplFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module example.com/triple\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	src := `package triple

// Reader is the shared contract.
type Reader interface {
	Read(id string) string
}

type A struct{}
type B struct{}
type C struct{}

func (A) Read(id string) string { return "a:" + id }
func (B) Read(id string) string { return "b:" + id }
func (C) Read(id string) string { return "c:" + id }
`
	if err := os.WriteFile(filepath.Join(dir, "triple.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write triple.go: %v", err)
	}
	return dir
}
