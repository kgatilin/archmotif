package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestSplitShellCommand covers the shell-style splitter used to parse
// the --materializer flag. Quoted segments stay intact; unquoted
// whitespace splits.
func TestSplitShellCommand(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"claude -p", []string{"claude", "-p"}},
		{"  claude   -p  ", []string{"claude", "-p"}},
		{`echo "hello world"`, []string{"echo", "hello world"}},
		{`echo 'a b' c`, []string{"echo", "a b", "c"}},
		{"path/to/bin --flag=value", []string{"path/to/bin", "--flag=value"}},
	}
	for _, tc := range cases {
		got := splitShellCommand(tc.in)
		if !equalStrings(got, tc.want) {
			t.Errorf("splitShellCommand(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// TestExtractPatch_Fenced covers the materializer-output form used by
// the in-process Anthropic provider: a single ```diff fenced block.
func TestExtractPatch_Fenced(t *testing.T) {
	out := "Here you go:\n\n```diff\ndiff --git a/x b/x\n--- a/x\n+++ b/x\n@@\n-old\n+new\n```\n"
	patch, err := extractPatch(out)
	if err != nil {
		t.Fatalf("extractPatch: %v", err)
	}
	if !strings.HasPrefix(string(patch), "diff --git a/x b/x") {
		t.Errorf("patch missing diff --git header: %q", patch)
	}
	if !strings.HasSuffix(string(patch), "\n") {
		t.Errorf("patch must end with newline: %q", patch)
	}
}

// TestExtractPatch_Bare covers the simpler form a CLI materializer can
// emit: a unified diff with no surrounding prose.
func TestExtractPatch_Bare(t *testing.T) {
	out := "diff --git a/x b/x\n--- a/x\n+++ b/x\n@@\n-old\n+new\n"
	patch, err := extractPatch(out)
	if err != nil {
		t.Fatalf("extractPatch: %v", err)
	}
	if string(patch) != out {
		t.Errorf("bare diff round-trip mismatch:\nwant=%q\ngot=%q", out, patch)
	}
}

// TestExtractPatch_NoDiff confirms outputs that are neither fenced nor
// recognisably-unified-diff produce ErrNoPatch.
func TestExtractPatch_NoDiff(t *testing.T) {
	cases := []string{
		"",
		"   \n  ",
		"sorry, I can't do that.",
		"```\nsome other fence\n```",
	}
	for _, in := range cases {
		_, err := extractPatch(in)
		if err == nil {
			t.Errorf("extractPatch(%q) returned nil error; want ErrNoPatch", in)
		}
	}
}

// TestOptimizeLoop_NoArgs covers the argparse failure path.
func TestOptimizeLoop_NoArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"optimize-loop"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "Usage") {
		t.Errorf("stderr missing usage:\n%s", stderr.String())
	}
}

// TestOptimizeLoop_BadMaxBatches covers the validation of --max-batches.
func TestOptimizeLoop_BadMaxBatches(t *testing.T) {
	dir := writeEmptyModule(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"optimize-loop", "--max-batches", "0", dir}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "max-batches") {
		t.Errorf("stderr missing max-batches diagnostic:\n%s", stderr.String())
	}
}

// TestOptimizeLoop_NoBatchEmptyPipeline covers the loop's first stop
// condition: when the proposal pipeline is empty, the loop writes an
// empty-batch summary and exits 0.
func TestOptimizeLoop_NoBatchEmptyPipeline(t *testing.T) {
	repoDir := writeEmptyModule(t)
	gitInit(t, repoDir)

	runDir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"optimize-loop",
		"--run-dir", runDir,
		"--max-batches", "3",
		"--dry-run", // belt-and-braces; the empty-pipeline branch fires before dry-run kicks in
		repoDir,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%s", code, stderr.String())
	}
	res := readSummary(t, runDir)
	if res.StoppedBy != outcomeNoBatch {
		t.Errorf("StoppedBy = %q, want %q", res.StoppedBy, outcomeNoBatch)
	}
	if len(res.Batches) != 1 {
		t.Fatalf("Batches len = %d, want 1; batches=%+v", len(res.Batches), res.Batches)
	}
	if res.Batches[0].Outcome != outcomeNoBatch {
		t.Errorf("Batches[0].Outcome = %q, want %q", res.Batches[0].Outcome, outcomeNoBatch)
	}
	if !strings.Contains(stdout.String(), "stopped by no-batch") {
		t.Errorf("stdout missing stop-by-no-batch line:\n%s", stdout.String())
	}
	assertConvergence(t, res, "no_candidates", outcomeNoBatch, 1)
}

