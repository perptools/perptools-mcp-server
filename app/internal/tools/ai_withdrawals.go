package tools

import (
	"context"

	"mcp-server/app/internal/clients/perptools"
	"mcp-server/app/internal/service"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterAIWithdrawalTools exposes the reverse money path of the deposit
// flow: agent vault -> Main Account (shares redemption) and Main Account ->
// on-chain wallet (custodied USDC payout).
func RegisterAIWithdrawalTools(svc *service.Service) []ToolDef {
	return []ToolDef{
		{
			Tool: mcp.NewTool("withdraw_from_agent",
				mcp.WithDescription(`Withdraw from an AI agent's vault back into the Main Account by redeeming SHARES. Requires authentication.

DENOMINATION — this is the most common mistake: 'shares' is in VAULT SHARES, NOT USDC. The USDC
you receive is approximately shares * sharePrice. Get the current share count and sharePrice from
get_my_agent_stats (or get_agent_details -> userShares). Example: to pull out ~$5 when sharePrice
is 1.25, withdraw 4 shares.

Pass EITHER 'shares' (0 < shares <= what you hold) OR 'all': true (redeems your entire stake —
resolved automatically from your current holding; there is no server-side withdraw-all).

BEHAVIOUR: submits the withdrawal and returns the acknowledgement QUICKLY (~10s; one short status
confirm, NO settlement wait). Returns {withdraw: {transactionId, queued, queuedRequestId}, status,
settled, shares_withdrawn}.

SAVE withdraw.transactionId FROM THE RESPONSE IMMEDIATELY — it is the handle for all follow-ups.
If you lost it (e.g. your client timed out), recover it with get_agent_transactions: the
withdrawal WAS created server-side even if you never saw this response.

SETTLEMENT IS ASYNCHRONOUS AND CAN TAKE UP TO 24 HOURS. Expect settled=false here; that is
normal, not an error. Check progress with get_agent_withdrawal_status (poll occasionally, e.g.
every few minutes — not in a tight loop):
- queued=true          -> the vault's liquidity is tied up in open positions; Envy executes the
                          withdrawal when liquidity frees up.
- WAITING_FOR_APPROVAL / PENDING_APPROVAL -> manual admin review; can take HOURS (up to ~24h).
                          The request is NOT stuck.

RULES:
- Only ONE pending withdrawal per agent at a time — a second one is rejected with "You already
  have a withdrawal in progress" while the first is in flight. If you hit that without a known
  transaction_id, find the pending one via get_agent_transactions.
- Proceeds land in the MAIN ACCOUNT (check with get_balances -> main_account_usdc), NOT the
  on-chain wallet. To reach the wallet, follow up with withdraw_to_wallet.
- LEDGER LAG: even after status COMPLETED, main_account_usdc may read 0 for a few minutes while
  Envy sweeps the credit into the ledger. Poll get_balances until it shows up before calling
  withdraw_to_wallet.`),
				mcp.WithString("wallet_address", mcp.Required(), mcp.Description("Holder's Solana wallet address (base58). Must match the authenticated user.")),
				mcp.WithString("agent_id", mcp.Required(), mcp.Description("Agent to withdraw from (the agent_id used for deposits)")),
				mcp.WithNumber("shares", mcp.Description("Number of vault SHARES to redeem (NOT USDC). Must be > 0 and <= your current holding. Omit when all=true.")),
				mcp.WithBoolean("all", mcp.Description("Withdraw the entire stake. When true, 'shares' is ignored and your full current share count is redeemed.")),
			),
			Handler: withdrawFromAgent(svc),
		},
		{
			Tool: mcp.NewTool("get_agent_withdrawal_status",
				mcp.WithDescription(`Check the settlement status of an agent vault withdrawal by transaction_id. Requires authentication.

Use this to follow up on a withdraw_from_agent (which returns immediately, before settlement).
Settlement is ASYNCHRONOUS and can take UP TO 24 HOURS — check occasionally (every few minutes,
then back off), don't poll in a tight loop. Terminal states: COMPLETED/CONFIRMED = success,
FAILED/REJECTED/CANCELLED = failure. Two non-terminal states are NORMAL and just mean "wait
longer":
- WAITING_FOR_APPROVAL / PENDING_APPROVAL — manual admin review (can take hours, up to ~24h).
- queued execution — vault liquidity is locked in open positions; Envy settles when it frees up.

Lost the transaction_id? List the agent's transactions with get_agent_transactions and take the
id of the pending WITHDRAWAL row.

After it completes, verify the proceeds with get_balances (main_account_usdc goes up).`),
				mcp.WithString("wallet_address", mcp.Required(), mcp.Description("Wallet address that made the withdrawal (base58)")),
				mcp.WithString("transaction_id", mcp.Required(), mcp.Description("The transaction_id returned by withdraw_from_agent (recoverable via get_agent_transactions)")),
			),
			Handler: getAgentWithdrawalStatus(svc),
		},
		{
			Tool: mcp.NewTool("get_agent_transactions",
				mcp.WithDescription(`List YOUR deposit/withdrawal history in one AI agent's vault, newest first. Requires authentication.

Each entry: {id, type: DEPOSIT|WITHDRAWAL, amount (USDC), shares, sharePrice, status,
transactionHash, notes, createdAt}. The "id" is a transaction_id usable with
get_agent_withdrawal_status / get_agent_deposit_status.

USE THIS TO RECOVER from a lost acknowledgement: if withdraw_from_agent or deposit_to_agent was
interrupted (client timeout, dropped connection) and you never saw the transaction_id, the
operation was still created server-side — find it here as the newest matching row. Also the way
to locate the in-flight withdrawal behind a "You already have a withdrawal in progress" rejection
(non-terminal status: PENDING / PENDING_APPROVAL / QUEUED).

agent_id is REQUIRED by the upstream. If you don't know which agent, list your agents with
get_my_portfolio and check each.`),
				mcp.WithString("wallet_address", mcp.Required(), mcp.Description("Holder's Solana wallet address (base58). Must match the authenticated user.")),
				mcp.WithString("agent_id", mcp.Required(), mcp.Description("Agent whose transaction history to list")),
				mcp.WithNumber("page", mcp.Description("Page number, 1-based (default 1)")),
				mcp.WithNumber("limit", mcp.Description("Page size (default 20)")),
			),
			Handler: getAgentTransactions(svc),
		},
		{
			Tool: mcp.NewTool("withdraw_to_wallet",
				mcp.WithDescription(`Withdraw USDC from the Main Account to the user's registered on-chain Solana wallet. Requires authentication.

This is the LAST hop of cashing out: agent vault --(withdraw_from_agent)--> Main Account
--(withdraw_to_wallet)--> on-chain wallet. It spends the Main Account balance (get_balances ->
main_account_usdc); withdraw from an agent first if it is too low.

BEHAVIOUR: fully server-side and SYNCHRONOUS — the custodied master key signs and submits the
on-chain USDC transfer before this call returns. No wallet signature, no follow-up polling.
Returns {transaction_hash, amount, new_master_balance, destination_address, success}.

DESTINATION: ALWAYS the registered wallet (wallet_address). There is no way to send to a third
party — this is a safety property, not a missing feature.

POSSIBLE REJECTIONS:
- Not enough Main Account balance (checked up front, with the current balance in the error).
  NOTE: right after an agent withdrawal completes, the credit can take a few minutes to appear in
  the ledger — if get_balances still shows 0, wait and retry rather than assuming the funds are lost.
- 403 "Withdrawals from your Main AI Wallet are currently disabled. Contact support" — the
  account's master is frozen or platform-funded; the user must contact support.`),
				mcp.WithString("wallet_address", mcp.Required(), mcp.Description("User's registered Solana wallet address (base58) — also the payout destination. Must match the authenticated user.")),
				mcp.WithNumber("amount", mcp.Required(), mcp.Description("USDC amount to withdraw from the Main Account to the wallet. Must be > 0.")),
			),
			Handler: withdrawToWallet(svc),
		},
	}
}

func withdrawFromAgent(svc *service.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		wallet, err := req.RequireString("wallet_address")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		agentID, err := req.RequireString("agent_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		all := optBool(req, "all", false)
		shares := optNumber(req, "shares", 0)
		if !all && shares <= 0 {
			return mcp.NewToolResultError("pass either shares > 0 (in VAULT SHARES, not USDC) or all=true"), nil
		}

		result, err := svc.WithdrawFromAgentAndPoll(ctx, perptools.AgentWithdrawRequest{
			WalletAddress: wallet,
			AgentID:       agentID,
			Shares:        shares,
		}, all)
		if err != nil {
			return mcp.NewToolResultError(formatAuthError("withdraw from agent", err)), nil
		}
		return jsonResult(result)
	}
}

// getAgentWithdrawalStatus is intentionally a near-duplicate of
// getAgentDepositStatus: both ride the same transaction-status service call,
// but the tools stay separate so each can carry flow-specific guidance.
func getAgentWithdrawalStatus(svc *service.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		wallet, err := req.RequireString("wallet_address")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		txID, err := req.RequireString("transaction_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		resp, err := svc.GetAgentTransactionStatus(ctx, perptools.AgentTransactionStatusRequest{
			WalletAddress: wallet,
			TransactionID: txID,
		})
		if err != nil {
			return mcp.NewToolResultError(formatAuthError("get agent withdrawal status", err)), nil
		}
		return jsonResult(resp)
	}
}

