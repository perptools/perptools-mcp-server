package tools_test

import (
	"encoding/json"
	"testing"

	"mcp-server/app/internal/clients/perptools"
)

// TestAgentTransactionsProbe is the non-destructive STEP-0 check for an agent
// transactions LIST endpoint — the recovery path for a transaction_id lost to
// a client-side timeout (a withdrawal exists upstream, but the caller never
// received the ack). Read-only: no funds can move.
//
// Candidates, in order of likelihood:
//  1. /api/sdk/agent/transactions (auth 0) — defined in jupbot-perps-api's
//     envy client (GetAgentTransactionsFromEnvy), but the /api/sdk/* namespace
//     404'd for withdraw/solana after Envy's migration to /api/v1/*.
//  2. /api/v1/agents/transactions (auth 0) — the migrated-path guess.
//  3. /api/data/transactions (auth 1, GET) — in the proxy's API-key allowlist
//     next to /api/data/arena.
//
// Run with: go test ./app/internal/tools/ -run TestAgentTransactionsProbe -v
func TestAgentTransactionsProbe(t *testing.T) {
	env := setupAndAuth(t)
	pk := env.walletAddress
	agentID := envOr("AGENT_ID", "1b5d9ef1-562e-41fe-be09-ee2002550e6b")

	probes := []struct {
		label    string
		envyPath string
		method   string
		authType int
		body     map[string]any
	}{
		{
			label:    "sdk list (botId)",
			envyPath: "/api/sdk/agent/transactions",
			method:   "POST",
			body:     map[string]any{"walletAddress": pk, "botId": agentID, "page": 1, "limit": 10},
		},
		{
			label:    "v1 list (agentId)",
			envyPath: "/api/v1/agents/transactions",
			method:   "POST",
			body:     map[string]any{"walletAddress": pk, "agentId": agentID, "page": 1, "limit": 10},
		},
		{
			label:    "v1 list (no agent)",
			envyPath: "/api/v1/agents/transactions",
			method:   "POST",
			body:     map[string]any{"walletAddress": pk, "page": 1, "limit": 10},
		},
		{
			label:    "data list (api-key GET)",
			envyPath: "/api/data/transactions?walletAddress=" + pk + "&agentId=" + agentID + "&page=1&limit=10",
			method:   "GET",
			authType: 1,
			body:     map[string]any{},
		},
	}

	for _, p := range probes {
		envelope := map[string]any{
			"public_key": pk,
			"path":       p.envyPath,
			"method":     p.method,
			"auth_type":  p.authType,
			"body":       p.body,
		}
		code, body, err := env.svc.ProbeOrderlyPost(env.ctx, "/v1/ai/proxy", envelope)
		if err != nil {
			t.Errorf("PROBE %s: transport error: %v", p.label, err)
			continue
		}
		t.Logf("PROBE %-28s %-50s [%d] %s", p.label, p.envyPath, code, trunc(body, 1500))
	}
}

// TestGetAgentTransactionsLive exercises the get_agent_transactions tool
// against PROD (read-only). The test agent has known history (deposits and a
// completed withdrawal), so the list must be non-empty with usable ids.
//
// Run with: go test ./app/internal/tools/ -run TestGetAgentTransactionsLive -v
func TestGetAgentTransactionsLive(t *testing.T) {
	env := setupAndAuth(t)
	agentID := envOr("AGENT_ID", "1b5d9ef1-562e-41fe-be09-ee2002550e6b")

	text, isErr := callToolSoft(t, env.ctx, env.toolMap, "get_agent_transactions", map[string]any{
		"wallet_address": env.walletAddress,
		"agent_id":       agentID,
	})
	if isErr {
		t.Fatalf("get_agent_transactions rejected:\n%s", text)
	}

	var resp perptools.AgentTransactionsResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("response is not the expected JSON: %v\n%s", err, text)
	}
	if !resp.Success {
		t.Fatalf("success=false: %s", text)
	}
	if len(resp.Transactions) == 0 {
		t.Fatalf("expected at least one transaction for the test agent, got none:\n%s", text)
	}
	for _, tx := range resp.Transactions {
		if tx.ID == "" || (tx.Type != "DEPOSIT" && tx.Type != "WITHDRAWAL") {
			t.Errorf("malformed transaction entry: %+v", tx)
		}
		t.Logf("tx %s  %-10s  %-16s  amount=%.4f shares=%.4f  %s", tx.ID, tx.Type, tx.Status, tx.Amount, tx.Shares, tx.CreatedAt)
	}
}
