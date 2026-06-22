package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
	"github.com/kgatilin/archmotif/pkg/graphval"
)

// runPPR implements the `archmotif ppr <graph.json>` subcommand. It reads an
// archmotif graph JSON file, builds the graph-agnostic matrix engine
// (pkg/graphval), and computes restart-biased ("personalized") PageRank — a
// random-surfer diffusion that ranks every node by structural proximity to a
// set of seed nodes. With no --seeds it degenerates to global PageRank.
//
// This is the standalone, tool-testable surface of the diffusion ranking that
// archai uses to order graph-search neighbourhoods: pipe any graph JSON in,
// inspect the ranking out.
//
// CLI:
//
//	archmotif ppr <graph.json> [--seeds a,b,c] [--restart 0.15] [--top N] [--format json|text]
//
// JSON output:
//
//	{
//	  "seeds": ["a"],
//	  "unknown_seeds": [],
//	  "restart": 0.15,
//	  "n": 12,
//	  "ranking": [{"name":"a","score":0.31}, ...]
//	}
//
// Exit codes: 0 ok; 1 read/parse/build error; 2 argument error.
func runPPR(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("archmotif ppr", flag.ContinueOnError)
	fs.SetOutput(stderr)
	seedsFlag := fs.String("seeds", "", "comma-separated seed node IDs to teleport to (empty = global PageRank)")
	restart := fs.Float64("restart", graphval.DefaultRestart, "teleport probability α ∈ (0,1] (damping = 1-α)")
	top := fs.Int("top", 0, "keep only the top N ranked nodes (0 = all)")
	format := fs.String("format", "json", "output format: json|text")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage:\n  archmotif ppr [flags] <graph.json>\n\nFlags:\n")
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
	if *format != "json" && *format != "text" {
		_, _ = fmt.Fprintf(stderr, "archmotif ppr: --format=%q (want: json|text)\n", *format)
		return 2
	}
	if *restart <= 0 || *restart > 1 {
		_, _ = fmt.Fprintf(stderr, "archmotif ppr: --restart must be in (0,1] (got %v)\n", *restart)
		return 2
	}
	if *top < 0 {
		_, _ = fmt.Fprintf(stderr, "archmotif ppr: --top must be ≥ 0 (got %d)\n", *top)
		return 2
	}

	path := fs.Arg(0)
	raw, err := os.ReadFile(path)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif ppr: read %s: %v\n", path, err)
		return 1
	}
	var doc mgraph.JSON
	if err := json.Unmarshal(raw, &doc); err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif ppr: parse %s: %v\n", path, err)
		return 1
	}

	report, err := computePPRReport(doc, parseSeeds(*seedsFlag), *restart, *top)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif ppr: %v\n", err)
		return 1
	}

	switch *format {
	case "text":
		writePPRText(stdout, report)
	default:
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif ppr: encode json: %v\n", err)
			return 1
		}
	}
	return 0
}

// pprReport is the JSON output schema for `archmotif ppr`.
type pprReport struct {
	Seeds        []string         `json:"seeds"`
	UnknownSeeds []string         `json:"unknown_seeds"`
	Restart      float64          `json:"restart"`
	N            int              `json:"n"`
	Ranking      []graphval.Score `json:"ranking"`
}

// computePPRReport builds the graphval graph from the archmotif graph doc and
// runs personalized PageRank. Duplicate node IDs are collapsed to first
// occurrence and edges with an endpoint outside the node set are dropped
// (same forgiving projection convention as communities.go), so an arbitrary
// exported graph never fails the stricter graphval.New contract.
func computePPRReport(doc mgraph.JSON, seeds []string, restart float64, top int) (pprReport, error) {
	report := pprReport{
		Seeds:        seeds,
		UnknownSeeds: []string{},
		Restart:      restart,
		Ranking:      []graphval.Score{},
	}

	nodes := make([]graphval.Node, 0, len(doc.Nodes))
	idSet := make(map[string]bool, len(doc.Nodes))
	for _, nd := range doc.Nodes {
		if nd.ID == "" || idSet[nd.ID] {
			continue
		}
		idSet[nd.ID] = true
		nodes = append(nodes, graphval.Node{Name: nd.ID})
	}
	report.N = len(nodes)
	if len(nodes) == 0 {
		return report, nil
	}

	edges := make([]graphval.Edge, 0, len(doc.Edges))
	for _, e := range doc.Edges {
		if !idSet[e.From] || !idSet[e.To] {
			continue
		}
		edges = append(edges, graphval.Edge{From: e.From, To: e.To})
	}

	g, err := graphval.New(nodes, edges)
	if err != nil {
		return report, fmt.Errorf("build graph: %w", err)
	}

	// Report which requested seeds are absent so callers can sanity-check.
	for _, sd := range seeds {
		if !idSet[sd] {
			report.UnknownSeeds = append(report.UnknownSeeds, sd)
		}
	}
	sort.Strings(report.UnknownSeeds)

	ranking := g.PersonalizedPageRankByNames(seeds, restart)
	if top > 0 && top < len(ranking) {
		ranking = ranking[:top]
	}
	report.Ranking = ranking
	return report, nil
}

// parseSeeds splits a comma-separated seed list, trimming whitespace and
// dropping empty entries. Order is preserved; duplicates are not collapsed here
// (graphval handles that).
func parseSeeds(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// writePPRText renders the report as a short human-readable ranking on w.
func writePPRText(w io.Writer, r pprReport) {
	_, _ = fmt.Fprintf(w, "personalized pagerank\n")
	if len(r.Seeds) == 0 {
		_, _ = fmt.Fprintf(w, "  seeds: (none → global PageRank)\n")
	} else {
		_, _ = fmt.Fprintf(w, "  seeds: %s\n", strings.Join(r.Seeds, ", "))
	}
	if len(r.UnknownSeeds) > 0 {
		_, _ = fmt.Fprintf(w, "  unknown seeds: %s\n", strings.Join(r.UnknownSeeds, ", "))
	}
	_, _ = fmt.Fprintf(w, "  restart: %.4f   nodes: %d\n", r.Restart, r.N)
	for i, sc := range r.Ranking {
		_, _ = fmt.Fprintf(w, "  %3d. %-40s %.6f\n", i+1, sc.Name, sc.Score)
	}
}
