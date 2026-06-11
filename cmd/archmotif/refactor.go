package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/kgatilin/archmotif/internal/anomalies"
	"github.com/kgatilin/archmotif/internal/llm"
	"github.com/kgatilin/archmotif/internal/metrics"
	"github.com/kgatilin/archmotif/internal/parser"
	"github.com/kgatilin/archmotif/internal/propose"
	"github.com/kgatilin/archmotif/internal/skeleton"
	"github.com/kgatilin/archmotif/internal/verify"
)

// runRefactor implements the `archmotif refactor` subcommand. It runs
// the Stage 3 → 4 → 5 pipeline, picks the proposal (matching --id, or
// auto-picks the highest-impact candidate when --id is omitted —
// ADR-031), reads the affected file contents into the LLM Proposal,
// and either:
//
//   - Lists all proposals in auto-pick order (--list), or
//   - Renders the prompt and prints it to stdout (--dry-run), or
//   - Calls the Anthropic materializer, applies the diff on a fresh
//     branch, and runs the Stage 8 verifier against the result.
//
// Per ADR-024 the surface is intentionally minimal: one positional
// path, optional --id, optional --model and --dry-run. ADR-028
// adds the post-Apply verifier knobs. ADR-031 makes --id optional,
// adds --list, and pins the empty-pipeline behaviour ("nothing to
// propose" + exit 0).
//
//	archmotif refactor [flags] <path>
//	  --id=<proposal-id>      explicit proposal id; auto-pick when omitted
//	  --list                  list proposals in auto-pick order and exit
//	  --model=...             override default Sonnet (e.g. claude-opus-4-7)
//	  --dry-run               render prompt; do not call API
//	  --tests                 include _test.go in the upstream pipeline
//	  --pattern=...           go/packages pattern
//	  --no-verify             skip Stage 8 verification after Apply
//	  --verify-format=text|json
//	                          verifier verdict format (default: text)
//
// Exit codes (per ADR-028 + ADR-031): 0 success or "nothing to
// propose", 1 LLM error or verifier mismatch (branch kept), 2
// argument error.
func runRefactor(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("archmotif refactor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	id := fs.String("id", "", "proposal id to materialize (default: auto-pick by impact)")
	list := fs.Bool("list", false, "list proposals in auto-pick order and exit")
	model := fs.String("model", "", "override LLM model (e.g. claude-opus-4-7)")
	dryRun := fs.Bool("dry-run", false, "print rendered prompt instead of calling the API")
	tests := fs.Bool("tests", false, "include _test.go files in pipeline")
	pattern := fs.String("pattern", "./...", "go/packages pattern")
	noVerify := fs.Bool("no-verify", false, "skip Stage 8 verification after the LLM diff is applied")
	verifyFormat := fs.String("verify-format", "text", "verifier verdict format: text|json")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage:\n  archmotif refactor [flags] <path>\n\nFlags:\n")
		fs.PrintDefaults()
		_, _ = fmt.Fprintf(stderr, "\nWhen --id is omitted, auto-picks the proposal with the highest Trigger.Value.\nSee ADR-031.\n")
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
	if *verifyFormat != "text" && *verifyFormat != "json" {
		_, _ = fmt.Fprintf(stderr, "archmotif refactor: --verify-format=%q (want: text|json)\n", *verifyFormat)
		return 2
	}
	dir := fs.Arg(0)

	// Run the Stage 3 → 4 → 5 pipeline once and pick (or list) from
	// the result. Single pipeline run keeps --list and the auto-pick
	// path consistent with each other.
	proposals, err := runProposalPipeline(dir, *pattern, *tests, stderr)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif refactor: %v\n", err)
		return 1
	}
	ranked := rankProposals(proposals)

	if *list {
		// ADR-031 §3: --list always exits 0 with the proposal stream.
		writeProposalList(stdout, ranked)
		return 0
	}

	// ADR-031 §2: empty pipeline → "nothing to propose" + exit 0.
	if len(ranked) == 0 {
		_, _ = fmt.Fprintln(stdout, "archmotif refactor: nothing to propose")
		return 0
	}

	prop, err := pickProposal(ranked, *id, stderr)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif refactor: %v\n", err)
		return 1
	}

	llmProp, err := buildLLMProposal(dir, prop, *model)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif refactor: %v\n", err)
		return 1
	}

	if *dryRun {
		prompt, err := llm.RenderPrompt(llmProp)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif refactor: render prompt: %v\n", err)
			return 1
		}
		_, _ = fmt.Fprintln(stdout, prompt)
		return 0
	}

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		_, _ = fmt.Fprintln(stderr, "archmotif refactor: ANTHROPIC_API_KEY not set; use --dry-run to render the prompt without calling the API")
		return 1
	}

	mat := llm.NewAnthropic(apiKey)
	mat.RepoDir = dir

	br, err := mat.Apply(context.Background(), llmProp)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif refactor: %v\n", err)
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "branch: %s\n", br.Name)
	_, _ = fmt.Fprintf(stdout, "applied_at: %s\n", br.AppliedAt)
	_, _ = fmt.Fprintf(stdout, "diff_bytes: %d\n", len(br.Diff))

	if *noVerify {
		return 0
	}
	matched, vErr := verifyAfterRefactor(dir, prop, *pattern, *tests, *verifyFormat, stdout, stderr)
	if vErr != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif refactor: verify: %v\n", vErr)
		return 1
	}
	if !matched {
		// ADR-028 §3: branch stays, verdict already on stdout, exit 1
		// so pipeline scripts don't auto-merge a mismatched refactor.
		return 1
	}
	return 0
}

