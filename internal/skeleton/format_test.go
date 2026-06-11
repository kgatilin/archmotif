// Package skeleton holds the renderer that emits annotated Go +
// YAML skeleton files per ADR-016. The renderer itself ships in
// Stage 6; this file pre-pins the format contract by smoke-testing
// that the worked-example annotated Go fixture parses with
// go/parser. If this test breaks, the on-disk skeleton format has
// drifted from "valid Go modulo placeholders treated as
// identifiers" — see ADR-016 and docs/skeleton-format.md.
package skeleton_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixturePath resolves the worked-example fixture path relative to
// the repo root. We walk up from the test working directory
// (which is internal/skeleton/) to the module root by looking for
// go.mod, then descend into testdata/skeletons/.
func fixturePath(t *testing.T, name string) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "testdata", "skeletons", name)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate go.mod walking up from %q", dir)
		}
		dir = parent
	}
}

func TestSkeletonGoFixtureParses(t *testing.T) {
	path := fixturePath(t, "motif-001.skeleton.go")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	if file.Name == nil || file.Name.Name == "" {
		t.Fatalf("parsed file has no package name")
	}
}

// TestSkeletonGoFixtureHasRequiredHeaders enforces the ADR-016
// header pins (// PROPOSAL:, // AFFECTED:, // SAMPLES:). It does
// not parse the content of those headers — Stage 6's renderer owns
// the structured forms; here we only guard against accidental
// removal.
func TestSkeletonGoFixtureHasRequiredHeaders(t *testing.T) {
	path := fixturePath(t, "motif-001.skeleton.go")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	text := string(src)
	for _, want := range []string{"// PROPOSAL:", "// AFFECTED:", "// SAMPLES:", "// ROLE "} {
		if !strings.Contains(text, want) {
			t.Errorf("fixture missing required header marker %q", want)
		}
	}
}
