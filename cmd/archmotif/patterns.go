package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/kgatilin/archmotif/internal/parser"
	"github.com/kgatilin/archmotif/internal/patterns"
)

func runPatterns(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("archmotif patterns", flag.ContinueOnError)
	fs.SetOutput(stderr)
	tests := fs.Bool("tests", false, "include _test.go files")
	pattern := fs.String("pattern", "./...", "go/packages pattern")
	format := fs.String("format", "text", "output format: text|json")
	only := fs.String("pattern-id", "", "comma-separated list of pattern IDs to run (default: all registered)")
	listOnly := fs.Bool("list", false, "list registered patterns and exit")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage:\n  archmotif patterns [flags] <path>\n\nFlags:\n")
		fs.PrintDefaults()
		_, _ = fmt.Fprintf(stderr, "\nRegistered patterns: %s\n", strings.Join(patterns.IDs(), ", "))
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if *listOnly {
		for _, p := range patterns.All() {
			_, _ = fmt.Fprintf(stdout, "%-28s %s\n", p.ID(), p.Description())
		}
		return 0
	}

	if fs.NArg() < 1 {
		fs.Usage()
		return 2
	}

	res, err := parser.Build(parser.Options{
		Dir:      fs.Arg(0),
		Patterns: []string{*pattern},
		Tests:    *tests,
	})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif patterns: %v\n", err)
		return 1
	}
	for _, e := range res.LoadErrors {
		_, _ = fmt.Fprintf(stderr, "load: %s\n", e)
	}
	g := res.Graph

	var ids []string
	if *only != "" {
		for _, s := range strings.Split(*only, ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				ids = append(ids, s)
			}
		}
	}

	rep := patterns.Run(g, ids)

	switch *format {
	case "text", "":
		if err := rep.WriteText(stdout); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif patterns: write text: %v\n", err)
			return 1
		}
	case "json":
		if err := rep.WriteJSON(stdout); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif patterns: write json: %v\n", err)
			return 1
		}
	default:
		_, _ = fmt.Fprintf(stderr, "archmotif patterns: unknown format %q (want: text|json)\n", *format)
		return 2
	}
	return 0
}
