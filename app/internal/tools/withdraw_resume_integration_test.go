package tools_test

import (
	"os"
	"testing"
	"time"

	"mcp-server/app/internal/clients/solfund"

	"github.com/gagliardetto/solana-go"
)

// TestWithdrawResume picks up a queued agent withdrawal (WITHDRAW_TX_ID in
// env) and finishes the round-trip once it settles: verify the Main Account
// credit, withdraw_to_wallet $3, verify on-chain, restore the fixture.
// Companion to TestWithdrawRoundTrip for the queued/deferred-execution case.
//
//	WITHDRAW_TX_ID=... go test ./app/internal/tools/ -run TestWithdrawResume -v -timeout 30m
func TestWithdrawResume(t *testing.T) {
	txID := os.Getenv("WITHDRAW_TX_ID")
	if txID == "" {
		t.Skip("WITHDRAW_TX_ID not set")
	}
	env := setupAndAuth(t)
	agentID := os.Getenv("AGENT_ID")
	if agentID == "" {
		t.Skip("AGENT_ID not set")
	}

	const walletAmount = float64(3)
	waitMin := optEnvFloat("WITHDRAW_WAIT_MINUTES", 20)

	sf := solfund.New(envOr("SOLANA_RPC_URL", "https://api.mainnet-beta.solana.com"))
	walletPub := solana.MustPublicKeyFromBase58(env.walletAddress)

	bal0 := getBalancesSnapshot(t, env)
	shares0, price0 := getStakeSnapshot(t, env, agentID)
	t.Logf("NOW: main=%.6f wallet=%.6f agent shares=%.6f @ %.4f", bal0.MainUSDC, bal0.WalletUSDC, shares0, price0)

	// The canonical fixture is 10 shares; shares0 may already reflect the
	// in-flight withdrawal, so restore against the explicit target.
	targetShares := optEnvFloat("FIXTURE_TARGET_SHARES", 10)

	status := pollWithdrawalToTerminal(t, env, txID, time.Duration(waitMin)*time.Minute)
	t.Logf("withdrawal %s status: %q", txID, status)
	if !isTerminalStatus(status) {
		t.Fatalf("withdrawal still not terminal after %.0f min (status %q) — re-run later with WITHDRAW_TX_ID=%s", waitMin, status, txID)
	}

	bal1 := waitForMainDelta(t, env, 0, walletAmount, 90*time.Second)
	t.Logf("main_account_usdc after settlement: %.6f (ledger may lag; the tool's preflight also checks the master's on-chain USDC)", bal1.MainUSDC)

	walRaw0, err := sf.USDCBalance(env.ctx, walletPub)
	if err != nil {
		t.Fatalf("read on-chain wallet USDC: %v", err)
	}

	t.Logf("withdraw_to_wallet — amount=%.0f", walletAmount)
	pResp, pErr := callToolSoft(t, env.ctx, env.toolMap, "withdraw_to_wallet", map[string]any{
		"wallet_address": env.walletAddress,
		"amount":         walletAmount,
	})
	if pErr {
		t.Fatalf("withdraw_to_wallet rejected:\n%s", pResp)
	}
	t.Logf("  withdraw_to_wallet response:\n%s", pResp)

	var payout struct {
		TransactionHash    string  `json:"transaction_hash"`
		Amount             float64 `json:"amount"`
		NewMasterBalance   float64 `json:"new_master_balance"`
		DestinationAddress string  `json:"destination_address"`
	}
	mustUnmarshal(t, pResp, &payout)
	if payout.TransactionHash == "" {
		t.Errorf("no transaction_hash in payout response")
	}
	if payout.DestinationAddress != env.walletAddress {
		t.Errorf("destination %q != test wallet %q", payout.DestinationAddress, env.walletAddress)
	}

	var walRaw1 uint64
	for i := 0; i < 10; i++ {
		time.Sleep(6 * time.Second)
		walRaw1, err = sf.USDCBalance(env.ctx, walletPub)
		if err == nil && walRaw1 > walRaw0 {
			break
		}
	}
	t.Logf("on-chain wallet USDC: %.6f -> %.6f (delta %.6f, tx %s)",
		float64(walRaw0)/1e6, float64(walRaw1)/1e6, float64(walRaw1-walRaw0)/1e6, payout.TransactionHash)
	if walRaw1 < walRaw0+solfund.ToRaw(walletAmount*0.95) {
		t.Errorf("on-chain delta below expected ~%.2f USDC", walletAmount)
	}

	restoreFixture(t, env, sf, agentID, walletAmount, targetShares, price0)

	balF := getBalancesSnapshot(t, env)
	sharesF, priceF := getStakeSnapshot(t, env, agentID)
	t.Logf("FINAL: wallet_usdc=%.6f main_account_usdc=%.6f agent shares=%.6f (sharePrice %.6f, ~$%.2f)",
		balF.WalletUSDC, balF.MainUSDC, sharesF, priceF, sharesF*priceF)
}
