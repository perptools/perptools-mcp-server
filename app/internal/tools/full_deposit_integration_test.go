package tools_test

import (
	"os"
	"testing"

	"mcp-server/app/internal/clients/solfund"

	"github.com/gagliardetto/solana-go"
)

// TestFullDepositViaMCP exercises the complete, MCP-only deposit path the way
// the real product funds an agent — without the browser session gateway:
//
//  1. orderly proxy  -> get the user's Main Account (master) address.
//  2. Solana RPC     -> plain USDC transfer wallet -> master (funds Main Account).
//  3. orderly proxy  -> deposit_to_agent (Main -> Agent), then poll status.
//
// Needs a wallet with on-chain USDC + SOL (for fees) and an AGENT_ID in .env.
// Run with:  go test ./app/internal/tools/ -run TestFullDepositViaMCP -v
func TestFullDepositViaMCP(t *testing.T) {
	env := setupAndAuth(t)
	signAgreement(t, env)

	agentID := os.Getenv("AGENT_ID")
	if agentID == "" {
		t.Skip("AGENT_ID not set — deploy an agent first (see TestDepositToAgent)")
	}
	amount := optEnvFloat("MAIN_DEPOSIT_AMOUNT", 1)

	// --- 1. master wallet via the orderly proxy ----------------------------
	master, err := env.svc.GetMasterWallet(env.ctx, env.walletAddress)
	if err != nil {
		t.Fatalf("get master wallet: %v", err)
	}
	t.Logf("master (Main Account) wallet: %s", master)

	masterPub := solana.MustPublicKeyFromBase58(master)
	walletPub := solana.MustPublicKeyFromBase58(env.walletAddress)

	// --- 2. on-chain USDC transfer wallet -> master ------------------------
	sf := solfund.New(envOr("SOLANA_RPC_URL", "https://api.mainnet-beta.solana.com"))

	walBal, err := sf.USDCBalance(env.ctx, walletPub)
	if err != nil {
		t.Fatalf("read wallet USDC balance: %v", err)
	}
	t.Logf("wallet USDC balance: %.6f", float64(walBal)/1e6)
	if walBal < solfund.ToRaw(amount) {
		t.Fatalf("wallet has %.6f USDC, need %.6f — fund the wallet first", float64(walBal)/1e6, amount)
	}

	beforeBal, _ := sf.USDCBalance(env.ctx, masterPub)
	t.Logf("master USDC balance before: %.6f", float64(beforeBal)/1e6)

	tx, err := sf.BuildUSDCTransfer(env.ctx, walletPub, masterPub, solfund.ToRaw(amount))
	if err != nil {
		t.Fatalf("build transfer: %v", err)
	}
	// sign with the test wallet (in production the wallet adapter signs the
	// base64 tx that prepare_main_deposit would return)
	if _, err := tx.Sign(func(k solana.PublicKey) *solana.PrivateKey {
		if k.Equals(walletPub) {
			pk := solana.PrivateKey(env.privKey)
			return &pk
		}
		return nil
	}); err != nil {
		t.Fatalf("sign transfer: %v", err)
	}

	t.Logf("submitting USDC transfer of %.6f wallet -> master ...", amount)
	sig, err := sf.Submit(env.ctx, tx)
	if err != nil {
		t.Fatalf("submit transfer: %v (sig=%s)", err, sig)
	}
	t.Logf("  ✅ on-chain transfer confirmed: %s", sig)

	afterBal, _ := sf.USDCBalance(env.ctx, masterPub)
	t.Logf("master USDC balance after: %.6f", float64(afterBal)/1e6)

	// --- 3. deposit_to_agent (Main -> Agent) via the proxy -----------------
	t.Logf("deposit_to_agent — agent=%s amount=%v", agentID, amount)
	resp, isErr := callToolSoft(t, env.ctx, env.toolMap, "deposit_to_agent", map[string]any{
		"wallet_address": env.walletAddress,
		"agent_id":       agentID,
		"amount":         amount,
	})
	if isErr {
		t.Fatalf("deposit_to_agent rejected:\n%s", resp)
	}
	t.Logf("  ✅ deposit_to_agent accepted:\n%s", resp)
}
