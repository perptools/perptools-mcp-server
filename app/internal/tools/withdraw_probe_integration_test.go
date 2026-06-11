package tools_test

import (
	"strings"
	"testing"
)

// TestWithdrawProbes is the non-destructive STEP-0 check for the withdrawal
// endpoints. It sends deliberately-invalid bodies through the orderly-signed
// AI proxy and only inspects the error shape — no funds can move:
//
//  1. /api/v1/users/withdraw/solana with amount=0 — expect an Envy
//     VALIDATION error ("Invalid amount"), proving the path is live.
//     NOTE: Envy migrated its house API from /api/sdk/* to /api/v1/* — the
//     legacy /api/sdk/withdraw/solana (still referenced by the jupbot
//     backend source) now 404s upstream with an EMPTY body
//     ("API error (404): "); the live master-withdraw path was found by
//     probing and is /api/v1/users/withdraw/solana (verified 2026-06-10).
//  2. /api/v1/agents/withdraw with a bogus agentId — expect an
//     agent-not-found-style error ("Agent not found in your house"),
//     proving the path is live and the body parses. The backend wraps that
//     business error in an HTTP 404 — the message is what matters.
//
// Run with: go test ./app/internal/tools/ -run TestWithdrawProbes -v
func TestWithdrawProbes(t *testing.T) {
	env := setupAndAuth(t)
	pk := env.walletAddress

	probes := []struct {
		label    string
		envyPath string
		body     map[string]any
	}{
		{
			label:    "master withdraw (amount=0 -> validation err expected)",
			envyPath: "/api/v1/users/withdraw/solana",
			body:     map[string]any{"walletAddress": pk, "amount": 0},
		},
		{
			label:    "agents withdraw (bogus agent -> not-found err expected)",
			envyPath: "/api/v1/agents/withdraw",
			body: map[string]any{
				"walletAddress": pk,
				"agentId":       "22222222-2222-4222-8222-222222222222",
				"shares":        1,
			},
		},
	}

	for _, p := range probes {
		envelope := map[string]any{
			"public_key": pk,
			"path":       p.envyPath,
			"method":     "POST",
			"auth_type":  0,
			"body":       p.body,
		}
		code, body, err := env.svc.ProbeOrderlyPost(env.ctx, "/v1/ai/proxy", envelope)
		if err != nil {
			t.Errorf("PROBE %s: transport error: %v", p.envyPath, err)
			continue
		}
		t.Logf("PROBE %-32s [%d] %s", p.envyPath, code, trunc(body, 500))
		// A pass-through upstream 404 looks like `"message":"API error (404): "`
		// (empty upstream text) — that means the Envy path itself is missing.
		// Business errors (e.g. "Agent not found in your house") also ride on
		// HTTP 404 but carry a real message, which is a PASS for these probes.
		if strings.Contains(body, "API error (404)") {
			t.Errorf("PROBE %s: upstream 404 — Envy path is gone; do not ship the tool against it", p.envyPath)
		}
	}
}
