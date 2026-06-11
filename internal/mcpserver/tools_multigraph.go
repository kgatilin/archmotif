package mcpserver

import (
	"context"
	"fmt"

	"github.com/kgatilin/archmotif/internal/metrics"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// registerMultigraphTools wires the multi-graph + metrics + drift tools onto
// s. It is called from registerTools after the base 7 tools have been
// registered. Every registered archmotif metric gets one MCP tool
// (`graph_metric_<name>`); they share the same handler that pipes through
// the disk cache.
func registerMultigraphTools(s *server.MCPServer, svc *Service) {
	registerGraphList(s, svc)
	registerGraphCheckout(s, svc)
	registerGraphFork(s, svc)
	registerGraphMerge(s, svc)
	registerGraphDiff(s, svc)
	registerGraphCompareMetrics(s, svc)
	registerGraphDrift(s, svc)
	registerGraphMetricsList(s)
	registerTargetTools(s, svc)

	// One tool per registered metric.
	for _, m := range metrics.All() {
		registerMetricTool(s, svc, m.Name(), m.Description())
	}

	// Contract lens (#57) — 6 read tools + 1 writer (contracts_tag).
	registerContractTools(s, svc)
}

// ----- graph_list ---------------------------------------------------------

func registerGraphList(s *server.MCPServer, svc *Service) {
	tool := mcp.NewTool("graph_list",
		mcp.WithDescription("Enumerate graphs in the workspace. Each entry includes id (`<slug>:<variant>`), path, node/edge counts."),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		refs, err := svc.ListGraphs()
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return resultJSON(map[string]any{"graphs": refs, "count": len(refs)})
	})
}

// ----- graph_checkout -----------------------------------------------------

