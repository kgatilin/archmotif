package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/kgatilin/archmotif/internal/targetcontract"
)

func runTarget(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		targetUsage(stderr)
		return 2
	}
	if args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		targetUsage(stderr)
		return 0
	}
	switch args[0] {
	case "contract":
		return runTargetContract(args[1:], stdout, stderr)
	case "scaffold":
		return runTargetScaffold(args[1:], stdout, stderr)
	case "verify":
		return runTargetVerify(args[1:], stdout, stderr)
	default:
		_, _ = fmt.Fprintf(stderr, "archmotif target: unknown subcommand %q (want: contract|scaffold|verify)\n", args[0])
		return 2
	}
}

func targetUsage(stderr io.Writer) {
	_, _ = fmt.Fprintf(stderr, "Usage:\n")
	_, _ = fmt.Fprintf(stderr, "  archmotif target contract [flags] <optimize-contract.json>\n")
	_, _ = fmt.Fprintf(stderr, "  archmotif target scaffold [flags] <target-contract.json>\n")
	_, _ = fmt.Fprintf(stderr, "  archmotif target verify [flags] <target-contract.json> <code-path>\n")
}

func runTargetContract(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("archmotif target contract", flag.ContinueOnError)
	fs.SetOutput(stderr)
	id := fs.String("id", "", "optimization contract or proposal id to render (default: first)")
	out := fs.String("out", "", "path to write target contract JSON (default: stdout)")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage:\n  archmotif target contract [flags] <optimize-contract.json>\n\nFlags:\n")
		fs.PrintDefaults()
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
	env, err := targetcontract.LoadOptimizeEnvelopeFile(fs.Arg(0))
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif target contract: %v\n", err)
		return 1
	}
	contract, err := targetcontract.BuildFromOptimizeEnvelope(env, *id)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif target contract: %v\n", err)
		return 1
	}
	if *out != "" {
		if err := targetcontract.WriteFile(*out, contract); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif target contract: write %s: %v\n", *out, err)
			return 1
		}
		_, _ = fmt.Fprintln(stdout, *out)
		return 0
	}
	if err := targetcontract.FormatJSON(stdout, contract); err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif target contract: render: %v\n", err)
		return 1
	}
	return 0
}

func runTargetScaffold(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("archmotif target scaffold", flag.ContinueOnError)
	fs.SetOutput(stderr)
	out := fs.String("out", ".", "output directory for scaffold files")
	force := fs.Bool("force", false, "overwrite existing scaffold files")
	format := fs.String("format", "text", "output format: text|json")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage:\n  archmotif target scaffold [flags] <target-contract.json>\n\nFlags:\n")
		fs.PrintDefaults()
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
	contract, err := targetcontract.LoadFile(fs.Arg(0))
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif target scaffold: %v\n", err)
		return 1
	}
	res, err := targetcontract.Scaffold(contract, *out, *force)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif target scaffold: %v\n", err)
		return 1
	}
	switch *format {
	case "json":
		if err := targetcontract.FormatJSON(stdout, res); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif target scaffold: render: %v\n", err)
			return 1
		}
	case "text", "":
		for _, path := range res.Created {
			_, _ = fmt.Fprintf(stdout, "created %s\n", path)
		}
		for _, path := range res.Skipped {
			_, _ = fmt.Fprintf(stdout, "skipped %s\n", path)
		}
	default:
		_, _ = fmt.Fprintf(stderr, "archmotif target scaffold: unknown format %q (want: text|json)\n", *format)
		return 2
	}
	return 0
}

func runTargetVerify(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("archmotif target verify", flag.ContinueOnError)
	fs.SetOutput(stderr)
	pattern := fs.String("pattern", "./...", "go/packages pattern")
	tests := fs.Bool("tests", false, "include _test.go files")
	format := fs.String("format", "text", "output format: text|json")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage:\n  archmotif target verify [flags] <target-contract.json> <code-path>\n\nFlags:\n")
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
	contract, err := targetcontract.LoadFile(fs.Arg(0))
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif target verify: %v\n", err)
		return 2
	}
	res, err := targetcontract.Verify(context.Background(), contract, fs.Arg(1), *pattern, *tests)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif target verify: %v\n", err)
		return 2
	}
	switch *format {
	case "json":
		if err := targetcontract.FormatJSON(stdout, res); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif target verify: render: %v\n", err)
			return 2
		}
	case "text", "":
		if err := targetcontract.FormatVerifyText(stdout, res); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif target verify: render: %v\n", err)
			return 2
		}
	default:
		_, _ = fmt.Fprintf(stderr, "archmotif target verify: unknown format %q (want: text|json)\n", *format)
		return 2
	}
	if !res.Match {
		return 1
	}
	return 0
}
