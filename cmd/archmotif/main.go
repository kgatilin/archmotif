// Command archmotif is the CLI entrypoint for archmotif — it builds a typed
// architecture graph from Go source and runs analysis, metric, spectral,
// community, quotient, optimization, contract, and MCP-server subcommands over
// it. This package is the composition root: it wires subcommands to the
// internal abstractions and owns no domain logic itself.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

var version = "0.0.0-dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("archmotif", flag.ContinueOnError)
	fs.SetOutput(stderr)
	showVersion := fs.Bool("version", false, "print version and exit")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "archmotif — code architecture as graph\n\n")
		_, _ = fmt.Fprintf(stderr, "Usage:\n  archmotif [flags] [command]\n\n")
		_, _ = fmt.Fprintf(stderr, "Graph-metrics commands (GraphML in, data out):\n"+
			"  analyze    run the metric suite over a graph\n"+
			"  calculate  compute one named metric\n"+
			"  embed      add Vertex text embeddings as node vectors\n"+
			"  pkg-graph  project typed graph JSON to package GraphML\n"+
			"  quotient   collapse a graph by a node attribute (--partition)\n"+
			"  policy     check a dependency policy, list violations\n"+
			"  diff       focus on a branch's added subgraph (BEFORE AFTER)\n\n")
		_, _ = fmt.Fprintf(stderr, "Flags:\n")
		fs.PrintDefaults()
		_, _ = fmt.Fprintf(stderr, "\nSee https://github.com/kgatilin/archmotif for the roadmap.\n")
	}

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if *showVersion {
		_, _ = fmt.Fprintln(stdout, version)
		return 0
	}

	if fs.NArg() == 0 {
		fs.Usage()
		return 0
	}

	cmd := fs.Arg(0)
	rest := fs.Args()[1:]
	switch cmd {
	case "view":
		return runView(rest, stdout, stderr)
	case "graph":
		return runGraph(rest, stdout, stderr)
	case "contracts":
		return runContracts(rest, stdout, stderr)
	case "metrics":
		return runMetrics(rest, stdout, stderr)
	case "embed":
		return runEmbed(rest, stdout, stderr)
	case "pkg-graph":
		return runPkgGraph(rest, stdout, stderr)
	case "optimize":
		return runOptimize(rest, stdout, stderr)
	case "optimize-batch":
		return runOptimizeBatch(rest, stdout, stderr)
	case "optimize-loop":
		return runOptimizeLoopCmd(rest, stdout, stderr)
	case "verify":
		return runVerify(rest, stdout, stderr)
	case "propose":
		return runPropose(rest, stdout, stderr)
	case "anomalies":
		return runAnomalies(rest, stdout, stderr)
	case "skeleton":
		return runSkeleton(rest, stdout, stderr)
	case "target":
		return runTarget(rest, stdout, stderr)
	case "refactor":
		return runRefactor(rest, stdout, stderr)
	case "patterns":
		return runPatterns(rest, stdout, stderr)
	case "coupling":
		return runCoupling(rest, stdout, stderr)
	case "graphml-scan":
		return runGraphMLScan(rest, stdout, stderr)
	case "diagram":
		return runDiagram(rest, stdout, stderr)
	case "export":
		return runExport(rest, stdout, stderr)
	case "catalog":
		return runCatalog(rest, stdout, stderr)
	case "drift":
		return runDrift(rest, stdout, stderr)
	case "mcp":
		return runMCP(rest, stdout, stderr)
	case "spectral":
		return runSpectral(rest, stdout, stderr)
	case "communities":
		return runCommunities(rest, stdout, stderr)
	case "analyze":
		return runAnalyze(rest, stdout, stderr)
	case "calculate":
		return runCalculate(rest, stdout, stderr)
	case "policy":
		return runPolicy(rest, stdout, stderr)
	case "diff":
		return runDiff(rest, stdout, stderr)
	case "quotient":
		// New graph-agnostic path when --partition is given; legacy otherwise.
		for _, a := range rest {
			if a == "--partition" || strings.HasPrefix(a, "--partition=") {
				return runQuotientContract(rest, stdout, stderr)
			}
		}
		return runQuotient(rest, stdout, stderr)
	case "curvature":
		return runCurvature(rest, stdout, stderr)
	}
	_, _ = fmt.Fprintf(stderr, "archmotif: command %q not implemented yet (Stage 0 scaffold)\n", cmd)
	return 1
}
