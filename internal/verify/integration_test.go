package verify_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kgatilin/archmotif/internal/parser"
	"github.com/kgatilin/archmotif/internal/verify"
)

// TestIntegration_Motif001 round-trips the on-disk fixture under
// testdata/verify/motif-001/: load skeleton.yaml, parse the code
// directory into a typed graph, run the verifier, expect Match.
//
// The fixture lives at the repo root (testdata/verify/...) — keep the
// path resolution relative to this test file so `go test ./...`
// works regardless of the caller's CWD.
func TestIntegration_Motif001(t *testing.T) {
	root := repoRoot(t)
	skeletonPath := filepath.Join(root, "testdata", "verify", "motif-001", "skeleton.yaml")
	codeDir := filepath.Join(root, "testdata", "verify", "motif-001", "code")

	skel, err := verify.LoadSkeletonFile(skeletonPath)
	if err != nil {
		t.Fatalf("LoadSkeletonFile: %v", err)
	}
	if skel.ProposalID != "motif-001" {
		t.Fatalf("ProposalID=%q, want motif-001", skel.ProposalID)
	}

	res, err := parser.Build(parser.Options{
		Dir:      codeDir,
		Patterns: []string{"./..."},
	})
	if err != nil {
		t.Fatalf("parser.Build: %v", err)
	}

	verdict := verify.NewBacktrackVerifier().Verify(context.Background(), skel, res.Graph)
	if !verdict.Match {
		t.Fatalf("Verify Match=false; diff=%+v", verdict.Diff)
	}
	for _, role := range []string{"Iface", "Impl", "Method"} {
		if _, ok := verdict.Mapping[role]; !ok {
			t.Errorf("mapping missing role %q; got %+v", role, verdict.Mapping)
		}
	}
	if got := verdict.Bindings["Iface"]; got != "UserStore" {
		t.Errorf("binding[Iface]=%q, want UserStore", got)
	}
	if got := verdict.Bindings["Impl"]; got != "SQLUserStore" {
		t.Errorf("binding[Impl]=%q, want SQLUserStore", got)
	}
	if got := verdict.Bindings["Method"]; got != "Find" {
		t.Errorf("binding[Method]=%q, want Find", got)
	}
}

// repoRoot walks up from the test file's directory to the module
// root by looking for go.mod. Lets the integration test resolve
// testdata paths regardless of where `go test` is invoked from.
func repoRoot(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("filepath.Abs: %v", err)
	}
	dir := abs
	for {
		if exists(filepath.Join(dir, "go.mod")) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate go.mod above %s", abs)
		}
		dir = parent
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
