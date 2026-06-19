package tools

import (
	"context"
	"encoding/base64"
	"strings"

	"mcp-server/app/internal/clients/perptools"
	"mcp-server/app/internal/service"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterAIAgentTools exposes the Envy-backed AI trading agent flow:
// deploy an agent, fund its vault, and follow up on a pending deposit.
func RegisterAIAgentTools(svc *service.Service) []ToolDef {
	return []ToolDef{
		{
			Tool: mcp.NewTool("create_agent",
				mcp.WithDescription(`Deploy a new AI trading agent (an Envy-backed vault) owned by the user's wallet. Requires authentication.

This is step 1 of getting an agent running. After it is created you fund it with USDC (see the
deposit flow below). The caller's wallet becomes the agent's owner; wallet_address MUST be the
authenticated user's Solana wallet (the server rejects a mismatch).

Returns agent_id (save it — you need it for deposit_to_agent) and deployment_id. Deployment is
asynchronous: status starts as "pending"; the agent becomes tradable after it is funded.

BEFORE calling this, make sure the risk-disclosure agreement is signed (prepare_agreement ->
complete_agreement) — the same agreement also gates deposits.

REQUIRED fields and their valid values:
- agent_name: a unique, non-blacklisted display name.
- symbol: a short ticker (e.g. MOMO), letters/digits.
- description: a short trading thesis.
- risk_level: EXACTLY one of "Low", "Medium", "High" (capitalized — other casings/values are rejected).
- avatar_url: the agent's avatar image URL. It MUST be a URL on OUR S3 bucket — arbitrary external
  image URLs are rejected. The standard way to get one is to call generate_agent_avatar FIRST and
  pass its avatar_url here. Uploading your OWN custom image to our S3 bucket is restricted to
  whitelisted users (role KOL or VERIFIED) and is done via the website, not this tool — check with
  check_custom_avatar_eligibility. Everyone else must generate.

Other prerequisites enforced server-side (may reject deployment):
- The wallet must be premint-eligible or hold a KOL role.
- agent_name must be unique and not blacklisted.

AFTER create_agent succeeds, fund the agent:
  1. get_balances — check main_account_usdc.
  2. If it is below your intended deposit (and remember the FIRST deposit into a new agent must be
     at least 10 USDC), top up: prepare_main_deposit -> sign -> complete_main_deposit.
  3. deposit_to_agent with the agent_id from this call.`),
				mcp.WithString("wallet_address", mcp.Required(), mcp.Description("Deployer's Solana wallet address (base58). Must match the authenticated user.")),
				mcp.WithString("agent_name", mcp.Required(), mcp.Description("Display name for the agent (must be unique, not blacklisted)")),
				mcp.WithString("symbol", mcp.Required(), mcp.Description("Short ticker/symbol for the agent (e.g. MOMO)")),
				mcp.WithString("description", mcp.Required(), mcp.Description("Agent description / trading thesis")),
				mcp.WithString("risk_level", mcp.Required(), mcp.Description(`Risk profile. Must be EXACTLY one of "Low", "Medium", or "High" (capitalized).`)),
				mcp.WithString("avatar_url", mcp.Required(), mcp.Description("Agent avatar image URL. MUST be on our S3 bucket — get one from generate_agent_avatar (custom uploads are whitelist-only, KOL/VERIFIED). Arbitrary external URLs are rejected.")),
				mcp.WithString("investment_horizon", mcp.Description("Investment horizon, e.g. short, medium, long (optional)")),
				mcp.WithString("coins_to_trade", mcp.Description("Comma-separated coins the agent may trade (optional)")),
				mcp.WithNumber("max_simultaneous_positions", mcp.Description("Cap on concurrent open positions (optional)")),
				mcp.WithNumber("degen_energy", mcp.Description("Aggressiveness knob, 0-100 (optional, default 0)")),
				mcp.WithNumber("trading_level", mcp.Description("Trading sophistication knob, 0-100 (optional, default 0)")),
			),
			Handler: createAgent(svc),
		},
		{
			Tool: mcp.NewTool("deposit_to_agent",
				mcp.WithDescription(`Deposit USDC from the Main Account into an AI agent's vault, minting vault shares. Requires authentication.

This spends funds that are ALREADY in the Main Account (it does NOT pull from the on-chain wallet).
It is signed server-side with the custodied master key — no wallet signature needed here.

PRECONDITIONS (check these first to avoid rejections):
- The risk-disclosure agreement must be signed (prepare_agreement -> complete_agreement). Otherwise
  the backend rejects with "Please sign the agreement".
- The Main Account must hold at least 'amount' USDC. Check with get_balances (main_account_usdc); if
  it is too low, top it up with prepare_main_deposit -> complete_main_deposit.
- MINIMUM: the FIRST deposit into a given agent must be at least 10 USDC. Later deposits to the same
  agent can be smaller.

BEHAVIOUR: submits the deposit and polls its status (~60s) until it settles. Returns the deposit
acknowledgement (transaction_id), the latest status, and a "settled" flag. If settled=false, follow
up later with get_agent_deposit_status using the transaction_id.

TYPICAL FULL FLOW for a fresh wallet:
  1. prepare_agreement -> sign -> complete_agreement   (once per wallet)
  2. create_agent                                      (get agent_id)
  3. get_balances                                      (see what's in the Main Account)
  4. prepare_main_deposit (>= 10) -> sign -> complete_main_deposit   (if the Main Account is short)
  5. deposit_to_agent`),
				mcp.WithString("wallet_address", mcp.Required(), mcp.Description("Depositor's Solana wallet address (base58). Must match the authenticated user.")),
				mcp.WithString("agent_id", mcp.Required(), mcp.Description("Target agent's ID (the agent_id returned by create_agent)")),
				mcp.WithNumber("amount", mcp.Required(), mcp.Description("USDC amount to deposit. The first deposit into a given agent must be >= 10.")),
			),
			Handler: depositToAgent(svc, false),
		},
		{
			Tool: mcp.NewTool("prepare_agreement",
				mcp.WithDescription(`Step 1/2 of signing the one-time risk-disclosure agreement. Requires authentication.

A signed agreement is a PRECONDITION for deposit_to_agent (the backend rejects deposits with
"Please sign the agreement" until it is on file). Do this once per wallet, early.

Returns the current status:
- If "signed": true — the agreement is already on file; skip complete_agreement and proceed.
- Otherwise — it returns 'message' and 'message_base64'. Have the user's Solana wallet sign the
  UTF-8 bytes of 'message' (decode 'message_base64' to get the exact bytes), base58-encode the
  signature, and pass it to complete_agreement.`),
				mcp.WithString("wallet_address", mcp.Required(), mcp.Description("User's Solana wallet address (base58). Must match the authenticated user.")),
			),
			Handler: prepareAgreement(svc),
		},
		{
			Tool: mcp.NewTool("complete_agreement",
				mcp.WithDescription(`Step 2/2 of signing the risk-disclosure agreement. Submits the wallet's signature. Requires authentication.

Call this only if prepare_agreement returned "signed": false. The signature MUST be the wallet's
ed25519 signature over the agreement message bytes from prepare_agreement, encoded as base58 (the
native format most Solana wallets return from signMessage). On success the agreement is recorded and
agent deposits are unlocked (create_agent + deposit_to_agent can proceed).`),
				mcp.WithString("wallet_address", mcp.Required(), mcp.Description("User's Solana wallet address (base58). Must match the authenticated user.")),
				mcp.WithString("signature", mcp.Required(), mcp.Description("Base58 ed25519 signature of the agreement message bytes (decodes to 64 bytes)")),
			),
			Handler: completeAgreement(svc),
		},
		{
			Tool: mcp.NewTool("get_agent_deposit_status",
				mcp.WithDescription(`Check the settlement status of an agent vault deposit by transaction_id. Requires authentication.

Use this only to follow up on a deposit_to_agent that returned "settled": false (i.e. it did not
finish within the in-call polling window). Poll until the status reaches a terminal state
(COMPLETED/CONFIRMED = success, or FAILED/REJECTED/CANCELLED = failure).`),
				mcp.WithString("wallet_address", mcp.Required(), mcp.Description("Wallet address that made the transaction (base58)")),
				mcp.WithString("transaction_id", mcp.Required(), mcp.Description("The transaction_id returned by deposit_to_agent")),
			),
			Handler: getAgentDepositStatus(svc),
		},
		{
			Tool: mcp.NewTool("stop_agent",
				mcp.WithDescription(`Stop (pause) an AI trading agent the user owns — halts its trading WITHOUT deleting it. Requires authentication.

The agent's vault and the caller's stake stay intact; only trading is suspended (status -> STOPPING/PAUSED). Use start_agent to resume. For permanent removal use delete_agent instead.

Only the agent's owner can stop it; wallet_address MUST be the authenticated user's wallet.`),
				mcp.WithString("wallet_address", mcp.Required(), mcp.Description("Owner's Solana wallet address (base58). Must match the authenticated user.")),
				mcp.WithString("agent_id", mcp.Required(), mcp.Description("The agent to stop (the agent_id / botId)")),
			),
			Handler: setAgentRunning(svc, false),
		},
		{
			Tool: mcp.NewTool("start_agent",
				mcp.WithDescription(`Resume a previously stopped AI trading agent (status -> ACTIVE). Requires authentication. The counterpart to stop_agent. Only the owner can start it; wallet_address MUST be the authenticated user's wallet.`),
				mcp.WithString("wallet_address", mcp.Required(), mcp.Description("Owner's Solana wallet address (base58). Must match the authenticated user.")),
				mcp.WithString("agent_id", mcp.Required(), mcp.Description("The agent to resume (the agent_id / botId)")),
			),
			Handler: setAgentRunning(svc, true),
		},
		{
			Tool: mcp.NewTool("delete_agent",
				mcp.WithDescription(`Permanently archive (DELETE) an AI trading agent the user owns. Requires authentication. THIS IS IRREVERSIBLE.

WITHDRAW FIRST: redeem any remaining stake with withdraw_from_agent before deleting — archiving an agent that still holds your funds can strand them. To merely pause trading, use stop_agent instead (reversible).

Only the agent's owner can delete it; wallet_address MUST be the authenticated user's wallet.`),
				mcp.WithString("wallet_address", mcp.Required(), mcp.Description("Owner's Solana wallet address (base58). Must match the authenticated user.")),
				mcp.WithString("agent_id", mcp.Required(), mcp.Description("The agent to delete/archive (the agent_id / botId)")),
			),
			Handler: deleteAgent(svc),
		},
		{
			Tool: mcp.NewTool("generate_agent_avatar",
				mcp.WithDescription(`Generate an avatar image for a new AI agent and return its URL. Requires authentication.

Agent avatars MUST live on our S3 bucket — you cannot point create_agent at an arbitrary external
image. This tool composes a random avatar, uploads it to our S3 bucket, and returns its public
avatar_url, which you then pass straight to create_agent's avatar_url. This is the normal,
everyone-can-use path for getting an avatar.

Uploading your OWN custom image is a separate, restricted flow: only whitelisted users (role KOL or
VERIFIED) may upload a custom avatar to our S3 bucket, and that is done on the website — not via
this MCP server. Use check_custom_avatar_eligibility to see whether a wallet qualifies; if not,
just generate one here.

Returns {avatar_url}. Generation is quota-limited per user.`),
				mcp.WithString("wallet_address", mcp.Required(), mcp.Description("Authenticated user's Solana wallet address (base58)")),
			),
			Handler: generateAgentAvatar(svc),
		},
		{
			Tool: mcp.NewTool("check_custom_avatar_eligibility",
				mcp.WithDescription(`Check whether the user may upload a CUSTOM agent avatar image to our S3 bucket. Requires authentication.

Custom avatar uploads are restricted to whitelisted users (role KOL or VERIFIED); everyone else
must use generate_agent_avatar. Returns {eligible}. When eligible=false, do not try to supply a
custom image — generate one instead. (The actual custom upload happens on the website, not here.)`),
				mcp.WithString("wallet_address", mcp.Required(), mcp.Description("Authenticated user's Solana wallet address (base58)")),
			),
			Handler: checkCustomAvatarEligibility(svc),
		},
		{
			Tool: mcp.NewTool("upload_custom_agent_avatar",
				mcp.WithDescription(`Upload a CUSTOM image as an agent avatar to our S3 bucket and return its URL. Requires authentication.

WHITELIST ONLY: this works ONLY for whitelisted users whose role is KOL or VERIFIED. Everyone else
gets ACCESS DENIED (role not allowed) — they must use generate_agent_avatar instead. Check first
with check_custom_avatar_eligibility; if eligible=false, do NOT call this.

The MCP server downloads the image at image_url and uploads it (≤ 5 MiB; PNG/JPEG/WebP/GIF only),
then returns {avatar_url} (on our S3 bucket) to pass to create_agent's avatar_url.`),
				mcp.WithString("wallet_address", mcp.Required(), mcp.Description("Authenticated user's Solana wallet address (base58). Must be whitelisted (role KOL/VERIFIED).")),
				mcp.WithString("image_url", mcp.Required(), mcp.Description("Public URL of the image to upload (≤ 5 MiB; PNG, JPEG, WebP, or GIF).")),
			),
			Handler: uploadCustomAgentAvatar(svc),
		},
	}
}

func uploadCustomAgentAvatar(svc *service.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		wallet, err := req.RequireString("wallet_address")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		imageURL, err := req.RequireString("image_url")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		resp, err := svc.UploadCustomAvatar(ctx, wallet, imageURL)
		if err != nil {
			low := strings.ToLower(err.Error())
			if strings.Contains(low, "allowed to upload") || strings.Contains(low, "403") {
				return mcp.NewToolResultError("ACCESS DENIED: custom avatar upload is whitelist-only (role KOL or VERIFIED). This wallet is not eligible — use generate_agent_avatar instead. (check_custom_avatar_eligibility confirms eligibility.)\n\n" + err.Error()), nil
			}
			return mcp.NewToolResultError(formatAuthError("upload custom agent avatar", err)), nil
		}
		return jsonResult(map[string]any{"avatar_url": resp.URL})
	}
}

