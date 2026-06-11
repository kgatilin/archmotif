package shape

import (
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strings"
)

// ReadGraphML loads a directed GraphML file into the generic shape graph.
// Node IDs prefer stable data attributes ("id", "archmotif_id") over XML IDs
// so contracts reference the domain identifiers Gephi displays.
func ReadGraphML(r io.Reader) (*Graph, error) {
	var doc graphMLDocument
	dec := xml.NewDecoder(r)
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("shape: parse graphml: %w", err)
	}
	keyNames := map[string]string{}
	for _, k := range doc.Keys {
		if k.ID == "" {
			continue
		}
		name := strings.TrimSpace(k.AttrName)
		if name == "" {
			name = k.ID
		}
		keyNames[k.ID] = name
	}

	g := &Graph{Nodes: map[string]Node{}}
	xmlToStable := map[string]string{}
	for _, raw := range doc.Graph.Nodes {
		attrs := dataAttrs(raw.Data, keyNames)
		id := firstNonEmpty(attrs["id"], attrs["archmotif_id"], raw.ID)
		if id == "" {
			return nil, fmt.Errorf("shape: graphml node with empty id")
		}
		label := firstNonEmpty(attrs["memory_title"], attrs["label"], attrs["name"], id)
		n := Node{
			ID:    id,
			XMLID: raw.ID,
			Label: label,
			Attrs: attrs,
		}
		g.Nodes[id] = n
		xmlToStable[raw.ID] = id
	}

	for _, raw := range doc.Graph.Edges {
		source := xmlToStable[raw.Source]
		target := xmlToStable[raw.Target]
		if source == "" {
			return nil, fmt.Errorf("shape: graphml edge %q references unknown source %q", raw.ID, raw.Source)
		}
		if target == "" {
			return nil, fmt.Errorf("shape: graphml edge %q references unknown target %q", raw.ID, raw.Target)
		}
		attrs := dataAttrs(raw.Data, keyNames)
		id := firstNonEmpty(attrs["id"], raw.ID)
		predicate := firstNonEmpty(attrs["predicate"], attrs["kind"], attrs["label"], attrs["relation"])
		layer := firstNonEmpty(attrs["relation_layer"], attrs["layer"])
		g.Edges = append(g.Edges, Edge{
			ID:        id,
			Source:    source,
			Target:    target,
			Predicate: predicate,
			Layer:     layer,
			Attrs:     attrs,
		})
	}
	g.computeDegrees()
	return g, nil
}

func (g *Graph) computeDegrees() {
	deg := map[string]int{}
	for _, e := range g.Edges {
		deg[e.Source]++
		deg[e.Target]++
	}
	for id, n := range g.Nodes {
		n.Degree = deg[id]
		g.Nodes[id] = n
	}
}

