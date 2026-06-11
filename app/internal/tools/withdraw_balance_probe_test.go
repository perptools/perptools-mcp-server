package tools_test

import (
	"testing"

	"mcp-server/app/internal/clients/solfund"

	"github.com/gagliardetto/solana-go"
)

// TestWithdrawBalanceProbe is a diagnostics snapshot: raw dashboard overview,
// users/info, master on-chain USDC and wallet on-chain USDC. Read-only.
func TestWithdrawBalanceProbe(t *testing.T) {
	env := setupAndAuth(t)
	pk := env.walletAddress

	for _, path := range []string{"/api/v1/dashboard/overview", "/api/v1/users/info"} {
		envelope := map[string]any{
			"public_key": pk,
			"path":       path,
			"method":     "POST",
			"auth_type":  0,
			"body":       map[string]any{"walletAddress": pk},
		}
		code, body, err := env.svc.ProbeOrderlyPost(env.ctx, "/v1/ai/proxy", envelope)
		if err != nil {
			t.Logf("PROBE %-30s ERR %v", path, err)
			continue
		}
		t.Logf("PROBE %-30s [%d] %s", path, code, trunc(body, 600))
	}

	master, err := env.svc.GetMasterWallet(env.ctx, pk)
	if err != nil {
		t.Fatalf("get master wallet: %v", err)
	}
	sf := solfund.New(envOr("SOLANA_RPC_URL", "https://api.mainnet-beta.solana.com"))
	mBal, _ := sf.USDCBalance(env.ctx, solana.MustPublicKeyFromBase58(master))
	wBal, _ := sf.USDCBalance(env.ctx, solana.MustPublicKeyFromBase58(pk))
	t.Logf("on-chain: master(%s)=%.6f USDC, wallet=%.6f USDC", master, float64(mBal)/1e6, float64(wBal)/1e6)
}
