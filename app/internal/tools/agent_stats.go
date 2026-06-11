package tools

import (
	"context"
	"fmt"

	"mcp-server/app/internal/service"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// arenaSortKeys are the sortBy values the arena leaderboard accepts (verified
// live, including pnl30d). The backend sorts ASCENDING by default; the client
// pins sortByDesc=sortBy so results always come back best-first.
var arenaSortKeys = map[string]bool{
	"aum":    true,
	"pnl1h":  true,
	"pnl24h": true,
	"pnl7d":  true,
	"pnl30d": true,
}

const maxArenaPageSize = 50

// RegisterAgentStatsTools exposes the read-only discover-and-track side of AI
// trading agents: browse the public leaderboard, inspect one agent, and check
// the user's own stake and portfolio. All read-only — nothing here moves funds.
func RegisterAgentStatsTools(svc *service.Service) []ToolDef {
	return []ToolDef{
		{
			Tool: mcp.NewTool("list_agents",
				mcp.WithDescription(`Browse the public AI trading agent leaderboard (the "arena"). Requires authentication. This is the DISCOVERY entry point: call it when the user wants to find an agent to invest in, compare agents, or see what is performing well.

Returns one page of agents sorted BEST-FIRST by sort_by, each with:
- id (the agent_id used by every other agent tool), name, riskLevel (Low/Medium/High), type
- aum               — assets under management in USDC (vault size; bigger = more invested)
- pnlPercentage1h / 24h / 7d / 30d, totalPnlPercentage — performance in percent over each window
- totalPnl          — lifetime profit in USDC
- activePositions, walletCount (investor count), hasFreshMetrics
plus pagination {currentPage, pageSize, totalPages, totalItems}.

How to read the numbers: positive pnlPercentage24h means the vault gained over the last day. A high pnl on a tiny aum is less meaningful than a moderate pnl on a large aum. riskLevel reflects the agent's configured aggressiveness, not realized risk.

By default chartData and latestReport are STRIPPED to keep the response small — set include_chart_data=true only if you actually need the time series.

NEXT STEPS: get_agent_details(agent_id) for a deep dive on a candidate; get_agent_positions(agent_id) to see its trades; deposit_to_agent(agent_id, amount) to invest (Main Account must be funded — see get_balances).`),
				mcp.WithString("wallet_address", mcp.Required(), mcp.Description("User's Solana wallet address (base58). Must match the authenticated user — the request is routed through the authenticated proxy.")),
				mcp.WithString("sort_by", mcp.Description(`Leaderboard sort key, best-first. One of "aum", "pnl1h", "pnl24h", "pnl7d", "pnl30d". Default "pnl24h".`)),
				mcp.WithNumber("page", mcp.Description("Page number, 1-based (default 1)")),
				mcp.WithNumber("page_size", mcp.Description("Agents per page (default 10, max 50)")),
				mcp.WithBoolean("include_chart_data", mcp.Description("Include per-agent chartData and latestReport (large; default false)")),
			),
			Handler: listAgents(svc),
		},
		{
			Tool: mcp.NewTool("get_agent_details",
				mcp.WithDescription(`Deep dive on ONE AI trading agent by agent_id. Requires authentication. Use it after list_agents to evaluate a candidate before depositing, or to review an agent the user already holds.

Returns:
- agent     — profile: name, description, riskLevel (Low/Medium/High), coinsToTrade, strategyDescription, pnlPercentage1h/24h/7d/30d, totalPnlPercentage
- vault     — vaultAddress, totalAssets (= AUM in USDC), totalShares, sharePrice, investorCount, status
- userShares — YOUR stake seen from wallet_address: shares, sharePrice, balance (current USD value), percentageOfVault (zeros if you hold nothing)
- analytics — closedPositionsStats {wins, losses, positionsClosed, avgPnlPercent, avgWinPnlPercent, profitFactor} and openPositionsStats {positionsOpen, avgPnlPercent, totalMargin, totalPnlUsd}
- latestReport — the agent's most recent published report file, if any

How to read it: profitFactor > 1 means closed trades made more than they lost; wins/(wins+losses) is the win rate; sharePrice > 1 means the vault has grown since inception.

The aumChart and pnlChart time series are STRIPPED by default (they are large) — set include_charts=true only if you need them.

NEXT STEPS: get_agent_positions(agent_id) for the individual trades; deposit_to_agent(agent_id, amount) to invest; get_my_agent_stats to track an existing stake.`),
				mcp.WithString("wallet_address", mcp.Required(), mcp.Description("User's Solana wallet address (base58). Must match the authenticated user; userShares is computed for this wallet.")),
				mcp.WithString("agent_id", mcp.Required(), mcp.Description("Agent's ID (the 'id' from list_agents, or the agent_id from create_agent)")),
				mcp.WithBoolean("include_charts", mcp.Description("Include the aumChart and pnlChart time series (large; default false)")),
			),
			Handler: getAgentDetails(svc),
		},
		{
			Tool: mcp.NewTool("get_agent_positions",
				mcp.WithDescription(`List an AI agent's trading positions (its actual trades), paginated. Requires authentication. Use it to inspect HOW an agent trades before investing, or to monitor what an agent the user holds is currently doing.

Each position has: type (long/short), token, totalUsd (position size), amount, entryPrice, price (current/exit), pnlPercent, leverage, stopLoss, takeProfit, status, active, age, live. Plus pagination {total, page, limit, totalPages}.

How to read it: pnlPercent is the position's own return (positive = in profit). active/live positions are currently open; the rest are history. An empty list just means the agent has not traded (yet) — common for newly funded agents.

Use type="open" to see only currently open positions. NEXT STEPS: get_agent_details(agent_id) for aggregate win/loss stats; deposit_to_agent to invest.`),
				mcp.WithString("wallet_address", mcp.Required(), mcp.Description("User's Solana wallet address (base58). Must match the authenticated user.")),
				mcp.WithString("agent_id", mcp.Required(), mcp.Description("Agent's ID (the 'id' from list_agents)")),
				mcp.WithString("type", mcp.Description(`Optional status filter, e.g. "open" for currently open positions only. Omit for all.`)),
				mcp.WithNumber("page", mcp.Description("Page number, 1-based (default 1)")),
				mcp.WithNumber("limit", mcp.Description("Positions per page (default 10)")),
			),
			Handler: getAgentPositions(svc),
		},
		{
			Tool: mcp.NewTool("get_my_agent_stats",
				mcp.WithDescription(`Show the user's OWN stake in one specific agent vault. Requires authentication. Call it after a deposit_to_agent settles to confirm the shares were minted, or any time the user asks "how is my investment in agent X doing?".

Returns:
- userShares — shares held, sharePrice, balance (current USD value of the stake), percentageOfVault, lastUpdated
- vault      — the vault aggregate: totalShares, totalAssets (AUM), sharePrice, investorCount

How to read it: balance ≈ shares * sharePrice is what the stake is worth NOW; compare it to what was deposited to see the return. percentageOfVault is the user's slice of the whole vault. All zeros means this wallet holds no shares in that agent.

For the whole picture across ALL agents use get_my_portfolio instead. For the agent's own performance (not your stake) use get_agent_details.`),
				mcp.WithString("wallet_address", mcp.Required(), mcp.Description("User's Solana wallet address (base58). Must match the authenticated user.")),
				mcp.WithString("agent_id", mcp.Required(), mcp.Description("Agent's ID (from list_agents or create_agent)")),
			),
			Handler: getMyAgentStats(svc),
		},
		{
			Tool: mcp.NewTool("get_my_portfolio",
				mcp.WithDescription(`One-call summary of the user's ENTIRE AI-agent portfolio. Requires authentication. Call it to answer "what do I hold?", after deposits to verify everything landed, or as the starting point of a portfolio review.

Returns:
- usdc_balance          — spendable Main Account USDC (what deposit_to_agent draws from; NOT the on-chain wallet — see get_balances for that)
- total_portfolio_value — Main Account balance plus the current value of all vault shares
- my_agent_count / active_agent_count
- top_performer         — the user's best agent, when reported
- agents[]              — every agent the wallet holds shares in: agent {id, name, ...}, shares, sharePrice, balance (current USD value of the stake), percentageOfVault, pnlPercentage1h/24h/7d/30d, totalPnlPercentage

How to read it: each agents[].balance is what that stake is worth now; the pnl percentages are the agent's performance over each window. An empty agents list with a positive usdc_balance means money is parked in the Main Account but not deployed into any agent.

NEXT STEPS: get_my_agent_stats(agent_id) or get_agent_details(agent_id) to drill into one holding; list_agents to find somewhere to deploy idle usdc_balance; deposit_to_agent to invest it.`),
				mcp.WithString("wallet_address", mcp.Required(), mcp.Description("User's Solana wallet address (base58). Must match the authenticated user.")),
			),
			Handler: getMyPortfolio(svc),
		},
	}
}

func listAgents(svc *service.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		wallet, err := req.RequireString("wallet_address")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		sortBy := optString(req, "sort_by")
		if sortBy == "" {
			sortBy = "pnl24h"
		}
		if !arenaSortKeys[sortBy] {
			return mcp.NewToolResultError(fmt.Sprintf(`invalid sort_by %q — must be one of "aum", "pnl1h", "pnl24h", "pnl7d", "pnl30d"`, sortBy)), nil
		}
		page := int(optNumber(req, "page", 1))
		if page < 1 {
			page = 1
		}
		pageSize := int(optNumber(req, "page_size", 10))
		if pageSize < 1 {
			pageSize = 10
		}
		if pageSize > maxArenaPageSize {
			pageSize = maxArenaPageSize
		}

		resp, err := svc.ListArenaAgents(ctx, wallet, sortBy, page, pageSize)
		if err != nil {
			return mcp.NewToolResultError(formatAuthError("list agents", err)), nil
		}
		if !optBool(req, "include_chart_data", false) {
			for i := range resp.Bots {
				resp.Bots[i].ChartData = nil
				resp.Bots[i].LatestReport = nil
			}
		}
		return jsonResult(resp)
	}
}

