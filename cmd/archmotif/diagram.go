package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/kgatilin/archmotif/internal/contracts"
	"github.com/kgatilin/archmotif/internal/diagram"
	"github.com/kgatilin/archmotif/internal/roles"
)

// splitDiagramArgs separates flag tokens from positional tokens so the
// CLI accepts both `archmotif diagram --format=d2 package-deps .` and
// `archmotif diagram package-deps --format d2 .`. Flags that take a
// value (e.g. `--format json`) are recognised so the value isn't
// treated as a positional. Flags that consume no value
// (`--include-foreign`, `--list`, `--tests`) and `--name=value`
// short-forms are passed through untouched.
func splitDiagramArgs(args []string) (flagArgs, positional []string) {
	valued := map[string]bool{
		"--format":  true,
		"-format":   true,
		"--seed":    true,
		"-seed":     true,
		"--depth":   true,
		"-depth":    true,
		"--pattern": true,
		"-pattern":  true,
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			// `--name=value` forms always parse cleanly.
			flagArgs = append(flagArgs, a)
			if valued[a] && !strings.Contains(a, "=") && i+1 < len(args) {
				flagArgs = append(flagArgs, args[i+1])
				i++
			}
			continue
		}
		positional = append(positional, a)
	}
	return flagArgs, positional
}

// runDiagram implements the `archmotif diagram <kind> [flags] <path>`
// subcommand. It loads the typed graph (via contracts.Build, so role
// + contract markers from .archmotif.yaml are applied), runs the
// requested projection, and renders to stdout in the chosen format.
//
// Exit codes:
//
//	0 — projection rendered successfully
//	1 — pipeline / parse / render error
//	2 — argument error or unknown kind/format
func runDiagram(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("archmotif diagram", flag.ContinueOnError)
	fs.SetOutput(stderr)
	tests := fs.Bool("tests", false, "include _test.go files")
	pattern := fs.String("pattern", "./...", "go/packages pattern")
	format := fs.String("format", "d2", "output format: d2|json|graphml")
	seedFlag := fs.String("seed", "", "comma-separated seed list (call-flow only); QName or stable ID")
	depth := fs.Int("depth", 0, "BFS depth for call-flow (default: 3)")
	includeForeign := fs.Bool("include-foreign", false, "keep nodes/packages flagged foreign=true")
	listOnly := fs.Bool("list", false, "list registered diagram kinds and exit")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage:\n  archmotif diagram <kind> [flags] <path>\n\nFlags:\n")
		fs.PrintDefaults()
		_, _ = fmt.Fprintf(stderr, "\nKinds:\n")
		for _, k := range diagram.AllKinds() {
			_, _ = fmt.Fprintf(stderr, "  %-16s %s\n", k, diagram.Description(k))
		}
		_, _ = fmt.Fprintf(stderr, "\nSee docs/decisions/035-diagram-projections.md for the projection model.\n")
	}
	// Allow kind/path positionals to appear interleaved with flags by
	// stripping them out of args before flag.Parse. Go's flag package
	// stops at the first non-flag, which would otherwise force users
	// to write `archmotif diagram --format=d2 package-deps ./` only.
	flagArgs, positional := splitDiagramArgs(args)
	if err := fs.Parse(flagArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if *listOnly {
		for _, k := range diagram.AllKinds() {
			_, _ = fmt.Fprintf(stdout, "%-16s %s\n", k, diagram.Description(k))
		}
		return 0
	}

	if len(positional) < 2 {
		fs.Usage()
		return 2
	}
	kind, err := diagram.ParseKind(positional[0])
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif diagram: %v\n", err)
		return 2
	}
	fmtVal, err := diagram.ParseFormat(*format)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif diagram: %v\n", err)
		return 2
	}
	dir := positional[len(positional)-1]

	res, err := contracts.Build(contracts.BuildOptions{
		Dir:      dir,
		Patterns: []string{*pattern},
		Tests:    *tests,
	})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif diagram: %v\n", err)
		return 1
	}
	for _, e := range res.LoadErrors {
		_, _ = fmt.Fprintf(stderr, "load: %s\n", e)
	}
	for _, m := range res.KindMismatches {
		_, _ = fmt.Fprintf(stderr, "warning: %s\n", m)
	}

	// Apply role metadata at the call site (matches coupling.go;
	// contracts.Build skips this to avoid an import cycle).
	if resolution := roles.Resolve(res.Graph, res.Config.Roles); len(resolution) > 0 {
		_ = roles.Apply(res.Graph, resolution)
	}

	opts := diagram.Options{
		Depth:          *depth,
		IncludeForeign: *includeForeign,
	}
	if strings.TrimSpace(*seedFlag) != "" {
		for _, s := range strings.Split(*seedFlag, ",") {
			if t := strings.TrimSpace(s); t != "" {
				opts.Seeds = append(opts.Seeds, t)
			}
		}
	}

	d, err := diagram.Build(res.Graph, kind, opts)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif diagram: %v\n", err)
		return 1
	}
	if err := diagram.Render(stdout, d, fmtVal); err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif diagram: render: %v\n", err)
		return 1
	}
	return 0
}
