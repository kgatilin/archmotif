package main

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// runQuotient implements the `archmotif quotient <graph.json> --communities <out.json>`
// subcommand. It collapses every community in the input partition to a
// single super-node and emits the macro-architecture super-graph as
// both JSON and GraphML (Gephi-compatible).
//
// Output files (written next to --out, or derived from the input path
// if --out is omitted):
//
//	<prefix>.json     — quotient JSON per the schema in #77
//	<prefix>.graphml  — undirected GraphML with size/weight attributes
//
// Part 3/5 of #74. The community input is the JSON produced by
// `archmotif communities` (#76); only the "members" map is required —
// other fields are ignored so future schema evolution doesn't break
// this consumer.
//
// CLI:
//
//	archmotif quotient [flags] <graph.json> --communities <out.json>
//
// Flags:
//
//	--communities <path>   community partition JSON (required)
//	--out <prefix>         output prefix; emits <prefix>.json and <prefix>.graphml
//	                       (default: <graph.json without .json>.quotient)
//	--format json|text     stdout summary format (files are always written
//	                       regardless; default json prints the JSON report
//	                       to stdout as well, text prints a human summary)
func runQuotient(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("archmotif quotient", flag.ContinueOnError)
	fs.SetOutput(stderr)
	communitiesPath := fs.String("communities", "", "path to communities JSON (required, output of `archmotif communities`)")
	outPrefix := fs.String("out", "", "output prefix (default: <graph>.quotient)")
	format := fs.String("format", "json", "stdout format: json|text")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage:\n  archmotif quotient [flags] <graph.json> --communities <communities.json>\n\nFlags:\n")
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
	if *communitiesPath == "" {
		_, _ = fmt.Fprintf(stderr, "archmotif quotient: --communities is required\n")
		fs.Usage()
		return 2
	}
	if *format != "json" && *format != "text" {
		_, _ = fmt.Fprintf(stderr, "archmotif quotient: --format=%q (want: json|text)\n", *format)
		return 2
	}

	graphPath := fs.Arg(0)
	graphRaw, err := os.ReadFile(graphPath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif quotient: read %s: %v\n", graphPath, err)
		return 1
	}
	var doc mgraph.JSON
	if err := json.Unmarshal(graphRaw, &doc); err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif quotient: parse %s: %v\n", graphPath, err)
		return 1
	}
	commRaw, err := os.ReadFile(*communitiesPath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif quotient: read %s: %v\n", *communitiesPath, err)
		return 1
	}
	var commIn struct {
		// Only the members map is needed for the quotient; we tolerate
		// any other fields in the communities JSON.
		Members map[string][]string `json:"members"`
	}
	if err := json.Unmarshal(commRaw, &commIn); err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif quotient: parse %s: %v\n", *communitiesPath, err)
		return 1
	}
	if commIn.Members == nil {
		_, _ = fmt.Fprintf(stderr, "archmotif quotient: %s: missing \"members\" key\n", *communitiesPath)
		return 1
	}

	report, err := computeQuotientReport(doc, commIn.Members)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif quotient: %v\n", err)
		return 1
	}

	prefix := *outPrefix
	if prefix == "" {
		prefix = strings.TrimSuffix(graphPath, filepath.Ext(graphPath)) + ".quotient"
	}
	jsonOut := prefix + ".json"
	graphmlOut := prefix + ".graphml"

	// Emit the JSON file.
	if err := writeQuotientJSONFile(jsonOut, report); err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif quotient: write %s: %v\n", jsonOut, err)
		return 1
	}
	// Emit the GraphML file.
	if err := writeQuotientGraphMLFile(graphmlOut, report); err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif quotient: write %s: %v\n", graphmlOut, err)
		return 1
	}

	switch *format {
	case "text":
		writeQuotientText(stdout, report, jsonOut, graphmlOut)
	default:
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif quotient: encode json: %v\n", err)
			return 1
		}
	}
	return 0
}

