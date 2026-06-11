package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"mcp-server/app/internal/service"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type ToolDef struct {
	Tool    mcp.Tool
	Handler server.ToolHandlerFunc
}

func RegisterPerptoolsTools(svc *service.Service) []ToolDef {
	return []ToolDef{
		{
			Tool: mcp.NewTool("health",
				mcp.WithDescription("Check Perptools API health status."),
			),
			Handler: healthTool(svc),
		},
		{
			Tool: mcp.NewTool("get_markets",
				mcp.WithDescription("Get paginated list of available trading markets."),
				mcp.WithNumber("limit", mcp.Description("Max results (default 10)")),
				mcp.WithNumber("offset", mcp.Description("Offset for pagination (default 0)")),
			),
			Handler: getMarkets(svc),
		},
		{
			Tool: mcp.NewTool("get_user_points",
				mcp.WithDescription("Get user points and distribution breakdown. Requires authentication."),
				mcp.WithString("public_key", mcp.Required(), mcp.Description("User's Solana public key (base58)")),
			),
			Handler: getUserPoints(svc),
		},
		{
			Tool: mcp.NewTool("get_leaderboard",
				mcp.WithDescription("Get points leaderboard. Requires authentication."),
				mcp.WithString("public_key", mcp.Required(), mcp.Description("User's Solana public key (base58)")),
				mcp.WithNumber("limit", mcp.Description("Max results (default 10)")),
				mcp.WithNumber("offset", mcp.Description("Offset for pagination (default 0)")),
			),
			Handler: getLeaderboard(svc),
		},
	}
}

func jsonResult(v any) (*mcp.CallToolResult, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshal error: %v", err)), nil
	}
	return mcp.NewToolResultText(string(b)), nil
}

func healthTool(svc *service.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		resp, err := svc.Health(ctx)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("health check failed: %v", err)), nil
		}
		return jsonResult(resp)
	}
}

func getMarkets(svc *service.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		limit := int32(optNumber(req, "limit", 10))
		offset := int32(optNumber(req, "offset", 0))

		resp, err := svc.GetMarkets(ctx, limit, offset)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("get markets failed: %v", err)), nil
		}
		return jsonResult(resp)
	}
}

func getUserPoints(svc *service.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		pk, err := req.RequireString("public_key")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		resp, err := svc.GetUserPoints(ctx, pk)
		if err != nil {
			return mcp.NewToolResultError(formatAuthError("get user points", err)), nil
		}
		return jsonResult(resp)
	}
}

func getLeaderboard(svc *service.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		pk, err := req.RequireString("public_key")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		limit := int32(optNumber(req, "limit", 10))
		offset := int32(optNumber(req, "offset", 0))

		resp, err := svc.GetLeaderboard(ctx, pk, limit, offset)
		if err != nil {
			return mcp.NewToolResultError(formatAuthError("get leaderboard", err)), nil
		}
		return jsonResult(resp)
	}
}

func optNumber(req mcp.CallToolRequest, key string, def float64) float64 {
	args := req.GetArguments()
	if v, ok := args[key]; ok {
		if n, ok := v.(float64); ok {
			return n
		}
	}
	return def
}

func optString(req mcp.CallToolRequest, key string) string {
	args := req.GetArguments()
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func optBool(req mcp.CallToolRequest, key string, def bool) bool {
	args := req.GetArguments()
	if v, ok := args[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return def
}