// verifyAfterRefactor implements ADR-028: render the proposal's
// skeleton YAML, parse it as a verifier Skeleton, build the typed
// graph from the post-Apply working tree, and run the Stage 8
// verifier. The verdict is written to stdout in the requested format
// using the same renderer as `archmotif verify`.
//
// Exposed within the package so refactor_verify_test.go can exercise
// it without touching the LLM materializer.
func verifyAfterRefactor(repoDir string, prop *propose.Proposal, pattern string, tests bool, format string, stdout, stderr io.Writer) (bool, error) {
	yamlBytes, err := skeleton.RenderYAML(prop)
	if err != nil {
		return false, fmt.Errorf("render skeleton yaml: %w", err)
	}
	skel, err := verify.ParseSkeleton(bytes.NewReader(yamlBytes))
	if err != nil {
		return false, fmt.Errorf("parse skeleton: %w", err)
	}

	res, err := parser.Build(parser.Options{
		Dir:      repoDir,
		Patterns: []string{pattern},
		Tests:    tests,
	})
	if err != nil {
		return false, fmt.Errorf("build graph: %w", err)
	}
	for _, e := range res.LoadErrors {
		_, _ = fmt.Fprintf(stderr, "load: %s\n", e)
	}

	verdict := verify.NewBacktrackVerifier().Verify(context.Background(), skel, res.Graph)

	switch format {
	case "json":
		if err := verify.FormatJSON(stdout, skel.ProposalID, verdict); err != nil {
			return false, fmt.Errorf("render verdict (json): %w", err)
		}
	default:
		if err := verify.FormatText(stdout, skel.ProposalID, verdict); err != nil {
			return false, fmt.Errorf("render verdict (text): %w", err)
		}
	}
	return verdict.Match, nil
}

// runProposalPipeline runs the Stage 3 → 4 → 5 pipeline once and
// returns the surfaced proposals. Errors at any stage propagate;
// per-record / per-rule warnings are emitted to stderr and don't
// abort the run.
func runProposalPipeline(dir, pattern string, tests bool, stderr io.Writer) ([]*propose.Proposal, error) {
	res, err := parser.Build(parser.Options{
		Dir:      dir,
		Patterns: []string{pattern},
		Tests:    tests,
	})
	if err != nil {
		return nil, err
	}
	for _, e := range res.LoadErrors {
		_, _ = fmt.Fprintf(stderr, "load: %s\n", e)
	}

	mres := metrics.Run(res.Graph, nil)
	for _, e := range mres.Errors {
		_, _ = fmt.Fprintf(stderr, "metric error: %s\n", e.Error())
	}
	ares := anomalies.Run(res.Graph, mres.Records, nil)
	for _, e := range ares.Errors {
		_, _ = fmt.Fprintf(stderr, "detector error: %s\n", e.Error())
	}
	pres := propose.NewProposer().Propose(res.Graph, ares.Anomalies)
	for _, e := range pres.Errors {
		_, _ = fmt.Fprintf(stderr, "rule error: %s\n", e.Error())
	}
	return pres.Proposals, nil
}

