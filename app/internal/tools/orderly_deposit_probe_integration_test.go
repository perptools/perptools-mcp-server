package tools_test

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

// TestPrepareOrderlyDepositLive exercises the restored prepare_orderly_deposit
// tool against PROD. It is NON-DESTRUCTIVE: it only builds the unsigned deposit
// transaction (live oappQuote fee simulation + recent blockhash against the
// Orderly Solana vault program) and asserts the shape. It NEVER signs or
// broadcasts, so no funds move — complete_orderly_deposit is intentionally not
// called here.
//
// Run with: go test ./app/internal/tools/ -run TestPrepareOrderlyDepositLive -v
func TestPrepareOrderlyDepositLive(t *testing.T) {
	env := setupAndAuth(t)

	text, isErr := callToolSoft(t, env.ctx, env.toolMap, "prepare_orderly_deposit", map[string]any{
		"wallet_address": env.walletAddress,
		"symbol":         "USDC",
		"amount":         1_000_000.0, // 1 USDC (float64, as JSON delivers it), never broadcast
	})
	if isErr {
		t.Fatalf("prepare_orderly_deposit rejected:\n%s", text)
	}

	var resp struct {
		TransactionBase64 string `json:"transaction_base64"`
		WalletAddress     string `json:"wallet_address"`
		Symbol            string `json:"symbol"`
		Amount            uint64 `json:"amount"`
		NativeFee         uint64 `json:"native_fee"`
	}
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("response is not the expected JSON: %v\n%s", err, text)
	}
	if resp.TransactionBase64 == "" {
		t.Fatalf("expected a non-empty unsigned transaction, got none:\n%s", text)
	}
	if resp.Symbol != "USDC" || resp.Amount != 1_000_000 {
		t.Errorf("echoed fields wrong: symbol=%q amount=%d", resp.Symbol, resp.Amount)
	}
	t.Logf("unsigned tx built: %d base64 chars, native_fee=%d lamports", len(resp.TransactionBase64), resp.NativeFee)
}

// TestPrepareOrderlyWithdrawLive exercises the restored prepare_orderly_withdraw
// tool against PROD. NON-DESTRUCTIVE: it fetches the withdraw nonce and builds
// the unsigned sign-message transaction; it does NOT call submit_orderly_withdraw
// (the step that actually moves funds), so nothing is withdrawn.
//
// Run: go test ./app/internal/tools/ -run TestPrepareOrderlyWithdrawLive -v
func TestPrepareOrderlyWithdrawLive(t *testing.T) {
	env := setupAndAuth(t)

	text, isErr := callToolSoft(t, env.ctx, env.toolMap, "prepare_orderly_withdraw", map[string]any{
		"wallet_address": env.walletAddress,
		"token":          "USDC",
		"amount":         1_000_000.0, // never submitted
	})
	if isErr {
		t.Fatalf("prepare_orderly_withdraw rejected:\n%s", text)
	}

	var resp struct {
		TransactionBase64 string `json:"transaction_base64"`
		Message           struct {
			BrokerID      string `json:"brokerId"`
			Receiver      string `json:"receiver"`
			Token         string `json:"token"`
			Amount        string `json:"amount"`
			WithdrawNonce string `json:"withdrawNonce"`
			Timestamp     string `json:"timestamp"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("response is not the expected JSON: %v\n%s", err, text)
	}
	if resp.TransactionBase64 == "" || resp.Message.WithdrawNonce == "" || resp.Message.Receiver != env.walletAddress {
		t.Fatalf("incomplete withdraw prepare: %s", text)
	}
	t.Logf("withdraw prepared: nonce=%s token=%s amount=%s tx=%d b64 chars",
		resp.Message.WithdrawNonce, resp.Message.Token, resp.Message.Amount, len(resp.TransactionBase64))
}

// TestSimulateOrderlyDepositLive proves the DEPOSIT (send) transaction's
// LayerZero account plumbing is correct WITHOUT moving funds: it builds the
// unsigned deposit tx via prepare_orderly_deposit, then SIMULATES it
// (sigVerify=false, replaceRecentBlockhash) on mainnet. A simulation never
// broadcasts and never changes state. The pass criterion is narrow: the run
// must get PAST the LayerZero ULN checks — i.e. it must NOT fail with error
// 6019 (InvalidAccountLength) or any other ULN account error. Failing later on
// the user's token balance/ATA is fine and expected for a wallet without USDC.
//
// Run: go test ./app/internal/tools/ -run TestSimulateOrderlyDepositLive -v
func TestSimulateOrderlyDepositLive(t *testing.T) {
	env := setupAndAuth(t)

	text, isErr := callToolSoft(t, env.ctx, env.toolMap, "prepare_orderly_deposit", map[string]any{
		"wallet_address": env.walletAddress,
		"symbol":         "USDC",
		"amount":         1_000_000.0,
	})
	if isErr {
		t.Fatalf("prepare_orderly_deposit rejected:\n%s", text)
	}
	var resp struct {
		TransactionBase64 string `json:"transaction_base64"`
	}
	if err := json.Unmarshal([]byte(text), &resp); err != nil || resp.TransactionBase64 == "" {
		t.Fatalf("no transaction in response: %v\n%s", err, text)
	}

	raw, err := base64.StdEncoding.DecodeString(resp.TransactionBase64)
	if err != nil {
		t.Fatalf("decode tx: %v", err)
	}
	tx, err := solana.TransactionFromBytes(raw)
	if err != nil {
		t.Fatalf("parse tx: %v", err)
	}

	rpcURL := os.Getenv("SOLANA_RPC_URL")
	if rpcURL == "" {
		rpcURL = "https://api.mainnet-beta.solana.com"
	}
	cl := rpc.New(rpcURL)
	sim, err := cl.SimulateTransactionWithOpts(env.ctx, tx, &rpc.SimulateTransactionOpts{
		SigVerify:              false,
		ReplaceRecentBlockhash: true,
	})
	if err != nil {
		t.Fatalf("simulate RPC error: %v", err)
	}

	var logs string
	if sim != nil && sim.Value != nil {
		logs = strings.Join(sim.Value.Logs, "\n")
	}
	t.Logf("deposit simulation err=%v", sim.Value.Err)
	t.Logf("logs:\n%s", logs)

	// Hard fail ONLY on a LayerZero ULN account error — that is the bug we fixed.
	if strings.Contains(logs, "6019") || strings.Contains(logs, "InvalidAccountLength") ||
		strings.Contains(logs, "InvalidDvn") || strings.Contains(logs, "InvalidExecutor") {
		t.Fatalf("LayerZero account plumbing still wrong:\n%s", logs)
	}
}
