package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"mcp-server/app/internal/service"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func RegisterAuthTools(svc *service.Service) []ToolDef {
	return []ToolDef{
		{
			Tool: mcp.NewTool("prepare_registration",
				mcp.WithDescription("Prepare Orderly account registration. Returns base64-encoded message that must be signed by the wallet (via Phantom MCP), the wallet address, and a debug hash."),
				mcp.WithString("wallet_address", mcp.Required(), mcp.Description("Solana wallet public key (base58)")),
			),
			Handler: prepareRegistration(svc),
		},
		{
			Tool: mcp.NewTool("complete_registration",
				mcp.WithDescription("Complete Orderly account registration by submitting the wallet signature."),
				mcp.WithString("wallet_address", mcp.Required(), mcp.Description("Solana wallet public key (base58)")),
				mcp.WithString("signature", mcp.Required(), mcp.Description("Wallet signature from Phantom (hex with 0x prefix)")),
			),
			Handler: completeRegistration(svc),
		},
		{
			Tool: mcp.NewTool("prepare_orderly_key",
				mcp.WithDescription("Generate a random ed25519 Orderly key and prepare the registration message. Returns base64-encoded message that must be signed by the wallet (via Phantom MCP), the wallet address, and a debug hash."),
				mcp.WithString("wallet_address", mcp.Required(), mcp.Description("Solana wallet public key (base58)")),
			),
			Handler: prepareOrderlyKey(svc),
		},
		{
			Tool: mcp.NewTool("complete_orderly_key",
				mcp.WithDescription("Complete Orderly key registration by submitting the wallet signature. Stores credentials in memory for subsequent API calls."),
				mcp.WithString("wallet_address", mcp.Required(), mcp.Description("Solana wallet public key (base58)")),
				mcp.WithString("signature", mcp.Required(), mcp.Description("Wallet signature from Phantom (hex with 0x prefix)")),
			),
			Handler: completeOrderlyKey(svc),
		},
	}
}

func prepareRegistration(svc *service.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		wallet, err := req.RequireString("wallet_address")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		result, err := svc.PrepareRegistration(ctx, wallet)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("prepare registration failed: %v", err)), nil
		}

		if result.AlreadyRegistered {
			out, _ := json.Marshal(map[string]any{
				"already_registered": true,
				"account_id":        result.AccountID,
				"wallet_address":    result.WalletAddress,
				"next_step":         "Account already registered. Skip complete_registration and proceed directly to prepare_orderly_key.",
			})
			return mcp.NewToolResultText(string(out)), nil
		}

		out, _ := json.Marshal(map[string]string{
			"message_base64": result.MessageBase64,
			"wallet_address": result.WalletAddress,
			"debug_hash":     result.DebugHash,
			"next_step":      "Sign the message_base64 with the wallet (via Phantom MCP) and call complete_registration with the signature.",
		})
		return mcp.NewToolResultText(string(out)), nil
	}
}

func completeRegistration(svc *service.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		wallet, err := req.RequireString("wallet_address")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		sig, err := req.RequireString("signature")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		accountID, err := svc.CompleteRegistration(ctx, wallet, sig)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("complete registration failed: %v", err)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("Account registered. account_id: %s", accountID)), nil
	}
}

func prepareOrderlyKey(svc *service.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		wallet, err := req.RequireString("wallet_address")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		result, err := svc.PrepareOrderlyKey(ctx, wallet)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("prepare orderly key failed: %v", err)), nil
		}

		out, _ := json.Marshal(map[string]string{
			"message_base64": result.MessageBase64,
			"wallet_address": result.WalletAddress,
			"debug_hash":     result.DebugHash,
		})
		return mcp.NewToolResultText(string(out)), nil
	}
}

func completeOrderlyKey(svc *service.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		wallet, err := req.RequireString("wallet_address")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		sig, err := req.RequireString("signature")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		if err := svc.CompleteOrderlyKey(ctx, wallet, sig); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("complete orderly key failed: %v", err)), nil
		}

		return mcp.NewToolResultText("Orderly key registered. Authentication complete — you can now use all tools."), nil
	}
}
