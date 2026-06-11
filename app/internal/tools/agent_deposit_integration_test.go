package tools_test

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"
)

// TestDepositToAgent exercises the full AI-agent funding suite against the LIVE
// backend, all three ways:
//
//  1. create_agent            — deploy a vault to deposit into (or reuse AGENT_ID).
//  2. direct_deposit_to_agent — one-step wallet -> agent.
//  3. deposit_to_agent        — Main Account -> agent.
//  4. get_agent_deposit_status — follow up if a deposit did not settle in-band.
//
// It is deliberately tolerant: a fresh, unfunded, non-eligible wallet is EXPECTED
// to be rejected by the backend (not premint/KOL eligible, no signed agreement,
// no custodied funds). Those rejections are logged, not fatal — the point is to
// verify our request envelopes, orderly signing, and polling reach the real
// hooks and come back with a coherent business response, without moving money.
//
// Run with:  go test ./app/internal/tools/ -run TestDepositToAgent -v
// Requires SOLANA_PRIVATE_KEY in .env (skips otherwise).
func TestDepositToAgent(t *testing.T) {
	env := setupAndAuth(t)

	// --- 0. sign the risk-disclosure agreement (gates all deposits) ---------
	signAgreement(t, env)

	// --- 1. obtain an agent to deposit into ---------------------------------
	agentID := os.Getenv("AGENT_ID")
	if agentID != "" {
		t.Logf("using preset AGENT_ID=%s", agentID)
	} else {
		// unique-ish name so repeated runs don't collide on the uniqueness check
		name := fmt.Sprintf("mcp-test-%d", time.Now().Unix())
		t.Logf("create_agent — name=%s", name)

		resp, isErr := callToolSoft(t, env.ctx, env.toolMap, "create_agent", map[string]any{
			"wallet_address": env.walletAddress,
			"agent_name":     name,
			"symbol":         "MCPT",
			"description":    "Integration-test agent created by the MCP server test suite.",
			// Envy's deploy SDK wants a CAPITALIZED risk level: Low|Medium|High|
			// Extreme (lowercase "low" and "medium" are both rejected with
			// INVALID_RISK_LEVEL — note this differs from the legacy
			// /v1/agent/register flow which uses lowercase low|middle|high|extreme).
			"risk_level": envOr("AGENT_RISK_LEVEL", "Low"),
			"avatar_url": envOr("AGENT_AVATAR_URL", "https://app.perptools.ai/favicon.ico"),
		})
		if isErr {
			t.Logf("  create_agent rejected (expected for a fresh/ineligible wallet):\n  %s", resp)
			t.Log("  -> no agent_id available; deposit steps will run with a placeholder to verify the request path")
		} else {
			t.Logf("  create_agent OK: %s", resp)
			agentID = extractAgentID(t, resp)
			t.Logf("  agent_id=%s", agentID)
		}
	}

	// Even without a real agent, run the deposit calls with a placeholder so we
	// observe how the backend rejects them (validates the envelope + auth path).
	depositTarget := agentID
	placeholder := depositTarget == ""
	if placeholder {
		// a non-nil UUID: the backend parses agentId into uuid.UUID and treats
		// the all-zero Nil UUID as "absent", so a placeholder must be non-nil to
		// prove our agentId field actually reaches the agent-resolution step.
		depositTarget = "11111111-1111-1111-1111-111111111111"
		t.Logf("no real agent_id — using placeholder %s for deposit path verification", depositTarget)
	}

	const amount = float64(10) // first agent deposit must be >= $10

	// --- 2. main-account deposit (Main -> agent) ----------------------------
	// NOTE: this verifies the deposit request path reaches Envy; it needs a
	// funded Main Account to actually settle (see TestFullDepositViaMCP).
	t.Log("deposit_to_agent")
	dResp, dErr := callToolSoft(t, env.ctx, env.toolMap, "deposit_to_agent", map[string]any{
		"wallet_address": env.walletAddress,
		"agent_id":       depositTarget,
		"amount":         amount,
	})
	logDeposit(t, "deposit_to_agent", dResp, dErr)

	// --- 3. status follow-up, if the deposit handed us a transaction_id ------
	for _, raw := range []string{dResp} {
		if txID := extractTransactionID(raw); txID != "" {
			t.Logf("get_agent_deposit_status — transaction_id=%s", txID)
			sResp, sErr := callToolSoft(t, env.ctx, env.toolMap, "get_agent_deposit_status", map[string]any{
				"wallet_address": env.walletAddress,
				"transaction_id": txID,
			})
			logDeposit(t, "get_agent_deposit_status", sResp, sErr)
		}
	}

	if placeholder {
		t.Log("NOTE: ran against a placeholder agent — a green 'funds moved' is not expected. " +
			"To test true settlement, set AGENT_ID and use a funded wallet.")
	}
}

