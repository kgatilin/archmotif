package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/kgatilin/archmotif/internal/contracts"
	"github.com/kgatilin/archmotif/internal/llm"
	"github.com/kgatilin/archmotif/internal/parser"
	"github.com/kgatilin/archmotif/internal/propose"
	"github.com/kgatilin/archmotif/internal/skeleton"
)

// runOptimizeLoopCmd implements the `archmotif optimize-loop` subcommand: a
// durable iteration loop on top of the Stage 3 → 4 → 5 pipeline that
// drives a configurable materializer command (e.g. `claude -p`)
// instead of the in-process Anthropic client used by `archmotif
// refactor`.
//
// Each iteration ("batch"):
//
//  1. Export a fresh GraphML snapshot of the working tree.
//  2. Run the deterministic Stage 5 next-batch selector and pick the
//     top-ranked proposal (ADR-031).
//  3. Render the contract (skeleton YAML) and the materializer prompt.
//  4. If --dry-run is unset, invoke the materializer command; capture
//     stdout as the patch.
//  5. Validate the patch with `git apply --check`.
//  6. Apply the patch to the working tree (when --apply is set).
//  7. Write per-batch artifacts under <run-dir>/batch-NNN/.
//
// The loop stops on:
//   - no proposal (empty pipeline),
//   - validation failure,
//   - apply failure,
//   - --max-batches reached.
//
// On exit a summary.json is written under the run directory and a
// human-readable summary is printed to stdout (per ADR-031 stdout =
// result, stderr = chatter)
//
//	archmotif optimize-loop [flags] <path>
//	  --max-batches=N        max iterations (default: 5)
//	  --materializer=CMD     shell command for materializer; receives the
//	                         rendered prompt on stdin; emits a unified
//	                         diff (optionally fenced as ```diff) on stdout.
//	                         Default: "claude -p"
//	  --run-dir=DIR          run-artifact directory (default:
//	                         .archmotif/runs/<timestamp>)
//	  --dry-run              skip the materializer call; emit prompt only
//	  --apply                apply validated patches to the working tree
//	                         (off by default — validation only)
//	  --tests                include _test.go in the upstream pipeline
//	  --pattern=...          go/packages pattern (default ./...)
//
// Exit codes: 0 success, 1 loop error (validation/apply/materializer
// failure that halted the run), 2 argument error.
func runOptimizeLoopCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("archmotif optimize-loop", flag.ContinueOnError)
	fs.SetOutput(stderr)
	maxBatches := fs.Int("max-batches", 5, "maximum number of optimization batches")
	materializer := fs.String("materializer", "claude -p", "shell command to run the materializer (receives prompt on stdin, emits patch on stdout)")
	runDir := fs.String("run-dir", "", "directory for run artifacts (default: .archmotif/runs/<timestamp>)")
	dryRun := fs.Bool("dry-run", false, "do not call the materializer; emit prompt and exit each batch")
	apply := fs.Bool("apply", false, "apply validated patches to the working tree")
	tests := fs.Bool("tests", false, "include _test.go files in pipeline")
	pattern := fs.String("pattern", "./...", "go/packages pattern")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage:\n  archmotif optimize-loop [flags] <path>\n\nFlags:\n")
		fs.PrintDefaults()
		_, _ = fmt.Fprintf(stderr, "\nThe materializer command runs once per batch. The rendered prompt is\nwritten to its stdin; its stdout is captured as the patch (a unified\ndiff, optionally inside a ```diff fenced block). Stderr is logged.\n")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() < 1 {
		fs.Usage()
		return 2
	}
	if *maxBatches <= 0 {
		_, _ = fmt.Fprintln(stderr, "archmotif optimize-loop: --max-batches must be > 0")
		return 2
	}
	dir := fs.Arg(0)

	cfg := optimizeLoopConfig{
		repoDir:         dir,
		runDir:          *runDir,
		materializer:    *materializer,
		maxBatches:      *maxBatches,
		dryRun:          *dryRun,
		apply:           *apply,
		tests:           *tests,
		pattern:         *pattern,
		now:             time.Now,
		runMaterializer: runMaterializerCommand,
	}
	res, err := runOptimizeLoopInner(cfg, stdout, stderr)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif optimize-loop: %v\n", err)
		// Still write the summary on failure — operator wants the trail.
		if res != nil {
			_ = writeSummary(res)
		}
		return 1
	}
	if err := writeSummary(res); err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif optimize-loop: write summary: %v\n", err)
		return 1
	}
	writeRunSummaryText(stdout, res)
	return 0
}

