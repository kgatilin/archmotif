package main

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/kgatilin/archmotif/internal/contracts"
)

// runContracts implements the `archmotif contracts <path>` subcommand.
// It loads the typed graph (Stage 1), reads `.archmotif.yaml` from the
// loaded module's root, marks the declared contracts in the graph, and
// emits a summary of contracts plus their one-hop producers (per
// ADR-010). Output format mirrors `archmotif graph`: JSON by default,
// `--format=pretty` for a grouped human-readable dump.
func runContracts(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("archmotif contracts", flag.ContinueOnError)
	fs.SetOutput(stderr)
	tests := fs.Bool("tests", false, "include _test.go files")
	pattern := fs.String("pattern", "./...", "go/packages pattern")
	format := fs.String("format", "pretty", "output format: pretty|json")
	configPath := fs.String("config", "", "explicit path to .archmotif.yaml (overrides module-root lookup)")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage:\n  archmotif contracts [flags] <path>\n\nFlags:\n")
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
	dir := fs.Arg(0)

	res, err := contracts.Build(contracts.BuildOptions{
		Dir:        dir,
		Patterns:   []string{*pattern},
		Tests:      *tests,
		ConfigPath: *configPath,
	})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif contracts: %v\n", err)
		return 1
	}
	for _, e := range res.LoadErrors {
		_, _ = fmt.Fprintf(stderr, "load: %s\n", e)
	}
	for _, m := range res.KindMismatches {
		_, _ = fmt.Fprintf(stderr, "warning: %s\n", m)
	}
	for _, u := range res.Unresolved {
		_, _ = fmt.Fprintf(stderr, "unresolved: %s — %s\n", u.Entry.Identifier(), u.Reason)
	}

	switch *format {
	case "pretty", "":
		if err := res.PrettyPrint(stdout); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif contracts: pretty-print: %v\n", err)
			return 1
		}
	case "json":
		if err := res.WriteJSON(stdout); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif contracts: write json: %v\n", err)
			return 1
		}
	default:
		_, _ = fmt.Fprintf(stderr, "archmotif contracts: unknown format %q (want: pretty|json)\n", *format)
		return 2
	}
	return 0
}
