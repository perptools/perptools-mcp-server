package tools_test

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/gagliardetto/solana-go"
)

// --- local signing helpers (the empirical flow needs the wallet to sign) ---

// signTxBase64 signs an unsigned Solana tx (base64) with the wallet key and
// returns the fully-signed tx as base64, ready to broadcast.
func signTxBase64(t *testing.T, privKey []byte, b64 string) string {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("decode tx: %v", err)
	}
	tx, err := solana.TransactionFromBytes(raw)
	if err != nil {
		t.Fatalf("parse tx: %v", err)
	}
	key := solana.PrivateKey(privKey)
	if _, err := tx.Sign(func(p solana.PublicKey) *solana.PrivateKey {
		if key.PublicKey().Equals(p) {
			return &key
		}
		return nil
	}); err != nil {
		t.Fatalf("sign tx: %v", err)
	}
	out, err := tx.ToBase64()
	if err != nil {
		t.Fatalf("serialize signed tx: %v", err)
	}
	return out
}

// signTxFirstSigHex signs the tx and returns its first signature as 0x-hex,
// which is what the Orderly withdraw API expects.
func signTxFirstSigHex(t *testing.T, privKey []byte, b64 string) string {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("decode tx: %v", err)
	}
	tx, err := solana.TransactionFromBytes(raw)
	if err != nil {
		t.Fatalf("parse tx: %v", err)
	}
	key := solana.PrivateKey(privKey)
	if _, err := tx.Sign(func(p solana.PublicKey) *solana.PrivateKey {
		if key.PublicKey().Equals(p) {
			return &key
		}
		return nil
	}); err != nil {
		t.Fatalf("sign tx: %v", err)
	}
	if len(tx.Signatures) == 0 {
		t.Fatalf("no signatures produced")
	}
	return "0x" + hex.EncodeToString(tx.Signatures[0][:])
}

func balField(t *testing.T, balJSON, field string) float64 {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(balJSON), &m); err != nil {
		t.Fatalf("parse balances: %v\n%s", err, balJSON)
	}
	f, _ := m[field].(float64)
	return f
}

// TestEmpiricalStep2WithdrawToWallet moves the Main Account balance on-chain to
// the wallet (server-signed). Run after the agent withdrawal settles.
func TestEmpiricalStep2WithdrawToWallet(t *testing.T) {
	env := setupAndAuth(t)

	bal, _ := callToolSoft(t, env.ctx, env.toolMap, "get_balances", map[string]any{"wallet_address": env.walletAddress})
	main := balField(t, bal, "main_account_usdc")
	t.Logf("main_account_usdc=%.6f", main)
	if main <= 0 {
		t.Skip("Main Account empty — agent withdrawal not settled yet")
	}

	res, isErr := callToolSoft(t, env.ctx, env.toolMap, "withdraw_to_wallet", map[string]any{
		"wallet_address": env.walletAddress,
		"amount":         main,
	})
	if isErr {
		t.Fatalf("withdraw_to_wallet rejected:\n%s", res)
	}
	t.Logf("withdraw_to_wallet result:\n%s", res)
}

