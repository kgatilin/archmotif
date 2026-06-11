package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/kgatilin/archmotif/internal/catalog"
)

// runDrift implements the `archmotif drift` subcommand: load two
// snapshots from the catalog file and emit their structured
// difference.
//
// Exit code is 0 on success regardless of whether anything drifted.
// Drift reporting is informational; gating CI on architectural
// regression is the operator's call (a thin shell wrapper that
// inspects the JSON output is enough).
func runDrift(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("archmotif drift", flag.ContinueOnError)
	fs.SetOutput(stderr)
	from := fs.String("from", "", "label of the baseline snapshot (required)")
	to := fs.String("to", "", "label of the target snapshot (required)")
	catalogPath := fs.String("catalog", catalog.DefaultPath, "catalog file to read")
	format := fs.String("format", "text", "output format: text|json")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage:\n  archmotif drift --from <label> --to <label> [--catalog <path>] [--format text|json]\n\nFlags:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *from == "" || *to == "" {
		_, _ = fmt.Fprintf(stderr, "archmotif drift: --from and --to are required\n")
		fs.Usage()
		return 2
	}

	cat, err := catalog.Load(*catalogPath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif drift: load %s: %v\n", *catalogPath, err)
		return 1
	}
	if len(cat.Snapshots) == 0 {
		_, _ = fmt.Fprintf(stderr, "archmotif drift: catalog %s is empty — capture snapshots first with `archmotif catalog --label <name>`\n", *catalogPath)
		return 1
	}

	fromSnap, ok := cat.Find(*from)
	if !ok {
		_, _ = fmt.Fprintf(stderr, "archmotif drift: snapshot %q not found in %s (available: %s)\n",
			*from, *catalogPath, strings.Join(cat.Labels(), ", "))
		return 1
	}
	toSnap, ok := cat.Find(*to)
	if !ok {
		_, _ = fmt.Fprintf(stderr, "archmotif drift: snapshot %q not found in %s (available: %s)\n",
			*to, *catalogPath, strings.Join(cat.Labels(), ", "))
		return 1
	}

	d := catalog.Diff(fromSnap, toSnap)
	switch *format {
	case "text", "":
		if err := d.WriteText(stdout); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif drift: write text: %v\n", err)
			return 1
		}
	case "json":
		if err := d.WriteJSON(stdout); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif drift: write json: %v\n", err)
			return 1
		}
	default:
		_, _ = fmt.Fprintf(stderr, "archmotif drift: unknown format %q (want: text|json)\n", *format)
		return 2
	}
	return 0
}