func generateAgentAvatar(svc *service.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		wallet, err := req.RequireString("wallet_address")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		resp, err := svc.GenerateAgentAvatar(ctx, wallet)
		if err != nil {
			return mcp.NewToolResultError(formatAuthError("generate agent avatar", err)), nil
		}
		return jsonResult(map[string]any{"avatar_url": resp.FileURL})
	}
}

func checkCustomAvatarEligibility(svc *service.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		wallet, err := req.RequireString("wallet_address")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		resp, err := svc.AvatarUploadEligibility(ctx, wallet)
		if err != nil {
			return mcp.NewToolResultError(formatAuthError("check custom avatar eligibility", err)), nil
		}
		return jsonResult(map[string]any{
			"eligible": resp.Eligible,
			"note":     "Custom avatar upload requires role KOL or VERIFIED and is done on the website. If not eligible, use generate_agent_avatar.",
		})
	}
}

func setAgentRunning(svc *service.Service, running bool) server.ToolHandlerFunc {
	op := "stop agent"
	if running {
		op = "start agent"
	}
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		wallet, err := req.RequireString("wallet_address")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		agentID, err := req.RequireString("agent_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		resp, err := svc.SetAgentRunning(ctx, wallet, agentID, running)
		if err != nil {
			return mcp.NewToolResultError(formatAuthError(op, err)), nil
		}
		return jsonResult(resp)
	}
}

