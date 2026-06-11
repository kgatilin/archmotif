// Package mcpserver implements an MCP (Model Context Protocol) server that
// exposes the archmotif graph store via 7 base read/write tools.
//
// The server is a thin layer over an on-disk GraphML graph. Reads parse the
// GraphML into memory via internal/graphmlx; writes mutate the in-memory copy,
// append a record to the mutations log, and write the GraphML back atomically.
//
// Storage layout (under root, default ~/.archmotif/):
//
//	graphs/<slug>/{actual,target,...}.graphml
//	log/mutations.jsonl
//
// The on-disk format is the same GraphML that `archmotif graph --format=graphml`
// produces, so the MCP server can operate on graphs extracted by the existing
// CLI. Foreign attributes on nodes and edges (anything not in the archmotif
// schema) are preserved verbatim on round-trip.
//
// Note: this package intentionally keeps its own minimal in-memory graph type
// (Node, Edge, Graph) rather than reusing internal/graph. The reason is that
// internal/graph models a fixed set of typed kinds + fixed attribute keys
// (archmotif's Go code graph), whereas MCP writes from agents may add nodes
// and edges of *any* kind with arbitrary attribute maps (e.g. memory-as-graph,
// design intent, contract notes).
package mcpserver

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/kgatilin/archmotif/internal/graphmlx"
)

// Node is one vertex in an archmotif graph as seen by the MCP server.
//
// ID is the producer-assigned stable identifier (preserved across reloads).
// Attrs holds the full attribute map; well-known attrs (name, kind, qname,
// package, tags, weight) are exposed as struct fields for convenience but
// always mirrored into Attrs so callers see one canonical form.
type Node struct {
	ID    string            `json:"id"`
	Kind  string            `json:"kind,omitempty"`
	Name  string            `json:"name,omitempty"`
	Attrs map[string]string `json:"attrs,omitempty"`
}

// Edge is one directed link.
type Edge struct {
	From  string            `json:"from"`
	To    string            `json:"to"`
	Kind  string            `json:"kind,omitempty"`
	Attrs map[string]string `json:"attrs,omitempty"`
}

// Graph is the writable in-memory graph used by the MCP server. It owns the
// canonical state between Load and Save calls. The zero value is not usable;
// construct with NewGraph or Load.
type Graph struct {
	// Directed mirrors the source GraphML's edgedefault. Always true for
	// archmotif-emitted graphs; we preserve it on round-trip.
	Directed bool
	Nodes    []Node
	Edges    []Edge

	byID map[string]int // stable Node.ID -> index in Nodes
}

// NewGraph returns an empty directed graph.
func NewGraph() *Graph {
	return &Graph{
		Directed: true,
		byID:     make(map[string]int),
	}
}

// Node looks up a node by its stable ID.
func (g *Graph) Node(id string) (Node, bool) {
	idx, ok := g.byID[id]
	if !ok {
		return Node{}, false
	}
	return g.Nodes[idx], true
}

// HasNode reports whether a node with the given ID exists.
func (g *Graph) HasNode(id string) bool {
	_, ok := g.byID[id]
	return ok
}

// AddNode inserts n. Returns an error if a node with the same ID already exists.
// The Attrs map is copied so subsequent caller-side mutations do not affect the graph.
func (g *Graph) AddNode(n Node) error {
	if n.ID == "" {
		return fmt.Errorf("mcpserver.AddNode: id is required")
	}
	if _, dup := g.byID[n.ID]; dup {
		return fmt.Errorf("mcpserver.AddNode: duplicate node id %q", n.ID)
	}
	clone := Node{ID: n.ID, Kind: n.Kind, Name: n.Name, Attrs: copyAttrs(n.Attrs)}
	// Make sure well-known fields are reflected into Attrs so on-disk
	// representation stays consistent.
	if clone.Attrs == nil {
		clone.Attrs = make(map[string]string)
	}
	if clone.Kind != "" {
		clone.Attrs["kind"] = clone.Kind
	}
	if clone.Name != "" {
		clone.Attrs["name"] = clone.Name
	}
	clone.Attrs["archmotif_id"] = clone.ID
	g.Nodes = append(g.Nodes, clone)
	g.byID[clone.ID] = len(g.Nodes) - 1
	return nil
}