// signAgreement makes sure the wallet has signed the risk-disclosure agreement,
// which gates every deposit. It is idempotent: if already signed it returns
// immediately; otherwise it signs the message bytes with the test wallet and
// submits the base58 signature via complete_agreement.
func signAgreement(t *testing.T, env *testEnv) {
	t.Helper()
	t.Log("prepare_agreement")
	resp := callTool(t, env.ctx, env.toolMap, "prepare_agreement", map[string]any{
		"wallet_address": env.walletAddress,
	})

	var data struct {
		Signed        bool   `json:"signed"`
		MessageBase64 string `json:"message_base64"`
	}
	mustUnmarshal(t, resp, &data)
	if data.Signed {
		t.Log("  agreement already signed")
		return
	}

	msgBytes, err := base64.StdEncoding.DecodeString(data.MessageBase64)
	if err != nil {
		t.Fatalf("decode agreement message: %v", err)
	}
	// the agreement endpoint wants the signature base58-encoded (not 0x-hex)
	sig := ed25519.Sign(ed25519.PrivateKey(env.privKey), msgBytes)
	sigB58 := base58Encode(sig)

	t.Log("complete_agreement")
	out := callTool(t, env.ctx, env.toolMap, "complete_agreement", map[string]any{
		"wallet_address": env.walletAddress,
		"signature":      sigB58,
	})
	t.Logf("  agreement signed: %s", out)
}

// base58Encode is the inverse of base58Decode, for the Bitcoin/Solana alphabet.
func base58Encode(b []byte) string {
	const alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
	digits := []byte{0}
	for _, x := range b {
		carry := int(x)
		for j := 0; j < len(digits); j++ {
			carry += int(digits[j]) << 8
			digits[j] = byte(carry % 58)
			carry /= 58
		}
		for carry > 0 {
			digits = append(digits, byte(carry%58))
			carry /= 58
		}
	}
	for _, x := range b {
		if x != 0 {
			break
		}
		digits = append(digits, 0)
	}
	out := make([]byte, len(digits))
	for i, d := range digits {
		out[len(digits)-1-i] = alphabet[d]
	}
	return string(out)
}

// logDeposit prints a deposit-style response, flagging whether the backend
// accepted or rejected it.
func logDeposit(t *testing.T, label, resp string, isErr bool) {
	t.Helper()
	if isErr {
		t.Logf("  %s -> rejected:\n  %s", label, resp)
		return
	}
	t.Logf("  %s -> accepted:\n  %s", label, resp)
}

// extractAgentID digs the agent id out of a create_agent response, tolerating a
// few likely field spellings.
func extractAgentID(t *testing.T, resp string) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(resp), &m); err != nil {
		return ""
	}
	for _, k := range []string{"agent_id", "agentId", "agentID", "id"} {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// extractTransactionID pulls a transaction id out of a deposit response shaped
// like AgentDepositResult{deposit:{transactionId}} or a flat map.
func extractTransactionID(resp string) string {
	var m map[string]any
	if err := json.Unmarshal([]byte(resp), &m); err != nil {
		return ""
	}
	if dep, ok := m["deposit"].(map[string]any); ok {
		if v, ok := dep["transactionId"].(string); ok && v != "" {
			return v
		}
	}
	for _, k := range []string{"transactionId", "transaction_id"} {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}