func deleteAgent(svc *service.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		wallet, err := req.RequireString("wallet_address")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		agentID, err := req.RequireString("agent_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		resp, err := svc.DeleteAgent(ctx, wallet, agentID)
		if err != nil {
			return mcp.NewToolResultError(formatAuthError("delete agent", err)), nil
		}
		return jsonResult(resp)
	}
}

func createAgent(svc *service.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		wallet, err := req.RequireString("wallet_address")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		name, err := req.RequireString("agent_name")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		symbol, err := req.RequireString("symbol")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		description, err := req.RequireString("description")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		riskLevel, err := req.RequireString("risk_level")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		switch riskLevel {
		case "Low", "Medium", "High":
		default:
			return mcp.NewToolResultError(`risk_level must be exactly one of "Low", "Medium", or "High"`), nil
		}
		avatarURL, err := req.RequireString("avatar_url")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		agentReq := perptools.DeployAgentRequest{
			WalletAddress:     wallet,
			AgentName:         name,
			Symbol:            symbol,
			AgentDescription:  description,
			RiskLevel:         riskLevel,
			AvatarUrl:         avatarURL,
			InvestmentHorizon: optString(req, "investment_horizon"),
			CoinsToTrade:      optString(req, "coins_to_trade"),
			DegenEnergy:       optNumber(req, "degen_energy", 0),
			TradingLevel:      optNumber(req, "trading_level", 0),
		}
		if v := optNumber(req, "max_simultaneous_positions", 0); v > 0 {
			n := int(v)
			agentReq.MaxSimultaneousPositions = &n
		}

		resp, err := svc.DeployAgent(ctx, agentReq)
		if err != nil {
			return mcp.NewToolResultError(formatAuthError("create agent", err)), nil
		}
		return jsonResult(resp)
	}
}

