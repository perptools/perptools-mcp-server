package tools_test

import (
	"testing"
)

// TestAgentControlLive verifies the new lifecycle tools against PROD without
// destroying the test agent:
//   - stop_agent then start_agent on the real agent (reversible; 0 funds),
//   - delete_agent against a BOGUS botId (confirms the archive route + payload
//     shape without deleting the real agent — the backend reports not-found).
//
// Run: go test ./app/internal/tools/ -run TestAgentControlLive -v
func TestAgentControlLive(t *testing.T) {
	env := setupAndAuth(t)
	agentID := envOr("AGENT_ID", "1b5d9ef1-562e-41fe-be09-ee2002550e6b")

	stop, isErr := callToolSoft(t, env.ctx, env.toolMap, "stop_agent", map[string]any{
		"wallet_address": env.walletAddress,
		"agent_id":       agentID,
	})
	t.Logf("stop_agent (isErr=%v):\n%s", isErr, stop)

	start, isErr := callToolSoft(t, env.ctx, env.toolMap, "start_agent", map[string]any{
		"wallet_address": env.walletAddress,
		"agent_id":       agentID,
	})
	t.Logf("start_agent (isErr=%v):\n%s", isErr, start)

	// IRREVERSIBLE in real use — probe with a non-existent agent so nothing is deleted.
	del, isErr := callToolSoft(t, env.ctx, env.toolMap, "delete_agent", map[string]any{
		"wallet_address": env.walletAddress,
		"agent_id":       "00000000-0000-0000-0000-000000000000",
	})
	t.Logf("delete_agent [BOGUS id, expect not-found] (isErr=%v):\n%s", isErr, del)
}