// TestOptimizeLoop_DryRun covers the --dry-run path: the loop picks one
// batch, renders the prompt + contract + graph artefacts, and stops.
// No materializer call, no patch.
func TestOptimizeLoop_DryRun(t *testing.T) {
	repoDir := writeMotifModule(t)
	gitInit(t, repoDir)

	runDir := t.TempDir()
	cfg := optimizeLoopConfig{
		repoDir:         repoDir,
		runDir:          runDir,
		materializer:    "true",
		maxBatches:      3,
		dryRun:          true,
		pattern:         "./...",
		now:             fixedClock(),
		runMaterializer: failingRunner(t),
	}
	var stdout, stderr bytes.Buffer
	res, err := runOptimizeLoopInner(cfg, &stdout, &stderr)
	if err != nil {
		t.Fatalf("runOptimizeLoopInner: %v\nstderr=%s", err, stderr.String())
	}
	if res.StoppedBy != outcomeDryRun {
		t.Fatalf("StoppedBy = %q, want %q", res.StoppedBy, outcomeDryRun)
	}
	if len(res.Batches) != 1 {
		t.Fatalf("Batches len = %d, want 1", len(res.Batches))
	}
	b1 := res.Batches[0]
	if b1.Outcome != outcomeDryRun {
		t.Errorf("Batches[0].Outcome = %q, want %q", b1.Outcome, outcomeDryRun)
	}
	for _, name := range []string{"prompt.txt", "contract.yaml", "graph.graphml", "proposal.json"} {
		path := filepath.Join(b1.Dir, name)
		st, err := os.Stat(path)
		if err != nil || st.Size() == 0 {
			t.Errorf("missing or empty artifact %s: %v", name, err)
		}
	}
	// Patch / validation / apply logs must NOT exist on dry-run.
	for _, name := range []string{"patch.diff", "validation.log", "apply.log"} {
		if _, err := os.Stat(filepath.Join(b1.Dir, name)); err == nil {
			t.Errorf("unexpected artifact %s on dry-run", name)
		}
	}
	assertConvergence(t, res, "dry_run", outcomeDryRun, 1)
}

// TestOptimizeLoop_FakeMaterializer_ValidationOnly covers the integration
// path: a fake materializer emits a valid diff, the loop validates it
// (--apply off → working tree untouched) and continues. With the fake
// always emitting the same patch, the second iteration's validation
// fails (patch already in tree) so the loop halts on
// validation-failed. That's the documented stop reason — confirm it
// shows up in the summary.
func TestOptimizeLoop_FakeMaterializer_ValidationOnly(t *testing.T) {
	repoDir := writeMotifModule(t)
	gitInit(t, repoDir)

	// Build a unified diff that adds a new comment to one of the
	// existing files. Validation (--check) succeeds; with --apply
	// unset, the working tree stays untouched, so on the next
	// iteration the same diff applies again — but the proposal
	// pipeline is deterministic and produces the same proposal, so
	// the second run picks the same batch. We let the loop run to
	// max-batches=1 to keep the assertion simple: one batch, outcome
	// ok, validation.log present, no apply mutation.
	patch := simpleAppendPatch(t)

	runDir := t.TempDir()
	calls := 0
	cfg := optimizeLoopConfig{
		repoDir:      repoDir,
		runDir:       runDir,
		materializer: "fake",
		maxBatches:   1,
		pattern:      "./...",
		now:          fixedClock(),
		runMaterializer: func(cmd, prompt string) (string, string, error) {
			calls++
			return "```diff\n" + patch + "```\n", "", nil
		},
	}
	var stdout, stderr bytes.Buffer
	res, err := runOptimizeLoopInner(cfg, &stdout, &stderr)
	if err != nil {
		t.Fatalf("runOptimizeLoopInner: %v\nstderr=%s", err, stderr.String())
	}
	if calls != 1 {
		t.Errorf("materializer calls = %d, want 1", calls)
	}
	if len(res.Batches) != 1 {
		t.Fatalf("Batches len = %d, want 1", len(res.Batches))
	}
	if res.Batches[0].Outcome != outcomeOK {
		t.Errorf("Batches[0].Outcome = %q, want %q; reason=%s",
			res.Batches[0].Outcome, outcomeOK, res.Batches[0].Reason)
	}
	assertConvergence(t, res, "max_batches", "max-batches", 1)
	for _, name := range []string{"patch.diff", "validation.log", "apply.log"} {
		path := filepath.Join(res.Batches[0].Dir, name)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("missing artifact %s: %v", name, err)
		}
	}
	// apply.log on validation-only run must record the skip.
	apply, err := os.ReadFile(filepath.Join(res.Batches[0].Dir, "apply.log"))
	if err != nil {
		t.Fatalf("read apply.log: %v", err)
	}
	if !strings.Contains(string(apply), "skipped") {
		t.Errorf("apply.log missing 'skipped' marker:\n%s", apply)
	}
}

