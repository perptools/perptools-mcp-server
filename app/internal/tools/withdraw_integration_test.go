package tools_test

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"mcp-server/app/internal/clients/solfund"

	"github.com/gagliardetto/solana-go"
)

// TestWithdrawRoundTrip exercises the full LIVE withdrawal path with REAL
// FUNDS (the ~$10 test fixture agent) — intended and approved:
//
//  1. Snapshot balances + agent stake.
//  2. withdraw_from_agent shares=5 (agent -> Main Account), poll to terminal,
//     verify main_account_usdc grew by ~5*sharePrice.
//  3. withdraw_to_wallet amount=3 (Main Account -> on-chain wallet),
//     verify the tx hash + on-chain USDC delta.
//  4. RESTORE the fixture: on-chain transfer of the withdrawn $3 back to the
//     master, then deposit_to_agent to bring the stake back to ~10 shares.
//
// Every step logs liberally so a partial failure still reports exactly where
// the money ended up. Run with:
//
//	go test ./app/internal/tools/ -run TestWithdrawRoundTrip -v -timeout 20m
func TestWithdrawRoundTrip(t *testing.T) {
	env := setupAndAuth(t)
	signAgreement(t, env)

	agentID := os.Getenv("AGENT_ID")
	if agentID == "" {
		t.Skip("AGENT_ID not set — need the funded fixture agent")
	}

	const (
		withdrawShares = float64(5)
		walletAmount   = float64(3)
	)

	sf := solfund.New(envOr("SOLANA_RPC_URL", "https://api.mainnet-beta.solana.com"))
	walletPub := solana.MustPublicKeyFromBase58(env.walletAddress)

	// --- 1. snapshot ---------------------------------------------------------
	bal0 := getBalancesSnapshot(t, env)
	t.Logf("BEFORE: wallet_usdc=%.6f main_account_usdc=%.6f", bal0.WalletUSDC, bal0.MainUSDC)

	shares0, price0 := getStakeSnapshot(t, env, agentID)
	t.Logf("BEFORE: agent stake: shares=%.6f sharePrice=%.6f (~$%.2f)", shares0, price0, shares0*price0)
	if shares0 < withdrawShares {
		t.Fatalf("fixture agent holds only %.4f shares — need >= %.0f; restore the fixture first", shares0, withdrawShares)
	}

	// --- 2. withdraw_from_agent (agent -> Main) ------------------------------
	t.Logf("withdraw_from_agent — agent=%s shares=%.0f", agentID, withdrawShares)
	wResp, wErr := callToolSoft(t, env.ctx, env.toolMap, "withdraw_from_agent", map[string]any{
		"wallet_address": env.walletAddress,
		"agent_id":       agentID,
		"shares":         withdrawShares,
	})
	if wErr {
		t.Fatalf("withdraw_from_agent rejected:\n%s", wResp)
	}
	t.Logf("  withdraw_from_agent response:\n%s", wResp)

	txID := extractWithdrawTxID(wResp)
	if txID == "" {
		t.Fatalf("no transaction_id in withdraw response:\n%s", wResp)
	}
	t.Logf("  transaction_id=%s", txID)

	status := pollWithdrawalToTerminal(t, env, txID, 3*time.Minute)
	if !isTerminalStatus(status) {
		// queued / WAITING_FOR_APPROVAL withdrawals can take minutes to hours
		// (observed live: queued=true -> PENDING -> PENDING_APPROVAL ->
		// COMPLETED in ~10 min). The money is in flight, not lost — finish the
		// round-trip later with the resume test.
		t.Fatalf("withdrawal not terminal yet (last status %q) — funds are in flight; finish with:\n"+
			"  WITHDRAW_TX_ID=%s go test ./app/internal/tools/ -run TestWithdrawResume -v -timeout 30m", status, txID)
	}
	t.Logf("  withdrawal terminal status: %s", status)

	// --- verify Main Account credited ----------------------------------------
	// NOTE: the dashboard ledger can lag the completed withdrawal by minutes
	// (verified live) — the proceeds sit on-chain at the master wallet first.
	// The payout preflight checks the on-chain balance too, so a shortfall
	// here is informational; the authoritative check is the on-chain wallet
	// delta after withdraw_to_wallet below.
	expectedCredit := withdrawShares * price0
	bal1 := waitForMainDelta(t, env, bal0.MainUSDC, expectedCredit*0.8, 90*time.Second)
	t.Logf("AFTER agent withdrawal: main_account_usdc=%.6f (was %.6f, expected +~%.4f; ledger may lag)", bal1.MainUSDC, bal0.MainUSDC, expectedCredit)

	// --- 3. withdraw_to_wallet (Main -> wallet) ------------------------------
	walRaw0, err := sf.USDCBalance(env.ctx, walletPub)
	if err != nil {
		t.Fatalf("read on-chain wallet USDC: %v", err)
	}
	t.Logf("on-chain wallet USDC before payout: %.6f", float64(walRaw0)/1e6)

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
		Success            bool    `json:"success"`
		TransactionHash    string  `json:"transaction_hash"`
		Amount             float64 `json:"amount"`
		NewMasterBalance   float64 `json:"new_master_balance"`
		DestinationAddress string  `json:"destination_address"`
	}
	mustUnmarshal(t, pResp, &payout)
	if payout.TransactionHash == "" {
		t.Errorf("withdraw_to_wallet returned no transaction_hash:\n%s", pResp)
	}
	if payout.DestinationAddress != env.walletAddress {
		t.Errorf("destination %q != test wallet %q", payout.DestinationAddress, env.walletAddress)
	}

	// on-chain delta, with retries for RPC lag
	var walRaw1 uint64
	for i := 0; i < 10; i++ {
		time.Sleep(6 * time.Second)
		walRaw1, err = sf.USDCBalance(env.ctx, walletPub)
		if err == nil && walRaw1 > walRaw0 {
			break
		}
	}
	t.Logf("on-chain wallet USDC after payout: %.6f (delta %.6f)", float64(walRaw1)/1e6, float64(walRaw1-walRaw0)/1e6)
	if walRaw1 < walRaw0+solfund.ToRaw(walletAmount*0.95) {
		t.Errorf("on-chain wallet delta %.6f — expected ~%.2f USDC (tx %s)", float64(walRaw1-walRaw0)/1e6, walletAmount, payout.TransactionHash)
	}

	// --- 4. restore the fixture ----------------------------------------------
	restoreFixture(t, env, sf, agentID, walletAmount, shares0, price0)

	// --- final state -----------------------------------------------------------
	balF := getBalancesSnapshot(t, env)
	sharesF, priceF := getStakeSnapshot(t, env, agentID)
	t.Logf("FINAL: wallet_usdc=%.6f main_account_usdc=%.6f agent shares=%.6f (sharePrice %.6f, ~$%.2f)",
		balF.WalletUSDC, balF.MainUSDC, sharesF, priceF, sharesF*priceF)
}

