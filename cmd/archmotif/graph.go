package main

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/kgatilin/archmotif/internal/contracts"
	mgraph "github.com/kgatilin/archmotif/internal/graph"
	"github.com/kgatilin/archmotif/internal/parser"
)

// runGraph implements the `archmotif graph <path>` subcommand. It builds
// the typed graph from the supplied path and emits JSON to stdout (and
// progress / errors to stderr). Use --format=pretty for a grouped
// human-readable dump, --format=graphml for Gephi/Cytoscape import, or
// --summary for just the node/edge counts.
func runGraph(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("archmotif graph", flag.ContinueOnError)
	fs.SetOutput(stderr)
	tests := fs.Bool("tests", false, "include _test.go files")
	pattern := fs.String("pattern", "./...", "go/packages pattern")
	summary := fs.Bool("summary", false, "print node/edge counts only")
	format := fs.String("format", "json", "output format: json|pretty|graphml")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage:\n  archmotif graph [flags] <path>\n\nFlags:\n")
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

	if *format == "graphml" && !*summary {
		res, err := contracts.Build(contracts.BuildOptions{
			Dir:      dir,
			Patterns: []string{*pattern},
			Tests:    *tests,
		})
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif graph: %v\n", err)
			return 1
		}
		printGraphContractDiagnostics(res, stderr)
		if err := res.Graph.WriteGraphML(stdout); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif graph: write graphml: %v\n", err)
			return 1
		}
		return 0
	}

	res, err := parser.Build(parser.Options{
		Dir:      dir,
		Patterns: []string{*pattern},
		Tests:    *tests,
	})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif graph: %v\n", err)
		return 1
	}
	for _, e := range res.LoadErrors {
		_, _ = fmt.Fprintf(stderr, "load: %s\n", e)
	}
	if *summary {
		_, _ = fmt.Fprintf(stdout, "nodes=%d edges=%d\n", res.Graph.NodeCount(), res.Graph.EdgeCount())
		return 0
	}
	switch *format {
	case "pretty":
		if err := mgraph.PrettyPrint(res.Graph, stdout); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif graph: pretty-print: %v\n", err)
			return 1
		}
	case "json", "":
		if err := res.Graph.WriteJSON(stdout); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif graph: write json: %v\n", err)
			return 1
		}
	default:
		_, _ = fmt.Fprintf(stderr, "archmotif graph: unknown format %q (want: json|pretty|graphml)\n", *format)
		return 2
	}
	return 0
}

func printGraphContractDiagnostics(res *contracts.Result, stderr io.Writer) {
	for _, e := range res.LoadErrors {
		_, _ = fmt.Fprintf(stderr, "load: %s\n", e)
	}
	for _, m := range res.KindMismatches {
		_, _ = fmt.Fprintf(stderr, "warning: %s\n", m)
	}
	for _, u := range res.Unresolved {
		_, _ = fmt.Fprintf(stderr, "unresolved: %s — %s\n", u.Entry.Identifier(), u.Reason)
	}
}
