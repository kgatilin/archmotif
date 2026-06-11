package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"sort"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
	exprand "golang.org/x/exp/rand"
	gncommunity "gonum.org/v1/gonum/graph/community"
	"gonum.org/v1/gonum/graph/simple"
)

// runCommunities implements the `archmotif communities <graph.json>`
// subcommand. It reads an archmotif graph JSON file from disk, projects it to an
// undirected simple graph, runs Gonum's pure-Go Louvain modularisation, then
// emits the canonical archmotif communities report.
//
// Part 2/5 of #74. Builds on the graph-loading and GraphML conventions
// established in part 1 (#75 spectral).
//
// CLI:
//
//	archmotif communities <graph.json> [--resolution 1.0] [--format json|text]
//
// JSON output:
//
//	{
//	  "n_communities": int,
//	  "modularity_q": float,
//	  "members": {"community_0": ["pkg/a","pkg/b"], ...},
//	  "intra_edges_per_community": {"community_0": int, ...},
//	  "inter_edges_per_pair": {"community_0__community_1": int, ...}
//	}
func runCommunities(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("archmotif communities", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("format", "json", "output format: json|text")
	resolution := fs.Float64("resolution", 1.0, "Louvain modularity resolution (higher → more, smaller communities)")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage:\n  archmotif communities [flags] <graph.json>\n\nFlags:\n")
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
		_, _ = fmt.Fprintf(stderr, "archmotif communities: --format=%q (want: json|text)\n", *format)
		return 2
	}
	if *resolution <= 0 {
		_, _ = fmt.Fprintf(stderr, "archmotif communities: --resolution must be > 0 (got %v)\n", *resolution)
		return 2
	}

	path := fs.Arg(0)
	raw, err := os.ReadFile(path)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif communities: read %s: %v\n", path, err)
		return 1
	}
	var doc mgraph.JSON
	if err := json.Unmarshal(raw, &doc); err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif communities: parse %s: %v\n", path, err)
		return 1
	}

	report, err := computeCommunitiesReport(doc, *resolution)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif communities: %v\n", err)
		return 1
	}

	switch *format {
	case "text":
		writeCommunitiesText(stdout, report)
	default:
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif communities: encode json: %v\n", err)
			return 1
		}
	}
	return 0
}

// communitiesReport is the JSON output schema documented in issue #76.
// Field order in JSON marshalling is fixed by struct order; map field
// keys are sorted by encoding/json so the output is deterministic.
type communitiesReport struct {
	NCommunities           int                 `json:"n_communities"`
	ModularityQ            float64             `json:"modularity_q"`
	Members                map[string][]string `json:"members"`
	IntraEdgesPerCommunity map[string]int      `json:"intra_edges_per_community"`
	InterEdgesPerPair      map[string]int      `json:"inter_edges_per_pair"`
}