// optimizeLoopConfig bundles the validated CLI flags + injection seams the
// loop needs. Tests construct one directly with a fake materializer
// runner and a fixed clock; production wires it from the flag set.
type optimizeLoopConfig struct {
	repoDir      string
	runDir       string
	materializer string
	maxBatches   int
	dryRun       bool
	apply        bool
	tests        bool
	pattern      string

	now             func() time.Time
	runMaterializer func(cmd, prompt string) (stdout, stderr string, err error)
}

type runResult = OptimizeLoopRunReport

// Outcome strings on OptimizeLoopBatchReport. Kept in one place so the summary
// renderer and tests don't drift.
const (
	outcomeOK              = "ok"
	outcomeDryRun          = "dry-run"
	outcomeNoBatch         = "no-batch"
	outcomeMaterializerErr = "materializer-error"
	outcomeNoDiff          = "no-diff"
	outcomeValidateFail    = "validation-failed"
	outcomeApplyFail       = "apply-failed"
	outcomePipelineErr     = "pipeline-error"
	outcomeArtifactErr     = "artifact-error"
)

// runOptimizeLoopInner drives the iteration loop. Returns a (possibly
// partial) runResult plus the first error that halted the loop. A nil
// error with a populated runResult means the loop ran to a natural
// stop (max-batches, no-batch, or dry-run).
func runOptimizeLoopInner(cfg optimizeLoopConfig, stdout, stderr io.Writer) (*OptimizeLoopRunReport, error) {
	startedAt := cfg.now().UTC()
	result := &OptimizeLoopRunReport{
		StartedAt:    startedAt.Format(time.RFC3339),
		RepoDir:      cfg.repoDir,
		Materializer: cfg.materializer,
		DryRun:       cfg.dryRun,
		Apply:        cfg.apply,
		MaxBatches:   cfg.maxBatches,
	}
	defer finalizeOptimizeLoopRunReport(result, cfg.now)

	// Resolve run-dir; default uses the start-of-run timestamp so
	// re-runs don't trample each other.
	rd := cfg.runDir
	if rd == "" {
		rd = filepath.Join(cfg.repoDir, ".archmotif", "runs", startedAt.Format("20060102-150405"))
	}
	if err := os.MkdirAll(rd, 0o755); err != nil {
		return result, fmt.Errorf("create run dir: %w", err)
	}
	result.RunDir = rd

	// Capture before-counts from the initial graph so the summary can
	// report the run's structural impact.
	if before, err := graphCounts(cfg.repoDir, cfg.pattern, cfg.tests); err == nil {
		result.BeforeNodes = before.nodes
		result.BeforeEdges = before.edges
	} else {
		_, _ = fmt.Fprintf(stderr, "archmotif optimize-loop: pre-loop graph counts: %v\n", err)
	}

	for i := 1; i <= cfg.maxBatches; i++ {
		br := &OptimizeLoopBatchReport{Index: i}
		batchDir := filepath.Join(rd, fmt.Sprintf("batch-%03d", i))
		if err := os.MkdirAll(batchDir, 0o755); err != nil {
			br.Outcome = outcomeArtifactErr
			br.Reason = err.Error()
			result.Batches = append(result.Batches, br)
			result.StoppedBy = outcomeArtifactErr
			return result, fmt.Errorf("create batch dir: %w", err)
		}
		br.Dir = batchDir

		// 1. Export a fresh GraphML snapshot.
		if err := exportGraphML(cfg.repoDir, cfg.pattern, cfg.tests, batchDir); err != nil {
			br.Outcome = outcomeArtifactErr
			br.Reason = err.Error()
			result.Batches = append(result.Batches, br)
			result.StoppedBy = outcomeArtifactErr
			return result, fmt.Errorf("export graphml: %w", err)
		}

		// 2. Run the proposal pipeline and pick the top batch.
		proposals, err := runProposalPipeline(cfg.repoDir, cfg.pattern, cfg.tests, stderr)
		if err != nil {
			br.Outcome = outcomePipelineErr
			br.Reason = err.Error()
			result.Batches = append(result.Batches, br)
			result.StoppedBy = outcomePipelineErr
			return result, fmt.Errorf("proposal pipeline: %w", err)
		}
		ranked := rankProposals(proposals)
		if counts, err := graphCounts(cfg.repoDir, cfg.pattern, cfg.tests); err == nil {
			br.NodeCount = counts.nodes
			br.EdgeCount = counts.edges
		}
		if len(ranked) == 0 {
			br.Outcome = outcomeNoBatch
			br.Reason = "no proposals from pipeline"
			result.Batches = append(result.Batches, br)
			result.StoppedBy = outcomeNoBatch
			_, _ = fmt.Fprintf(stderr, "archmotif optimize-loop: batch %d — nothing to propose; stopping\n", i)
			break
		}
		picked := ranked[0]
		br.ProposalID = picked.ID
		br.Description = picked.Description
		br.RuleKind = proposalRuleName(picked)

		_, _ = fmt.Fprintf(stderr, "archmotif optimize-loop: batch %d — picked %s (rule=%s, value=%g)\n",
			i, picked.ID, br.RuleKind, triggerValue(picked))

		// 3. Render the contract (skeleton YAML) and the prompt.
		if err := writeBatchContract(batchDir, picked); err != nil {
			br.Outcome = outcomeArtifactErr
			br.Reason = err.Error()
			result.Batches = append(result.Batches, br)
			result.StoppedBy = outcomeArtifactErr
			return result, fmt.Errorf("render contract: %w", err)
		}
		llmProp, err := buildLLMProposal(cfg.repoDir, picked, "")
		if err != nil {
			br.Outcome = outcomePipelineErr
			br.Reason = err.Error()
			result.Batches = append(result.Batches, br)
			result.StoppedBy = outcomePipelineErr
			return result, fmt.Errorf("build llm proposal: %w", err)
		}
		prompt, err := llm.RenderPrompt(llmProp)
		if err != nil {
			br.Outcome = outcomePipelineErr
			br.Reason = err.Error()
			result.Batches = append(result.Batches, br)
			result.StoppedBy = outcomePipelineErr
			return result, fmt.Errorf("render prompt: %w", err)
		}
		if err := os.WriteFile(filepath.Join(batchDir, "prompt.txt"), []byte(prompt), 0o644); err != nil {
			br.Outcome = outcomeArtifactErr
			br.Reason = err.Error()
			result.Batches = append(result.Batches, br)
			result.StoppedBy = outcomeArtifactErr
			return result, fmt.Errorf("write prompt: %w", err)
		}
		if err := writeBatchProposal(batchDir, picked); err != nil {
			br.Outcome = outcomeArtifactErr
			br.Reason = err.Error()
			result.Batches = append(result.Batches, br)
			result.StoppedBy = outcomeArtifactErr
			return result, fmt.Errorf("write proposal: %w", err)
		}

		// 4. Dry-run short-circuit: emit prompt artifacts and stop the
		// loop. Operator wanted to see what would be sent, not to
		// iterate.
		if cfg.dryRun {
			br.Outcome = outcomeDryRun
			result.Batches = append(result.Batches, br)
			result.StoppedBy = outcomeDryRun
			_, _ = fmt.Fprintf(stderr, "archmotif optimize-loop: batch %d — dry-run; prompt written to %s\n", i, filepath.Join(batchDir, "prompt.txt"))
			break
		}

		// 5. Run the materializer.
		matOut, matErr, err := cfg.runMaterializer(cfg.materializer, prompt)
		if err := writeBatchLog(batchDir, "materializer.stderr.log", matErr); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif optimize-loop: write materializer.stderr.log: %v\n", err)
		}
		if err != nil {
			br.Outcome = outcomeMaterializerErr
			br.Reason = err.Error()
			result.Batches = append(result.Batches, br)
			result.StoppedBy = outcomeMaterializerErr
			return result, fmt.Errorf("materializer: %w", err)
		}

		patch, perr := extractPatch(matOut)
		if perr != nil {
			_ = writeBatchLog(batchDir, "materializer.stdout.log", matOut)
			br.Outcome = outcomeNoDiff
			br.Reason = perr.Error()
			result.Batches = append(result.Batches, br)
			result.StoppedBy = outcomeNoDiff
			return result, fmt.Errorf("extract patch: %w", perr)
		}
		if err := os.WriteFile(filepath.Join(batchDir, "patch.diff"), patch, 0o644); err != nil {
			br.Outcome = outcomeArtifactErr
			br.Reason = err.Error()
			result.Batches = append(result.Batches, br)
			result.StoppedBy = outcomeArtifactErr
			return result, fmt.Errorf("write patch: %w", err)
		}

		// 6. Validate the patch.
		if err := gitApplyCheck(cfg.repoDir, patch); err != nil {
			_ = writeBatchLog(batchDir, "validation.log", err.Error())
			br.Outcome = outcomeValidateFail
			br.Reason = err.Error()
			result.Batches = append(result.Batches, br)
			result.StoppedBy = outcomeValidateFail
			return result, fmt.Errorf("validate patch: %w", err)
		}
		_ = writeBatchLog(batchDir, "validation.log", "ok\n")

		// 7. Apply (only when --apply is set; default is validate-only
		// so the operator can review patches before mutating their tree).
		if cfg.apply {
			if err := gitApply(cfg.repoDir, patch); err != nil {
				_ = writeBatchLog(batchDir, "apply.log", err.Error())
				br.Outcome = outcomeApplyFail
				br.Reason = err.Error()
				result.Batches = append(result.Batches, br)
				result.StoppedBy = outcomeApplyFail
				return result, fmt.Errorf("apply patch: %w", err)
			}
			_ = writeBatchLog(batchDir, "apply.log", "ok\n")
		} else {
			_ = writeBatchLog(batchDir, "apply.log", "skipped (use --apply to mutate the working tree)\n")
		}

		br.Outcome = outcomeOK
		result.Batches = append(result.Batches, br)
	}

	if result.StoppedBy == "" {
		result.StoppedBy = "max-batches"
	}

	// After-counts reflect the post-loop graph (only meaningful when
	// --apply was set; otherwise these equal the before-counts).
	if after, err := graphCounts(cfg.repoDir, cfg.pattern, cfg.tests); err == nil {
		result.AfterNodes = after.nodes
		result.AfterEdges = after.edges
	}
	return result, nil
}