func registerGraphCheckout(s *server.MCPServer, svc *Service) {
	tool := mcp.NewTool("graph_checkout",
		mcp.WithDescription("Validate that a graph_id resolves to an existing graph and return its reference. v1 is stateless: there is no session-pinned 'current' graph; callers continue to pass graph_id explicitly to subsequent tools."),
		mcp.WithString("graph_id", mcp.Required(), mcp.Description("Graph identifier (`<slug>` or `<slug>:<variant>`)")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		graphID, err := req.RequireString("graph_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		ref, err := svc.CheckoutGraph(graphID)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return resultJSON(ref)
	})
}

// ----- graph_fork ---------------------------------------------------------

func registerGraphFork(s *server.MCPServer, svc *Service) {
	tool := mcp.NewTool("graph_fork",
		mcp.WithDescription("Copy source_id to new_id. Use force=true to overwrite an existing target."),
		mcp.WithString("source_id", mcp.Required(), mcp.Description("Source graph id")),
		mcp.WithString("new_id", mcp.Required(), mcp.Description("Destination graph id")),
		mcp.WithBoolean("force", mcp.Description("Overwrite the destination if it exists (default false)")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		src, err := req.RequireString("source_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		dst, err := req.RequireString("new_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		force := req.GetBool("force", false)
		ref, err := svc.ForkGraph(src, dst, force)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return resultJSON(ref)
	})
}

// ----- graph_merge --------------------------------------------------------

func registerGraphMerge(s *server.MCPServer, svc *Service) {
	tool := mcp.NewTool("graph_merge",
		mcp.WithDescription("Combine graph_b into graph_a. Strategies: union (default; keep destination attrs on conflict), strict (abort on node id collision)."),
		mcp.WithString("graph_a", mcp.Required(), mcp.Description("Destination/base graph id")),
		mcp.WithString("graph_b", mcp.Required(), mcp.Description("Graph whose contents are merged into graph_a")),
		mcp.WithString("out_id", mcp.Description("Optional output id; defaults to graph_a")),
		mcp.WithString("strategy", mcp.Description("union|strict (default union)")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		a, err := req.RequireString("graph_a")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		b, err := req.RequireString("graph_b")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		out := req.GetString("out_id", "")
		strategy := req.GetString("strategy", "union")
		res, err := svc.MergeGraphs(a, b, out, strategy)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return resultJSON(res)
	})
}

// ----- graph_diff ---------------------------------------------------------

func registerGraphDiff(s *server.MCPServer, svc *Service) {
	tool := mcp.NewTool("graph_diff",
		mcp.WithDescription("Compute the structural delta from graph_a to graph_b. Returns added/removed/changed nodes and added/removed edges (matched on (from, to, kind))."),
		mcp.WithString("graph_a", mcp.Required(), mcp.Description("Baseline graph id")),
		mcp.WithString("graph_b", mcp.Required(), mcp.Description("Target graph id")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		a, err := req.RequireString("graph_a")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		b, err := req.RequireString("graph_b")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		d, err := svc.DiffGraphs(a, b)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return resultJSON(d)
	})
}

// ----- graph_compare_metrics ---------------------------------------------

func registerGraphCompareMetrics(s *server.MCPServer, svc *Service) {
	tool := mcp.NewTool("graph_compare_metrics",
		mcp.WithDescription("Run a list of metrics against two graphs and return a delta table (b - a) for the graph-scope value of each metric. Empty `metrics` runs every registered metric."),
		mcp.WithString("graph_a", mcp.Required(), mcp.Description("First graph id")),
		mcp.WithString("graph_b", mcp.Required(), mcp.Description("Second graph id")),
		mcp.WithArray("metrics",
			mcp.Description("Optional list of metric names; empty runs every registered metric"),
			mcp.Items(map[string]any{"type": "string"}),
		),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		a, err := req.RequireString("graph_a")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		b, err := req.RequireString("graph_b")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		names, err := stringListArg(args, "metrics")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		report, err := svc.CompareMetrics(ctx, a, b, names)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return resultJSON(report)
	})
}

// ----- graph_drift --------------------------------------------------------

func registerGraphDrift(s *server.MCPServer, svc *Service) {
	tool := mcp.NewTool("graph_drift",
		mcp.WithDescription("Compute per-metric drift of an actual graph against a target graph. Positive delta means actual has more of the metric than target (regression signal for cycle_rank / forbidden_role_edges, improvement signal for modularity)."),
		mcp.WithString("actual_id", mcp.Required(), mcp.Description("Actual graph id")),
		mcp.WithString("target_id", mcp.Required(), mcp.Description("Target / reference graph id")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		actual, err := req.RequireString("actual_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		target, err := req.RequireString("target_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		report, err := svc.ComputeDrift(ctx, actual, target)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return resultJSON(report)
	})
}

// ----- graph_metrics_list -------------------------------------------------

func registerGraphMetricsList(s *server.MCPServer) {
	tool := mcp.NewTool("graph_metrics_list",
		mcp.WithDescription("List every metric the MCP server can compute. Each entry corresponds to a `graph_metric_<name>` tool."),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return resultJSON(map[string]any{"metrics": RegisteredMetrics()})
	})
}

// registerMetricTool wires one MCP tool per registered metric. The handler
// runs through the disk cache so repeated review-session calls are free.
func registerMetricTool(s *server.MCPServer, svc *Service, metricName, desc string) {
	toolName := "graph_metric_" + metricName
	if desc == "" {
		desc = fmt.Sprintf("Run the %s metric against a graph and return all records (graph + region scope) plus the graph-scope summary value.", metricName)
	}
	tool := mcp.NewTool(toolName,
		mcp.WithDescription(desc),
		mcp.WithString("graph_id", mcp.Required(), mcp.Description("Graph identifier")),
		mcp.WithBoolean("force", mcp.Description("Skip the on-disk cache and recompute (default false)")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		graphID, err := req.RequireString("graph_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		force := req.GetBool("force", false)
		res, err := svc.ComputeMetric(ctx, graphID, metricName, !force)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return resultJSON(res)
	})
}