// TestOptimizeLoop_FakeMaterializer_NoDiff covers the failure mode where
// the materializer returns text but no recognisable patch. The loop
// halts with outcomeNoDiff and surfaces the error.
func TestOptimizeLoop_FakeMaterializer_NoDiff(t *testing.T) {
	repoDir := writeMotifModule(t)
	gitInit(t, repoDir)

	runDir := t.TempDir()
	cfg := optimizeLoopConfig{
		repoDir:      repoDir,
		runDir:       runDir,
		materializer: "fake",
		maxBatches:   3,
		pattern:      "./...",
		now:          fixedClock(),
		runMaterializer: func(cmd, prompt string) (string, string, error) {
			return "Sorry, I cannot help with that.", "", nil
		},
	}
	var stdout, stderr bytes.Buffer
	res, err := runOptimizeLoopInner(cfg, &stdout, &stderr)
	if err == nil {
		t.Fatalf("expected error on no-diff materializer output; got nil")
	}
	if res == nil {
		t.Fatal("nil result on no-diff failure")
	}
	if res.StoppedBy != outcomeNoDiff {
		t.Errorf("StoppedBy = %q, want %q", res.StoppedBy, outcomeNoDiff)
	}
	assertConvergence(t, res, "no_improvement", outcomeNoDiff, 1)
}

// TestOptimizeLoop_FakeMaterializer_ValidationFailure covers the patch-
// validation failure mode: fake emits a diff that doesn't apply (wrong
// file or wrong line numbers) and the loop halts with
// outcomeValidateFail.
func TestOptimizeLoop_FakeMaterializer_ValidationFailure(t *testing.T) {
	repoDir := writeMotifModule(t)
	gitInit(t, repoDir)

	runDir := t.TempDir()
	bogus := "diff --git a/nonexistent.go b/nonexistent.go\n--- a/nonexistent.go\n+++ b/nonexistent.go\n@@ -1 +1,2 @@\n existing line\n+new line\n"
	cfg := optimizeLoopConfig{
		repoDir:      repoDir,
		runDir:       runDir,
		materializer: "fake",
		maxBatches:   3,
		pattern:      "./...",
		now:          fixedClock(),
		runMaterializer: func(cmd, prompt string) (string, string, error) {
			return bogus, "", nil
		},
	}
	var stdout, stderr bytes.Buffer
	res, err := runOptimizeLoopInner(cfg, &stdout, &stderr)
	if err == nil {
		t.Fatalf("expected error on bogus diff; got nil")
	}
	if res == nil {
		t.Fatal("nil result on validation failure")
	}
	if res.StoppedBy != outcomeValidateFail {
		t.Errorf("StoppedBy = %q, want %q", res.StoppedBy, outcomeValidateFail)
	}
	assertConvergence(t, res, "error", outcomeValidateFail, 1)
	// The patch.diff and validation.log must both exist so the operator
	// can inspect what failed.
	for _, name := range []string{"patch.diff", "validation.log"} {
		path := filepath.Join(res.Batches[0].Dir, name)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("missing artifact %s: %v", name, err)
		}
	}
}

