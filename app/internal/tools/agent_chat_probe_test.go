package tools_test

import (
	"os"
	"testing"
)

// TestAgentChatHelpProbe is a manual probe of the agent terminal's command
// set: it sends "help" and logs the reply. Gated behind CHAT_PROBE=1 so it
// does not run with the normal suite.
func TestAgentChatHelpProbe(t *testing.T) {
	if os.Getenv("CHAT_PROBE") != "1" {
		t.Skip("set CHAT_PROBE=1 to run the help probe")
	}
	env := setupAndAuth(t)
	registerChatTools(env)

	resp := callTool(t, env.ctx, env.toolMap, "send_agent_message", map[string]any{
		"wallet_address":  env.walletAddress,
		"agent_id":        testChatAgentID,
		"message":         "help",
		"timeout_seconds": float64(90),
	})
	t.Logf("help reply:\n%s", resp)
}
