package tools_test

import (
	"testing"
)

// TestGatewayProbe checks whether the session-gateway endpoints (auth/nonce,
// wallet deposit) are reachable with ORDERLY auth via the perptools backend's
// /v1/envy/* legacy mirror — i.e. whether a fully MCP-only top-up is possible
// without the browser session. It just reports status codes; it does not assert.
func TestGatewayProbe(t *testing.T) {
	env := setupAndAuth(t)
	pk := env.walletAddress

	posts := []struct {
		path string
		body any
	}{
		{"/v1/envy/api/auth/nonce", map[string]any{"publicKey": pk}},
		{"/v1/ai/api/auth/nonce", map[string]any{"publicKey": pk}},
		{"/v1/envy/api/wallet/solana/deposit/prepare", map[string]any{"walletAddress": pk, "amount": 1}},
		{"/v1/envy/api/agents/deposit", map[string]any{"walletAddress": pk, "amount": 1}},
		{"/v1/auth/nonce", map[string]any{"publicKey": pk}},
		{"/auth/nonce", map[string]any{"publicKey": pk}},
	}
	for _, p := range posts {
		code, body, err := env.svc.ProbeOrderlyPost(env.ctx, p.path, p.body)
		if err != nil {
			t.Logf("POST %-45s ERR %v", p.path, err)
			continue
		}
		t.Logf("POST %-45s [%d] %s", p.path, code, trunc(body, 140))
	}

	// Probe Envy SDK endpoints THROUGH the orderly proxy envelope to find the
	// user's solanaMasterWallet (the on-chain Main Account deposit address).
	proxyProbes := []struct {
		envyPath string
		body     map[string]any
	}{
		{"/api/v1/dashboard/overview", map[string]any{"walletAddress": pk}},
		{"/api/v1/users/info", map[string]any{"walletAddress": pk}},
		{"/api/v1/balance", map[string]any{"walletAddress": pk}},
	}
	for _, pp := range proxyProbes {
		envelope := map[string]any{
			"public_key": pk,
			"path":       pp.envyPath,
			"method":     "POST",
			"auth_type":  0,
			"body":       pp.body,
		}
		code, body, err := env.svc.ProbeOrderlyPost(env.ctx, "/v1/ai/proxy", envelope)
		if err != nil {
			t.Logf("PROXY %-35s ERR %v", pp.envyPath, err)
			continue
		}
		t.Logf("PROXY %-35s [%d] %s", pp.envyPath, code, trunc(body, 400))
	}

	gets := []struct{ path, qk, qv string }{
		{"/v1/envy/api/auth/nonce", "public_key", pk},
		{"/v1/session", "public_key", pk},
	}
	for _, g := range gets {
		code, body, err := env.svc.ProbeOrderlyGet(env.ctx, g.path, g.qk, g.qv)
		if err != nil {
			t.Logf("GET  %-45s ERR %v", g.path, err)
			continue
		}
		t.Logf("GET  %-45s [%d] %s", g.path, code, trunc(body, 140))
	}
}

func trunc(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
