package tools_test

import (
	"testing"
)

// TestEmpiricalStep1WithdrawAgent starts the real withdrawal of the entire
// agent stake (10 shares ~= $11) back to the Main Account. Server-signed; no
// local signature needed. Settlement is async (can queue / await approval), so
// this only kicks it off and reports the ack + first status.
//
// Run: go test ./app/internal/tools/ -run TestEmpiricalStep1WithdrawAgent -v
func TestEmpiricalStep1WithdrawAgent(t *testing.T) {
	env := setupAndAuth(t)
	agentID := envOr("AGENT_ID", "1b5d9ef1-562e-41fe-be09-ee2002550e6b")

	text, isErr := callToolSoft(t, env.ctx, env.toolMap, "withdraw_from_agent", map[string]any{
		"wallet_address": env.walletAddress,
		"agent_id":       agentID,
		"all":            true,
	})
	if isErr {
		t.Fatalf("withdraw_from_agent rejected:\n%s", text)
	}
	t.Logf("withdraw_from_agent ack:\n%s", text)
}
