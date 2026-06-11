package tools_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

// TestAgentStatsProbe pins down the behaviour the agent-stats tools depend on
// BEFORE they are built. It probes, logging only (no asserts):
//  1. whether POST {base}/v1/ai/proxy/public is reachable headless (no auth,
//     no browser session) — the public proxy allow-lists /api/data/arena,
//     /api/v1/agents/details and /api/v1/agents/positions;
//  2. arena sort direction with vs without sortByDesc;
//  3. whether sortBy=pnl30d is accepted;
//  4. whether the authed proxy path /api/v1/agents/list exists (the /api/sdk
//     variant is deprecated), plus /api/v1/agents/shares.
func TestAgentStatsProbe(t *testing.T) {
	if os.Getenv("SOLANA_PRIVATE_KEY") == "" {
		t.Skip("SOLANA_PRIVATE_KEY not set — skipping integration probe")
	}
	base := envOr("PERPTOOLS_BASE_URL", "https://app.perptools.ai/api")
	agentID := envOr("AGENT_ID", "1b5d9ef1-562e-41fe-be09-ee2002550e6b")

	httpc := &http.Client{Timeout: 30 * time.Second}

	publicProxy := func(path, method string, body any) (int, string) {
		t.Helper()
		envelope := map[string]any{
			"path":      path,
			"method":    method,
			"auth_type": 0,
			"body":      body,
		}
		if method == http.MethodGet {
			envelope["auth_type"] = 1 // arena is house-API-key signed
		}
		raw, err := json.Marshal(envelope)
		if err != nil {
			t.Fatalf("marshal envelope: %v", err)
		}
		req, err := http.NewRequest(http.MethodPost, base+"/v1/ai/proxy/public", bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("build request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := httpc.Do(req)
		if err != nil {
			t.Fatalf("POST public proxy %s: %v", path, err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}

	logArenaOrder := func(label, body string) {
		var out struct {
			Success bool `json:"success"`
			Bots    []struct {
				Name   string  `json:"name"`
				AUM    float64 `json:"aum"`
				Pnl24h float64 `json:"pnlPercentage24h"`
			} `json:"bots"`
		}
		if err := json.Unmarshal([]byte(body), &out); err != nil {
			t.Logf("  %s: unparseable: %v", label, err)
			return
		}
		var line string
		for _, b := range out.Bots {
			line += fmt.Sprintf(" [%s aum=%.0f pnl24h=%.2f]", b.Name, b.AUM, b.Pnl24h)
		}
		t.Logf("  %s: success=%v%s", label, out.Success, line)
	}

	// --- 1. public proxy reachability + 2. sort direction + 3. pnl30d ---
	arenaPaths := []struct{ label, path string }{
		{"plain sortBy=pnl24h", "/api/data/arena?page=1&pageSize=3&sortBy=pnl24h"},
		{"with sortByDesc=pnl24h", "/api/data/arena?page=1&pageSize=3&sortBy=pnl24h&sortByDesc=pnl24h"},
		{"plain sortBy=aum", "/api/data/arena?page=1&pageSize=3&sortBy=aum"},
		{"with sortByDesc=aum", "/api/data/arena?page=1&pageSize=3&sortBy=aum&sortByDesc=aum"},
		{"sortBy=pnl30d", "/api/data/arena?page=1&pageSize=3&sortBy=pnl30d"},
	}
	for _, p := range arenaPaths {
		code, body := publicProxy(p.path, http.MethodGet, map[string]any{})
		t.Logf("PUBLIC GET %-55s [%d] %s", p.path, code, trunc(body, 200))
		if code == http.StatusOK {
			logArenaOrder(p.label, body)
		}
	}

	// details + positions on the PUBLIC proxy with the system wallet (the
	// backend itself uses the all-ones wallet for anonymous detail reads).
	const systemWallet = "11111111111111111111111111111111"
	for _, p := range []struct {
		path string
		body map[string]any
	}{
		{"/api/v1/agents/details", map[string]any{"walletAddress": systemWallet, "agentId": agentID}},
		{"/api/v1/agents/positions", map[string]any{"walletAddress": systemWallet, "agentId": agentID, "page": 1, "limit": 3}},
	} {
		code, body := publicProxy(p.path, http.MethodPost, p.body)
		t.Logf("PUBLIC POST %-54s [%d] %s", p.path, code, trunc(body, 300))
	}

	// --- 4. authed proxy probes ---
	env := setupAndAuth(t)
	pk := env.walletAddress

	authedProxy := func(path, method string, authType int, body map[string]any) (int, string) {
		t.Helper()
		envelope := map[string]any{
			"public_key": pk,
			"path":       path,
			"method":     method,
			"auth_type":  authType,
			"body":       body,
		}
		code, respBody, err := env.svc.ProbeOrderlyPost(env.ctx, "/v1/ai/proxy", envelope)
		if err != nil {
			t.Fatalf("authed proxy %s: %v", path, err)
		}
		return code, respBody
	}

	// arena through the AUTHED proxy (auth_type 1 is allow-listed there too):
	// pin sort direction and pnl30d support.
	for _, p := range arenaPaths {
		code, body := authedProxy(p.path, http.MethodGet, 1, map[string]any{})
		t.Logf("AUTHED GET %-55s [%d] %s", p.path, code, trunc(body, 120))
		if code == http.StatusOK {
			logArenaOrder(p.label, body)
		}
	}

	// details + positions through the authed proxy with the user's own wallet.
	for _, pp := range []struct {
		path string
		body map[string]any
	}{
		{"/api/v1/agents/details", map[string]any{"walletAddress": pk, "agentId": agentID}},
		{"/api/v1/agents/positions", map[string]any{"walletAddress": pk, "agentId": agentID, "page": 1, "limit": 3}},
		{"/api/v1/agents/list", map[string]any{"walletAddress": pk}},
		{"/api/v1/agents/shares", map[string]any{"walletAddress": pk, "agentId": agentID}},
	} {
		code, body := authedProxy(pp.path, http.MethodPost, 0, pp.body)
		t.Logf("AUTHED %-35s [%d] %s", pp.path, code, trunc(body, 1200))
	}
}
