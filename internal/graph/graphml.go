package graph

import (
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
)

// WriteGraphML encodes the graph as directed GraphML. The generated node IDs
// are GraphML-safe (n0, n1, ...); archmotif's stable node ID is preserved as a
// node data attribute so graph tools can display it without XML ID constraints.
func (g *Graph) WriteGraphML(w io.Writer) error {
	nodes := g.Nodes()
	edges := g.Edges()

	graphIDs := make(map[string]string, len(nodes))
	for i, n := range nodes {
		graphIDs[n.ID] = "n" + strconv.Itoa(i)
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
	for _, k := range graphMLNodeKeys {
		if _, err := fmt.Fprintf(w, `  <key id="%s" for="node" attr.name="%s" attr.type="%s"/>`+"\n", k.id, k.name, k.typ); err != nil {
			return err
		}
	}
	for _, k := range graphMLEdgeKeys {
		if _, err := fmt.Fprintf(w, `  <key id="%s" for="edge" attr.name="%s" attr.type="%s"/>`+"\n", k.id, k.name, k.typ); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w, `  <graph id="G" edgedefault="directed">`); err != nil {
		return err
	}
	for _, n := range nodes {
		if _, err := fmt.Fprintf(w, `    <node id="%s">`+"\n", graphIDs[n.ID]); err != nil {
			return err
		}
		for _, data := range graphMLNodeData(n) {
			if err := writeGraphMLData(w, "      ", data.key, data.value); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w, `    </node>`); err != nil {
			return err
		}
	}
	for i, e := range edges {
		from, ok := graphIDs[e.From]
		if !ok {
			return fmt.Errorf("graph.WriteGraphML: unknown from-node %q", e.From)
		}
		to, ok := graphIDs[e.To]
		if !ok {
			return fmt.Errorf("graph.WriteGraphML: unknown to-node %q", e.To)
		}
		if _, err := fmt.Fprintf(w, `    <edge id="e%d" source="%s" target="%s">`+"\n", i, from, to); err != nil {
			return err
		}
		for _, data := range graphMLEdgeData(e) {
			if err := writeGraphMLData(w, "      ", data.key, data.value); err != nil {
				return err
			}
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

type graphMLKey struct {
	id   string
	name string
	typ  string
}

var graphMLNodeKeys = []graphMLKey{
	{id: "n_label", name: "label", typ: "string"},
	{id: "n_id", name: "archmotif_id", typ: "string"},
	{id: "n_kind", name: "kind", typ: "string"},
	{id: "n_name", name: "name", typ: "string"},
	{id: "n_qname", name: "qname", typ: "string"},
	{id: "n_file", name: "file", typ: "string"},
	{id: "n_line", name: "line", typ: "int"},
	{id: "n_col", name: "col", typ: "int"},
	{id: "n_foreign", name: "foreign", typ: "boolean"},
	{id: "n_layer", name: "layer", typ: "string"},
	{id: "n_detail_level", name: "detail_level", typ: "int"},
	{id: "n_is_contract", name: "is_contract", typ: "boolean"},
	{id: "n_contract_kind", name: "contract_kind", typ: "string"},
	{id: "n_contract_source", name: "contract_source", typ: "string"},
	{id: "n_type_kind", name: "type_kind", typ: "string"},
	{id: "n_role", name: "role", typ: "string"},
	{id: "n_role_source", name: "role_source", typ: "string"},
}

var graphMLEdgeKeys = []graphMLKey{
	{id: "e_label", name: "label", typ: "string"},
	{id: "e_kind", name: "kind", typ: "string"},
	{id: "e_layer", name: "layer", typ: "string"},
	{id: "e_detail_level", name: "detail_level", typ: "int"},
}

type graphMLData struct {
	key   string
	value string
}

func graphMLNodeData(n Node) []graphMLData {
	label := n.Name
	if label == "" {
		label = n.ID
	}
	out := []graphMLData{
		{key: "n_label", value: label},
		{key: "n_id", value: n.ID},
		{key: "n_kind", value: string(n.Kind)},
		{key: "n_foreign", value: strconv.FormatBool(boolAttr(n.Attrs, "foreign"))},
		{key: "n_layer", value: nodeLayer(n.Kind)},
		{key: "n_detail_level", value: strconv.Itoa(nodeDetailLevel(n.Kind))},
		{key: "n_is_contract", value: strconv.FormatBool(n.IsContract())},
	}
	if n.Name != "" {
		out = append(out, graphMLData{key: "n_name", value: n.Name})
	}
	if n.QName != "" {
		out = append(out, graphMLData{key: "n_qname", value: n.QName})
	}
	if n.Pos.File != "" {
		out = append(out,
			graphMLData{key: "n_file", value: n.Pos.File},
			graphMLData{key: "n_line", value: strconv.Itoa(n.Pos.Line)},
			graphMLData{key: "n_col", value: strconv.Itoa(n.Pos.Col)},
		)
	}
	if kind := n.ContractKind(); kind != "" {
		out = append(out, graphMLData{key: "n_contract_kind", value: kind})
	}
	if source := n.ContractSource(); source != "" {
		out = append(out, graphMLData{key: "n_contract_source", value: source})
	}
	if typeKind := stringAttr(n.Attrs, "typeKind"); typeKind != "" {
		out = append(out, graphMLData{key: "n_type_kind", value: typeKind})
	}
	if role := n.Role(); role != "" {
		out = append(out, graphMLData{key: "n_role", value: string(role)})
	}
	if src := n.RoleSource(); src != "" {
		out = append(out, graphMLData{key: "n_role_source", value: src})
	}
	return out
}

func graphMLEdgeData(e Edge) []graphMLData {
	return []graphMLData{
		{key: "e_label", value: string(e.Kind)},
		{key: "e_kind", value: string(e.Kind)},
		{key: "e_layer", value: edgeLayer(e.Kind)},
		{key: "e_detail_level", value: strconv.Itoa(edgeDetailLevel(e.Kind))},
	}
}

func nodeLayer(kind NodeKind) string {
	switch kind {
	case NodePackage, NodeFile:
		return "structure"
	case NodeType, NodeField:
		return "model"
	case NodeFunction, NodeMethod:
		return "behavior"
	case NodeLoop, NodeBranch, NodeDefer:
		return "control"
	case NodeGoroutine, NodeChannelOp, NodeSyncPrim:
		return "concurrency"
	default:
		return "unknown"
	}
}

func nodeDetailLevel(kind NodeKind) int {
	switch kind {
	case NodePackage:
		return 0
	case NodeFile:
		return 1
	case NodeType:
		return 2
	case NodeFunction, NodeMethod, NodeField:
		return 3
	case NodeLoop, NodeBranch, NodeGoroutine, NodeDefer, NodeChannelOp, NodeSyncPrim:
		return 4
	default:
		return 9
	}
}

func edgeLayer(kind EdgeKind) string {
	switch kind {
	case EdgeContains:
		return "structure"
	case EdgeDependsOn:
		return "dependency"
	case EdgeReturns, EdgeImplements, EdgeEmbeds, EdgeUsesType:
		return "type"
	case EdgeCalls, EdgeCallsFrom, EdgeReferences:
		return "call"
	default:
		return "unknown"
	}
}

func edgeDetailLevel(kind EdgeKind) int {
	switch kind {
	case EdgeContains:
		return 0
	case EdgeDependsOn:
		return 1
	case EdgeReturns, EdgeImplements, EdgeEmbeds:
		return 2
	case EdgeUsesType, EdgeCalls, EdgeReferences:
		return 3
	case EdgeCallsFrom:
		return 4
	default:
		return 9
	}
}

func writeGraphMLData(w io.Writer, indent, key, value string) error {
	if _, err := fmt.Fprintf(w, `%s<data key="%s">`, indent, key); err != nil {
		return err
	}
	if err := xml.EscapeText(w, []byte(value)); err != nil {
		return err
	}
	_, err := fmt.Fprintln(w, `</data>`)
	return err
}

func boolAttr(attrs map[string]any, key string) bool {
	if attrs == nil {
		return false
	}
	v, _ := attrs[key].(bool)
	return v
}

func stringAttr(attrs map[string]any, key string) string {
	if attrs == nil {
		return ""
	}
	v, _ := attrs[key].(string)
	return v
}