// TestEmpiricalStep3OrderlyDeposit is THE empirical proof of the 6019 send-path
// fix: it deposits the wallet's USDC into the Orderly trading vault for real.
// prepare -> sign -> complete (broadcast). solfund.Submit waits for on-chain
// confirmation and errors if the tx fails, so a clean result == the 3-DVN send
// plumbing is correct end to end.
func TestEmpiricalStep3OrderlyDeposit(t *testing.T) {
	env := setupAndAuth(t)

	bal, _ := callToolSoft(t, env.ctx, env.toolMap, "get_balances", map[string]any{"wallet_address": env.walletAddress})
	walletUSDC := balField(t, bal, "wallet_usdc")
	t.Logf("wallet_usdc=%.6f", walletUSDC)
	if walletUSDC <= 0 {
		t.Skip("wallet has no USDC yet — run step 2 first")
	}
	amount := uint64(walletUSDC * 1e6) // deposit the full on-chain balance

	prep, isErr := callToolSoft(t, env.ctx, env.toolMap, "prepare_orderly_deposit", map[string]any{
		"wallet_address": env.walletAddress,
		"symbol":         "USDC",
		"amount":         float64(amount),
	})
	if isErr {
		t.Fatalf("prepare_orderly_deposit rejected:\n%s", prep)
	}
	var pr struct {
		TransactionBase64 string `json:"transaction_base64"`
		NativeFee         uint64 `json:"native_fee"`
	}
	if err := json.Unmarshal([]byte(prep), &pr); err != nil || pr.TransactionBase64 == "" {
		t.Fatalf("bad prepare response: %v\n%s", err, prep)
	}
	t.Logf("prepared deposit of %d base units (native_fee=%d lamports)", amount, pr.NativeFee)

	signed := signTxBase64(t, env.privKey, pr.TransactionBase64)

	res, isErr := callToolSoft(t, env.ctx, env.toolMap, "complete_orderly_deposit", map[string]any{
		"signed_transaction": signed,
	})
	if isErr {
		t.Fatalf("complete_orderly_deposit FAILED (send-path bug if 6019/ULN):\n%s", res)
	}
	t.Logf("DEPOSIT CONFIRMED ON-CHAIN:\n%s", res)
}

// TestEmpiricalStep4OrderlyWithdraw withdraws from the Orderly trading vault for
// real: prepare -> sign (0x-hex) -> submit. Proves submit_orderly_withdraw.
// AMOUNT_USDC env (default 1) sets the amount in whole USDC.
func TestEmpiricalStep4OrderlyWithdraw(t *testing.T) {
	env := setupAndAuth(t)
	amountStr := envOr("AMOUNT_USDC", "1")

	prep, isErr := callToolSoft(t, env.ctx, env.toolMap, "prepare_orderly_withdraw", map[string]any{
		"wallet_address": env.walletAddress,
		"token":          "USDC",
		"amount":         parseUSDC(t, amountStr),
	})
	if isErr {
		t.Fatalf("prepare_orderly_withdraw rejected:\n%s", prep)
	}
	var pr struct {
		TransactionBase64 string `json:"transaction_base64"`
		Message           struct {
			BrokerID      string `json:"brokerId"`
			ChainID       int    `json:"chainId"`
			Receiver      string `json:"receiver"`
			Token         string `json:"token"`
			Amount        string `json:"amount"`
			WithdrawNonce string `json:"withdrawNonce"`
			Timestamp     string `json:"timestamp"`
			ChainType     string `json:"chainType"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(prep), &pr); err != nil || pr.TransactionBase64 == "" {
		t.Fatalf("bad prepare response: %v\n%s", err, prep)
	}

	sigHex := signTxFirstSigHex(t, env.privKey, pr.TransactionBase64)

	res, isErr := callToolSoft(t, env.ctx, env.toolMap, "submit_orderly_withdraw", map[string]any{
		"signature":      sigHex,
		"broker_id":      pr.Message.BrokerID,
		"chain_id":       float64(pr.Message.ChainID),
		"receiver":       pr.Message.Receiver,
		"token":          pr.Message.Token,
		"amount":         pr.Message.Amount,
		"withdraw_nonce": pr.Message.WithdrawNonce,
		"timestamp":      pr.Message.Timestamp,
		"chain_type":     pr.Message.ChainType,
	})
	if isErr {
		t.Fatalf("submit_orderly_withdraw FAILED:\n%s", res)
	}
	t.Logf("ORDERLY WITHDRAW SUBMITTED:\n%s", res)
}

func parseUSDC(t *testing.T, whole string) float64 {
	t.Helper()
	switch whole {
	case "1":
		return 1_000_000
	case "2":
		return 2_000_000
	case "5":
		return 5_000_000
	case "10":
		return 10_000_000
	default:
		return 1_000_000
	}
}
