package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/kgatilin/archmotif/internal/parser"
	"github.com/kgatilin/archmotif/internal/verify"
)

// runVerify implements the `archmotif verify <skeleton.yaml> <code-path>`
// subcommand. It loads the Stage 6 skeleton, builds the typed graph
// from the code path, runs the verifier, and emits the result in the
// requested format.
//
// Exit codes:
//
//	0 — Match
//	1 — Mismatch (diff written to stdout in the chosen format)
//	2 — argument or load error (message on stderr)
func runVerify(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("archmotif verify", flag.ContinueOnError)
	fs.SetOutput(stderr)
	tests := fs.Bool("tests", false, "include _test.go files")
	pattern := fs.String("pattern", "./...", "go/packages pattern")
	format := fs.String("format", "text", "output format: text|json")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage:\n  archmotif verify [flags] <skeleton.yaml> <code-path>\n\nFlags:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() < 2 {
		fs.Usage()
		return 2
	}
	skeletonPath := fs.Arg(0)
	codeDir := fs.Arg(1)

	skel, err := verify.LoadSkeletonFile(skeletonPath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif verify: %v\n", err)
		return 2
	}

	res, err := parser.Build(parser.Options{
		Dir:      codeDir,
		Patterns: []string{*pattern},
		Tests:    *tests,
	})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif verify: %v\n", err)
		return 2
	}
	for _, e := range res.LoadErrors {
		_, _ = fmt.Fprintf(stderr, "load: %s\n", e)
	}

	verdict := verify.NewBacktrackVerifier().Verify(context.Background(), skel, res.Graph)

	switch *format {
	case "text", "":
		if err := verify.FormatText(stdout, skel.ProposalID, verdict); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif verify: render: %v\n", err)
			return 2
		}
	case "json":
		if err := verify.FormatJSON(stdout, skel.ProposalID, verdict); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif verify: render: %v\n", err)
			return 2
		}
	default:
		_, _ = fmt.Fprintf(stderr, "archmotif verify: unknown format %q (want: text|json)\n", *format)
		return 2
	}

	if !verdict.Match {
		return 1
	}
	return 0
}