// AddEdge inserts an edge between existing nodes. Returns an error if either
// endpoint is unknown.
func (g *Graph) AddEdge(e Edge) error {
	if _, ok := g.byID[e.From]; !ok {
		return fmt.Errorf("mcpserver.AddEdge: unknown from-node %q", e.From)
	}
	if _, ok := g.byID[e.To]; !ok {
		return fmt.Errorf("mcpserver.AddEdge: unknown to-node %q", e.To)
	}
	clone := Edge{From: e.From, To: e.To, Kind: e.Kind, Attrs: copyAttrs(e.Attrs)}
	if clone.Attrs == nil {
		clone.Attrs = make(map[string]string)
	}
	if clone.Kind != "" {
		clone.Attrs["kind"] = clone.Kind
	}
	g.Edges = append(g.Edges, clone)
	return nil
}

// UpdateNodeAttr sets a single attribute on the node identified by id. It is
// the primitive used by graph_update_weight and graph_activate. Returns an
// error if the node is unknown.
func (g *Graph) UpdateNodeAttr(id, key, value string) error {
	idx, ok := g.byID[id]
	if !ok {
		return fmt.Errorf("mcpserver.UpdateNodeAttr: unknown node %q", id)
	}
	if g.Nodes[idx].Attrs == nil {
		g.Nodes[idx].Attrs = make(map[string]string)
	}
	g.Nodes[idx].Attrs[key] = value
	return nil
}

// Load reads a GraphML file from path into a writable Graph. The caller owns
// closing of the returned graph (it is just in-memory data).
func Load(path string) (*Graph, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("mcpserver.Load: %w", err)
	}
	defer func() { _ = f.Close() }()
	parsed, err := graphmlx.Read(f)
	if err != nil {
		return nil, fmt.Errorf("mcpserver.Load: %w", err)
	}
	g := &Graph{
		Directed: parsed.Directed,
		byID:     make(map[string]int, len(parsed.Nodes)),
	}
	for _, n := range parsed.Nodes {
		// Copy the attrs map; graphmlx returns a freshly-allocated map per node.
		attrs := copyAttrs(n.Attrs)
		if attrs == nil {
			attrs = make(map[string]string)
		}
		attrs["archmotif_id"] = n.ID
		name := attrs["name"]
		if name == "" {
			name = n.Label
		}
		g.Nodes = append(g.Nodes, Node{
			ID:    n.ID,
			Kind:  n.Kind,
			Name:  name,
			Attrs: attrs,
		})
	}
	for i, n := range g.Nodes {
		if _, dup := g.byID[n.ID]; dup {
			return nil, fmt.Errorf("mcpserver.Load: duplicate node id %q", n.ID)
		}
		g.byID[n.ID] = i
	}
	for _, e := range parsed.Edges {
		attrs := copyAttrs(e.Attrs)
		if attrs == nil {
			attrs = make(map[string]string)
		}
		if e.Kind != "" {
			attrs["kind"] = e.Kind
		}
		g.Edges = append(g.Edges, Edge{
			From:  e.From,
			To:    e.To,
			Kind:  e.Kind,
			Attrs: attrs,
		})
	}
	return g, nil
}

// Save writes g to path in GraphML format. The write is atomic: the data is
// first written to a sibling temp file and then renamed.
func (g *Graph) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mcpserver.Save: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("mcpserver.Save: %w", err)
	}
	tmpName := tmp.Name()
	// Ensure tmp cleanup on error paths.
	defer func() {
		if tmp != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()
	if err := g.writeGraphML(tmp); err != nil {
		return fmt.Errorf("mcpserver.Save: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("mcpserver.Save: close temp: %w", err)
	}
	tmp = nil
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("mcpserver.Save: rename: %w", err)
	}
	return nil
}