// restoreFixture sends the withdrawn USDC back: on-chain wallet -> master,
// then Main Account -> agent, so the fixture ends near its starting state.
// Failures are reported, not fatal — the final-state log tells the truth.
func restoreFixture(t *testing.T, env *testEnv, sf *solfund.Client, agentID string, walletAmount, shares0, price0 float64) {
	t.Helper()
	t.Log("RESTORE — returning funds to the fixture")

	master, err := env.svc.GetMasterWallet(env.ctx, env.walletAddress)
	if err != nil {
		t.Errorf("restore: get master wallet: %v", err)
		return
	}
	walletPub := solana.MustPublicKeyFromBase58(env.walletAddress)
	masterPub := solana.MustPublicKeyFromBase58(master)

	// 4a. on-chain wallet -> master (the $3 that was paid out)
	tx, err := sf.BuildUSDCTransfer(env.ctx, walletPub, masterPub, solfund.ToRaw(walletAmount))
	if err != nil {
		t.Errorf("restore: build transfer: %v", err)
		return
	}
	if _, err := tx.Sign(func(k solana.PublicKey) *solana.PrivateKey {
		if k.Equals(walletPub) {
			pk := solana.PrivateKey(env.privKey)
			return &pk
		}
		return nil
	}); err != nil {
		t.Errorf("restore: sign transfer: %v", err)
		return
	}
	sig, err := sf.Submit(env.ctx, tx)
	if err != nil {
		t.Errorf("restore: submit transfer: %v (sig=%s)", err, sig)
		return
	}
	t.Logf("  restore: on-chain transfer %.2f USDC wallet -> master confirmed: %s", walletAmount, sig)

	// Envy sweeps the on-chain master balance into the ledger — give it a moment.
	time.Sleep(15 * time.Second)

	// 4b. Main Account -> agent: top the stake back up to ~shares0.
	sharesNow, priceNow := getStakeSnapshot(t, env, agentID)
	if priceNow <= 0 {
		priceNow = price0
	}
	missingUSD := (shares0 - sharesNow) * priceNow
	if missingUSD <= 0.01 {
		t.Logf("  restore: stake already at %.6f shares — no deposit needed", sharesNow)
		return
	}
	t.Logf("  restore: deposit_to_agent %.4f USDC to rebuild ~%.2f shares (have %.4f)", missingUSD, shares0, sharesNow)
	dResp, dErr := callToolSoft(t, env.ctx, env.toolMap, "deposit_to_agent", map[string]any{
		"wallet_address": env.walletAddress,
		"agent_id":       agentID,
		"amount":         missingUSD,
	})
	if dErr {
		// A minimum-deposit rule may block small restores — report honestly.
		t.Errorf("restore: deposit_to_agent rejected (fixture left short ~%.4f USD in Main Account):\n%s", missingUSD, dResp)
		return
	}
	t.Logf("  restore: deposit_to_agent accepted:\n%s", dResp)
}