// quotientSuperNode is one entry in the super_nodes array of the JSON
// output. ID is the community key from the input partition; Members is
// the list of original node IDs collapsed into this super-node; Size is
// |Members| denormalised for downstream tooling that doesn't want to
// recount.
type quotientSuperNode struct {
	ID      string   `json:"id"`
	Members []string `json:"members"`
	Size    int      `json:"size"`
}

// quotientSuperEdge represents one inter-community super-edge. Weight
// is the count of underlying directed edges; Underlying enumerates them
// in "src->dst" form (deterministically sorted) so a consumer can drill
// back to the source graph.
type quotientSuperEdge struct {
	Src        string   `json:"src"`
	Dst        string   `json:"dst"`
	Weight     int      `json:"weight"`
	Underlying []string `json:"underlying"`
}

// quotientReport is the on-disk / stdout shape documented in #77.
type quotientReport struct {
	NSuperNodes int                 `json:"n_super_nodes"`
	NSuperEdges int                 `json:"n_super_edges"`
	SuperNodes  []quotientSuperNode `json:"super_nodes"`
	SuperEdges  []quotientSuperEdge `json:"super_edges"`
}

// computeQuotientReport collapses doc according to the members map and
// builds the super-graph.
//
// Semantics:
//   - Every node in members[*] must be a node in doc; unknown IDs are
//     dropped with no error (defensive — the partition may have been
//     computed on a slightly different snapshot).
//   - Nodes in doc that are NOT in any community are dropped. The
//     quotient only describes the partitioned subgraph.
//   - Self-edges (src == dst) are dropped before tallying — they can't
//     contribute to inter-community structure.
//   - Multi-edges between the same (src, dst) pair are collapsed (same
//     convention as spectral/communities — the topology is what matters
//     for macro views).
//   - Intra-community edges are *not* counted in super-edge weight by
//     definition: a super-edge is by construction inter-community.
//   - Direction is preserved: a super-edge A→B is distinct from B→A.
//     The underlying graph is directed, so the quotient is too.
func computeQuotientReport(doc mgraph.JSON, members map[string][]string) (quotientReport, error) {
	report := quotientReport{
		SuperNodes: []quotientSuperNode{},
		SuperEdges: []quotientSuperEdge{},
	}

	// 1. Build the node -> community lookup. Walk members in sorted key
	// order so deterministic output.
	commIDs := make([]string, 0, len(members))
	for k := range members {
		commIDs = append(commIDs, k)
	}
	sort.Strings(commIDs)

	knownNode := make(map[string]struct{}, len(doc.Nodes))
	for _, n := range doc.Nodes {
		knownNode[n.ID] = struct{}{}
	}
	nodeToComm := make(map[string]string, len(doc.Nodes))
	for _, cid := range commIDs {
		seen := make(map[string]struct{}, len(members[cid]))
		for _, nodeID := range members[cid] {
			if _, ok := knownNode[nodeID]; !ok {
				continue
			}
			if _, dup := nodeToComm[nodeID]; dup {
				// Partition must be disjoint — surface the violation
				// rather than silently mis-classifying.
				return report, fmt.Errorf("node %q appears in multiple communities", nodeID)
			}
			nodeToComm[nodeID] = cid
			seen[nodeID] = struct{}{}
		}
	}

	// 2. Emit super-nodes in deterministic order. Member lists are sorted
	// inside each community so the output is identical between runs.
	for _, cid := range commIDs {
		ms := make([]string, 0, len(members[cid]))
		for _, m := range members[cid] {
			if _, ok := knownNode[m]; ok {
				ms = append(ms, m)
			}
		}
		sort.Strings(ms)
		report.SuperNodes = append(report.SuperNodes, quotientSuperNode{
			ID:      cid,
			Members: ms,
			Size:    len(ms),
		})
	}
	report.NSuperNodes = len(report.SuperNodes)

	// 3. Tally inter-community edges. Key is the directed pair (src
	// community, dst community); underlying is collected as a sorted
	// list of "src->dst" strings. Within a single (src_comm, dst_comm)
	// pair, deduplicate underlying edges so multi-edges don't inflate
	// the weight.
	type pair struct{ src, dst string }
	weights := make(map[pair]map[string]struct{})
	for _, e := range doc.Edges {
		if e.From == e.To {
			continue
		}
		fromC, okF := nodeToComm[e.From]
		toC, okT := nodeToComm[e.To]
		if !okF || !okT {
			continue
		}
		if fromC == toC {
			continue
		}
		key := pair{src: fromC, dst: toC}
		if weights[key] == nil {
			weights[key] = make(map[string]struct{})
		}
		weights[key][e.From+"->"+e.To] = struct{}{}
	}

	// 4. Emit super-edges in deterministic order: (src, dst) lexicographic.
	pairs := make([]pair, 0, len(weights))
	for p := range weights {
		pairs = append(pairs, p)
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].src != pairs[j].src {
			return pairs[i].src < pairs[j].src
		}
		return pairs[i].dst < pairs[j].dst
	})
	for _, p := range pairs {
		underlying := make([]string, 0, len(weights[p]))
		for u := range weights[p] {
			underlying = append(underlying, u)
		}
		sort.Strings(underlying)
		report.SuperEdges = append(report.SuperEdges, quotientSuperEdge{
			Src:        p.src,
			Dst:        p.dst,
			Weight:     len(underlying),
			Underlying: underlying,
		})
	}
	report.NSuperEdges = len(report.SuperEdges)
	return report, nil
}

