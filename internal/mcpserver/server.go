package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// ServerName is the MCP server name reported in tools/list metadata.
const ServerName = "archmotif"

// Version is reported in MCP metadata; it tracks the archmotif CLI version
// at build time when invoked from cmd/archmotif. Defaults to "dev".
var Version = "dev"

// New builds the MCP server with all 7 base tools registered against svc.
//
// Callers typically pass it to Serve (which wraps server.ServeStdio with the
// archmotif version pinned).
func New(svc *Service) *server.MCPServer {
	s := server.NewMCPServer(
		ServerName,
		Version,
		server.WithToolCapabilities(false),
	)
	registerTools(s, svc)
	return s
}

// Serve runs the stdio MCP server until stdin closes or ctx is cancelled.
//
// The two extra writers are exposed so tests can swap them; production code
// passes os.Stdin / os.Stdout and discards stderr-style logs internally.
func Serve(ctx context.Context, svc *Service, stdin io.Reader, stdout io.Writer) error {
	s := New(svc)
	stdio := server.NewStdioServer(s)
	return stdio.Listen(ctx, stdin, stdout)
}

// registerTools wires the base read/write tools, the multi-graph tools, and
// one tool per registered metric onto s.
//
// Tool count: 7 base + 6 multi-graph (list/checkout/fork/merge/diff/compare) +
// 2 derived (drift, metrics_list) + one per registered metric.
func registerTools(s *server.MCPServer, svc *Service) {
	registerQuery(s, svc)
	registerNeighbors(s, svc)
	registerPath(s, svc)
	registerActivate(s, svc)
	registerAddNode(s, svc)
	registerAddEdge(s, svc)
	registerUpdateWeight(s, svc)

	registerMultigraphTools(s, svc)
}

// ----- helpers ------------------------------------------------------------

