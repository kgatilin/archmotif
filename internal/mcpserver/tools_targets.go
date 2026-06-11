package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kgatilin/archmotif/internal/targetcontract"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerTargetTools(s *server.MCPServer, svc *Service) {
	registerTargetPut(s, svc)
	registerTargetList(s, svc)
	registerTargetShow(s, svc)
}

func registerTargetPut(s *server.MCPServer, svc *Service) {
	tool := mcp.NewTool("target_put",
		mcp.WithDescription("Store a target architecture contract as a graph variant in the workspace. Returns the target graph_id, which can be opened with graph_checkout/query/neighbors or browser /api/graph?graph_id=..."),
		mcp.WithString("graph_id", mcp.Required(), mcp.Description("Source graph id whose target branch should be created")),
		mcp.WithString("target_id", mcp.Description("Target id / branch name. Defaults to contract.id")),
		mcp.WithObject("contract", mcp.Description("Target architecture contract JSON")),
		mcp.WithBoolean("force", mcp.Description("Overwrite existing target graph variant if present")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		graphID, err := req.RequireString("graph_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		raw, ok := args["contract"]
		if !ok || raw == nil {
			return mcp.NewToolResultError("contract is required"), nil
		}
		contract, err := decodeTargetContract(raw)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		ref, err := svc.PutTargetGraph(graphID, req.GetString("target_id", ""), contract, req.GetBool("force", false))
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return resultJSON(ref)
	})
}

func registerTargetList(s *server.MCPServer, svc *Service) {
	tool := mcp.NewTool("target_list",
		mcp.WithDescription("List target graph variants stored for a source graph id."),
		mcp.WithString("graph_id", mcp.Required(), mcp.Description("Source graph id")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		graphID, err := req.RequireString("graph_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		refs, err := svc.ListTargets(graphID)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return resultJSON(map[string]any{"targets": refs, "count": len(refs)})
	})
}

func registerTargetShow(s *server.MCPServer, svc *Service) {
	tool := mcp.NewTool("target_show",
		mcp.WithDescription("Return the nodes and edges of a stored target architecture graph."),
		mcp.WithString("graph_id", mcp.Required(), mcp.Description("Source graph id")),
		mcp.WithString("target_id", mcp.Required(), mcp.Description("Target id used with target_put")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		graphID, err := req.RequireString("graph_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		targetID, err := req.RequireString("target_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		shape, err := svc.ShowTarget(graphID, targetID)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return resultJSON(shape)
	})
}

func decodeTargetContract(raw any) (targetcontract.Contract, error) {
	var contract targetcontract.Contract
	b, err := json.Marshal(raw)
	if err != nil {
		return contract, fmt.Errorf("decode target contract: marshal: %w", err)
	}
	if err := json.Unmarshal(b, &contract); err != nil {
		return contract, fmt.Errorf("decode target contract: %w", err)
	}
	return contract, nil
}