// gitApply shells out to `git apply` (without --check) to actually
// mutate the working tree. Mirrors gitApplyCheck in internal/llm but
// kept local so the optimize loop doesn't depend on the in-process
// Anthropic materializer.
func gitApply(repoDir string, diff []byte) error {
	cmd := exec.Command("git", "-C", repoDir, "apply", "-")
	cmd.Stdin = bytes.NewReader(diff)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git apply: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// gitApplyCheck shells out to `git apply --check`. Distinct from the
// internal/llm copy so the optimize package stays decoupled from the
// Anthropic-specific materializer.
func gitApplyCheck(repoDir string, diff []byte) error {
	cmd := exec.Command("git", "-C", repoDir, "apply", "--check", "-")
	cmd.Stdin = bytes.NewReader(diff)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git apply --check failed: %s", strings.TrimSpace(stderr.String()))
	}
	return nil
}

// graphCountSnapshot is a tiny tuple for the before/after graph stats
// recorded in the run summary.
type graphCountSnapshot struct {
	nodes int
	edges int
}

// graphCounts builds the typed graph at repoDir and returns node/edge
// counts. Used for before/after summary stats.
func graphCounts(repoDir, pattern string, tests bool) (graphCountSnapshot, error) {
	res, err := parser.Build(parser.Options{
		Dir:      repoDir,
		Patterns: []string{pattern},
		Tests:    tests,
	})
	if err != nil {
		return graphCountSnapshot{}, err
	}
	return graphCountSnapshot{nodes: res.Graph.NodeCount(), edges: res.Graph.EdgeCount()}, nil
}

