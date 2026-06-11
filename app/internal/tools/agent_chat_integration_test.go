package tools_test

import (
	"encoding/json"
	"strings"
	"testing"

	"mcp-server/app/internal/clients/perptools"
	"mcp-server/app/internal/tools"
)

// testChatAgentID is our test agent (Low risk).
const testChatAgentID = "1b5d9ef1-562e-41fe-be09-ee2002550e6b"

// registerChatTools adds the agent-chat tools to the shared test env
// (setupAndAuth predates them).
func registerChatTools(env *testEnv) {
	for _, td := range tools.RegisterAgentChatTools(env.svc) {
		env.toolMap[td.Tool.Name] = td.Handler
	}
}

// TestAgentChat is the live protocol probe + end-to-end check:
//  1. send_agent_message to the test agent and assert a non-empty reply
//     (generous 90s timeout — replies routinely take tens of seconds);
//  2. get_agent_chat_history and assert the exchange was persisted.
func TestAgentChat(t *testing.T) {
	env := setupAndAuth(t)
	registerChatTools(env)

	// Mirror every raw websocket frame into the test log — this is the live
	// protocol probe (auth handshake, greeting replay, echo, output shapes).
	env.ctx = perptools.WithChatFrameLogger(env.ctx, func(dir, frame string) {
		if len(frame) > 2000 {
			frame = frame[:2000] + "...(truncated)"
		}
		t.Logf("ws %s %s", dir, frame)
	})

	// "strategy" is a real terminal command (verified via the help probe) —
	// free-form text would only get an "Unknown command" reply back.
	question := "strategy"

	t.Logf("send_agent_message -> agent %s: %q", testChatAgentID, question)
	sendResp := callTool(t, env.ctx, env.toolMap, "send_agent_message", map[string]any{
		"wallet_address":  env.walletAddress,
		"agent_id":        testChatAgentID,
		"message":         question,
		"timeout_seconds": float64(90),
	})
	t.Logf("send_agent_message response:\n%s", sendResp)

	var send struct {
		Reply          *string `json:"reply"`
		EchoedCommand  string  `json:"echoed_command"`
		TimedOut       bool    `json:"timed_out"`
		FramesReceived int     `json:"frames_received"`
	}
	mustUnmarshal(t, sendResp, &send)

	if send.TimedOut {
		t.Fatalf("send_agent_message timed out after 90s (frames_received=%d, echoed_command=%q)",
			send.FramesReceived, send.EchoedCommand)
	}
	if send.Reply == nil || strings.TrimSpace(*send.Reply) == "" {
		t.Fatalf("expected a non-empty reply, got %v", send.Reply)
	}
	t.Logf("agent reply (%d chars): %s", len(*send.Reply), *send.Reply)

	// --- history: the exchange must be persisted ---
	t.Log("get_agent_chat_history — verifying persistence")
	histResp := callTool(t, env.ctx, env.toolMap, "get_agent_chat_history", map[string]any{
		"wallet_address": env.walletAddress,
		"agent_id":       testChatAgentID,
	})
	t.Logf("get_agent_chat_history response:\n%s", histResp)

	var hist struct {
		Messages []struct {
			MessageID string `json:"message_id"`
			Text      string `json:"text"`
			IsFromBot bool   `json:"is_from_bot"`
			Timestamp string `json:"timestamp"`
		} `json:"messages"`
		Cursor  string `json:"cursor"`
		HasMore bool   `json:"has_more"`
	}
	mustUnmarshal(t, histResp, &hist)

	if len(hist.Messages) == 0 {
		t.Fatal("expected chat history to contain messages after the exchange")
	}
	var sawQuestion, sawBotReply bool
	for _, m := range hist.Messages {
		if !m.IsFromBot && strings.Contains(m.Text, question) {
			sawQuestion = true
		}
		if m.IsFromBot && strings.TrimSpace(m.Text) != "" {
			sawBotReply = true
		}
	}
	if !sawQuestion {
		t.Errorf("our question %q not found in history (it may be paginated out)", question)
	}
	if !sawBotReply {
		t.Error("no bot message found in history")
	}

	// --- pagination smoke: page older via load_more. The tool only returns a
	// cursor when the initial batch is full, so fall back to the oldest
	// message id to exercise the load_more path either way.
	cursor := hist.Cursor
	if cursor == "" && len(hist.Messages) > 0 {
		cursor = hist.Messages[0].MessageID
	}
	if cursor != "" {
		t.Logf("get_agent_chat_history — paging with before=%s", cursor)
		pageResp, isErr := callToolSoft(t, env.ctx, env.toolMap, "get_agent_chat_history", map[string]any{
			"wallet_address": env.walletAddress,
			"agent_id":       testChatAgentID,
			"before":         cursor,
			"limit":          float64(20),
		})
		t.Logf("paged history (isErr=%v):\n%s", isErr, pageResp)
	}
}

// TestAgentChatHistoryOnly is a cheaper standalone check of the history path
// (no message sent).
func TestAgentChatHistoryOnly(t *testing.T) {
	env := setupAndAuth(t)
	registerChatTools(env)

	histResp := callTool(t, env.ctx, env.toolMap, "get_agent_chat_history", map[string]any{
		"wallet_address": env.walletAddress,
		"agent_id":       testChatAgentID,
	})
	t.Logf("history:\n%s", histResp)

	var hist map[string]any
	if err := json.Unmarshal([]byte(histResp), &hist); err != nil {
		t.Fatalf("history response is not JSON: %v", err)
	}
}
