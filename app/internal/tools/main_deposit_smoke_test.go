package tools_test

import (
	"testing"
)

// TestMainDepositToolsSmoke verifies the productionized top-up tools at the MCP
// tool layer (get_master_wallet + prepare_main_deposit) against the live
// backend, WITHOUT submitting any transaction — so it is safe to run with an
// empty wallet. It confirms master-wallet resolution (orderly proxy) and that
// prepare_main_deposit assembles an unsigned Solana transaction.
func TestMainDepositToolsSmoke(t *testing.T) {
	env := setupAndAuth(t)

	t.Log("get_master_wallet")
	mResp := callTool(t, env.ctx, env.toolMap, "get_master_wallet", map[string]any{
		"wallet_address": env.walletAddress,
	})
	var m struct {
		MasterWallet string `json:"master_wallet"`
	}
	mustUnmarshal(t, mResp, &m)
	if m.MasterWallet == "" {
		t.Fatalf("no master wallet resolved: %s", mResp)
	}
	t.Logf("  master_wallet=%s", m.MasterWallet)

	t.Log("prepare_main_deposit (amount=10)")
	pResp := callTool(t, env.ctx, env.toolMap, "prepare_main_deposit", map[string]any{
		"wallet_address": env.walletAddress,
		"amount":         float64(10),
	})
	var p struct {
		MasterWallet        string  `json:"master_wallet"`
		Amount              float64 `json:"amount"`
		UnsignedTransaction string  `json:"unsigned_transaction"`
	}
	mustUnmarshal(t, pResp, &p)
	if p.UnsignedTransaction == "" {
		t.Fatalf("prepare_main_deposit returned no unsigned tx: %s", pResp)
	}
	t.Logf("  ✅ unsigned tx built (%d base64 chars), master=%s amount=%v",
		len(p.UnsignedTransaction), p.MasterWallet, p.Amount)
}