// --- snapshot + polling helpers ---------------------------------------------

type balSnapshot struct {
	WalletUSDC float64 `json:"wallet_usdc"`
	MainUSDC   float64 `json:"main_account_usdc"`
}

func getBalancesSnapshot(t *testing.T, env *testEnv) balSnapshot {
	t.Helper()
	resp := callTool(t, env.ctx, env.toolMap, "get_balances", map[string]any{
		"wallet_address": env.walletAddress,
	})
	var b balSnapshot
	mustUnmarshal(t, resp, &b)
	return b
}

func getStakeSnapshot(t *testing.T, env *testEnv, agentID string) (shares, sharePrice float64) {
	t.Helper()
	resp := callTool(t, env.ctx, env.toolMap, "get_my_agent_stats", map[string]any{
		"wallet_address": env.walletAddress,
		"agent_id":       agentID,
	})
	var data struct {
		UserShares struct {
			Shares     float64 `json:"shares"`
			SharePrice float64 `json:"sharePrice"`
		} `json:"userShares"`
		Vault struct {
			SharePrice float64 `json:"sharePrice"`
		} `json:"vault"`
	}
	mustUnmarshal(t, resp, &data)
	price := data.UserShares.SharePrice
	if price == 0 {
		price = data.Vault.SharePrice
	}
	return data.UserShares.Shares, price
}

// waitForMainDelta polls get_balances until main_account_usdc >= base+minDelta
// or the deadline passes; returns the last snapshot either way.
func waitForMainDelta(t *testing.T, env *testEnv, base, minDelta float64, timeout time.Duration) balSnapshot {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last balSnapshot
	for {
		last = getBalancesSnapshot(t, env)
		if last.MainUSDC >= base+minDelta || time.Now().After(deadline) {
			return last
		}
		time.Sleep(6 * time.Second)
	}
}

// pollWithdrawalToTerminal polls get_agent_withdrawal_status until a terminal
// status or the deadline; returns the last observed status string.
func pollWithdrawalToTerminal(t *testing.T, env *testEnv, txID string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	last := ""
	for {
		resp, isErr := callToolSoft(t, env.ctx, env.toolMap, "get_agent_withdrawal_status", map[string]any{
			"wallet_address": env.walletAddress,
			"transaction_id": txID,
		})
		if !isErr {
			var st struct {
				Status string `json:"status"`
			}
			if err := json.Unmarshal([]byte(resp), &st); err == nil && st.Status != "" {
				if st.Status != last {
					t.Logf("  status: %s", st.Status)
				}
				last = st.Status
				if isTerminalStatus(last) {
					return last
				}
			}
		}
		if time.Now().After(deadline) {
			return last
		}
		time.Sleep(5 * time.Second)
	}
}

func isTerminalStatus(status string) bool {
	switch status {
	case "COMPLETED", "FAILED", "CANCELLED", "CANCELED", "REJECTED", "CONFIRMED", "SUCCESS", "ERROR", "FAILURE":
		return true
	}
	return false
}

// extractWithdrawTxID digs the transaction id out of an AgentWithdrawResult
// ({withdraw:{transactionId}}) or a flat map.
func extractWithdrawTxID(resp string) string {
	var m map[string]any
	if err := json.Unmarshal([]byte(resp), &m); err != nil {
		return ""
	}
	if wd, ok := m["withdraw"].(map[string]any); ok {
		if v, ok := wd["transactionId"].(string); ok && v != "" {
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