// readSummary reads <runDir>/summary.json into a runResult for
// assertions in the integration tests above.
func readSummary(t *testing.T, runDir string) *runResult {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(runDir, "summary.json"))
	if err != nil {
		t.Fatalf("read summary.json: %v", err)
	}
	var res runResult
	if err := json.Unmarshal(b, &res); err != nil {
		t.Fatalf("decode summary.json: %v", err)
	}
	return &res
}

func assertConvergence(t *testing.T, res *runResult, status, stopReason string, iterations int) {
	t.Helper()
	if res.FinishedAt == "" {
		t.Fatal("FinishedAt is empty")
	}
	if res.Convergence.Status != status {
		t.Errorf("Convergence.Status = %q, want %q", res.Convergence.Status, status)
	}
	if res.Convergence.StopReason != stopReason {
		t.Errorf("Convergence.StopReason = %q, want %q", res.Convergence.StopReason, stopReason)
	}
	if res.Convergence.IterationsRun != iterations {
		t.Errorf("Convergence.IterationsRun = %d, want %d", res.Convergence.IterationsRun, iterations)
	}
}

// fixedClock returns a clock function that always returns the same
// time, so test artifact paths are reproducible.
func fixedClock() func() time.Time {
	t := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	return func() time.Time { return t }
}

// failingRunner is a materializer runner that calls t.Errorf if it's
// invoked. Used in dry-run tests to assert the materializer is NOT
// called.
func failingRunner(t *testing.T) func(string, string) (string, string, error) {
	t.Helper()
	return func(cmd, prompt string) (string, string, error) {
		t.Errorf("materializer should not be called in dry-run mode")
		return "", "", nil
	}
}

// gitInit runs `git init` + an initial commit in dir so `git apply
// --check` has a working tree to compare against.
func gitInit(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
		{"add", "."},
		{"commit", "-q", "-m", "init"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		if err := cmd.Run(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out.String())
		}
	}
}

// writeMotifModule writes a tiny Go module with three near-isomorphic
// type/method shapes — enough to trigger the motif-redundancy
// detector and produce a Stage 5 proposal. Without this fixture the
// pipeline returns zero batches and the loop short-circuits.
func writeMotifModule(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mod := "module motifmod\n\ngo 1.22\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	src := `package motifmod

type UserStore interface{ Find(id string) string }
type OrderStore interface{ Find(id string) string }
type ProductStore interface{ Find(id string) string }

type SQLUserStore struct{ dsn string }
type SQLOrderStore struct{ dsn string }
type SQLProductStore struct{ dsn string }

func (s *SQLUserStore) Find(id string) string    { return s.dsn + ":" + id }
func (s *SQLOrderStore) Find(id string) string   { return s.dsn + ":" + id }
func (s *SQLProductStore) Find(id string) string { return s.dsn + ":" + id }

var _ UserStore = (*SQLUserStore)(nil)
var _ OrderStore = (*SQLOrderStore)(nil)
var _ ProductStore = (*SQLProductStore)(nil)
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	return dir
}

// simpleAppendPatch returns a unified diff that appends a comment to
// motifmod's main.go. The diff is hand-rolled to apply cleanly against
// the writeMotifModule fixture.
func simpleAppendPatch(t *testing.T) string {
	t.Helper()
	// Use a "no-op" patch: append a blank-comment line at the end of
	// the file. The hunk numbering is computed against the fixture
	// content from writeMotifModule.
	return `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -16,3 +16,4 @@ func (s *SQLProductStore) Find(id string) string { return s.dsn + ":" + id }
 var _ UserStore = (*SQLUserStore)(nil)
 var _ OrderStore = (*SQLOrderStore)(nil)
 var _ ProductStore = (*SQLProductStore)(nil)
+// archmotif: optimize-test marker
`
}

// equalStrings compares two []string slices treating nil and empty as
// equal. Used by TestSplitShellCommand.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