// rankProposals returns proposals sorted by impact descending, with
// ID ascending as the deterministic tiebreaker. ADR-031 §1 commits to
// Trigger.Value as the impact scalar; proposals without a Trigger
// (e.g. test fixtures with no anomaly) sort to the bottom.
func rankProposals(in []*propose.Proposal) []*propose.Proposal {
	out := make([]*propose.Proposal, len(in))
	copy(out, in)
	sort.SliceStable(out, func(i, j int) bool {
		vi := triggerValue(out[i])
		vj := triggerValue(out[j])
		if vi != vj {
			return vi > vj
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// triggerValue extracts the impact scalar for ranking. Returns 0 when
// the proposal carries no Trigger; that's a defensive default — the
// v1 proposer always populates Trigger.
func triggerValue(p *propose.Proposal) float64 {
	if p == nil || p.Trigger == nil {
		return 0
	}
	return p.Trigger.Value
}

// pickProposal selects one proposal from a non-empty ranked slice. If
// id is non-empty, returns the proposal with that ID or an error
// listing available IDs. If id is empty, returns the top-ranked
// proposal and announces the auto-pick on stderr per ADR-031 §4.
func pickProposal(ranked []*propose.Proposal, id string, stderr io.Writer) (*propose.Proposal, error) {
	if len(ranked) == 0 {
		// pickProposal must not be called on an empty slice; the
		// caller handles "nothing to propose" before reaching this
		// path. Defence in depth.
		return nil, fmt.Errorf("no proposals available")
	}
	if id != "" {
		for _, p := range ranked {
			if p.ID == id {
				return p, nil
			}
		}
		ids := make([]string, 0, len(ranked))
		for _, p := range ranked {
			ids = append(ids, p.ID)
		}
		sort.Strings(ids)
		return nil, fmt.Errorf("proposal %q not found; available: %v", id, ids)
	}
	picked := ranked[0]
	_, _ = fmt.Fprintf(stderr, "archmotif refactor: auto-picked %s (value=%g; %d candidates)\n",
		picked.ID, triggerValue(picked), len(ranked))
	return picked, nil
}

// writeProposalList renders the ranked proposal stream for the --list
// flag: one line per proposal, "id<TAB>value<TAB>description". Stable
// across runs (ADR-031 §3).
func writeProposalList(w io.Writer, ranked []*propose.Proposal) {
	if len(ranked) == 0 {
		_, _ = fmt.Fprintln(w, "archmotif refactor: nothing to propose")
		return
	}
	for _, p := range ranked {
		_, _ = fmt.Fprintf(w, "%s\tvalue=%g\t%s\n", p.ID, triggerValue(p), p.Description)
	}
}

// buildLLMProposal converts a Stage 5 propose.Proposal into the LLM
// Proposal expected by the materializer. Reads each AffectedFile from
// disk so the prompt sees current contents, not stale snapshots.
func buildLLMProposal(repoDir string, src *propose.Proposal, model string) (llm.Proposal, error) {
	out := llm.Proposal{
		ID:            src.ID,
		Description:   src.Description,
		Samples:       src.Samples,
		Model:         model,
		AffectedFiles: map[string][]byte{},
	}
	for _, rel := range src.AffectedFiles {
		full := filepath.Join(repoDir, rel)
		b, err := os.ReadFile(full)
		if err != nil {
			return llm.Proposal{}, fmt.Errorf("read %s: %w", rel, err)
		}
		out.AffectedFiles[rel] = b
	}
	return out, nil
}
