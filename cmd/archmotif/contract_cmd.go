package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/kgatilin/archmotif/internal/contract"
	"github.com/kgatilin/archmotif/internal/graphmlx"
)

// runAnalyze implements `archmotif analyze GRAPH [--json]`: the graph-agnostic
// metric suite over a GraphML file (PRD contract).
func runAnalyze(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("analyze", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit JSON")
	pos, ok := parsePermissive(fs, args)
	if !ok {
		return 2
	}
	g, code := loadGraphML(arg(pos, 0), stderr)
	if g == nil {
		return code
	}
	rep := contract.Analyze(g)
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(rep)
		return 0
	}
	fmt.Fprintf(stdout, "nodes=%d edges=%d components=%d\n", rep.Nodes, rep.Edges, rep.Components)
	fmt.Fprintf(stdout, "lambda2=%.4f  modularity=%.4f  layering=%.4f\n", rep.Lambda2, rep.Modularity, rep.Layering)
	fmt.Fprintf(stdout, "cycles: %d\n", len(rep.Cycles))
	for _, c := range rep.Cycles {
		fmt.Fprintf(stdout, "  cycle: %v\n", c)
	}
	fmt.Fprintf(stdout, "god-nodes: %d\n", len(rep.GodNodes))
	for _, gn := range rep.GodNodes {
		fmt.Fprintf(stdout, "  %s — %s\n", gn.ID, gn.Reason)
	}
	return 0
}

// runCalculate implements `archmotif calculate METRIC GRAPH`.
func runCalculate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("calculate", flag.ContinueOnError)
	pos, ok := parsePermissive(fs, args)
	if !ok {
		return 2
	}
	if len(pos) < 2 {
		fmt.Fprintln(stderr, "usage: archmotif calculate METRIC GRAPH")
		return 2
	}
	metric := pos[0]
	g, code := loadGraphML(pos[1], stderr)
	if g == nil {
		return code
	}
	res, err := contract.Calculate(metric, g)
	if err != nil {
		fmt.Fprintln(stderr, "archmotif calculate:", err)
		return 1
	}
	fmt.Fprintf(stdout, "%s = ", metric)
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(res)
	return 0
}

// runQuotientContract implements `archmotif quotient GRAPH --partition KEY`.
func runQuotientContract(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("quotient", flag.ContinueOnError)
	part := fs.String("partition", "group", "node attribute to group by")
	asJSON := fs.Bool("json", false, "emit JSON")
	pos, ok := parsePermissive(fs, args)
	if !ok {
		return 2
	}
	g, code := loadGraphML(arg(pos, 0), stderr)
	if g == nil {
		return code
	}
	q := contract.Quotient(g, contract.PartitionBy(g, *part))
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(q)
		return 0
	}
	fmt.Fprintf(stdout, "macro graph by %q — %d groups, acyclic=%v\n", *part, len(q.Groups), q.Acyclic)
	for _, gr := range q.Groups {
		fmt.Fprintf(stdout, "  [%s] n=%d fan-in=%d fan-out=%d\n", gr.Group, len(gr.Members), gr.FanIn, gr.FanOut)
	}
	for _, e := range q.Edges {
		fmt.Fprintf(stdout, "  %s -> %s  x%d\n", e.From, e.To, e.Weight)
	}
	return 0
}

// runPolicy implements `archmotif policy GRAPH RULES`.
func runPolicy(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("policy", flag.ContinueOnError)
	pos, ok := parsePermissive(fs, args)
	if !ok {
		return 2
	}
	if len(pos) < 2 {
		fmt.Fprintln(stderr, "usage: archmotif policy GRAPH RULES")
		return 2
	}
	g, code := loadGraphML(pos[0], stderr)
	if g == nil {
		return code
	}
	data, err := os.ReadFile(pos[1])
	if err != nil {
		fmt.Fprintln(stderr, "archmotif policy:", err)
		return 2
	}
	rules, err := contract.ParseRules(data)
	if err != nil {
		fmt.Fprintln(stderr, "archmotif policy:", err)
		return 2
	}
	vs := contract.Residual(g, rules)
	if len(vs) == 0 {
		fmt.Fprintln(stdout, "policy OK: 0 violations")
		return 0
	}
	fmt.Fprintf(stdout, "policy FAILED: %d violations\n", len(vs))
	for _, v := range vs {
		fmt.Fprintf(stdout, "  %s -> %s  (%s)\n", v.From, v.To, v.Reason)
	}
	return 1
}

// runDiff implements `archmotif diff BEFORE AFTER [--key qname] [--context N]
// [--named-only] [--json]`: emit the focused added subgraph (GraphML) so review
// centres on what a branch changes, not the whole graph.
func runDiff(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	key := fs.String("key", "qname", "node attribute to use as stable identity (id|label|kind|<attr>)")
	context := fs.Int("context", 1, "hops of neighbour context to keep around added nodes")
	namedOnly := fs.Bool("named-only", false, "ignore nodes that lack the key attribute (drops structural filler)")
	asJSON := fs.Bool("json", false, "emit the diff summary as JSON instead of the focused GraphML")
	pos, ok := parsePermissive(fs, args)
	if !ok {
		return 2
	}
	if len(pos) < 2 {
		fmt.Fprintln(stderr, "usage: archmotif diff BEFORE AFTER [--key qname] [--context N] [--named-only] [--json]")
		return 2
	}
	before, code := loadGraphML(pos[0], stderr)
	if before == nil {
		return code
	}
	after, code := loadGraphML(pos[1], stderr)
	if after == nil {
		return code
	}
	focus, sum := contract.Diff(before, after, *key, *context, *namedOnly)
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(sum)
		return 0
	}
	fmt.Fprintf(stderr, "diff by %q: +%d added, -%d removed, %d context -> focus %d nodes / %d edges\n",
		sum.Key, sum.AddedN, sum.RemovedN, sum.ContextN, sum.FocusNodes, sum.FocusEdges)
	if err := contract.WriteGraphML(stdout, focus); err != nil {
		fmt.Fprintln(stderr, "archmotif diff: write graphml:", err)
		return 1
	}
	return 0
}

// parsePermissive parses flags that may be interspersed with positional args
// (Go's flag package otherwise stops at the first positional). Returns the
// collected positionals, or ok=false on a parse error.
func arg(pos []string, i int) string {
	if i < len(pos) {
		return pos[i]
	}
	return ""
}

func parsePermissive(fs *flag.FlagSet, args []string) ([]string, bool) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, false
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return positional, true
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}

func loadGraphML(path string, stderr io.Writer) (*graphmlx.Graph, int) {
	if path == "" {
		fmt.Fprintln(stderr, "archmotif: missing GRAPH (a GraphML file)")
		return nil, 2
	}
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintln(stderr, "archmotif: open graph:", err)
		return nil, 2
	}
	defer f.Close()
	g, err := graphmlx.Read(f)
	if err != nil {
		fmt.Fprintln(stderr, "archmotif: read graph:", err)
		return nil, 1
	}
	return g, 0
}