// exportGraphML builds the typed graph at repoDir (with contracts
// resolved) and writes GraphML to <batchDir>/graph.graphml. Mirrors the
// `archmotif graph --format graphml` path so the snapshots match what
// an operator would inspect interactively.
func exportGraphML(repoDir, pattern string, tests bool, batchDir string) error {
	res, err := contracts.Build(contracts.BuildOptions{
		Dir:      repoDir,
		Patterns: []string{pattern},
		Tests:    tests,
	})
	if err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(batchDir, "graph.graphml"))
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return res.Graph.WriteGraphML(f)
}

// writeBatchContract renders the proposal's skeleton YAML (the
// "contract" the materializer is asked to satisfy) into batchDir.
func writeBatchContract(batchDir string, p *propose.Proposal) error {
	yaml, err := skeleton.RenderYAML(p)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(batchDir, "contract.yaml"), yaml, 0o644)
}

// writeBatchProposal pickles the picked proposal as JSON so the run
// directory carries enough state to re-run a single batch out-of-band.
func writeBatchProposal(batchDir string, p *propose.Proposal) error {
	f, err := os.Create(filepath.Join(batchDir, "proposal.json"))
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(p)
}

// writeBatchLog writes content to <batchDir>/<name>, creating the file
// if absent. Best-effort — caller logs the error and continues.
func writeBatchLog(batchDir, name, content string) error {
	return os.WriteFile(filepath.Join(batchDir, name), []byte(content), 0o644)
}

