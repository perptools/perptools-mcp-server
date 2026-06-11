package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"mcp-server/app/internal/service"
	"mcp-server/app/internal/tools"

	"github.com/mark3labs/mcp-go/server"
)

func main() {
	svc := service.NewService(service.Config{
		OrderlyBaseURL:   "https://api.orderly.org",
		PerptoolsBaseURL: "https://app.perptools.ai/api",
		BrokerID:         "dextools",
		SolanaRPCURL:     envOrDefault("SOLANA_RPC_URL", "https://api.mainnet-beta.solana.com"),
	})

	s := server.NewMCPServer(
		"perptools-mcp",
		"1.0.0",
		server.WithToolCapabilities(true),
		server.WithInstructions(`You are connected to the Perptools/Orderly MCP server for Solana perpetual futures trading.

PREREQUISITE — Solana wallet access:
This server requires the ability to sign messages and transactions with a Solana wallet. You MUST have access to either a wallet provider (e.g. connected wallet MCP, wallet extension) or the user's private key. Before starting any auth or trading flow, CHECK if you have a way to sign with the user's Solana wallet. If not, ask the user to provide a connected wallet or configure wallet access for you.

AUTHENTICATION (required before trading):
1. Get the user's Solana wallet address (ask the user if needed).
2. Call prepare_registration with wallet_address → if already_registered=true, skip to step 4.
3. Sign the returned message_base64 with the user's Solana wallet → call complete_registration with the signature.
4. Call prepare_orderly_key with wallet_address.
5. Sign the returned message_base64 with the user's Solana wallet → call complete_orderly_key with the signature.
6. Authentication is complete. All trading tools are now available.

TRADING TOOLS (require authentication):
- create_order        — place MARKET/LIMIT orders on PERP markets. Use order_quantity in base currency (ETH, BTC), NOT USDC.
- cancel_order       — cancel an open order by order_id.
- get_positions      — view all open positions, collateral, margin info.
- set_position_tp_sl  — set take-profit and stop-loss on existing position (one TP/SL order per symbol).
- get_algo_orders    — list TP/SL and other algo orders. Optional symbol filter.
- cancel_algo_order  — cancel TP/SL by algo_order_id.

AI TRADING AGENTS — create an agent and fund it (all require authentication):

  MENTAL MODEL — money lives in two places and moves in two hops:
      on-chain wallet  --(top-up, user signs a tx)-->  Main Account  --(deposit_to_agent)-->  Agent vault
  The Main Account is a per-user custodied USDC balance. deposit_to_agent spends the Main Account,
  NOT the on-chain wallet — so you usually must top up the Main Account first.

  TOOLS:
  - prepare_agreement / complete_agreement — sign the one-time risk-disclosure agreement. It gates BOTH
        creating and depositing. prepare returns a message (or signed=true if already done); sign the
        message bytes with the wallet and pass the base58 signature to complete.
  - create_agent             — deploy a new agent. Required: agent_name (unique), symbol, description,
        risk_level (EXACTLY "Low", "Medium", or "High"), avatar_url (any public image URL). Returns
        agent_id (save it). Needs premint/KOL eligibility + the signed agreement.
  - get_balances             — show wallet_usdc (on-chain, the top-up source) and main_account_usdc
        (what deposit_to_agent spends). Use it to decide whether/how much to top up.
  - get_master_wallet        — (optional) the Main Account on-chain address.
  - prepare_main_deposit     — build an UNSIGNED USDC transfer (wallet -> Main Account) for 'amount'; user signs it.
  - complete_main_deposit    — submit the signed top-up tx to Solana; once confirmed the Main Account is funded.
  - deposit_to_agent         — move USDC from the Main Account into the agent (mints shares); polls ~60s.
  - get_agent_deposit_status — follow up on a deposit that returned settled=false (by transaction_id).

  END-TO-END (fresh wallet, only goal is "fund an agent"):
  1. Authenticate (steps 1-6 above).
  2. prepare_agreement -> if not signed, wallet signs the message -> complete_agreement.   (once per wallet)
  3. create_agent (risk_level Low/Medium/High, avatar_url set) -> note the agent_id.
  4. get_balances. Decide the deposit size. NOTE: the FIRST deposit into a new agent must be at least 10 USDC.
  5. If main_account_usdc is short: prepare_main_deposit(amount >= what you need) -> wallet signs -> complete_main_deposit.
  6. deposit_to_agent(agent_id, amount). If it returns settled=false, poll get_agent_deposit_status.

  KEY RULES: risk_level is one of Low/Medium/High only. The first deposit per agent is >= 10 USDC.
  The wallet needs a little SOL to pay the top-up network fee.

  DISCOVER & TRACK — read-only stats (require authentication, move no funds):
  - list_agents          — browse the public agent leaderboard, best-first by sort_by
        (aum/pnl1h/pnl24h/pnl7d/pnl30d). Returns each agent's id, riskLevel, aum and pnl numbers.
        This is how you FIND an agent_id to invest in.
  - get_agent_details    — deep dive on one agent: vault (AUM, sharePrice, investorCount),
        win/loss analytics, strategy description, and the user's own stake in it.
  - get_agent_positions  — the agent's actual trades (long/short, entry, pnlPercent, leverage);
        use type="open" for currently open ones.
  - get_my_agent_stats   — the user's stake in ONE agent: shares, current value, % of vault.
        Call it after a deposit settles to confirm shares were minted.
  - get_my_portfolio     — everything at once: Main Account USDC, total portfolio value, and all
        agents the user holds shares in. Good first call for "what do I own?" and post-deposit checks.

  TYPICAL DISCOVERY FLOW: list_agents -> get_agent_details / get_agent_positions on a candidate ->
  deposit_to_agent -> get_my_portfolio (or get_my_agent_stats) to verify the stake.

  CHAT — talk to an agent's command terminal (require authentication):
  - send_agent_message      — send a TERMINAL COMMAND (not free-form chat — free text returns
        "Unknown command"). Read-only commands: strategy, positions, backtest [days] [coin],
        report, help. CONTROL commands (change the live agent — only on explicit user request):
        add/remove <coin>, alloc, risk <level>, close <coin>, stop, resume. Replies can take
        TENS OF SECONDS and arrive in chunks — the tool holds the session open and returns the
        full concatenated reply. timed_out=true means no reply yet: retry or check history
        shortly after. Default wait 60s, max 120s.
  - get_agent_chat_history  — the persisted exchanges (newest batch first; pass the returned
        cursor as 'before' to page older). Use it to review past commands or to find a reply
        that landed after a send timed out.
  For structured numbers prefer get_agent_details / get_agent_positions over terminal text.

  WITHDRAWALS — the reverse path, money moves back in two hops (require authentication):
      Agent vault  --(withdraw_from_agent)-->  Main Account  --(withdraw_to_wallet)-->  on-chain wallet

  - withdraw_from_agent         — redeem vault SHARES (NOT USDC!) back into the Main Account.
        USDC received ~= shares * sharePrice (get both from get_my_agent_stats). Pass shares=N or
        all=true for the entire stake. Polls ~60s; returns settled + transaction_id.
  - get_agent_withdrawal_status — follow up on a withdrawal that returned settled=false (by
        transaction_id). Terminal: COMPLETED/CONFIRMED ok, FAILED/REJECTED/CANCELLED not.
  - withdraw_to_wallet          — pay USDC out of the Main Account to the user's REGISTERED wallet
        (never a third party). Synchronous and on-chain: returns transaction_hash immediately.

  KEY RULES for withdrawals:
  - shares vs USDC: withdraw_from_agent is denominated in SHARES; convert with sharePrice first.
  - Large agent withdrawals may go WAITING_FOR_APPROVAL (manual admin review, can take HOURS).
    That is normal — keep polling get_agent_withdrawal_status; do not resubmit.
  - Only ONE pending withdrawal per agent at a time; a second is rejected while one is in flight.
  - queued=true means the vault's funds are tied up in open positions; Envy executes later.
  - Agent withdrawal proceeds land in the MAIN ACCOUNT (verify with get_balances), not the wallet —
    chain withdraw_to_wallet after it settles to fully cash out.
  - LEDGER LAG: after a withdrawal COMPLETES, main_account_usdc may stay 0 for a few minutes while
    the credit is swept into the ledger. Poll get_balances before calling withdraw_to_wallet.

MARKET DATA (no auth needed):
- get_markets       — list available trading pairs with prices.
- health            — check API status.

WORKFLOW EXAMPLE — Open a LONG ETH position:
1. Authenticate (steps 1-6 above).
2. Call get_markets to check current ETH price.
3. Call get_positions to check available collateral.
4. Call create_order with symbol=PERP_ETH_USDC, order_type=MARKET, side=BUY, order_quantity=0.005.
5. Call get_positions to confirm the position was opened.
6. Optionally: set_position_tp_sl with take_profit_price and stop_loss_price to protect the position.

To close a position: use create_order with the OPPOSITE side and reduce_only=true.`),
	)

	for _, t := range tools.RegisterAuthTools(svc) {
		s.AddTool(t.Tool, t.Handler)
	}
	for _, t := range tools.RegisterPerptoolsTools(svc) {
		s.AddTool(t.Tool, t.Handler)
	}
	for _, t := range tools.RegisterOrderlyTools(svc) {
		s.AddTool(t.Tool, t.Handler)
	}
	for _, t := range tools.RegisterAIAgentTools(svc) {
		s.AddTool(t.Tool, t.Handler)
	}
	for _, t := range tools.RegisterMainDepositTools(svc) {
		s.AddTool(t.Tool, t.Handler)
	}
	for _, t := range tools.RegisterAgentStatsTools(svc) {
		s.AddTool(t.Tool, t.Handler)
	}
	for _, t := range tools.RegisterAIWithdrawalTools(svc) {
		s.AddTool(t.Tool, t.Handler)
	}
	for _, t := range tools.RegisterAgentChatTools(svc) {
		s.AddTool(t.Tool, t.Handler)
	}

	transport := strings.ToLower(envOrDefault("TRANSPORT", "sse"))

	switch transport {
	case "sse":
		addr := envOrDefault("ADDR", ":8080")
		basePath := envOrDefault("BASE_PATH", "/mcp")
		baseURL := envOrDefault("BASE_URL", "")

		opts := []server.SSEOption{
			server.WithStaticBasePath(basePath),
			server.WithKeepAliveInterval(30 * time.Second),
		}
		if baseURL != "" {
			opts = append(opts, server.WithBaseURL(baseURL))
		}

		sseServer := server.NewSSEServer(s, opts...)
		log.Printf("Starting SSE server on %s (base path: %s)", addr, basePath)
		if err := sseServer.Start(addr); err != nil {
			log.Fatalf("SSE server error: %v", err)
		}

	case "stdio":
		if err := server.ServeStdio(s); err != nil {
			fmt.Fprintf(os.Stderr, "mcp server error: %v\n", err)
			os.Exit(1)
		}

	default:
		log.Fatalf("Unknown transport: %s (supported: sse, stdio)", transport)
	}
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
