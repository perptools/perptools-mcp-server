package tools

import (
	"context"
	"time"

	"mcp-server/app/internal/service"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const (
	chatDefaultTimeoutSec = 60
	chatMaxTimeoutSec     = 120
	chatMaxHistoryLimit   = 100
)

// RegisterAgentChatTools exposes the conversational side of AI trading
// agents: talk to an agent's terminal and read the persisted chat history.
// Both tools wrap short-lived websocket sessions to the backend's agent-chat
// relay — each call opens, authenticates, works, and closes its own session.
func RegisterAgentChatTools(svc *service.Service) []ToolDef {
	return []ToolDef{
		{
			Tool: mcp.NewTool("send_agent_message",
				mcp.WithDescription(`Send a command to an AI trading agent's terminal and wait for its reply. Requires authentication. The agent terminal is COMMAND-BASED, not free-form chat — free text gets "Unknown command" back. Verified command set (send "help" to re-check):
- strategy [coin]        — describe the agent's current strategy (read-only)
- positions [coin]       — the agent's positions, e.g. "pos eth" (read-only)
- backtest [days] [coin] — run a backtest, e.g. "bt 30" or "backtest 7d eth" (read-only)
- report                 — queue a PDF report (read-only)
- help                   — list available commands
CONTROL commands that CHANGE the agent's live behaviour — use only when the user explicitly asks:
- add/remove <coin>, alloc <coin> <pct>  — change the traded coins / allocations
- risk <level|leverage>                  — e.g. "risk low", "risk 5x"
- close <coin>, stop, resume             — close a position / emergency-stop / restart trading

BEHAVIOUR: opens a short-lived websocket session to the agent's terminal, sends the command, and HOLDS the session open until the agent finishes replying (replies can take TENS OF SECONDS and may arrive in several chunks — they are concatenated for you), then closes. Be patient; do not re-send while a call is in flight.

Returns:
- reply           — the agent's full reply text (null if it timed out)
- echoed_command  — the server's echo confirming the agent received your command
- timed_out       — true when no reply arrived within timeout_seconds
- frames_received — diagnostic frame count for the session

IF timed_out=true: the agent may still answer later, but ONLY a live session captures it — retry send_agent_message, or check get_agent_chat_history shortly after to see whether a reply was persisted.

The exchange (your command + the reply) is persisted server-side. NEXT STEPS: get_agent_chat_history to review past exchanges; get_agent_details / get_agent_positions for structured numbers instead of terminal text.`),
				mcp.WithString("wallet_address", mcp.Required(), mcp.Description("User's Solana wallet address (base58). Must match the authenticated user.")),
				mcp.WithString("agent_id", mcp.Required(), mcp.Description("Agent's ID (the 'id' from list_agents, get_my_portfolio, or create_agent)")),
				mcp.WithString("message", mcp.Required(), mcp.Description(`The terminal command to send (e.g. "strategy", "positions", "backtest 7d", "help"). Free-form questions are rejected with "Unknown command".`)),
				mcp.WithNumber("timeout_seconds", mcp.Description("How long to wait for the reply (default 60, max 120). Agent replies routinely take 10-60s.")),
			),
			Handler: sendAgentMessage(svc),
		},
		{
			Tool: mcp.NewTool("get_agent_chat_history",
				mcp.WithDescription(`Fetch the persisted chat history between the user and an AI trading agent. Requires authentication. Use it to review past conversation, or to check whether a reply landed after a send_agent_message timed out.

Without 'before' it returns the NEWEST messages (up to ~10). To page further back, pass the returned 'cursor' as 'before' on the next call (and optionally 'limit', max 100).

Returns:
- messages[] — {message_id, text, is_from_bot, timestamp}; is_from_bot=true marks the agent's lines
- cursor     — pass as 'before' to fetch the previous (older) page
- has_more   — whether older messages exist

History is stored server-side and survives across sessions. An empty list just means this wallet has never chatted with that agent. NEXT STEP: send_agent_message to continue the conversation.`),
				mcp.WithString("wallet_address", mcp.Required(), mcp.Description("User's Solana wallet address (base58). Must match the authenticated user.")),
				mcp.WithString("agent_id", mcp.Required(), mcp.Description("Agent's ID (the 'id' from list_agents, get_my_portfolio, or create_agent)")),
				mcp.WithString("before", mcp.Description("Pagination cursor — a message_id from a previous page (use the returned 'cursor'). Omit for the newest messages.")),
				mcp.WithNumber("limit", mcp.Description("Messages per page when paginating with 'before' (default 10, max 100)")),
			),
			Handler: getAgentChatHistory(svc),
		},
	}
}

func sendAgentMessage(svc *service.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		wallet, err := req.RequireString("wallet_address")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		agentID, err := req.RequireString("agent_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		message, err := req.RequireString("message")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		timeoutSec := optNumber(req, "timeout_seconds", chatDefaultTimeoutSec)
		if timeoutSec < 1 {
			timeoutSec = chatDefaultTimeoutSec
		}
		if timeoutSec > chatMaxTimeoutSec {
			timeoutSec = chatMaxTimeoutSec
		}

		reply, err := svc.SendAgentMessage(ctx, wallet, agentID, message, time.Duration(timeoutSec)*time.Second)
		if err != nil {
			return mcp.NewToolResultError(formatAuthError("send agent message", err)), nil
		}
		out := map[string]any{
			"reply":           reply.Reply,
			"echoed_command":  reply.EchoedCommand,
			"timed_out":       reply.TimedOut,
			"frames_received": reply.FramesReceived,
		}
		if reply.TimedOut {
			out["reply"] = nil
			out["guidance"] = "No reply arrived within the timeout. The agent may still reply later, but only a live session captures it — retry send_agent_message (possibly with a larger timeout_seconds), or call get_agent_chat_history shortly to see if a reply was persisted."
		}
		return jsonResult(out)
	}
}

func getAgentChatHistory(svc *service.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		wallet, err := req.RequireString("wallet_address")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		agentID, err := req.RequireString("agent_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		limit := int(optNumber(req, "limit", 10))
		if limit < 1 {
			limit = 10
		}
		if limit > chatMaxHistoryLimit {
			limit = chatMaxHistoryLimit
		}

		hist, err := svc.AgentChatHistory(ctx, wallet, agentID, optString(req, "before"), limit)
		if err != nil {
			return mcp.NewToolResultError(formatAuthError("get agent chat history", err)), nil
		}
		return jsonResult(hist)
	}
}