// writeGraphML serialises g as a GraphML document. Schema-wise we declare one
// node `<key>` per attribute name we see (typed as string for simplicity — this
// is enough for round-trip; numeric attributes are perfectly readable as strings)
// and likewise for edge attributes.
//
// We use stable attribute order (sorted by name) so byte output is reproducible
// for tests and so two consecutive writes diff cleanly.
func (g *Graph) writeGraphML(w io.Writer) error {
	// Collect attribute names that appear on at least one node / edge.
	nodeAttrs := collectAttrNames(func() []map[string]string {
		out := make([]map[string]string, len(g.Nodes))
		for i, n := range g.Nodes {
			out[i] = n.Attrs
		}
		return out
	}())
	edgeAttrs := collectAttrNames(func() []map[string]string {
		out := make([]map[string]string, len(g.Edges))
		for i, e := range g.Edges {
			out[i] = e.Attrs
		}
		return out
	}())

	edgeDefault := "directed"
	if !g.Directed {
		edgeDefault = "undirected"
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
	if _, err := fmt.Fprintln(w, `         xsi:schemaLocation="http://graphml.graphdrawing.org/xmlns http://graphml.graphdrawing.org/xmlns/1.0/graphml.xsd">`); err != nil {
		return err
	}

	// Declare keys deterministically. Key id is "n_<attr>" / "e_<attr>" — these
	// only need to be unique within the document; we don't have to match the
	// original producer's ids on round-trip because graphmlx indexes by attr.name.
	for _, name := range nodeAttrs {
		if _, err := fmt.Fprintf(w, "  <key id=%q for=\"node\" attr.name=%q attr.type=\"string\"/>\n", "n_"+name, name); err != nil {
			return err
		}
	}
	for _, name := range edgeAttrs {
		if _, err := fmt.Fprintf(w, "  <key id=%q for=\"edge\" attr.name=%q attr.type=\"string\"/>\n", "e_"+name, name); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintf(w, "  <graph id=\"G\" edgedefault=%q>\n", edgeDefault); err != nil {
		return err
	}

	// Emit nodes. GraphML element id must be a NMTOKEN — we always use n0,n1,...
	// since the stable id may contain characters that aren't legal as XML ids.
	xmlIDs := make(map[string]string, len(g.Nodes))
	for i, n := range g.Nodes {
		xmlID := "n" + strconv.Itoa(i)
		xmlIDs[n.ID] = xmlID
		if _, err := fmt.Fprintf(w, "    <node id=%q>\n", xmlID); err != nil {
			return err
		}
		for _, key := range nodeAttrs {
			val, ok := n.Attrs[key]
			if !ok {
				continue
			}
			if err := writeData(w, "      ", "n_"+key, val); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w, "    </node>"); err != nil {
			return err
		}
	}

	// Emit edges.
	for i, e := range g.Edges {
		from, ok := xmlIDs[e.From]
		if !ok {
			return fmt.Errorf("mcpserver.writeGraphML: edge references unknown node %q", e.From)
		}
		to, ok := xmlIDs[e.To]
		if !ok {
			return fmt.Errorf("mcpserver.writeGraphML: edge references unknown node %q", e.To)
		}
		if _, err := fmt.Fprintf(w, "    <edge id=\"e%d\" source=%q target=%q>\n", i, from, to); err != nil {
			return err
		}
		for _, key := range edgeAttrs {
			val, ok := e.Attrs[key]
			if !ok {
				continue
			}
			if err := writeData(w, "      ", "e_"+key, val); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w, "    </edge>"); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintln(w, "  </graph>"); err != nil {
		return err
	}
	_, err := fmt.Fprintln(w, "</graphml>")
	return err
}

func writeData(w io.Writer, indent, key, value string) error {
	if _, err := fmt.Fprintf(w, "%s<data key=%q>", indent, key); err != nil {
		return err
	}
	if err := xml.EscapeText(w, []byte(value)); err != nil {
		return err
	}
	_, err := fmt.Fprintln(w, `</data>`)
	return err
}

// collectAttrNames returns the deterministic sorted union of attribute names
// found in all input maps.
func collectAttrNames(maps []map[string]string) []string {
	seen := make(map[string]struct{})
	for _, m := range maps {
		for k := range m {
			seen[k] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func copyAttrs(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// Slug normalises a graph identifier into a safe directory name. Slashes
// (forward and backward) and characters outside [A-Za-z0-9-_.] are replaced
// with `_`. The result is never empty (defaults to "graph").
func Slug(graphID string) string {
	var b strings.Builder
	b.Grow(len(graphID))
	for _, r := range graphID {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	s := b.String()
	if s == "" {
		return "graph"
	}
	return s
}
