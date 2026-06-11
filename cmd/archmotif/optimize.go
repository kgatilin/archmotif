package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/kgatilin/archmotif/internal/shape"
)

// runOptimize implements the `archmotif optimize <graphml-file>` subcommand.
// It is a POC for deterministic target-shape optimization: detect graph
// regions whose structure violates a self-similar envelope, then emit a
// rewrite contract for a later semantic materializer.
func runOptimize(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("archmotif optimize", flag.ContinueOnError)
	fs.SetOutput(stderr)
	mode := fs.String("mode", "auto", "optimization mode: auto|shape|architecture")
	format := fs.String("format", "json", "output format: json|table")
	predicate := fs.String("predicate", "part-of", "structural edge predicate to optimize")
	layer := fs.String("layer", "", "optional edge layer filter")
	parentDirection := fs.String("parent-direction", "in", "structural direction: in/child-to-parent or out/parent-to-child")
	maxDirect := fs.Int("max-direct-children", 12, "target max direct structural children per hub")
	groupMin := fs.Int("group-min-children", 4, "target min children per introduced group")
	groupMax := fs.Int("group-max-children", 12, "target max children per introduced group")
	minLeafRatio := fs.Float64("min-leaf-ratio", 0.70, "minimum direct-child leaf ratio for flat-star detection")
	top := fs.Int("top", 10, "limit output to top N candidates (0 = all)")
	targetGraphMLOut := fs.String("target-graphml-out", "", "path to write the top candidate target graph shape as GraphML")
	currentGraphMLOut := fs.String("current-graphml-out", "", "path to write the top candidate current graph region as GraphML (architecture mode)")
	contractOut := fs.String("contract-out", "", "path to write the full optimization contract JSON")
	tests := fs.Bool("tests", false, "include _test.go files in architecture mode")
	pattern := fs.String("pattern", "./...", "go/packages pattern in architecture mode")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage:\n  archmotif optimize [flags] <graphml-file|code-path>\n\nFlags:\n")
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
	input := fs.Arg(0)
	resolvedMode := resolveOptimizeMode(*mode, input)
	if resolvedMode == "" {
		_, _ = fmt.Fprintf(stderr, "archmotif optimize: unsupported mode %q (want auto|shape|architecture)\n", *mode)
		return 2
	}
	if resolvedMode == "architecture" {
		return runArchitectureOptimize(input, architectureOptimizeOptions{
			Format:            *format,
			Pattern:           *pattern,
			Tests:             *tests,
			Limit:             *top,
			TargetGraphMLOut:  *targetGraphMLOut,
			CurrentGraphMLOut: *currentGraphMLOut,
			ContractOut:       *contractOut,
		}, stdout, stderr)
	}
	dir := normalizeDirection(*parentDirection)
	if dir == "" {
		_, _ = fmt.Fprintf(stderr, "archmotif optimize: unsupported parent direction %q (want in|child-to-parent|out|parent-to-child)\n", *parentDirection)
		return 2
	}

	f, err := os.Open(input)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif optimize: open %s: %v\n", input, err)
		return 1
	}
	defer func() { _ = f.Close() }()

	g, err := shape.ReadGraphML(f)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif optimize: %v\n", err)
		return 1
	}
	res := shape.Optimize(g, shape.Options{
		Predicate:         *predicate,
		Layer:             *layer,
		ParentDirection:   dir,
		MaxDirectChildren: *maxDirect,
		GroupMinChildren:  *groupMin,
		GroupMaxChildren:  *groupMax,
		MinLeafRatio:      *minLeafRatio,
		Top:               *top,
	})
	if *targetGraphMLOut != "" && len(res.Candidates) > 0 {
		out, err := os.Create(*targetGraphMLOut)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif optimize: create target graphml: %v\n", err)
			return 1
		}
		if err := shape.WriteShapeGraphML(out, res.Candidates[0].TargetGraph); err != nil {
			_ = out.Close()
			_, _ = fmt.Fprintf(stderr, "archmotif optimize: write target graphml: %v\n", err)
			return 1
		}
		if err := out.Close(); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif optimize: close target graphml: %v\n", err)
			return 1
		}
	}

	switch *format {
	case "json", "":
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif optimize: write json: %v\n", err)
			return 1
		}
	case "table":
		writeOptimizeTable(stdout, res)
	default:
		_, _ = fmt.Fprintf(stderr, "archmotif optimize: unknown format %q (want: json|table)\n", *format)
		return 2
	}
	return 0
}

func resolveOptimizeMode(raw, input string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "auto":
		if strings.HasSuffix(strings.ToLower(input), ".graphml") {
			return "shape"
		}
		return "architecture"
	case "shape", "graphml":
		return "shape"
	case "architecture", "arch", "code":
		return "architecture"
	default:
		return ""
	}
}

func normalizeDirection(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "in", "child-to-parent", "childtoparent":
		return "in"
	case "out", "parent-to-child", "parenttochild":
		return "out"
	default:
		return ""
	}
}

func writeOptimizeTable(w io.Writer, res shape.Result) {
	_, _ = fmt.Fprintf(w, "optimize: %d candidate(s), graph=%d nodes/%d edges, target fanout<=%d\n",
		len(res.Candidates), res.Input.Nodes, res.Input.Edges, res.Target.MaxDirectChildren)
	if len(res.Candidates) == 0 {
		_, _ = fmt.Fprintln(w, "(no structural shape violations found)")
		return
	}
	_, _ = fmt.Fprintf(w, "\n%-4s %-18s %-7s %-38s %-10s %-10s %-8s\n",
		"#", "pattern", "score", "center", "children", "groups", "feasible")
	for i, c := range res.Candidates {
		_, _ = fmt.Fprintf(w, "%-4d %-18s %-7.2f %-38s %-10d %-10d %-8t\n",
			i+1,
			c.Pattern,
			c.Score,
			truncateOptimize(c.Center.Label, 38),
			c.Metrics.DirectStructuralChildren,
			c.Metrics.TargetGroupCount,
			c.Metrics.Feasible,
		)
	}
}

func truncateOptimize(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 3 {
		return s[:n]
	}
	return s[:n-3] + "..."
}
