package diagram

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// CurrentJSONVersion is the version emitted by RenderJSON. Bump on
// breaking schema changes to the Diagram envelope.
const CurrentJSONVersion = 1

// Format identifies a renderer.
type Format string

// Supported output formats.
const (
	FormatD2      Format = "d2"
	FormatJSON    Format = "json"
	FormatGraphML Format = "graphml"
)

// AllFormats returns the list of renderable formats in stable order.
func AllFormats() []Format {
	return []Format{FormatD2, FormatJSON, FormatGraphML}
}

// ParseFormat normalises a CLI string into a Format. Empty string
// defaults to D2 (matches the CLI's `--format=d2` default).
func ParseFormat(s string) (Format, error) {
	switch strings.TrimSpace(s) {
	case "", string(FormatD2):
		return FormatD2, nil
	case string(FormatJSON):
		return FormatJSON, nil
	case string(FormatGraphML):
		return FormatGraphML, nil
	}
	return "", fmt.Errorf("unknown diagram format %q (want: d2|json|graphml)", s)
}

// Render writes d in the requested format to w.
func Render(w io.Writer, d *Diagram, f Format) error {
	switch f {
	case FormatD2:
		return RenderD2(w, d)
	case FormatJSON:
		return RenderJSON(w, d)
	case FormatGraphML:
		return RenderGraphML(w, d)
	}
	return fmt.Errorf("diagram: unsupported format %q", f)
}

// JSONEnvelope is the on-disk JSON shape.
type JSONEnvelope struct {
	Version int      `json:"version"`
	Diagram *Diagram `json:"diagram"`
}

// RenderJSON encodes the diagram as JSON with two-space indentation.
func RenderJSON(w io.Writer, d *Diagram) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(JSONEnvelope{Version: CurrentJSONVersion, Diagram: d})
}

// RenderD2 writes a deterministic D2 source rendering of d. Clusters
// (when present on diagram nodes) become D2 containers; nodes without
// a cluster render at the top level.
//
// Output shape:
//
//	# <title>
//	# kind: <kind>
//	cluster_a: {
//	  "id1": {label: "Foo"; tooltip: "..."}
//	  ...
//	}
//	"id1" -> "id2": "kind"
//
// IDs are quoted so colons / slashes in archmotif's stable IDs (per
// ADR-005) survive D2 lexing.
func RenderD2(w io.Writer, d *Diagram) error {
	if d == nil {
		return fmt.Errorf("diagram: nil diagram")
	}
	var b strings.Builder
	if d.Title != "" {
		fmt.Fprintf(&b, "# %s\n", d.Title)
	}
	fmt.Fprintf(&b, "# kind: %s\n", d.Kind)
	for _, note := range d.Notes {
		fmt.Fprintf(&b, "# note: %s\n", note)
	}
	b.WriteString("\n")

	// Group nodes by cluster, preserving stable order.
	type clusterGroup struct {
		name  string
		nodes []DiagNode
	}
	groups := []clusterGroup{}
	groupIdx := map[string]int{}
	add := func(n DiagNode) {
		key := n.Cluster
		if idx, ok := groupIdx[key]; ok {
			groups[idx].nodes = append(groups[idx].nodes, n)
			return
		}
		groupIdx[key] = len(groups)
		groups = append(groups, clusterGroup{name: key, nodes: []DiagNode{n}})
	}
	for _, n := range d.Nodes {
		add(n)
	}
	// Stable cluster order: empty cluster first, then alphabetical.
	sort.SliceStable(groups, func(i, j int) bool {
		if (groups[i].name == "") != (groups[j].name == "") {
			return groups[i].name == ""
		}
		return groups[i].name < groups[j].name
	})

	for _, g := range groups {
		indent := ""
		if g.name != "" {
			fmt.Fprintf(&b, "%s: {\n", d2Quote(g.name))
			indent = "  "
		}
		for _, n := range g.nodes {
			fmt.Fprintf(&b, "%s%s: {label: %s}\n",
				indent, d2Quote(n.ID), d2QuoteValue(n.Label))
		}
		if g.name != "" {
			b.WriteString("}\n")
		}
	}

	if len(d.Nodes) > 0 && len(d.Edges) > 0 {
		b.WriteString("\n")
	}
	for _, e := range d.Edges {
		label := e.Label
		if label == "" {
			label = string(e.Kind)
		}
		fmt.Fprintf(&b, "%s -> %s: %s\n",
			d2Quote(e.From), d2Quote(e.To), d2QuoteValue(label))
	}

	_, err := w.Write([]byte(b.String()))
	return err
}

