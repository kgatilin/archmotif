package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/kgatilin/archmotif/internal/graphmlx"
)

// runGraphMLScan implements `archmotif graphml-scan <graphml>` (issue #37).
//
// It reads any GraphML file (code GraphML produced by `archmotif graph
// --format=graphml` OR foreign GraphML such as a memory-graph tool's snapshots)
// and runs every registered graphmlx detector against it:
//
//   - orphan_bucket
//   - duplicate_titles
//   - label_entropy_hub
//   - hierarchy_cycle
//   - articulation
//   - community_parent_mismatch
//
// Output is a deterministic next-batch JSON envelope with
// metricEvidence, beforeMetrics, targetMetrics, severity buckets, and
// a stable selectionReason. The result is byte-stable across runs.
//
// `archmotif optimize-batch` (PR #45 / commit e5e4cef) implements a
// complementary, narrower selector focused on orphan_bucket and
// flat-star hubs with materializer-prompt support; this command is
// the broader anomaly-scan view.
func runGraphMLScan(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("archmotif graphml-scan", flag.ContinueOnError)
	fs.SetOutput(stderr)
	maxBatch := fs.Int("max-batch", 10, "max findings to include in the proposed batch")
	minSeverity := fs.String("min-severity", "", "drop findings below this severity: info|low|medium|high|critical")
	onlyDetectors := fs.String("detector", "", "comma-separated list of detectors (default: all registered)")
	listDetectors := fs.Bool("list", false, "list registered GraphML detectors and exit")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage:\n  archmotif graphml-scan [flags] <graphml-file>\n\nFlags:\n")
		fs.PrintDefaults()
		_, _ = fmt.Fprintf(stderr, "\nRegistered GraphML detectors: %s\n", strings.Join(graphmlx.Names(), ", "))
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *listDetectors {
		for _, d := range graphmlx.All() {
			_, _ = fmt.Fprintf(stdout, "%-26s %s\n", d.Name(), d.Description())
		}
		return 0
	}
	if fs.NArg() < 1 {
		fs.Usage()
		return 2
	}
	path := fs.Arg(0)
	f, err := os.Open(path)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif graphml-scan: open %s: %v\n", path, err)
		return 1
	}
	defer func() { _ = f.Close() }()
	g, err := graphmlx.Read(f)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif graphml-scan: %v\n", err)
		return 1
	}

	var detectors []string
	if *onlyDetectors != "" {
		for _, n := range strings.Split(*onlyDetectors, ",") {
			n = strings.TrimSpace(n)
			if n != "" {
				detectors = append(detectors, n)
			}
		}
	}

	res := graphmlx.OptimizeBatch(g, graphmlx.OptimizeBatchOptions{
		MaxBatchSize: *maxBatch,
		Detectors:    detectors,
		MinSeverity:  graphmlx.Severity(*minSeverity),
	})
	if err := res.WriteJSON(stdout); err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif graphml-scan: write json: %v\n", err)
		return 1
	}
	if len(res.Errors) > 0 {
		for _, e := range res.Errors {
			_, _ = fmt.Fprintf(stderr, "detector error: %s\n", e)
		}
		return 1
	}
	return 0
}