// computeCommunitiesReport runs pure-Go Louvain modularisation and computes the
// per-community / per-pair edge tallies from the original directed edges
// projected to a simple undirected graph (multi-edges and self-edges dropped,
// same convention as spectral.go).
func computeCommunitiesReport(doc mgraph.JSON, resolution float64) (communitiesReport, error) {
	report := communitiesReport{
		Members:                map[string][]string{},
		IntraEdgesPerCommunity: map[string]int{},
		InterEdgesPerPair:      map[string]int{},
	}
	if len(doc.Nodes) == 0 {
		return report, nil
	}

	ug, ids, index := communitiesUndirectedGraph(doc)
	reduced := gncommunity.Modularize(ug, resolution, exprand.NewSource(1))
	communities := reduced.Communities()
	report.ModularityQ = gncommunity.Q(ug, communities, resolution)
	if math.IsNaN(report.ModularityQ) || math.IsInf(report.ModularityQ, 0) {
		report.ModularityQ = 0
	}

	assignment := make(map[string]int, len(ids))
	memberLists := make(map[int][]string, len(communities))
	for c, members := range communities {
		for _, node := range members {
			idx := int(node.ID())
			if idx < 0 || idx >= len(ids) {
				continue
			}
			id := ids[idx]
			assignment[id] = c
			memberLists[c] = append(memberLists[c], id)
		}
	}
	for _, id := range ids {
		if _, ok := assignment[id]; ok {
			continue
		}
		c := len(memberLists)
		assignment[id] = c
		memberLists[c] = append(memberLists[c], id)
	}

	// Tally edges in Go. Use an undirected projection (same convention
	// as spectral.go): collapse duplicate endpoints, drop self-edges.
	seen := make(map[[2]string]struct{})
	intra := make(map[int]int)
	inter := make(map[[2]int]int)
	for _, e := range doc.Edges {
		if e.From == e.To {
			continue
		}
		a, b := e.From, e.To
		if a > b {
			a, b = b, a
		}
		key := [2]string{a, b}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		if _, ok := index[e.From]; !ok {
			continue
		}
		if _, ok := index[e.To]; !ok {
			continue
		}
		cf, ok1 := assignment[e.From]
		ct, ok2 := assignment[e.To]
		if !ok1 || !ok2 {
			continue
		}
		if cf == ct {
			intra[cf]++
			continue
		}
		pa, pb := cf, ct
		if pa > pb {
			pa, pb = pb, pa
		}
		inter[[2]int{pa, pb}]++
	}

	for c := range memberLists {
		sort.Strings(memberLists[c])
	}

	// Normalise community indices to the dense range [0, N). The
	// Louvain output order can vary with reduction history; sort by the
	// first member ID so output stays deterministic.
	communityKeys := make([]int, 0, len(memberLists))
	for c := range memberLists {
		communityKeys = append(communityKeys, c)
	}
	sort.Slice(communityKeys, func(i, j int) bool {
		a, b := memberLists[communityKeys[i]], memberLists[communityKeys[j]]
		if len(a) == 0 || len(b) == 0 {
			return len(a) < len(b)
		}
		return a[0] < b[0]
	})
	remap := make(map[int]int, len(communityKeys))
	for newIdx, oldIdx := range communityKeys {
		remap[oldIdx] = newIdx
	}

	report.NCommunities = len(communityKeys)
	for oldIdx, members := range memberLists {
		key := fmt.Sprintf("community_%d", remap[oldIdx])
		report.Members[key] = members
	}
	for oldIdx, count := range intra {
		key := fmt.Sprintf("community_%d", remap[oldIdx])
		report.IntraEdgesPerCommunity[key] = count
	}
	// Ensure every community has an entry in IntraEdgesPerCommunity
	// (zero when no intra-edges exist) so downstream consumers can
	// assume the map is keyed by every community.
	for _, c := range communityKeys {
		key := fmt.Sprintf("community_%d", remap[c])
		if _, ok := report.IntraEdgesPerCommunity[key]; !ok {
			report.IntraEdgesPerCommunity[key] = 0
		}
	}
	for pair, count := range inter {
		a := remap[pair[0]]
		b := remap[pair[1]]
		if a > b {
			a, b = b, a
		}
		key := fmt.Sprintf("community_%d__community_%d", a, b)
		report.InterEdgesPerPair[key] = count
	}
	return report, nil
}

func communitiesUndirectedGraph(doc mgraph.JSON) (*simple.UndirectedGraph, []string, map[string]int) {
	ids := make([]string, 0, len(doc.Nodes))
	for _, node := range doc.Nodes {
		ids = append(ids, node.ID)
	}
	sort.Strings(ids)
	index := make(map[string]int, len(ids))
	ug := simple.NewUndirectedGraph()
	for i, id := range ids {
		index[id] = i
		ug.AddNode(simple.Node(int64(i)))
	}
	for _, e := range doc.Edges {
		fi, ok1 := index[e.From]
		ti, ok2 := index[e.To]
		if !ok1 || !ok2 || fi == ti {
			continue
		}
		if ug.HasEdgeBetween(int64(fi), int64(ti)) {
			continue
		}
		ug.SetEdge(simple.Edge{F: simple.Node(int64(fi)), T: simple.Node(int64(ti))})
	}
	return ug, ids, index
}

// writeCommunitiesText renders the report as a short human-readable
// summary on w. Pairs and per-community counts are sorted for
// determinism.
func writeCommunitiesText(w io.Writer, r communitiesReport) {
	_, _ = fmt.Fprintf(w, "communities report\n")
	_, _ = fmt.Fprintf(w, "  n_communities: %d\n", r.NCommunities)
	_, _ = fmt.Fprintf(w, "  modularity Q: %.6f\n", r.ModularityQ)
	keys := make([]string, 0, len(r.Members))
	for k := range r.Members {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		members := r.Members[k]
		intra := r.IntraEdgesPerCommunity[k]
		_, _ = fmt.Fprintf(w, "  %s: %d members, %d intra-edges\n", k, len(members), intra)
	}
	pairs := make([]string, 0, len(r.InterEdgesPerPair))
	for p := range r.InterEdgesPerPair {
		pairs = append(pairs, p)
	}
	sort.Strings(pairs)
	if len(pairs) > 0 {
		_, _ = fmt.Fprintf(w, "  inter-community edges:\n")
		for _, p := range pairs {
			_, _ = fmt.Fprintf(w, "    %s: %d\n", p, r.InterEdgesPerPair[p])
		}
	}
}
