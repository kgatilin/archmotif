package mcpserver

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// registerContractTools wires the six contract-lens tools plus contracts_tag
// (the writer that materialises tags on disk) onto s. Called from
// registerMultigraphTools after the multi-graph tools so the chain is:
//
//	base (7) -> multi-graph (8 + per-metric) -> contracts (7)
func registerContractTools(s *server.MCPServer, svc *Service) {
	registerContractsTag(s, svc)
	registerContractsList(s, svc)
	registerContractsDiff(s, svc)
	registerContractsConsumers(s, svc)
	registerContractsProducers(s, svc)
	registerContractsFieldHistory(s, svc)
	registerContractsExport(s, svc)
}

// ----- contracts_tag (write) ----------------------------------------------

func registerContractsTag(s *server.MCPServer, svc *Service) {
	tool := mcp.NewTool("contracts_tag",
		mcp.WithDescription("Apply the contract tagging heuristic to graph_id and persist the result. Idempotent — running twice produces the same tags and contract_kind attributes."),
		mcp.WithString("graph_id", mcp.Required(), mcp.Description("Graph identifier")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		graphID, err := req.RequireString("graph_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		hist, err := svc.TagAndPersist(graphID)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		total := 0
		for _, v := range hist {
			total += v
		}
		return resultJSON(map[string]any{"counts": hist, "total": total})
	})
}

// ----- contracts_list -----------------------------------------------------

func registerContractsList(s *server.MCPServer, svc *Service) {
	tool := mcp.NewTool("contracts_list",
		mcp.WithDescription("List every node in graph_id that the contract-tagging heuristic recognises as a contract. Optional filters: contract kind and visibility (public/private)."),
		mcp.WithString("graph_id", mcp.Required(), mcp.Description("Graph identifier")),
		mcp.WithString("kind", mcp.Description("Optional contract kind filter: dto, http_handler, config_schema, event, cli_flag, env_var")),
		mcp.WithString("visibility", mcp.Description("Optional visibility filter (public|private)")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		graphID, err := req.RequireString("graph_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		kind := req.GetString("kind", "")
		visibility := req.GetString("visibility", "")
		recs, err := svc.ContractsList(graphID, kind, visibility)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return resultJSON(map[string]any{"contracts": recs, "count": len(recs)})
	})
}

// ----- contracts_diff -----------------------------------------------------

func registerContractsDiff(s *server.MCPServer, svc *Service) {
	tool := mcp.NewTool("contracts_diff",
		mcp.WithDescription("Structured contract delta between graph_a and graph_b: {added, removed, changed:[{name, kind, field_diff}]}. Optional scope restricts to one contract kind (e.g. http_handler)."),
		mcp.WithString("graph_a", mcp.Required(), mcp.Description("Baseline graph id")),
		mcp.WithString("graph_b", mcp.Required(), mcp.Description("Target graph id")),
		mcp.WithString("scope", mcp.Description("Optional contract_kind filter (dto, http_handler, ...)")),
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
		scope := req.GetString("scope", "")
		diff, err := svc.ContractsDiff(a, b, scope)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return resultJSON(diff)
	})
}

// ----- contracts_consumers -----------------------------------------------

func registerContractsConsumers(s *server.MCPServer, svc *Service) {
	tool := mcp.NewTool("contracts_consumers",
		mcp.WithDescription("Return every node with an inbound 'uses-like' edge (uses, usesType, calls, callsFrom, references, dependsOn, contains, embeds) to contract_id."),
		mcp.WithString("graph_id", mcp.Required(), mcp.Description("Graph identifier")),
		mcp.WithString("contract_id", mcp.Required(), mcp.Description("Contract node id")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		graphID, err := req.RequireString("graph_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		contractID, err := req.RequireString("contract_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		consumers, err := svc.ContractsConsumers(graphID, contractID)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return resultJSON(map[string]any{"consumers": consumers, "count": len(consumers)})
	})
}

// ----- contracts_producers -----------------------------------------------

func registerContractsProducers(s *server.MCPServer, svc *Service) {
	tool := mcp.NewTool("contracts_producers",
		mcp.WithDescription("Return every node with an inbound producer edge (returns, implements, publishes, writes, route_registers) to contract_id."),
		mcp.WithString("graph_id", mcp.Required(), mcp.Description("Graph identifier")),
		mcp.WithString("contract_id", mcp.Required(), mcp.Description("Contract node id")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		graphID, err := req.RequireString("graph_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		contractID, err := req.RequireString("contract_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		producers, err := svc.ContractsProducers(graphID, contractID)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return resultJSON(map[string]any{"producers": producers, "count": len(producers)})
	})
}

// ----- contracts_field_history -------------------------------------------

func registerContractsFieldHistory(s *server.MCPServer, svc *Service) {
	tool := mcp.NewTool("contracts_field_history",
		mcp.WithDescription("Walk every graph in the workspace and report the value of `field` on contract_id at each variant. Useful to see how a contract attribute drifts across branches."),
		mcp.WithString("graph_id", mcp.Required(), mcp.Description("Graph identifier (must exist; the workspace it lives in is walked)")),
		mcp.WithString("contract_id", mcp.Required(), mcp.Description("Contract node id")),
		mcp.WithString("field", mcp.Required(), mcp.Description("Attribute name to track")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		graphID, err := req.RequireString("graph_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		contractID, err := req.RequireString("contract_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		field, err := req.RequireString("field")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		entries, err := svc.ContractsFieldHistory(graphID, contractID, field)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return resultJSON(map[string]any{"history": entries, "count": len(entries)})
	})
}

// ----- contracts_export ---------------------------------------------------

func registerContractsExport(s *server.MCPServer, svc *Service) {
	tool := mcp.NewTool("contracts_export",
		mcp.WithDescription("Export the contract subset of graph_id in a structured format. Supported: openapi (HTTP handlers + DTOs as schema stubs), json (raw contract list)."),
		mcp.WithString("graph_id", mcp.Required(), mcp.Description("Graph identifier")),
		mcp.WithString("format", mcp.Description("Export format (openapi|json; default openapi)")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		graphID, err := req.RequireString("graph_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		format := req.GetString("format", "openapi")
		doc, err := svc.ContractsExport(graphID, format)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return resultJSON(doc)
	})
}
