package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// runCurvature implements the `archmotif curvature <graph.json>`
// subcommand. For every edge it computes a combinatorial Forman-Ricci
// curvature κ over the undirected projection of the input graph, then
// surfaces the K most-negative κ (bridge edges — critical, candidates
// for hardening) and the K most-positive κ (redundant — candidates for
// removal).
//
// Part 3/5 of #74 — sibling of `archmotif quotient`. The ticket
// explicitly excludes Ollivier-Ricci (optimal-transport heavy); this
// implementation is pure Go and runs in O(|E| · d_avg) for the triangle
// count, which is fast enough for codebase-sized graphs.
//
// Formula (simplified Forman-Ricci for unweighted undirected graphs,
// Sreejith et al. 2016 "Forman curvature for complex networks",
// Eq. 1 specialised to w_v = w_e = 1):
//
//	κ(e=(u,v)) = 4 − deg(u) − deg(v) + 3 · |triangles through e|
//
// The +3·triangles term is the standard "face contribution" with
// triangles as 2-cells. Empirically this version puts star-spokes
// (degree-1 leaves of a hub) deeply negative — they are the bridges the
// ticket asks for — while triangle edges land at κ ≥ 1 (redundant).
//
// Direction is preserved in the output (src→dst from the source edge)
// but ignored for the κ computation itself: archmotif graphs are
// inherently directed, but Forman-Ricci is a metric on the underlying
// simple undirected topology. Self-edges and multi-edges collapse to
// the bare undirected topology (same convention as spectral.go).
//
// CLI:
//
//	archmotif curvature [flags] <graph.json>
//
// Flags:
//
//	-k <int>    number of top edges to surface in each tail (default 10)
//	--format    json|text (default json)
func runCurvature(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("archmotif curvature", flag.ContinueOnError)
	fs.SetOutput(stderr)
	k := fs.Int("k", 10, "number of most-negative / most-positive edges to surface (>=1)")
	format := fs.String("format", "json", "output format: json|text")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage:\n  archmotif curvature [flags] <graph.json>\n\nFlags:\n")
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
		_, _ = fmt.Fprintf(stderr, "archmotif curvature: --format=%q (want: json|text)\n", *format)
		return 2
	}
	if *k < 1 {
		_, _ = fmt.Fprintf(stderr, "archmotif curvature: -k must be >= 1 (got %d)\n", *k)
		return 2
	}

	path := fs.Arg(0)
	raw, err := os.ReadFile(path)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif curvature: read %s: %v\n", path, err)
		return 1
	}
	var doc mgraph.JSON
	if err := json.Unmarshal(raw, &doc); err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif curvature: parse %s: %v\n", path, err)
		return 1
	}

	report := computeCurvatureReport(doc, *k)

	switch *format {
	case "text":
		writeCurvatureText(stdout, report)
	default:
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif curvature: encode json: %v\n", err)
			return 1
		}
	}
	return 0
}

// curvatureEdge is a single edge κ entry in the JSON output.
//
// Src/Dst preserve the *first* observed direction from the source
// graph. KappaForman is the combinatorial Forman-Ricci value as
// described in runCurvature's doc comment.
type curvatureEdge struct {
	Src         string  `json:"src"`
	Dst         string  `json:"dst"`
	KappaForman float64 `json:"kappa_forman"`
}

// curvatureReport is the JSON output schema documented in issue #77.
//
// Field names follow the snake_case convention used by spectral and
// communities. Top-K slices are sorted ascending for most-negative and
// descending for most-positive so consumers can scan the head of the
// list without re-sorting.
type curvatureReport struct {
	NNodes        int             `json:"n_nodes"`
	NEdges        int             `json:"n_edges"`
	Edges         []curvatureEdge `json:"edges"`
	MostNegativeK []curvatureEdge `json:"most_negative_k"`
	MostPositiveK []curvatureEdge `json:"most_positive_k"`
}

