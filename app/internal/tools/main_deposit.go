package tools

import (
	"context"

	"mcp-server/app/internal/service"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterMainDepositTools exposes the wallet -> Main Account top-up and the
// balance check. Funding an AI agent is a TWO-balance process:
//
//	on-chain wallet  --(top-up, you sign)-->  Main Account  --(deposit_to_agent)-->  Agent vault
//
// The Main Account is a custodied master wallet on Solana. Funding it is just a
// plain on-chain USDC transfer from the user's wallet to that master address —
// no special gateway, fully doable from here. deposit_to_agent then spends the
// Main Account balance.
func RegisterMainDepositTools(svc *service.Service) []ToolDef {
	return []ToolDef{
		{
			Tool: mcp.NewTool("get_balances",
				mcp.WithDescription(`Show the two balances that matter for funding an agent. Requires authentication. Call this FIRST when planning a deposit.

Returns:
- wallet_usdc       — USDC sitting in the user's on-chain wallet. This is the SOURCE for a Main Account top-up.
- main_account_usdc — USDC in the Main Account. This is what deposit_to_agent actually spends.
- master_wallet     — the Main Account (custodied) Solana address.

How to read it:
- To deposit X USDC into an agent you need main_account_usdc >= X.
- If main_account_usdc is too low, top it up from wallet_usdc first (prepare_main_deposit -> complete_main_deposit).
- Remember the FIRST deposit into any given agent must be at least 10 USDC, so make sure main_account_usdc >= 10 for a new agent.`),
				mcp.WithString("wallet_address", mcp.Required(), mcp.Description("User's Solana wallet address (base58). Must match the authenticated user.")),
			),
			Handler: getBalances(svc),
		},
		{
			Tool: mcp.NewTool("get_master_wallet",
				mcp.WithDescription(`Return the user's Main Account (custodied master) Solana address. Requires authentication.

This is the on-chain destination for a wallet -> Main Account top-up: USDC sent to this address is
credited to the Main Account, which deposit_to_agent then spends. You usually do NOT need to call
this directly — prepare_main_deposit resolves and uses it for you. Use it only if you want to show
or verify the address.`),
				mcp.WithString("wallet_address", mcp.Required(), mcp.Description("User's Solana wallet address (base58). Must match the authenticated user.")),
			),
			Handler: getMasterWallet(svc),
		},
		{
			Tool: mcp.NewTool("prepare_main_deposit",
				mcp.WithDescription(`Step 1 of 2 of funding the Main Account (wallet -> Main Account). Requires authentication.

WHEN TO USE: the Main Account (main_account_usdc from get_balances) does not have enough USDC for the
agent deposit you want to make. This moves USDC from the on-chain wallet into the Main Account.

WHAT IT DOES: resolves the user's custodied master wallet and builds an UNSIGNED Solana transaction
that transfers 'amount' USDC from the wallet to it (creating the recipient token account if needed).

RETURNS: 'unsigned_transaction' (base64) and 'master_wallet'. NEXT: have the user's Solana wallet sign
'unsigned_transaction' (deserialize -> sign -> re-serialize to base64), then call complete_main_deposit
with the signed transaction.

REQUIREMENTS: the wallet must hold at least 'amount' USDC (see get_balances -> wallet_usdc) plus a small
amount of SOL for the network fee. Tip: since a new agent's first deposit must be >= 10 USDC, top up at
least 10 if the Main Account is empty.`),
				mcp.WithString("wallet_address", mcp.Required(), mcp.Description("User's Solana wallet address (base58). Must match the authenticated user.")),
				mcp.WithNumber("amount", mcp.Required(), mcp.Description("USDC amount to move from the wallet into the Main Account (e.g. 10).")),
			),
			Handler: prepareMainDeposit(svc),
		},
		{
			Tool: mcp.NewTool("complete_main_deposit",
				mcp.WithDescription(`Step 2 of 2 of funding the Main Account. Submits the signed top-up transaction. Requires authentication.

Takes the base64 transaction from prepare_main_deposit AFTER the user's wallet has signed it, broadcasts
it to Solana, and waits for confirmation. Returns the on-chain 'transaction_signature'. Once confirmed,
the USDC is credited to the Main Account — verify with get_balances, then call deposit_to_agent.`),
				mcp.WithString("signed_transaction", mcp.Required(), mcp.Description("Base64-encoded SIGNED transaction produced from prepare_main_deposit's unsigned_transaction.")),
			),
			Handler: completeMainDeposit(svc),
		},
	}
}

func getBalances(svc *service.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		wallet, err := req.RequireString("wallet_address")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		bal, err := svc.GetBalances(ctx, wallet)
		if err != nil {
			return mcp.NewToolResultError(formatAuthError("get balances", err)), nil
		}
		return jsonResult(bal)
	}
}

func getMasterWallet(svc *service.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		wallet, err := req.RequireString("wallet_address")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		master, err := svc.GetMasterWallet(ctx, wallet)
		if err != nil {
			return mcp.NewToolResultError(formatAuthError("get master wallet", err)), nil
		}
		return jsonResult(map[string]any{"master_wallet": master})
	}
}

func prepareMainDeposit(svc *service.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		wallet, err := req.RequireString("wallet_address")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		amount := optNumber(req, "amount", 0)
		if amount <= 0 {
			return mcp.NewToolResultError("amount is required and must be > 0"), nil
		}
		plan, err := svc.PrepareMainDeposit(ctx, wallet, amount)
		if err != nil {
			return mcp.NewToolResultError(formatAuthError("prepare main deposit", err)), nil
		}
		return jsonResult(plan)
	}
}

func completeMainDeposit(svc *service.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		signedTx, err := req.RequireString("signed_transaction")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		sig, err := svc.CompleteMainDeposit(ctx, signedTx)
		if err != nil {
			return mcp.NewToolResultError(formatAuthError("complete main deposit", err)), nil
		}
		return jsonResult(map[string]any{"transaction_signature": sig, "submitted": true})
	}
}