// d2Quote returns the D2-safe form of s. We always quote because
// archmotif IDs contain colons and slashes that D2 would otherwise
// interpret as syntax.
func d2Quote(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

// d2QuoteValue quotes a label value for D2. Values can contain spaces;
// quoting is the safest path.
func d2QuoteValue(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

// xmlWriter wraps an io.Writer and accumulates the first write error,
// allowing sequential XML writes without per-call error checks.
type xmlWriter struct {
	w   io.Writer
	err error
}

func (xw *xmlWriter) Write(p []byte) (int, error) {
	if xw.err != nil {
		return 0, xw.err
	}
	n, err := xw.w.Write(p)
	xw.err = err
	return n, err
}

func (xw *xmlWriter) printf(format string, args ...any) {
	if xw.err == nil {
		_, xw.err = fmt.Fprintf(xw.w, format, args...)
	}
}

func (xw *xmlWriter) println(s string) {
	if xw.err == nil {
		_, xw.err = fmt.Fprintln(xw.w, s)
	}
}

// RenderGraphML writes a GraphML subgraph of d. The structure mirrors
// internal/graph's full GraphML export (same key ids where applicable)
// so downstream Gephi templates work on either source.
func RenderGraphML(w io.Writer, d *Diagram) error {
	if d == nil {
		return fmt.Errorf("diagram: nil diagram")
	}
	graphIDs := make(map[string]string, len(d.Nodes))
	for i, n := range d.Nodes {
		graphIDs[n.ID] = "n" + strconv.Itoa(i)
	}

	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<graphml xmlns="http://graphml.graphdrawing.org/xmlns"` + "\n")
	b.WriteString(`         xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"` + "\n")
	b.WriteString(`         xsi:schemaLocation="http://graphml.graphdrawing.org/xmlns http://graphml.graphdrawing.org/xmlns/1.0/graphml.xsd">` + "\n")
	for _, k := range diagramNodeKeys {
		fmt.Fprintf(&b, `  <key id="%s" for="node" attr.name="%s" attr.type="%s"/>`+"\n", k.id, k.name, k.typ)
	}
	for _, k := range diagramEdgeKeys {
		fmt.Fprintf(&b, `  <key id="%s" for="edge" attr.name="%s" attr.type="%s"/>`+"\n", k.id, k.name, k.typ)
	}
	fmt.Fprintf(&b, `  <graph id="diagram-%s" edgedefault="directed">`+"\n", string(d.Kind))

	if _, err := w.Write([]byte(b.String())); err != nil {
		return err
	}

	xw := &xmlWriter{w: w}
	for _, n := range d.Nodes {
		xw.printf(`    <node id="%s">`+"\n", graphIDs[n.ID])
		writeData(xw, "      ", "n_label", n.Label)
		writeData(xw, "      ", "n_id", n.ID)
		if n.Kind != "" {
			writeData(xw, "      ", "n_kind", string(n.Kind))
		}
		if n.Role != "" {
			writeData(xw, "      ", "n_role", string(n.Role))
		}
		if n.Cluster != "" {
			writeData(xw, "      ", "n_cluster", n.Cluster)
		}
		if len(n.EvidenceIDs) > 0 {
			writeData(xw, "      ", "n_evidence", strings.Join(n.EvidenceIDs, ","))
		}
		xw.println("    </node>")
	}
	for i, e := range d.Edges {
		from, ok := graphIDs[e.From]
		if !ok {
			return fmt.Errorf("diagram: graphml: unknown from-node %q", e.From)
		}
		to, ok := graphIDs[e.To]
		if !ok {
			return fmt.Errorf("diagram: graphml: unknown to-node %q", e.To)
		}
		xw.printf(`    <edge id="e%d" source="%s" target="%s">`+"\n", i, from, to)
		label := e.Label
		if label == "" {
			label = string(e.Kind)
		}
		writeData(xw, "      ", "e_label", label)
		writeData(xw, "      ", "e_kind", string(e.Kind))
		if len(e.EvidenceIDs) > 0 {
			writeData(xw, "      ", "e_evidence", strings.Join(e.EvidenceIDs, ","))
		}
		xw.println("    </edge>")
	}
	xw.println("  </graph>")
	xw.println("</graphml>")
	return xw.err
}

type graphMLKey struct {
	id   string
	name string
	typ  string
}

var diagramNodeKeys = []graphMLKey{
	{id: "n_label", name: "label", typ: "string"},
	{id: "n_id", name: "archmotif_id", typ: "string"},
	{id: "n_kind", name: "kind", typ: "string"},
	{id: "n_role", name: "role", typ: "string"},
	{id: "n_cluster", name: "cluster", typ: "string"},
	{id: "n_evidence", name: "evidence_ids", typ: "string"},
}

var diagramEdgeKeys = []graphMLKey{
	{id: "e_label", name: "label", typ: "string"},
	{id: "e_kind", name: "kind", typ: "string"},
	{id: "e_evidence", name: "evidence_ids", typ: "string"},
}

func writeData(xw *xmlWriter, indent, key, value string) {
	xw.printf(`%s<data key="%s">`, indent, key)
	if xw.err == nil {
		xw.err = xml.EscapeText(xw.w, []byte(value))
	}
	xw.println(`</data>`)
}
