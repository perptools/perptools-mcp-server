package tools_test

import (
	"encoding/json"
	"testing"
)

// TestAgentStatsTools exercises the read-only agent-stats tools live against
// prod: the arena leaderboard for every supported sort key, details+positions
// for a known agent, and the authed user's own stake + portfolio. All reads —
// no funds move.
func TestAgentStatsTools(t *testing.T) {
	env := setupAndAuth(t)
	agentID := envOr("AGENT_ID", "1b5d9ef1-562e-41fe-be09-ee2002550e6b")

	// --- list_agents: every supported sort key returns a non-empty page ---
	type arenaBotLite struct {
		ID     string  `json:"id"`
		Name   string  `json:"name"`
		AUM    float64 `json:"aum"`
		Pnl24h float64 `json:"pnlPercentage24h"`
	}
	type arenaLite struct {
		Success    bool           `json:"success"`
		Bots       []arenaBotLite `json:"bots"`
		Pagination *struct {
			CurrentPage int `json:"currentPage"`
			PageSize    int `json:"pageSize"`
		} `json:"pagination"`
	}

	for _, sortBy := range []string{"aum", "pnl1h", "pnl24h", "pnl7d", "pnl30d"} {
		t.Logf("list_agents sort_by=%s", sortBy)
		resp := callTool(t, env.ctx, env.toolMap, "list_agents", map[string]any{
			"wallet_address": env.walletAddress,
			"sort_by":        sortBy,
			"page":           1,
			"page_size":      5,
		})
		var out arenaLite
		mustUnmarshal(t, resp, &out)
		if !out.Success {
			t.Fatalf("list_agents sort_by=%s: success=false: %s", sortBy, trunc(resp, 300))
		}
		if len(out.Bots) == 0 {
			t.Fatalf("list_agents sort_by=%s: empty bots", sortBy)
		}
		// chartData/latestReport must be stripped by default
		if json.Valid([]byte(resp)) {
			var raw map[string]any
			mustUnmarshal(t, resp, &raw)
			if bots, ok := raw["bots"].([]any); ok && len(bots) > 0 {
				if b, ok := bots[0].(map[string]any); ok {
					if _, has := b["chartData"]; has {
						t.Fatalf("list_agents: chartData not stripped by default")
					}
				}
			}
		}
		// ordering must be descending (best first) for the numerically checkable keys
		switch sortBy {
		case "aum":
			for i := 1; i < len(out.Bots); i++ {
				if out.Bots[i].AUM > out.Bots[i-1].AUM {
					t.Fatalf("list_agents sort_by=aum: not descending: %v > %v at %d", out.Bots[i].AUM, out.Bots[i-1].AUM, i)
				}
			}
		case "pnl24h":
			for i := 1; i < len(out.Bots); i++ {
				if out.Bots[i].Pnl24h > out.Bots[i-1].Pnl24h {
					t.Fatalf("list_agents sort_by=pnl24h: not descending: %v > %v at %d", out.Bots[i].Pnl24h, out.Bots[i-1].Pnl24h, i)
				}
			}
		}
		t.Logf("  ok: %d bots, top=%s", len(out.Bots), out.Bots[0].Name)
	}

	// invalid sort key is rejected client-side
	if _, isErr := callToolSoft(t, env.ctx, env.toolMap, "list_agents", map[string]any{
		"wallet_address": env.walletAddress,
		"sort_by":        "bogus",
	}); !isErr {
		t.Fatal("list_agents accepted bogus sort_by")
	}

	// --- get_agent_details ---
	t.Logf("get_agent_details agent_id=%s", agentID)
	detResp := callTool(t, env.ctx, env.toolMap, "get_agent_details", map[string]any{
		"wallet_address": env.walletAddress,
		"agent_id":       agentID,
	})
	var det struct {
		Success bool `json:"success"`
		Agent   struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"agent"`
		Vault struct {
			TotalAssets float64 `json:"totalAssets"`
			SharePrice  float64 `json:"sharePrice"`
		} `json:"vault"`
		Analytics struct {
			AumChart []any `json:"aumChart"`
			PnlChart []any `json:"pnlChart"`
		} `json:"analytics"`
	}
	mustUnmarshal(t, detResp, &det)
	if !det.Success {
		t.Fatalf("get_agent_details: success=false: %s", trunc(detResp, 300))
	}
	if det.Agent.ID != agentID {
		t.Fatalf("get_agent_details: got agent id %q, want %q", det.Agent.ID, agentID)
	}
	if len(det.Analytics.AumChart) != 0 || len(det.Analytics.PnlChart) != 0 {
		t.Fatal("get_agent_details: charts not stripped by default")
	}
	t.Logf("  ok: %s aum=%.2f sharePrice=%.4f", det.Agent.Name, det.Vault.TotalAssets, det.Vault.SharePrice)

	// --- get_agent_positions ---
	t.Logf("get_agent_positions agent_id=%s", agentID)
	posResp := callTool(t, env.ctx, env.toolMap, "get_agent_positions", map[string]any{
		"wallet_address": env.walletAddress,
		"agent_id":       agentID,
		"page":           1,
		"limit":          5,
	})
	var pos struct {
		Success    bool  `json:"success"`
		Positions  []any `json:"positions"`
		Pagination struct {
			Total int `json:"total"`
			Page  int `json:"page"`
		} `json:"pagination"`
	}
	mustUnmarshal(t, posResp, &pos)
	if !pos.Success {
		t.Fatalf("get_agent_positions: success=false: %s", trunc(posResp, 300))
	}
	t.Logf("  ok: %d positions on page %d (total %d)", len(pos.Positions), pos.Pagination.Page, pos.Pagination.Total)

	// --- get_my_agent_stats (authed wallet's stake in the known agent) ---
	t.Logf("get_my_agent_stats agent_id=%s", agentID)
	statsResp := callTool(t, env.ctx, env.toolMap, "get_my_agent_stats", map[string]any{
		"wallet_address": env.walletAddress,
		"agent_id":       agentID,
	})
	var stats struct {
		Success    bool `json:"success"`
		UserShares struct {
			Shares     float64 `json:"shares"`
			SharePrice float64 `json:"sharePrice"`
			Balance    float64 `json:"balance"`
		} `json:"userShares"`
		Vault struct {
			TotalAssets   float64 `json:"totalAssets"`
			InvestorCount int     `json:"investorCount"`
		} `json:"vault"`
	}
	mustUnmarshal(t, statsResp, &stats)
	if !stats.Success {
		t.Fatalf("get_my_agent_stats: success=false: %s", trunc(statsResp, 300))
	}
	t.Logf("  ok: shares=%.4f balance=%.2f vaultAUM=%.2f investors=%d",
		stats.UserShares.Shares, stats.UserShares.Balance, stats.Vault.TotalAssets, stats.Vault.InvestorCount)

	// --- get_my_portfolio ---
	t.Log("get_my_portfolio")
	portResp := callTool(t, env.ctx, env.toolMap, "get_my_portfolio", map[string]any{
		"wallet_address": env.walletAddress,
	})
	var port struct {
		USDCBalance         float64 `json:"usdc_balance"`
		TotalPortfolioValue float64 `json:"total_portfolio_value"`
		MyAgentCount        int     `json:"my_agent_count"`
		Agents              []struct {
			Agent struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"agent"`
			Shares  float64 `json:"shares"`
			Balance float64 `json:"balance"`
		} `json:"agents"`
	}
	mustUnmarshal(t, portResp, &port)
	t.Logf("  ok: usdc=%.2f total=%.2f my_agents=%d holdings=%d",
		port.USDCBalance, port.TotalPortfolioValue, port.MyAgentCount, len(port.Agents))

	// the test wallet holds shares in the known agent (10 USDC deposited by the
	// full-deposit test) — its portfolio should not be empty, and pnlChart /
	// latestReport must be stripped.
	if len(port.Agents) == 0 {
		t.Fatal("get_my_portfolio: expected at least one agent holding for the test wallet")
	}
	var rawPort map[string]any
	mustUnmarshal(t, portResp, &rawPort)
	if agents, ok := rawPort["agents"].([]any); ok && len(agents) > 0 {
		if a, ok := agents[0].(map[string]any); ok {
			if _, has := a["pnlChart"]; has {
				t.Fatal("get_my_portfolio: pnlChart not stripped")
			}
			if _, has := a["latestReport"]; has {
				t.Fatal("get_my_portfolio: latestReport not stripped")
			}
		}
	}
}