func dataAttrs(data []graphMLData, keyNames map[string]string) map[string]string {
	attrs := map[string]string{}
	for _, d := range data {
		name := keyNames[d.Key]
		if name == "" {
			name = d.Key
		}
		value := strings.TrimSpace(d.Value)
		if value == "" {
			continue
		}
		if d.Key != "" {
			attrs[d.Key] = value
		}
		attrs[name] = value
	}
	return attrs
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// WriteShapeGraphML writes a selected currentGraph/targetGraph shape as GraphML.
// XML node IDs are generated (n0, n1, ...) and the stable graph IDs are stored
// in the "id" data attribute so Gephi can display them safely.
func WriteShapeGraphML(w io.Writer, graph Subgraph) error {
	if _, err := fmt.Fprintln(w, `<graphml xmlns="http://graphml.graphdrawing.org/xmlns"`); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, `         xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"`); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, `         xsi:schemaLocation="http://graphml.graphdrawing.org/xmlns http://graphml.graphdrawing.org/xmlns/1.0/graphml.xsd">`); err != nil {
		return err
	}
	for _, key := range []struct {
		id, scope, name, typ string
	}{
		{"id", "node", "id", "string"},
		{"label", "node", "label", "string"},
		{"kind", "node", "kind", "string"},
		{"layer", "node", "layer", "string"},
		{"role", "node", "role", "string"},
		{"origin", "node", "origin", "string"},
		{"degree", "node", "degree", "int"},
		{"e_id", "edge", "id", "string"},
		{"e_predicate", "edge", "predicate", "string"},
		{"e_layer", "edge", "layer", "string"},
	} {
		if _, err := fmt.Fprintf(w, `  <key id="%s" for="%s" attr.name="%s" attr.type="%s"/>`+"\n",
			key.id, key.scope, key.name, key.typ); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w, `  <graph id="target-shape" edgedefault="directed">`); err != nil {
		return err
	}

	xmlIDs := map[string]string{}
	nodes := append([]NodeRef(nil), graph.Nodes...)
	sort.SliceStable(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	for i, node := range nodes {
		xmlID := fmt.Sprintf("n%d", i)
		xmlIDs[node.ID] = xmlID
		if _, err := fmt.Fprintf(w, `    <node id="%s">`+"\n", xmlID); err != nil {
			return err
		}
		if err := writeShapeGraphMLData(w, "      ", "id", node.ID); err != nil {
			return err
		}
		if err := writeShapeGraphMLData(w, "      ", "label", firstNonEmpty(node.Label, node.ID)); err != nil {
			return err
		}
		for _, key := range []string{"kind", "layer", "role", "origin"} {
			if value := node.Attrs[key]; value != "" {
				if err := writeShapeGraphMLData(w, "      ", key, value); err != nil {
					return err
				}
			}
		}
		if node.Degree > 0 {
			if err := writeShapeGraphMLData(w, "      ", "degree", fmt.Sprint(node.Degree)); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w, `    </node>`); err != nil {
			return err
		}
	}

	edges := append([]EdgeRef(nil), graph.Edges...)
	sort.SliceStable(edges, func(i, j int) bool {
		if edges[i].Source != edges[j].Source {
			return edges[i].Source < edges[j].Source
		}
		if edges[i].Target != edges[j].Target {
			return edges[i].Target < edges[j].Target
		}
		return edges[i].Predicate < edges[j].Predicate
	})
	for i, edge := range edges {
		source := xmlIDs[edge.Source]
		target := xmlIDs[edge.Target]
		if source == "" || target == "" {
			return fmt.Errorf("shape: target graph edge references missing node: %s -> %s", edge.Source, edge.Target)
		}
		edgeID := firstNonEmpty(edge.ID, fmt.Sprintf("e%d", i))
		if _, err := fmt.Fprintf(w, `    <edge id="e%d" source="%s" target="%s">`+"\n", i, source, target); err != nil {
			return err
		}
		if err := writeShapeGraphMLData(w, "      ", "e_id", edgeID); err != nil {
			return err
		}
		if err := writeShapeGraphMLData(w, "      ", "e_predicate", edge.Predicate); err != nil {
			return err
		}
		if err := writeShapeGraphMLData(w, "      ", "e_layer", edge.Layer); err != nil {
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

func writeShapeGraphMLData(w io.Writer, indent, key, value string) error {
	if value == "" {
		return nil
	}
	if _, err := fmt.Fprintf(w, `%s<data key="%s">`, indent, key); err != nil {
		return err
	}
	if err := xml.EscapeText(w, []byte(value)); err != nil {
		return err
	}
	_, err := fmt.Fprintln(w, `</data>`)
	return err
}

type graphMLDocument struct {
	Keys  []graphMLKey `xml:"key"`
	Graph graphMLGraph `xml:"graph"`
}

type graphMLKey struct {
	ID       string `xml:"id,attr"`
	AttrName string `xml:"attr.name,attr"`
}

type graphMLGraph struct {
	Nodes []graphMLNode `xml:"node"`
	Edges []graphMLEdge `xml:"edge"`
}

type graphMLNode struct {
	ID   string        `xml:"id,attr"`
	Data []graphMLData `xml:"data"`
}

type graphMLEdge struct {
	ID     string        `xml:"id,attr"`
	Source string        `xml:"source,attr"`
	Target string        `xml:"target,attr"`
	Data   []graphMLData `xml:"data"`
}

type graphMLData struct {
	Key   string `xml:"key,attr"`
	Value string `xml:",chardata"`
}
