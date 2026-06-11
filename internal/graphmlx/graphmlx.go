// Package graphmlx is a tooling-agnostic, format-driven view over
// GraphML files. It reads a `.graphml` document into a typed but
// generic in-memory graph and provides anomaly detectors that operate
// directly on that view — without depending on archmotif's typed Go
// parser (internal/graph).
//
// The motivation (issue #37): the next-batch optimizer must work on
// any GraphML — code GraphML produced by `archmotif graph
// --format=graphml` AND foreign GraphML such as a memory-graph tool's
// snapshots. Both encode useful structure (parents, labels, degree,
// communities) without sharing archmotif's NodeKind taxonomy.
//
// Design choices:
//
//   - We index attributes by their archmotif `<key>` name, not by raw
//     `<key id=...>`, because the human-meaningful attribute name is
//     stable across producers.
//   - Detectors return a stable, deterministic ordering so JSON output
//     is byte-stable across runs. Ranking ties are broken on
//     (Severity desc, Detector asc, PrimaryID asc).
//   - The package is intentionally read-only: it never mutates a
//     loaded graph and exposes no edit hooks.
package graphmlx

import (
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Graph is the in-memory view of a parsed GraphML document.
// All slices are sorted: Nodes by stable ID, Edges by (Source, Target, Kind).
type Graph struct {
	// Directed mirrors the `edgedefault` attribute on <graph>.
	Directed bool
	// Nodes are sorted ascending by their stable ID (NodeID).
	Nodes []Node
	// Edges are sorted ascending by (From, To, Kind) for deterministic iteration.
	Edges []Edge

	byID    map[string]int // stable ID -> index in Nodes
	byXMLID map[string]int // graphml element id (n0,n1,...) -> index in Nodes
}

// Node is one vertex in a GraphML document.
type Node struct {
	// XMLID is the GraphML element id (e.g. "n0"). Stable per file but
	// not stable across regenerations.
	XMLID string
	// ID is the producer-assigned stable id; for archmotif it lives on
	// the `archmotif_id` data attribute, but other producers may set
	// it directly on the <node id> attribute. We always populate it,
	// preferring `archmotif_id` then `id` then XMLID.
	ID string
	// Label is best-effort human label: `label` data > `name` data > ID.
	Label string
	// Kind is the value of the `kind` attribute when present (used by
	// detectors that group by kind/entity).
	Kind string
	// Attrs maps attribute name (the GraphML `attr.name` rather than
	// the internal key id) to the raw string value. Numeric attributes
	// are also kept as strings; helpers convert on demand.
	Attrs map[string]string
}

// Edge is one directed (or undirected) connection.
type Edge struct {
	XMLID string
	From  string // stable Node.ID, not XMLID
	To    string
	Kind  string // "kind" attribute when present, else "label" attribute
	Attrs map[string]string
}

// Node looks up a node by its stable ID.
func (g *Graph) Node(id string) (Node, bool) {
	if g == nil {
		return Node{}, false
	}
	idx, ok := g.byID[id]
	if !ok {
		return Node{}, false
	}
	return g.Nodes[idx], true
}

// OutgoingFrom returns edges whose From == id.
func (g *Graph) OutgoingFrom(id string) []Edge {
	if g == nil {
		return nil
	}
	out := make([]Edge, 0)
	for _, e := range g.Edges {
		if e.From == id {
			out = append(out, e)
		}
	}
	return out
}

// IncomingTo returns edges whose To == id.
func (g *Graph) IncomingTo(id string) []Edge {
	if g == nil {
		return nil
	}
	out := make([]Edge, 0)
	for _, e := range g.Edges {
		if e.To == id {
			out = append(out, e)
		}
	}
	return out
}

// Degree returns total degree (in+out for directed, edges touching
// node for undirected) for the given node.
func (g *Graph) Degree(id string) int {
	if g == nil {
		return 0
	}
	d := 0
	for _, e := range g.Edges {
		if e.From == id {
			d++
		}
		if e.To == id && e.From != e.To {
			d++
		}
	}
	return d
}

// xmlGraphML is the top-level <graphml> element decoded by Go's
// encoding/xml. We only consume the bits we care about; unknown
// elements are ignored.
type xmlGraphML struct {
	XMLName xml.Name `xml:"graphml"`
	Keys    []xmlKey `xml:"key"`
	Graphs  []xmlGr  `xml:"graph"`
}

type xmlKey struct {
	ID       string `xml:"id,attr"`
	For      string `xml:"for,attr"`
	AttrName string `xml:"attr.name,attr"`
	AttrType string `xml:"attr.type,attr"`
}

type xmlGr struct {
	ID          string    `xml:"id,attr"`
	EdgeDefault string    `xml:"edgedefault,attr"`
	Nodes       []xmlNode `xml:"node"`
	Edges       []xmlEdge `xml:"edge"`
}

type xmlNode struct {
	ID   string    `xml:"id,attr"`
	Data []xmlData `xml:"data"`
}

type xmlEdge struct {
	ID     string    `xml:"id,attr"`
	Source string    `xml:"source,attr"`
	Target string    `xml:"target,attr"`
	Data   []xmlData `xml:"data"`
}

type xmlData struct {
	Key   string `xml:"key,attr"`
	Value string `xml:",chardata"`
}

// Read parses a GraphML document from r into a Graph. The document
// must contain exactly one <graph>; multi-graph documents return an
// error so callers can split them upstream if they need to.
func Read(r io.Reader) (*Graph, error) {
	var doc xmlGraphML
	dec := xml.NewDecoder(r)
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("graphmlx.Read: %w", err)
	}
	if len(doc.Graphs) == 0 {
		return nil, fmt.Errorf("graphmlx.Read: no <graph> element found")
	}
	if len(doc.Graphs) > 1 {
		return nil, fmt.Errorf("graphmlx.Read: %d <graph> elements found, expected 1", len(doc.Graphs))
	}

	keyByID := make(map[string]xmlKey, len(doc.Keys))
	for _, k := range doc.Keys {
		keyByID[k.ID] = k
	}

	gr := doc.Graphs[0]
	out := &Graph{
		Directed: !strings.EqualFold(gr.EdgeDefault, "undirected"),
		byID:     make(map[string]int, len(gr.Nodes)),
		byXMLID:  make(map[string]int, len(gr.Nodes)),
	}

	for _, n := range gr.Nodes {
		attrs := decodeData(n.Data, keyByID, "node")
		stableID := pickFirst(attrs, "archmotif_id", "id")
		if stableID == "" {
			stableID = n.ID
		}
		label := pickFirst(attrs, "label", "name")
		if label == "" {
			label = stableID
		}
		out.Nodes = append(out.Nodes, Node{
			XMLID: n.ID,
			ID:    stableID,
			Label: label,
			Kind:  attrs["kind"],
			Attrs: attrs,
		})
	}
	sort.SliceStable(out.Nodes, func(i, j int) bool { return out.Nodes[i].ID < out.Nodes[j].ID })
	for i, n := range out.Nodes {
		if _, dup := out.byID[n.ID]; dup {
			return nil, fmt.Errorf("graphmlx.Read: duplicate stable node id %q", n.ID)
		}
		out.byID[n.ID] = i
		out.byXMLID[n.XMLID] = i
	}

	for _, e := range gr.Edges {
		attrs := decodeData(e.Data, keyByID, "edge")
		fromIdx, ok := out.byXMLID[e.Source]
		if !ok {
			return nil, fmt.Errorf("graphmlx.Read: edge %s references unknown source %q", e.ID, e.Source)
		}
		toIdx, ok := out.byXMLID[e.Target]
		if !ok {
			return nil, fmt.Errorf("graphmlx.Read: edge %s references unknown target %q", e.ID, e.Target)
		}
		kind := attrs["kind"]
		if kind == "" {
			kind = attrs["label"]
		}
		out.Edges = append(out.Edges, Edge{
			XMLID: e.ID,
			From:  out.Nodes[fromIdx].ID,
			To:    out.Nodes[toIdx].ID,
			Kind:  kind,
			Attrs: attrs,
		})
	}
	sort.SliceStable(out.Edges, func(i, j int) bool {
		if out.Edges[i].From != out.Edges[j].From {
			return out.Edges[i].From < out.Edges[j].From
		}
		if out.Edges[i].To != out.Edges[j].To {
			return out.Edges[i].To < out.Edges[j].To
		}
		return out.Edges[i].Kind < out.Edges[j].Kind
	})
	return out, nil
}

// decodeData translates the slice of <data key="..."> entries into a
// name-keyed map. Unknown keys are kept under their raw key id so
// callers can still reach them.
func decodeData(data []xmlData, keyByID map[string]xmlKey, scope string) map[string]string {
	out := make(map[string]string, len(data))
	for _, d := range data {
		name := d.Key
		if k, ok := keyByID[d.Key]; ok {
			// `for="all"` is permitted; we keep it. The `for` filter is
			// advisory — graphmlx never demands a key declares its scope
			// because foreign producers sometimes omit it.
			if k.For != "" && k.For != "all" && k.For != scope {
				// Still record the value but only if there isn't already
				// a name-keyed entry from a properly-scoped key.
				if _, exists := out[k.AttrName]; exists {
					continue
				}
			}
			if k.AttrName != "" {
				name = k.AttrName
			}
		}
		out[name] = strings.TrimSpace(d.Value)
	}
	return out
}

// pickFirst returns the value of the first attribute name in m that
// resolves to a non-empty string.
func pickFirst(m map[string]string, names ...string) string {
	for _, n := range names {
		if v, ok := m[n]; ok && v != "" {
			return v
		}
	}
	return ""
}