func getAgentDetails(svc *service.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		wallet, err := req.RequireString("wallet_address")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		agentID, err := req.RequireString("agent_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		resp, err := svc.AgentDetails(ctx, wallet, agentID)
		if err != nil {
			return mcp.NewToolResultError(formatAuthError("get agent details", err)), nil
		}
		if !optBool(req, "include_charts", false) {
			resp.Analytics.AumChart = nil
			resp.Analytics.PnlChart = nil
		}
		return jsonResult(resp)
	}
}

func getAgentPositions(svc *service.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		wallet, err := req.RequireString("wallet_address")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		agentID, err := req.RequireString("agent_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		page := int(optNumber(req, "page", 1))
		if page < 1 {
			page = 1
		}
		limit := int(optNumber(req, "limit", 10))
		if limit < 1 {
			limit = 10
		}

		resp, err := svc.AgentPositions(ctx, wallet, agentID, optString(req, "type"), page, limit)
		if err != nil {
			return mcp.NewToolResultError(formatAuthError("get agent positions", err)), nil
		}
		return jsonResult(resp)
	}
}

func getMyAgentStats(svc *service.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		wallet, err := req.RequireString("wallet_address")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		agentID, err := req.RequireString("agent_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		resp, err := svc.MyAgentStats(ctx, wallet, agentID)
		if err != nil {
			return mcp.NewToolResultError(formatAuthError("get my agent stats", err)), nil
		}
		return jsonResult(resp)
	}
}

func getMyPortfolio(svc *service.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		wallet, err := req.RequireString("wallet_address")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		resp, err := svc.MyPortfolio(ctx, wallet)
		if err != nil {
			return mcp.NewToolResultError(formatAuthError("get my portfolio", err)), nil
		}
		return jsonResult(resp)
	}
}