// writeQuotientJSONFile encodes v to path as indented JSON with a
// trailing newline. Used for the quotient.json artefact. Named with a
// "Quotient" prefix to avoid colliding with optimize_batch.go's
// writeJSONFile (same package).
func writeQuotientJSONFile(path string, v any) (err error) {
	f, err := os.Create(path) //nolint:gosec // user-supplied path is intentional
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// writeQuotientGraphMLFile emits the quotient as GraphML 1.1 to path.
// The super-graph is directed (mirroring the underlying archmotif
// graph) so Gephi treats arrows correctly when rendering call graphs.
// Two node-data keys are exposed (size, members_list); one edge-data
// key (weight). Keep the schema small — Gephi will surface the keys in
// the Data Laboratory automatically.
func writeQuotientGraphMLFile(path string, r quotientReport) (err error) {
	f, err := os.Create(path) //nolint:gosec
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	return writeQuotientGraphML(f, r)
}

// writeQuotientGraphML is the buffer-level emitter. Factored out so
// tests can assert on the bytes without touching the filesystem.
func writeQuotientGraphML(w io.Writer, r quotientReport) error {
	// Map super-node IDs to GraphML-safe IDs (n0, n1, ...) to satisfy
	// the XML ID NameStartChar rule. Stable original IDs are exposed as
	// node data attributes.
	graphIDs := make(map[string]string, len(r.SuperNodes))
	for i, n := range r.SuperNodes {
		graphIDs[n.ID] = fmt.Sprintf("n%d", i)
	}

	if _, err := fmt.Fprintln(w, `<?xml version="1.0" encoding="UTF-8"?>`); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, `<graphml xmlns="http://graphml.graphdrawing.org/xmlns"`); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, `         xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"`); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, `         xsi:schemaLocation="http://graphml.graphdrawing.org/xmlns http://graphml.graphdrawing.org/xmlns/1.1/graphml.xsd">`); err != nil {
		return err
	}
	// Node keys.
	if _, err := fmt.Fprintln(w, `  <key id="n_label" for="node" attr.name="label" attr.type="string"/>`); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, `  <key id="n_id" for="node" attr.name="community_id" attr.type="string"/>`); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, `  <key id="n_size" for="node" attr.name="size" attr.type="int"/>`); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, `  <key id="n_members" for="node" attr.name="members" attr.type="string"/>`); err != nil {
		return err
	}
	// Edge keys.
	if _, err := fmt.Fprintln(w, `  <key id="e_weight" for="edge" attr.name="weight" attr.type="int"/>`); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, `  <graph id="Q" edgedefault="directed">`); err != nil {
		return err
	}
	for _, n := range r.SuperNodes {
		if _, err := fmt.Fprintf(w, `    <node id="%s">`+"\n", graphIDs[n.ID]); err != nil {
			return err
		}
		if err := writeGraphMLDataEsc(w, "      ", "n_label", n.ID); err != nil {
			return err
		}
		if err := writeGraphMLDataEsc(w, "      ", "n_id", n.ID); err != nil {
			return err
		}
		if err := writeGraphMLDataEsc(w, "      ", "n_size", fmt.Sprintf("%d", n.Size)); err != nil {
			return err
		}
		// Encode members as a comma-separated list — small enough for
		// Gephi's data table, and easy to split downstream.
		if err := writeGraphMLDataEsc(w, "      ", "n_members", strings.Join(n.Members, ",")); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(w, `    </node>`); err != nil {
			return err
		}
	}
	for i, e := range r.SuperEdges {
		from, ok := graphIDs[e.Src]
		if !ok {
			return fmt.Errorf("quotient GraphML: unknown super-node %q (src)", e.Src)
		}
		to, ok := graphIDs[e.Dst]
		if !ok {
			return fmt.Errorf("quotient GraphML: unknown super-node %q (dst)", e.Dst)
		}
		if _, err := fmt.Fprintf(w, `    <edge id="e%d" source="%s" target="%s">`+"\n", i, from, to); err != nil {
			return err
		}
		if err := writeGraphMLDataEsc(w, "      ", "e_weight", fmt.Sprintf("%d", e.Weight)); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(w, `    </edge>`); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w, `  </graph>`); err != nil {
		return err
	}
	_, err := fmt.Fprintln(w, `</graphml>`)
	return err
}

