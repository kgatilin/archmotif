package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/kgatilin/archmotif/internal/anomalies"
	"github.com/kgatilin/archmotif/internal/metrics"
	"github.com/kgatilin/archmotif/internal/parser"
	"github.com/kgatilin/archmotif/internal/propose"
	"github.com/kgatilin/archmotif/internal/skeleton"
)

// runSkeleton implements the `archmotif skeleton` subcommand. It
// drives the same Stage 3 → 4 → 5 pipeline as `archmotif propose`,
// then renders one skeleton pair (.go + .yaml) per accepted proposal
// using the package-level renderer (ADR-016 / ADR-023).
//
// Usage:
//
//	archmotif skeleton [flags] <path>
//	  --id=<proposal-id>   render only the proposal with this ID
//	  --out=<dir>          output directory (default ./skeletons/)
//	  --tests              include _test.go files
//	  --pattern=...        go/packages pattern
//
// Exit codes:
//
//	0 — success (one or more skeletons written)
//	1 — pipeline error or render error
//	2 — argument error or no proposal matched --id
func runSkeleton(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("archmotif skeleton", flag.ContinueOnError)
	fs.SetOutput(stderr)
	id := fs.String("id", "", "render only the proposal with this ID")
	out := fs.String("out", "./skeletons", "output directory for rendered skeletons")
	tests := fs.Bool("tests", false, "include _test.go files")
	pattern := fs.String("pattern", "./...", "go/packages pattern")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage:\n  archmotif skeleton [flags] <path>\n\nFlags:\n")
		fs.PrintDefaults()
		_, _ = fmt.Fprintf(stderr, "\nWrites <id>.skeleton.go and <id>.skeleton.yaml per proposal into --out.\n")
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
	dir := fs.Arg(0)

	proposals, code := buildProposals(dir, *pattern, *tests, stderr)
	if code != 0 {
		return code
	}

	// Filter by --id when present.
	if *id != "" {
		filtered := proposals[:0]
		for _, p := range proposals {
			if p.ID == *id {
				filtered = append(filtered, p)
			}
		}
		if len(filtered) == 0 {
			_, _ = fmt.Fprintf(stderr, "archmotif skeleton: no proposal with id %q (have %d)\n",
				*id, len(proposals))
			return 2
		}
		proposals = filtered
	}

	if len(proposals) == 0 {
		_, _ = fmt.Fprintln(stderr, "archmotif skeleton: no proposals to render")
		return 1
	}

	if err := os.MkdirAll(*out, 0o755); err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif skeleton: mkdir %s: %v\n", *out, err)
		return 1
	}
	for _, p := range proposals {
		goPath := filepath.Join(*out, p.ID+".skeleton.go")
		yamlPath := filepath.Join(*out, p.ID+".skeleton.yaml")
		goBytes, err := skeleton.RenderGo(p)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif skeleton: render Go for %s: %v\n", p.ID, err)
			return 1
		}
		yamlBytes, err := skeleton.RenderYAML(p)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif skeleton: render YAML for %s: %v\n", p.ID, err)
			return 1
		}
		if err := os.WriteFile(goPath, goBytes, 0o644); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif skeleton: write %s: %v\n", goPath, err)
			return 1
		}
		if err := os.WriteFile(yamlPath, yamlBytes, 0o644); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif skeleton: write %s: %v\n", yamlPath, err)
			return 1
		}
		_, _ = fmt.Fprintln(stdout, goPath)
		_, _ = fmt.Fprintln(stdout, yamlPath)
	}
	return 0
}

// buildProposals runs the propose pipeline on dir and returns
// accepted proposals. Returns an exit code on error so the caller
// can propagate it. Mirrors the wiring in cmd/archmotif/propose.go.
func buildProposals(dir, pattern string, tests bool, stderr io.Writer) ([]*propose.Proposal, int) {
	res, err := parser.Build(parser.Options{
		Dir:      dir,
		Patterns: []string{pattern},
		Tests:    tests,
	})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif skeleton: %v\n", err)
		return nil, 1
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
	return pres.Proposals, 0
}
