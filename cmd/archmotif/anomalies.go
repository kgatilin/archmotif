package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/kgatilin/archmotif/internal/anomalies"
	mgraph "github.com/kgatilin/archmotif/internal/graph"
	"github.com/kgatilin/archmotif/internal/metrics"
	"github.com/kgatilin/archmotif/internal/parser"
)

// runAnomalies implements the `archmotif anomalies <path>`
// subcommand. It builds the typed graph from the supplied path, runs
// every registered metric (or only those named in --metric), passes
// the records through every registered detector (or only those named
// in --detector), and emits a ranked list of anomalies.
//
// Default output is JSON for machine consumption; --format=table
// renders a ranked human-readable summary.
func runAnomalies(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("archmotif anomalies", flag.ContinueOnError)
	fs.SetOutput(stderr)
	tests := fs.Bool("tests", false, "include _test.go files")
	pattern := fs.String("pattern", "./...", "go/packages pattern")
	format := fs.String("format", "json", "output format: json|table")
	onlyMetrics := fs.String("metric", "", "comma-separated list of metric names to compute (default: all registered)")
	onlyDetectors := fs.String("detector", "", "comma-separated list of detector names to apply (default: all registered)")
	listDetectors := fs.Bool("list", false, "list registered detectors and exit")
	metricsFile := fs.String("metrics-file", "", "load records from a metrics JSON file instead of recomputing")
	motifMaxSize := fs.Int("motif-max-size", metrics.DefaultMotifMaxSize, "max motif size for motif_redundancy (3..5)")
	motifSampleLimit := fs.Int("motif-sample-limit", metrics.DefaultMotifSampleLimit, "max sampled subgraphs for motif_redundancy")
	topN := fs.Int("top", 0, "limit output to top N anomalies (0 = all)")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage:\n  archmotif anomalies [flags] <path>\n\nFlags:\n")
		fs.PrintDefaults()
		_, _ = fmt.Fprintf(stderr, "\nRegistered detectors: %s\n", anomalies.JoinNames())
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *listDetectors {
		for _, d := range anomalies.All() {
			_, _ = fmt.Fprintf(stdout, "%-22s %s\n", d.Name(), d.Description())
		}
		return 0
	}
	if fs.NArg() < 1 && *metricsFile == "" {
		fs.Usage()
		return 2
	}

	records, graph, exitCode := loadAnomaliesInput(fs, stderr, *metricsFile, *tests, *pattern, *onlyMetrics, *motifMaxSize, *motifSampleLimit)
	if exitCode != 0 {
		return exitCode
	}

	var detNames []string
	if *onlyDetectors != "" {
		for _, n := range strings.Split(*onlyDetectors, ",") {
			n = strings.TrimSpace(n)
			if n != "" {
				detNames = append(detNames, n)
			}
		}
	}

	res := anomalies.Run(graph, records, detNames)

	if *topN > 0 && *topN < len(res.Anomalies) {
		res.Anomalies = res.Anomalies[:*topN]
	}

	switch *format {
	case "json", "":
		if err := res.WriteJSON(stdout); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif anomalies: write json: %v\n", err)
			return 1
		}
	case "table":
		if err := res.WriteTable(stdout); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif anomalies: write table: %v\n", err)
			return 1
		}
	default:
		_, _ = fmt.Fprintf(stderr, "archmotif anomalies: unknown format %q (want: json|table)\n", *format)
		return 2
	}
	if len(res.Errors) > 0 {
		for _, e := range res.Errors {
			_, _ = fmt.Fprintf(stderr, "detector error: %s\n", e.Error())
		}
		return 1
	}
	return 0
}

// loadAnomaliesInput either reads a metrics JSON file (no graph
// available for region resolution) or computes records in-process
// from the supplied path. Returns (records, graph, exitCode); graph
// may be nil when records came from a file.
func loadAnomaliesInput(fs *flag.FlagSet, stderr io.Writer, metricsFile string, tests bool, pattern, onlyMetrics string, motifMaxSize, motifSampleLimit int) ([]metrics.Record, *mgraph.Graph, int) {
	if metricsFile != "" {
		f, err := os.Open(metricsFile)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif anomalies: open %s: %v\n", metricsFile, err)
			return nil, nil, 1
		}
		defer func() { _ = f.Close() }()
		recs, err := anomalies.LoadMetricsJSON(f)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif anomalies: %v\n", err)
			return nil, nil, 1
		}
		return recs, nil, 0
	}

	dir := fs.Arg(0)
	res, err := parser.Build(parser.Options{
		Dir:      dir,
		Patterns: []string{pattern},
		Tests:    tests,
	})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif anomalies: %v\n", err)
		return nil, nil, 1
	}
	for _, e := range res.LoadErrors {
		_, _ = fmt.Fprintf(stderr, "load: %s\n", e)
	}

	useCustomMotif := motifMaxSize != metrics.DefaultMotifMaxSize ||
		motifSampleLimit != metrics.DefaultMotifSampleLimit

	var names []string
	if onlyMetrics != "" {
		for _, n := range strings.Split(onlyMetrics, ",") {
			n = strings.TrimSpace(n)
			if n != "" {
				names = append(names, n)
			}
		}
	}

	var run metrics.Result
	if useCustomMotif {
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
			tuned := metrics.MotifRedundancy{MaxSize: motifMaxSize, SampleLimit: motifSampleLimit}
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
	for _, e := range run.Errors {
		_, _ = fmt.Fprintf(stderr, "metric error: %s\n", e.Error())
	}
	return run.Records, res.Graph, 0
}