// writeGraphMLDataEsc writes a single <data> element with XML-escaped
// text content. Mirrors internal/graph/graphml.go's writeGraphMLData
// but is duplicated here so quotient is self-contained (the internal
// helper isn't exported).
func writeGraphMLDataEsc(w io.Writer, indent, key, value string) error {
	if _, err := fmt.Fprintf(w, `%s<data key="%s">`, indent, key); err != nil {
		return err
	}
	if err := xml.EscapeText(w, []byte(value)); err != nil {
		return err
	}
	_, err := fmt.Fprintln(w, `</data>`)
	return err
}

// writeQuotientText prints a short human-readable summary on w.
func writeQuotientText(w io.Writer, r quotientReport, jsonOut, graphmlOut string) {
	_, _ = fmt.Fprintf(w, "quotient report\n")
	_, _ = fmt.Fprintf(w, "  super_nodes: %d\n", r.NSuperNodes)
	_, _ = fmt.Fprintf(w, "  super_edges: %d\n", r.NSuperEdges)
	_, _ = fmt.Fprintf(w, "  json:    %s\n", jsonOut)
	_, _ = fmt.Fprintf(w, "  graphml: %s\n", graphmlOut)
	if len(r.SuperNodes) > 0 {
		_, _ = fmt.Fprintf(w, "  communities:\n")
		for _, n := range r.SuperNodes {
			_, _ = fmt.Fprintf(w, "    %s: %d members\n", n.ID, n.Size)
		}
	}
	if len(r.SuperEdges) > 0 {
		_, _ = fmt.Fprintf(w, "  inter-community edges:\n")
		for _, e := range r.SuperEdges {
			_, _ = fmt.Fprintf(w, "    %s -> %s  weight=%d\n", e.Src, e.Dst, e.Weight)
		}
	}
}