// resultJSON serialises v and returns it as a single-text-content tool result.
// MCP tools return free-form text; we use JSON so callers (LLMs, scripts)
// can re-parse the result.
func resultJSON(v any) (*mcp.CallToolResult, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshal result: %v", err)), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

// stringListArg extracts a []string from arguments at the given key. Accepts
// either a JSON array of strings or a single string (treated as a one-element
// list). Returns nil if the key is absent.
func stringListArg(args map[string]any, key string) ([]string, error) {
	v, ok := args[key]
	if !ok || v == nil {
		return nil, nil
	}
	switch t := v.(type) {
	case []any:
		out := make([]string, 0, len(t))
		for i, elem := range t {
			s, ok := elem.(string)
			if !ok {
				return nil, fmt.Errorf("%s[%d] is not a string", key, i)
			}
			out = append(out, s)
		}
		return out, nil
	case []string:
		return t, nil
	case string:
		return []string{t}, nil
	default:
		return nil, fmt.Errorf("%s is not a string array", key)
	}
}

// mapStringArg extracts a map[string]string from arguments at the given key.
// Each value is stringified (numbers, booleans coerced to their string form)
// so that all of attrs ends up on the GraphML as a string attribute.
func mapStringArg(args map[string]any, key string) (map[string]string, error) {
	v, ok := args[key]
	if !ok || v == nil {
		return nil, nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s is not an object", key)
	}
	out := make(map[string]string, len(m))
	for k, val := range m {
		switch t := val.(type) {
		case string:
			out[k] = t
		case bool:
			if t {
				out[k] = "true"
			} else {
				out[k] = "false"
			}
		case float64:
			// JSON numbers come through as float64; format losslessly.
			out[k] = formatFloat(t)
		case nil:
			out[k] = ""
		default:
			b, err := json.Marshal(t)
			if err != nil {
				return nil, fmt.Errorf("%s[%s]: %w", key, k, err)
			}
			out[k] = string(b)
		}
	}
	return out, nil
}

// formatFloat formats f as the shortest decimal that round-trips losslessly.
// Integers print as e.g. "42", non-integers as "3.14".
func formatFloat(f float64) string {
	if f == float64(int64(f)) {
		return fmt.Sprintf("%d", int64(f))
	}
	return fmt.Sprintf("%g", f)
}

// ----- tool: graph_query --------------------------------------------------

func registerQuery(s *server.MCPServer, svc *Service) {
	tool := mcp.NewTool("graph_query",
		mcp.WithDescription("Query nodes in an archmotif graph by kind/tag/name/package. All filter fields are optional and AND together when set."),
		mcp.WithString("graph_id",
			mcp.Required(),
			mcp.Description("Graph identifier (e.g. repo slug). Maps to graphs/<slug>/actual.graphml under the workspace root."),
		),
		mcp.WithObject("filter",
			mcp.Description("Filter constraints; empty filter returns every node."),
			mcp.Properties(map[string]any{
				"kind":    map[string]any{"type": "string", "description": "Exact match on node kind (function, type, package, ...)"},
				"tag":     map[string]any{"type": "string", "description": "Match one tag from the comma-separated 'tags' attribute"},
				"name":    map[string]any{"type": "string", "description": "Case-insensitive substring match on node name"},
				"package": map[string]any{"type": "string", "description": "Case-insensitive substring match on the 'package' attribute or qname prefix"},
			}),
		),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		graphID, err := req.RequireString("graph_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		var filter QueryFilter
		if raw, ok := args["filter"]; ok && raw != nil {
			b, err := json.Marshal(raw)
			if err == nil {
				_ = json.Unmarshal(b, &filter)
			}
		}
		nodes, err := svc.Query(graphID, filter)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return resultJSON(map[string]any{"nodes": nodes, "count": len(nodes)})
	})
}

// ----- tool: graph_neighbors ----------------------------------------------

func registerNeighbors(s *server.MCPServer, svc *Service) {
	tool := mcp.NewTool("graph_neighbors",
		mcp.WithDescription("Return nodes within `depth` hops of node_id, optionally restricted to specific edge kinds."),
		mcp.WithString("graph_id", mcp.Required(), mcp.Description("Graph identifier")),
		mcp.WithString("node_id", mcp.Required(), mcp.Description("Stable id of the seed node")),
		mcp.WithArray("edge_kinds",
			mcp.Description("Optional list of edge kinds to traverse; empty = any kind"),
			mcp.Items(map[string]any{"type": "string"}),
		),
		mcp.WithNumber("depth",
			mcp.Description("Maximum hops to expand (default 1)"),
			mcp.Min(0),
			mcp.Max(8),
		),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		graphID, err := req.RequireString("graph_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		nodeID, err := req.RequireString("node_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		kinds, err := stringListArg(args, "edge_kinds")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		depth := req.GetInt("depth", 1)
		neighbors, err := svc.Neighbors(graphID, nodeID, kinds, depth)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return resultJSON(map[string]any{"nodes": neighbors, "count": len(neighbors)})
	})
}

// ----- tool: graph_path ---------------------------------------------------

func registerPath(s *server.MCPServer, svc *Service) {
	tool := mcp.NewTool("graph_path",
		mcp.WithDescription("Return one shortest directed path from `from` to `to`, optionally restricted to specific edge kinds. Empty path means unreachable."),
		mcp.WithString("graph_id", mcp.Required(), mcp.Description("Graph identifier")),
		mcp.WithString("from", mcp.Required(), mcp.Description("Source node id")),
		mcp.WithString("to", mcp.Required(), mcp.Description("Destination node id")),
		mcp.WithArray("edge_kinds",
			mcp.Description("Optional list of edge kinds; empty = any kind"),
			mcp.Items(map[string]any{"type": "string"}),
		),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		graphID, err := req.RequireString("graph_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		from, err := req.RequireString("from")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		to, err := req.RequireString("to")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		kinds, err := stringListArg(args, "edge_kinds")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		path, err := svc.Path(graphID, from, to, kinds)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return resultJSON(map[string]any{"path": path, "reachable": len(path) > 0, "length": maxInt(len(path)-1, 0)})
	})
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ----- tool: graph_activate -----------------------------------------------

func registerActivate(s *server.MCPServer, svc *Service) {
	tool := mcp.NewTool("graph_activate",
		mcp.WithDescription("Increment the 'activation' attribute on the given nodes by `weight`. Used by the memory-as-graph experiment."),
		mcp.WithString("graph_id", mcp.Required(), mcp.Description("Graph identifier")),
		mcp.WithArray("node_ids",
			mcp.Required(),
			mcp.Description("Node ids to activate"),
			mcp.Items(map[string]any{"type": "string"}),
		),
		mcp.WithNumber("weight",
			mcp.Required(),
			mcp.Description("Activation delta (added to existing value)"),
		),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		graphID, err := req.RequireString("graph_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		ids, err := stringListArg(args, "node_ids")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if len(ids) == 0 {
			return mcp.NewToolResultError("node_ids is required"), nil
		}
		weight, err := req.RequireFloat("weight")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		result, err := svc.Activate(graphID, ids, weight)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return resultJSON(result)
	})
}

// ----- tool: graph_add_node -----------------------------------------------

func registerAddNode(s *server.MCPServer, svc *Service) {
	tool := mcp.NewTool("graph_add_node",
		mcp.WithDescription("Insert a new node. The id is taken from attrs.id, or derived as '<kind>:<name>' / '<kind>:<index>' if not provided."),
		mcp.WithString("graph_id", mcp.Required(), mcp.Description("Graph identifier")),
		mcp.WithString("kind", mcp.Required(), mcp.Description("Node kind (function, type, memory, contract, ...)")),
		mcp.WithObject("attrs",
			mcp.Description("Arbitrary attribute map. Conventional keys: id, name, qname, package, tags, weight, activation."),
		),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		graphID, err := req.RequireString("graph_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		kind, err := req.RequireString("kind")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		attrs, err := mapStringArg(args, "attrs")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		result, err := svc.AddNode(graphID, kind, attrs)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return resultJSON(result)
	})
}

// ----- tool: graph_add_edge -----------------------------------------------

func registerAddEdge(s *server.MCPServer, svc *Service) {
	tool := mcp.NewTool("graph_add_edge",
		mcp.WithDescription("Insert a directed edge between two existing nodes."),
		mcp.WithString("graph_id", mcp.Required(), mcp.Description("Graph identifier")),
		mcp.WithString("from", mcp.Required(), mcp.Description("Source node id")),
		mcp.WithString("to", mcp.Required(), mcp.Description("Destination node id")),
		mcp.WithString("kind", mcp.Required(), mcp.Description("Edge kind (calls, contains, references, supports, ...)")),
		mcp.WithObject("attrs",
			mcp.Description("Arbitrary edge attribute map"),
		),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		graphID, err := req.RequireString("graph_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		from, err := req.RequireString("from")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		to, err := req.RequireString("to")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		kind, err := req.RequireString("kind")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		attrs, err := mapStringArg(args, "attrs")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		result, err := svc.AddEdge(graphID, from, to, kind, attrs)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return resultJSON(result)
	})
}

// ----- tool: graph_update_weight ------------------------------------------

func registerUpdateWeight(s *server.MCPServer, svc *Service) {
	tool := mcp.NewTool("graph_update_weight",
		mcp.WithDescription("Increment the 'weight' attribute of a node by `delta`."),
		mcp.WithString("graph_id", mcp.Required(), mcp.Description("Graph identifier")),
		mcp.WithString("node_id", mcp.Required(), mcp.Description("Node id")),
		mcp.WithNumber("delta", mcp.Required(), mcp.Description("Weight delta (added to existing value)")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		graphID, err := req.RequireString("graph_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		nodeID, err := req.RequireString("node_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		delta, err := req.RequireFloat("delta")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		result, err := svc.UpdateWeight(graphID, nodeID, delta)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return resultJSON(result)
	})
}
