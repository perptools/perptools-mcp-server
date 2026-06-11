package tools_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"mcp-server/app/internal/clients/perptools"
	"mcp-server/app/internal/telemetry"

	"github.com/mark3labs/mcp-go/mcp"
)

// TestTelemetryEventReachesBackend posts one well-formed mcp_tool_called
// event through the SAME unsigned client+path the emitter uses, live against
// prod. Two outcomes prove the wiring reaches the app handler:
//   - nil error            -> 201, the backend allow-list already includes
//     mcp_tool_called (deployed);
//   - "unknown event type" -> 400 from the event handler itself, the type is
//     not deployed yet but the route, body shape and transport are correct.
//
// Anything else (401/404/5xx/timeout) is a wiring failure.
func TestTelemetryEventReachesBackend(t *testing.T) {
	if os.Getenv("SOLANA_PRIVATE_KEY") == "" {
		t.Skip("SOLANA_PRIVATE_KEY not set — skipping integration test")
	}

	client := perptools.NewClient(envOr("PERPTOOLS_BASE_URL", "https://app.perptools.ai/api"))

	data, err := json.Marshal(telemetry.EventData{
		Tool:       "health",
		Success:    true,
		DurationMS: 1,
	})
	if err != nil {
		t.Fatalf("marshal event data: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = client.CreateEvent(ctx, perptools.CreateEventRequest{
		PublicKey: telemetry.SentinelPublicKey,
		Type:      telemetry.EventType,
		Data:      data,
	})
	switch {
	case err == nil:
		t.Log("event accepted (201) — backend allow-list includes mcp_tool_called")
	case strings.Contains(err.Error(), "unknown event type"):
		t.Logf("backend handler reached, type not deployed yet (expected pre-deploy): %v", err)
	default:
		t.Fatalf("event did not reach the backend event handler: %v", err)
	}
}

// TestTelemetryMiddlewarePassthroughLive wraps real tool handlers with the
// telemetry middleware (emitter pointed at prod) and verifies the tools
// behave exactly as without it. Read-only tools only — no funds move.
func TestTelemetryMiddlewarePassthroughLive(t *testing.T) {
	env := setupAndAuth(t)

	em := telemetry.NewEmitter(telemetry.Config{
		BaseURL: envOr("PERPTOOLS_BASE_URL", "https://app.perptools.ai/api"),
	})
	defer em.Close()

	mw := telemetry.ToolMiddleware(em, env.svc)
	wrapped := make(map[string]func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error), len(env.toolMap))
	for name, h := range env.toolMap {
		wrapped[name] = mw(h)
	}

	// success path, unauthenticated-capable tool
	resp := callTool(t, env.ctx, wrapped, "get_markets", map[string]any{"limit": 2})
	if !strings.Contains(resp, "markets") {
		t.Fatalf("get_markets through middleware returned unexpected body: %s", resp)
	}

	// success path, authenticated tool (read-only)
	resp = callTool(t, env.ctx, wrapped, "get_balances", map[string]any{
		"wallet_address": env.walletAddress,
	})
	if !strings.Contains(resp, "main_account_usdc") {
		t.Fatalf("get_balances through middleware returned unexpected body: %s", resp)
	}

	// failure path: result+error must pass through unchanged (IsError result)
	text, isErr := callToolSoft(t, env.ctx, wrapped, "get_user_points", nil)
	if !isErr || !strings.Contains(text, "public_key") {
		t.Fatalf("expected invalid-args error to pass through, got isErr=%v text=%s", isErr, text)
	}

	if em.Dropped() != 0 {
		t.Fatalf("emitter dropped %d events during a 3-call test", em.Dropped())
	}
}