func getAgentTransactions(svc *service.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		wallet, err := req.RequireString("wallet_address")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		agentID, err := req.RequireString("agent_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		resp, err := svc.GetAgentTransactions(ctx, perptools.AgentTransactionsRequest{
			WalletAddress: wallet,
			AgentID:       agentID,
			Page:          int(optNumber(req, "page", 1)),
			Limit:         int(optNumber(req, "limit", 20)),
		})
		if err != nil {
			return mcp.NewToolResultError(formatAuthError("get agent transactions", err)), nil
		}
		return jsonResult(resp)
	}
}

func withdrawToWallet(svc *service.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		wallet, err := req.RequireString("wallet_address")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		amount := optNumber(req, "amount", 0)
		if amount <= 0 {
			return mcp.NewToolResultError("amount is required and must be > 0"), nil
		}

		resp, err := svc.WithdrawToWallet(ctx, wallet, amount)
		if err != nil {
			return mcp.NewToolResultError(formatAuthError("withdraw to wallet", err)), nil
		}
		return jsonResult(map[string]any{
			"success":             resp.Success,
			"transaction_hash":    resp.TransactionHash,
			"amount":              resp.Amount.Float64(),
			"new_master_balance":  resp.NewMasterBalance.Float64(),
			"destination_chain":   resp.DestinationChain,
			"destination_address": resp.DestinationAddress,
		})
	}
}