// depositToAgent handles both the main->agent deposit and the one-step direct
// deposit; the only difference is the proxy path selected by `direct`.
func depositToAgent(svc *service.Service, direct bool) server.ToolHandlerFunc {
	op := "deposit to agent"
	if direct {
		op = "direct deposit to agent"
	}
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		wallet, err := req.RequireString("wallet_address")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		agentID, err := req.RequireString("agent_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		amount := optNumber(req, "amount", 0)
		if amount <= 0 {
			return mcp.NewToolResultError("amount is required and must be > 0"), nil
		}

		depositReq := perptools.AgentDepositRequest{
			WalletAddress: wallet,
			AgentID:       agentID,
			Amount:        amount,
		}

		var result *service.AgentDepositResult
		if direct {
			result, err = svc.DirectDepositToAgent(ctx, depositReq)
		} else {
			result, err = svc.DepositToAgent(ctx, depositReq)
		}
		if err != nil {
			return mcp.NewToolResultError(formatAuthError(op, err)), nil
		}
		return jsonResult(result)
	}
}

func prepareAgreement(svc *service.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		wallet, err := req.RequireString("wallet_address")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		resp, err := svc.AgreementStatus(ctx, wallet)
		if err != nil {
			return mcp.NewToolResultError(formatAuthError("prepare agreement", err)), nil
		}
		out := map[string]any{
			"signed":    resp.Signed,
			"signed_at": resp.SignedAt,
		}
		if resp.Signed {
			out["instructions"] = "Agreement already signed — you can deposit into an agent without further action."
		} else {
			out["message"] = resp.Message
			out["message_base64"] = base64.StdEncoding.EncodeToString([]byte(resp.Message))
			out["instructions"] = "Sign the UTF-8 bytes of 'message' with the Solana wallet, base58-encode the signature, then call complete_agreement."
		}
		return jsonResult(out)
	}
}

func completeAgreement(svc *service.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		wallet, err := req.RequireString("wallet_address")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		signature, err := req.RequireString("signature")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		resp, err := svc.SignAgreement(ctx, wallet, signature)
		if err != nil {
			return mcp.NewToolResultError(formatAuthError("complete agreement", err)), nil
		}
		return jsonResult(map[string]any{
			"signed":    resp.Signed,
			"signed_at": resp.SignedAt,
		})
	}
}

func getAgentDepositStatus(svc *service.Service) server.ToolHandlerFunc {
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
			return mcp.NewToolResultError(formatAuthError("get agent deposit status", err)), nil
		}
		return jsonResult(resp)
	}
}
