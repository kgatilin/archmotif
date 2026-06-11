package main

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/kgatilin/archmotif/internal/catalog"
	"github.com/kgatilin/archmotif/internal/parser"
)

// runCatalog implements the `archmotif catalog` subcommand: capture a
// snapshot of metric values, motif counts, and pattern reports for
// the supplied path and persist it to .archmotif/catalog.yaml (or
// the path given via --catalog).
//
// Snapshots are upserted by --label so re-running with the same
// label overwrites the previous entry. This is intentional: a
// typical workflow runs `archmotif catalog --label main` on every CI
// build at HEAD, and we want the file bounded by distinct labels,
// not build count.
//
// Per ADR-037, the snapshot persists only graph-scope information
// plus a per-canonical-form motif histogram — node-level IDs would
// not survive across refs.
func runCatalog(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("archmotif catalog", flag.ContinueOnError)
	fs.SetOutput(stderr)
	tests := fs.Bool("tests", false, "include _test.go files")
	pattern := fs.String("pattern", "./...", "go/packages pattern")
	label := fs.String("label", "", "snapshot label (required) — primary key in the catalog")
	ref := fs.String("ref", "", "free-form reference (e.g. git SHA), recorded verbatim")
	catalogPath := fs.String("catalog", catalog.DefaultPath, "catalog file to read/write")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage:\n  archmotif catalog --label <name> [flags] <path>\n\nFlags:\n")
		fs.PrintDefaults()
		_, _ = fmt.Fprintf(stderr, "\nThe catalog file is created if it doesn't exist.\nRunning with the same --label overwrites the prior snapshot.\n")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *label == "" {
		_, _ = fmt.Fprintf(stderr, "archmotif catalog: --label is required\n")
		fs.Usage()
		return 2
	}
	if fs.NArg() < 1 {
		fs.Usage()
		return 2
	}
	dir := fs.Arg(0)

	res, err := parser.Build(parser.Options{
		Dir:      dir,
		Patterns: []string{*pattern},
		Tests:    *tests,
	})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif catalog: %v\n", err)
		return 1
	}
	for _, e := range res.LoadErrors {
		_, _ = fmt.Fprintf(stderr, "load: %s\n", e)
	}

	snap, capErr := catalog.Capture(res.Graph, catalog.CaptureOptions{
		Label:   *label,
		Ref:     *ref,
		Path:    dir,
		Pattern: *pattern,
	})
	if capErr != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif catalog: %v\n", capErr)
		// We still persist whatever we managed to compute — that
		// matches `archmotif metrics`'s behaviour where one broken
		// metric does not blank the whole run.
	}

	cat, err := catalog.Load(*catalogPath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif catalog: load %s: %v\n", *catalogPath, err)
		return 1
	}
	cat.Upsert(snap)
	if err := catalog.Save(*catalogPath, cat); err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif catalog: save %s: %v\n", *catalogPath, err)
		return 1
	}

	_, _ = fmt.Fprintf(stdout,
		"catalog: snapshot %q saved to %s — metrics=%d motifs=%d/%d patterns=%d\n",
		snap.Label, *catalogPath,
		len(snap.Metrics),
		snap.Motifs.TotalGroups, snap.Motifs.TotalInstances,
		len(snap.Patterns),
	)
	if capErr != nil {
		return 1
	}
	return 0
}
