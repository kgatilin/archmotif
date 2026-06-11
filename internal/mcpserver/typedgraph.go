package mcpserver

import (
	"fmt"
	"strconv"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
)

// toTypedGraph converts a mcpserver in-memory Graph into the typed
// internal/graph.Graph form that the metrics package operates on.
//
// The mcpserver Graph is intentionally schema-loose (it accepts any node
// kind and any attribute map, so agents can write memory/contract/spec
// graphs as well as code graphs). The metrics package requires the typed
// shape from internal/graph (NodeKind, EdgeKind, structured Attrs).
//
// The conversion preserves the stable ID, copies the `kind` attribute into
// the typed NodeKind/EdgeKind, and forwards every other attribute through
// the typed Attrs map. Numeric attributes are stored as float64 so metrics
// that inspect them (cycle, modularity) work without re-parsing.
//
// Self-edges and duplicates are silently dropped — the metric layer rejects
// both anyway, and rejecting here would make every metric call fail on
// agent-mutated graphs that happen to contain a self-call.
func toTypedGraph(g *Graph) *mgraph.Graph {
	out := mgraph.New()
	if g == nil {
		return out
	}
	for _, n := range g.Nodes {
		typed := mgraph.Node{
			ID:    n.ID,
			Kind:  mgraph.NodeKind(n.Kind),
			Name:  n.Name,
			QName: n.Attrs["qname"],
			Attrs: typedAttrs(n.Attrs),
		}
		out.AddNode(typed)
	}
	for _, e := range g.Edges {
		_, _ = out.AddEdge(mgraph.Edge{
			From:  e.From,
			To:    e.To,
			Kind:  mgraph.EdgeKind(e.Kind),
			Attrs: typedAttrs(e.Attrs),
		})
	}
	return out
}

// typedAttrs upgrades a string→string attr map (mcpserver's on-disk shape)
// to the string→any shape that internal/graph uses. Numeric and boolean
// values are coerced when their string form matches the expected literal so
// downstream metrics see the right type without per-call detection.
func typedAttrs(in map[string]string) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = coerceAttr(v)
	}
	return out
}

func coerceAttr(s string) any {
	switch s {
	case "true":
		return true
	case "false":
		return false
	}
	if s == "" {
		return s
	}
	// Integers and floats are commonly stored as strings in GraphML — surface
	// them as numbers for metrics that read e.g. detail_level.
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	return s
}

// loadTypedGraph loads the mcpserver graph for graphID from disk and returns
// the typed-graph view used by the metrics runner.
func (s *Service) loadTypedGraph(graphID string) (*mgraph.Graph, error) {
	g, err := s.LoadGraph(graphID)
	if err != nil {
		return nil, fmt.Errorf("loadTypedGraph: %w", err)
	}
	return toTypedGraph(g), nil
}
