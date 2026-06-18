package tools_test

import (
	"testing"
)

// TestEmpiricalAgentStatus checks the agent withdrawal status + current balances.
// Re-run as needed while the async withdrawal settles.
// WITHDRAW_TX_ID env overrides the transaction id.
//
// Run: go test ./app/internal/tools/ -run TestEmpiricalAgentStatus -v
func TestEmpiricalAgentStatus(t *testing.T) {
	env := setupAndAuth(t)
	txID := envOr("WITHDRAW_TX_ID", "fa8010f2-ddbe-46c1-baa5-4271c70072d4")

	st, _ := callToolSoft(t, env.ctx, env.toolMap, "get_agent_withdrawal_status", map[string]any{
		"wallet_address": env.walletAddress,
		"transaction_id": txID,
	})
	t.Logf("withdrawal status:\n%s", st)

	bal, _ := callToolSoft(t, env.ctx, env.toolMap, "get_balances", map[string]any{
		"wallet_address": env.walletAddress,
	})
	t.Logf("balances:\n%s", bal)
}