// computeCurvatureReport builds the undirected projection of doc and
// computes Forman-Ricci κ for every projected edge.
//
// "Edges" in the output uses the original directed (src, dst) the first
// time the projected edge is seen; subsequent parallel / reverse edges
// in the input are silently dropped so |edges| matches the size of the
// undirected simple graph.
func computeCurvatureReport(doc mgraph.JSON, k int) curvatureReport {
	report := curvatureReport{
		NNodes:        len(doc.Nodes),
		NEdges:        0,
		Edges:         []curvatureEdge{},
		MostNegativeK: []curvatureEdge{},
		MostPositiveK: []curvatureEdge{},
	}
	if len(doc.Nodes) == 0 {
		return report
	}

	// Build neighbour sets keyed by stable node ID. Self-edges and
	// duplicates collapse — Forman-Ricci is a property of the simple
	// undirected topology.
	knownNode := make(map[string]struct{}, len(doc.Nodes))
	for _, n := range doc.Nodes {
		knownNode[n.ID] = struct{}{}
	}
	neighbors := make(map[string]map[string]struct{}, len(doc.Nodes))
	// edgeKey -> directed (src, dst) of the first occurrence. Key is the
	// canonical (min, max) endpoint pair so reverse and parallel edges
	// merge.
	type pair struct{ a, b string }
	type oriented struct{ src, dst string }
	firstSeen := make(map[pair]oriented)
	order := make([]pair, 0, len(doc.Edges))

	for _, e := range doc.Edges {
		if _, ok := knownNode[e.From]; !ok {
			continue
		}
		if _, ok := knownNode[e.To]; !ok {
			continue
		}
		if e.From == e.To {
			continue
		}
		a, b := e.From, e.To
		if a > b {
			a, b = b, a
		}
		key := pair{a: a, b: b}
		if _, dup := firstSeen[key]; dup {
			continue
		}
		firstSeen[key] = oriented{src: e.From, dst: e.To}
		order = append(order, key)

		if neighbors[e.From] == nil {
			neighbors[e.From] = make(map[string]struct{})
		}
		if neighbors[e.To] == nil {
			neighbors[e.To] = make(map[string]struct{})
		}
		neighbors[e.From][e.To] = struct{}{}
		neighbors[e.To][e.From] = struct{}{}
	}
	report.NEdges = len(order)
	if len(order) == 0 {
		return report
	}

	// Compute κ per edge. |triangles through (u,v)| = |N(u) ∩ N(v)|.
	report.Edges = make([]curvatureEdge, 0, len(order))
	for _, key := range order {
		o := firstSeen[key]
		u := key.a
		v := key.b
		du := len(neighbors[u])
		dv := len(neighbors[v])
		// Iterate the smaller neighbour set for the intersection.
		small, big := neighbors[u], neighbors[v]
		if len(big) < len(small) {
			small, big = big, small
		}
		triangles := 0
		for w := range small {
			if _, ok := big[w]; ok {
				triangles++
			}
		}
		// 3·|triangles| / 2 would double-count if we walked both
		// endpoints — but we only walk one (N(u) ∩ N(v) directly), so
		// triangles is already the simplex count for the unordered
		// triple {u,v,w}.
		kappa := 4.0 - float64(du) - float64(dv) + 3.0*float64(triangles)
		report.Edges = append(report.Edges, curvatureEdge{
			Src:         o.src,
			Dst:         o.dst,
			KappaForman: kappa,
		})
	}

	// Sort the canonical edge list by (κ asc, src, dst) so the JSON
	// emission is deterministic.
	sort.Slice(report.Edges, func(i, j int) bool {
		if report.Edges[i].KappaForman != report.Edges[j].KappaForman {
			return report.Edges[i].KappaForman < report.Edges[j].KappaForman
		}
		if report.Edges[i].Src != report.Edges[j].Src {
			return report.Edges[i].Src < report.Edges[j].Src
		}
		return report.Edges[i].Dst < report.Edges[j].Dst
	})

	// most_negative_k: head of the ascending-κ list (clamp to len).
	negK := k
	if negK > len(report.Edges) {
		negK = len(report.Edges)
	}
	report.MostNegativeK = make([]curvatureEdge, negK)
	copy(report.MostNegativeK, report.Edges[:negK])

	// most_positive_k: tail of the ascending list, reversed.
	posK := k
	if posK > len(report.Edges) {
		posK = len(report.Edges)
	}
	report.MostPositiveK = make([]curvatureEdge, posK)
	tail := report.Edges[len(report.Edges)-posK:]
	for i, e := range tail {
		report.MostPositiveK[posK-1-i] = e
	}
	return report
}

// writeCurvatureText renders a short human-readable summary of the
// curvature report, mirroring spectral/communities text mode.
func writeCurvatureText(w io.Writer, r curvatureReport) {
	_, _ = fmt.Fprintf(w, "curvature report\n")
	_, _ = fmt.Fprintf(w, "  nodes: %d\n", r.NNodes)
	_, _ = fmt.Fprintf(w, "  edges: %d\n", r.NEdges)
	if r.NEdges == 0 {
		return
	}
	_, _ = fmt.Fprintf(w, "  most negative κ (bridges):\n")
	for _, e := range r.MostNegativeK {
		_, _ = fmt.Fprintf(w, "    %s -> %s  κ=%.3f\n", e.Src, e.Dst, e.KappaForman)
	}
	_, _ = fmt.Fprintf(w, "  most positive κ (redundant):\n")
	for _, e := range r.MostPositiveK {
		_, _ = fmt.Fprintf(w, "    %s -> %s  κ=%.3f\n", e.Src, e.Dst, e.KappaForman)
	}
}
