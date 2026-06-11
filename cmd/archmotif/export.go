package main

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/kgatilin/archmotif/internal/archai"
	"github.com/kgatilin/archmotif/internal/contracts"
	"github.com/kgatilin/archmotif/internal/roles"
)

// runExport implements `archmotif export [flags] <path>`.
//
// Today the only supported export format is `archai-model`: a stable
// JSON or YAML document that mirrors the typed graph in Archai's
// package / symbol / dependency / implementation vocabulary (issue #30,
// ADR-034). The command exists as its own subcommand — rather than
// piling another flag onto `archmotif graph` — because `graph` is the
// "raw typed graph" surface and `export` is the "projected for another
// tool" surface; mixing them would make the flag semantics fuzzy.
//
// Exit codes:
//
//	0 — model rendered to stdout
//	1 — pipeline / parse error
//	2 — argument or load error
func runExport(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("archmotif export", flag.ContinueOnError)
	fs.SetOutput(stderr)
	tests := fs.Bool("tests", false, "include _test.go files")
	pattern := fs.String("pattern", "./...", "go/packages pattern")
	format := fs.String("format", "archai-model", "export format: archai-model")
	encoding := fs.String("encoding", "json", "document encoding: json|yaml")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage:\n  archmotif export [flags] <path>\n\nFlags:\n")
		fs.PrintDefaults()
		_, _ = fmt.Fprintf(stderr, "\nFormats:\n  archai-model    Stable architecture-model document\n                  (see ADR-034 / docs/decisions/034-archai-bridge.md)\n")
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
	if *format != "archai-model" {
		_, _ = fmt.Fprintf(stderr, "archmotif export: unknown --format %q (want: archai-model)\n", *format)
		return 2
	}
	if *encoding != "json" && *encoding != "yaml" {
		_, _ = fmt.Fprintf(stderr, "archmotif export: unknown --encoding %q (want: json|yaml)\n", *encoding)
		return 2
	}

	dir := fs.Arg(0)

	// Use contracts.Build so contract markers (ADR-009) are applied
	// before projection. Roles are applied here at the call site for
	// the same reason coupling.go does — see ADR-027 / contracts.Build
	// import-cycle note.
	res, err := contracts.Build(contracts.BuildOptions{
		Dir:      dir,
		Patterns: []string{*pattern},
		Tests:    *tests,
	})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif export: %v\n", err)
		return 1
	}
	for _, e := range res.LoadErrors {
		_, _ = fmt.Fprintf(stderr, "load: %s\n", e)
	}
	for _, m := range res.KindMismatches {
		_, _ = fmt.Fprintf(stderr, "warning: %s\n", m)
	}
	if resolution := roles.Resolve(res.Graph, res.Config.Roles); len(resolution) > 0 {
		_ = roles.Apply(res.Graph, resolution)
	}

	model := archai.FromGraph(res.Graph)

	switch *encoding {
	case "yaml":
		if err := archai.WriteYAML(stdout, model); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif export: write yaml: %v\n", err)
			return 1
		}
	default:
		if err := archai.WriteJSON(stdout, model); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif export: write json: %v\n", err)
			return 1
		}
	}
	return 0
}
