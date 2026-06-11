package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/kgatilin/archmotif/internal/metrics"
	"github.com/kgatilin/archmotif/internal/parser"
)

// runMetrics implements the `archmotif metrics <path>` subcommand. It
// builds the typed graph from the supplied path, runs every registered
// metric (or only the metric named in --metric), and emits structured
// records.
//
// Output format defaults to JSON for machine consumption (Stage 4
// anomaly detection will read the file). --format=pretty renders a
// grouped human-readable summary.
func runMetrics(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("archmotif metrics", flag.ContinueOnError)
	fs.SetOutput(stderr)
	tests := fs.Bool("tests", false, "include _test.go files")
	pattern := fs.String("pattern", "./...", "go/packages pattern")
	format := fs.String("format", "json", "output format: json|pretty")
	only := fs.String("metric", "", "comma-separated list of metric names to run (default: all registered)")
	listMetrics := fs.Bool("list", false, "list registered metrics and exit")
	motifMaxSize := fs.Int("motif-max-size", metrics.DefaultMotifMaxSize, "max motif size for motif_redundancy (3..5)")
	motifSampleLimit := fs.Int("motif-sample-limit", metrics.DefaultMotifSampleLimit, "max sampled subgraphs for motif_redundancy")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage:\n  archmotif metrics [flags] <path>\n\nFlags:\n")
		fs.PrintDefaults()
		_, _ = fmt.Fprintf(stderr, "\nRegistered metrics: %s\n", strings.Join(metrics.Names(), ", "))
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *listMetrics {
		for _, m := range metrics.All() {
			_, _ = fmt.Fprintf(stdout, "%-20s %s\n", m.Name(), m.Description())
		}
		return 0
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
		_, _ = fmt.Fprintf(stderr, "archmotif metrics: %v\n", err)
		return 1
	}
	for _, e := range res.LoadErrors {
		_, _ = fmt.Fprintf(stderr, "load: %s\n", e)
	}

	// Re-register MotifRedundancy with user-tuned bounds when defaults
	// were overridden. The metric type is value-receiver so we can
	// instantiate fresh; we don't mutate the registry here — instead
	// we run that metric directly when knobs are non-default and
	// suppress the default-instance run.
	useCustomMotif := *motifMaxSize != metrics.DefaultMotifMaxSize ||
		*motifSampleLimit != metrics.DefaultMotifSampleLimit

	var names []string
	if *only != "" {
		for _, n := range strings.Split(*only, ",") {
			n = strings.TrimSpace(n)
			if n != "" {
				names = append(names, n)
			}
		}
	}

	var run metrics.Result
	if useCustomMotif {
		// Build the metric set by filtering out motif_redundancy from
		// the runner, then injecting our tuned instance.
		runset := names
		if len(runset) == 0 {
			for _, m := range metrics.All() {
				if m.Name() == "motif_redundancy" {
					continue
				}
				runset = append(runset, m.Name())
			}
		} else {
			filtered := runset[:0]
			runMotif := false
			for _, n := range runset {
				if n == "motif_redundancy" {
					runMotif = true
					continue
				}
				filtered = append(filtered, n)
			}
			runset = filtered
			if !runMotif {
				useCustomMotif = false
			}
		}
		run = metrics.Run(res.Graph, runset)
		if useCustomMotif {
			tuned := metrics.MotifRedundancy{MaxSize: *motifMaxSize, SampleLimit: *motifSampleLimit}
			recs, mErr := tuned.Compute(context.Background(), res.Graph)
			if mErr != nil {
				run.Errors = append(run.Errors, metrics.MetricError{Metric: tuned.Name(), Err: mErr})
			} else {
				run.Records = append(run.Records, recs...)
				run.Ran = append(run.Ran, tuned.Name())
			}
		}
	} else {
		run = metrics.Run(res.Graph, names)
	}

	switch *format {
	case "json", "":
		if err := run.WriteJSON(stdout); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif metrics: write json: %v\n", err)
			return 1
		}
	case "pretty":
		if err := run.PrettyPrint(stdout); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif metrics: pretty: %v\n", err)
			return 1
		}
	default:
		_, _ = fmt.Fprintf(stderr, "archmotif metrics: unknown format %q (want: json|pretty)\n", *format)
		return 2
	}
	if len(run.Errors) > 0 {
		for _, e := range run.Errors {
			_, _ = fmt.Fprintf(stderr, "metric error: %s\n", e.Error())
		}
		return 1
	}
	return 0
}