// writeSummary persists the runResult as <runDir>/summary.json.
func writeSummary(r *OptimizeLoopRunReport) error {
	if r == nil || r.RunDir == "" {
		return nil
	}
	f, err := os.Create(filepath.Join(r.RunDir, "summary.json"))
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// writeRunSummaryText prints a one-screen run summary to stdout.
// Format mirrors the `archmotif refactor` style: result lines on
// stdout, chatter on stderr.
func writeRunSummaryText(w io.Writer, r *OptimizeLoopRunReport) {
	if r == nil {
		return
	}
	_, _ = fmt.Fprintf(w, "archmotif optimize-loop: %d batch(es), stopped by %s\n", len(r.Batches), r.StoppedBy)
	_, _ = fmt.Fprintf(w, "  run dir: %s\n", r.RunDir)
	_, _ = fmt.Fprintf(w, "  graph: nodes %d → %d, edges %d → %d\n",
		r.BeforeNodes, r.AfterNodes, r.BeforeEdges, r.AfterEdges)
	if len(r.Batches) > 0 {
		_, _ = fmt.Fprintln(w, "  batches:")
		for _, b := range r.Batches {
			label := b.RuleKind
			if label == "" {
				label = "(none)"
			}
			_, _ = fmt.Fprintf(w, "    %d. %s — %s [%s]\n", b.Index, b.ProposalID, label, b.Outcome)
		}
	}
}

// runMaterializerCommand is the production materializer runner. It
// invokes the configured shell command with the rendered prompt on
// stdin and returns its stdout/stderr. The command string is parsed
// like a POSIX shell would (whitespace splits args; no globbing or
// pipes).
//
// Tests inject a fake runner via optimizeLoopConfig.runMaterializer so the
// loop can be exercised without spawning a subprocess.
func runMaterializerCommand(cmd, prompt string) (string, string, error) {
	parts := splitShellCommand(cmd)
	if len(parts) == 0 {
		return "", "", fmt.Errorf("empty materializer command")
	}
	c := exec.Command(parts[0], parts[1:]...)
	c.Stdin = strings.NewReader(prompt)
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	err := c.Run()
	if err != nil {
		return stdout.String(), stderr.String(), fmt.Errorf("%w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), stderr.String(), nil
}

// splitShellCommand splits a command string into argv. Honours single
// and double quotes; does not honour backslash escapes (the
// materializer command is config, not user input). Empty input → empty
// slice.
func splitShellCommand(s string) []string {
	var out []string
	var cur strings.Builder
	var quote rune
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
				continue
			}
			cur.WriteRune(r)
		case r == '\'' || r == '"':
			quote = r
		case r == ' ' || r == '\t':
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// extractPatch pulls the patch body from materializer stdout. Accepts
// two forms:
//
//  1. A ```diff fenced block (the convention used by the in-process
//     Anthropic materializer in internal/llm).
//  2. Bare unified-diff text (the simpler form a CLI materializer like
//     `claude -p` may emit when configured to skip prose).
//
// Returns ErrNoPatch when neither form is detectable.
func extractPatch(out string) ([]byte, error) {
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		return nil, ErrNoPatch
	}
	// Try fenced form first; the in-process Anthropic materializer
	// already validates this shape so we reuse the same regex.
	if strings.Contains(trimmed, "```diff") {
		matches := fencedDiffOptimizeRE.FindAllStringSubmatch(out, -1)
		if len(matches) >= 1 {
			body := matches[0][1]
			if !strings.HasSuffix(body, "\n") {
				body += "\n"
			}
			return []byte(body), nil
		}
	}
	// Fall back to bare unified diff. A unified diff starts with
	// "diff --git " or with a "--- " / "+++ " pair on consecutive
	// lines; we accept either.
	if strings.HasPrefix(trimmed, "diff --git ") || (strings.HasPrefix(trimmed, "--- ") && strings.Contains(trimmed, "\n+++ ")) {
		body := out
		if !strings.HasSuffix(body, "\n") {
			body += "\n"
		}
		return []byte(body), nil
	}
	return nil, ErrNoPatch
}

// ErrNoPatch is returned by extractPatch when the materializer stdout
// contains neither a ```diff fenced block nor a recognisable unified
// diff. The loop converts this to outcomeNoDiff in the summary.
var ErrNoPatch = errors.New("optimize: no patch in materializer output")

// fencedDiffOptimizeRE matches a ```diff fenced block. Mirrors the
// regex in internal/llm so this loop stays decoupled from the
// Anthropic-specific materializer's package-private internals.
var fencedDiffOptimizeRE = regexp.MustCompile("(?s)```diff\\s*\\n(.*?)\\n```")
