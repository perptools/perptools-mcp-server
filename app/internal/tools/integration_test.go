package tools_test

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"mcp-server/app/internal/service"
	"mcp-server/app/internal/tools"

	bin "github.com/gagliardetto/binary"
	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/mark3labs/mcp-go/mcp"
)

func init() {
	loadEnvFile("../../../.env")
}

// ---------------------------------------------------------------------------
// setup — shared across all integration tests
// ---------------------------------------------------------------------------

type testEnv struct {
	ctx           context.Context
	toolMap       map[string]func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)
	walletAddress string
	privKey       []byte
	privKeyBase58 string
	rpcClient     *rpc.Client
}

// setupAndAuth creates the service, registers all tools, and runs the full
// authentication flow (register account if needed + orderly key).
// Every integration test that needs auth should call this first.
func setupAndAuth(t *testing.T) *testEnv {
	t.Helper()

	privKeyBase58 := os.Getenv("SOLANA_PRIVATE_KEY")
	if privKeyBase58 == "" {
		t.Skip("SOLANA_PRIVATE_KEY not set — skipping integration test")
	}

	privKeyBytes := base58Decode(t, privKeyBase58)
	walletAddress := ed25519PubKeyToBase58(privKeyBytes)

	svc := service.NewService(service.Config{
		OrderlyBaseURL:   envOr("ORDERLY_BASE_URL", "https://api.orderly.org"),
		PerptoolsBaseURL: envOr("PERPTOOLS_BASE_URL", "https://app.perptools.ai/api"),
		BrokerID:         envOr("BROKER_ID", "dextools"),
		SolanaRPCURL:     envOr("SOLANA_RPC_URL", "https://api.mainnet-beta.solana.com"),
	})

	toolMap := make(map[string]func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error))
	for _, td := range tools.RegisterAuthTools(svc) {
		toolMap[td.Tool.Name] = td.Handler
	}
	for _, td := range tools.RegisterOrderlyTools(svc) {
		toolMap[td.Tool.Name] = td.Handler
	}
	for _, td := range tools.RegisterPerptoolsTools(svc) {
		toolMap[td.Tool.Name] = td.Handler
	}

	rpcURL := envOr("SOLANA_RPC_URL", "https://api.mainnet-beta.solana.com")
	ctx := context.Background()
	env := &testEnv{
		ctx:           ctx,
		toolMap:       toolMap,
		walletAddress: walletAddress,
		privKey:       privKeyBytes,
		privKeyBase58: privKeyBase58,
		rpcClient:     rpc.New(rpcURL),
	}

	// --- registration ---
	t.Logf("wallet: %s", walletAddress)
	t.Log("auth — prepare_registration")

	regResp := callTool(t, ctx, toolMap, "prepare_registration", map[string]any{
		"wallet_address": walletAddress,
	})

	var regData map[string]any
	mustUnmarshal(t, regResp, &regData)

	if alreadyReg, _ := regData["already_registered"].(bool); alreadyReg {
		t.Logf("  account already registered (account_id: %s)", regData["account_id"])
	} else {
		msgBase64, _ := regData["message_base64"].(string)
		sig := signMessageWithKey(t, privKeyBytes, msgBase64)

		t.Log("auth — complete_registration")
		callTool(t, ctx, toolMap, "complete_registration", map[string]any{
			"wallet_address": walletAddress,
			"signature":      sig,
		})
		t.Log("  registered")
	}

	// --- orderly key ---
	t.Log("auth — prepare_orderly_key")
	keyResp := callTool(t, ctx, toolMap, "prepare_orderly_key", map[string]any{
		"wallet_address": walletAddress,
	})

	var keyData map[string]string
	mustUnmarshal(t, keyResp, &keyData)

	keySig := signMessageWithKey(t, privKeyBytes, keyData["message_base64"])

	t.Log("auth — complete_orderly_key")
	callTool(t, ctx, toolMap, "complete_orderly_key", map[string]any{
		"wallet_address": walletAddress,
		"signature":      keySig,
	})
	t.Log("  authentication complete")

	return env
}

// ---------------------------------------------------------------------------
// TestAuthAndDeposit — full auth + deposit tx preparation, sign and send
// ---------------------------------------------------------------------------

func TestAuthAndDeposit(t *testing.T) {
	env := setupAndAuth(t)

	t.Log("deposit — prepare_orderly_deposit (1 USDC)")
	depositResp := callTool(t, env.ctx, env.toolMap, "prepare_orderly_deposit", map[string]any{
		"wallet_address": env.walletAddress,
		"symbol":         "USDC",
		"amount":         float64(1_000_000),
	})

	var depositData map[string]any
	mustUnmarshal(t, depositResp, &depositData)
	txBase64, _ := depositData["transaction_base64"].(string)
	t.Logf("  transaction prepared (len=%d bytes)", len(txBase64))

	// Decode and parse transaction
	txBytes, err := base64.StdEncoding.DecodeString(txBase64)
	if err != nil {
		t.Fatalf("decode tx base64: %v", err)
	}
	tx, err := solana.TransactionFromDecoder(bin.NewBinDecoder(txBytes))
	if err != nil {
		t.Fatalf("parse transaction: %v", err)
	}

	// Sign with private key from .env
	privKey, err := solana.PrivateKeyFromBase58(env.privKeyBase58)
	if err != nil {
		t.Fatalf("invalid private key: %v", err)
	}
	_, err = tx.Sign(func(key solana.PublicKey) *solana.PrivateKey {
		if privKey.PublicKey().Equals(key) {
			return &privKey
		}
		return nil
	})
	if err != nil {
		t.Fatalf("sign transaction: %v", err)
	}
	t.Log("  transaction signed")

	// Send to Solana (skip send if wallet has insufficient USDC)
	sig, err := env.rpcClient.SendTransaction(env.ctx, tx)
	if err != nil {
		if strings.Contains(err.Error(), "insufficient funds") {
			t.Logf("  send skipped: wallet has insufficient USDC (prepare+sign OK)")
			return
		}
		t.Fatalf("send transaction: %v", err)
	}
	t.Logf("  transaction sent: %s", sig.String())
}

// ---------------------------------------------------------------------------
// TestWithdraw — auth + prepare 1.5 USDC withdraw, sign tx, submit to Orderly API
// ---------------------------------------------------------------------------

func TestWithdraw(t *testing.T) {
	env := setupAndAuth(t)
	privKey, err := solana.PrivateKeyFromBase58(env.privKeyBase58)
	if err != nil {
		t.Fatalf("invalid private key: %v", err)
	}

	const maxRetries = 3 // nonce is single-use; retry if stale from parallel/prior run
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(2 * time.Second) // allow Orderly to update nonce after prior attempt
			t.Logf("  retry %d — fresh prepare (new nonce)", attempt+1)
		}

		t.Log("withdraw — prepare_orderly_withdraw (1.5 USDC)")
		withdrawResp := callTool(t, env.ctx, env.toolMap, "prepare_orderly_withdraw", map[string]any{
			"wallet_address": env.walletAddress,
			"token":          "USDC",
			"amount":         float64(1_500_000),
		})

		var withdrawData map[string]any
		mustUnmarshal(t, withdrawResp, &withdrawData)

		txBase64, _ := withdrawData["transaction_base64"].(string)
		msgObj, _ := withdrawData["message"].(map[string]any)
		if txBase64 == "" || msgObj == nil {
			t.Fatalf("prepare response missing transaction_base64 or message")
		}
		t.Logf("  [debug] withdrawNonce=%v", msgObj["withdrawNonce"])

		txBytes, err := base64.StdEncoding.DecodeString(txBase64)
		if err != nil {
			t.Fatalf("decode tx base64: %v", err)
		}

		tx, err := solana.TransactionFromDecoder(bin.NewBinDecoder(txBytes))
		if err != nil {
			t.Fatalf("parse transaction: %v", err)
		}

		_, err = tx.Sign(func(key solana.PublicKey) *solana.PrivateKey {
			if privKey.PublicKey().Equals(key) {
				return &privKey
			}
			return nil
		})
		if err != nil {
			t.Fatalf("sign transaction: %v", err)
		}
		if len(tx.Signatures) == 0 {
			t.Fatalf("no signatures after sign")
		}
		sigHex := "0x" + hex.EncodeToString(tx.Signatures[0][:])

		// Ensure types: chainId as number, withdrawNonce/timestamp/amount as string (per Orderly API)
		chainID, _ := toNumber(msgObj["chainId"])
		submitArgs := map[string]any{
			"signature":      sigHex,
			"broker_id":      toString(msgObj["brokerId"]),
			"chain_id":       chainID,
			"receiver":       toString(msgObj["receiver"]),
			"token":          toString(msgObj["token"]),
			"amount":         toString(msgObj["amount"]),
			"withdraw_nonce": toString(msgObj["withdrawNonce"]),
			"timestamp":      toString(msgObj["timestamp"]),
			"chain_type":     "SOL",
		}

		submitResp, submitErr := callToolOrError(t, env.ctx, env.toolMap, "submit_orderly_withdraw", submitArgs)
		t.Logf("  submit response: %s", submitResp)
		if submitErr == nil {
			return // success
		}
		if !strings.Contains(submitResp, "Nonce error") {
			t.Fatalf("submit_orderly_withdraw failed: %s", submitResp)
		}
	}
	t.Fatalf("submit_orderly_withdraw failed with Nonce error after %d attempts (nonce may be consumed by parallel run)", maxRetries)
}

func callToolOrError(t *testing.T, ctx context.Context, toolMap map[string]func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error), name string, args map[string]any) (string, error) {
	t.Helper()
	handler, ok := toolMap[name]
	if !ok {
		return "", fmt.Errorf("tool %q not registered", name)
	}
	req := mcp.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args
	result, err := handler(ctx, req)
	if err != nil {
		return "", err
	}
	text := extractText(result)
	if result.IsError {
		return text, fmt.Errorf("%s", text)
	}
	return text, nil
}

func keysOf(m map[string]any) []string {
	var k []string
	for key := range m {
		k = append(k, key)
	}
	return k
}

func toString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func toNumber(v any) (float64, bool) {
	if v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

// ---------------------------------------------------------------------------
// TestOpenETHLong — auth + MARKET BUY PERP_ETH_USDC for 11 USDC
// ---------------------------------------------------------------------------

func TestOpenETHLong(t *testing.T) {
	env := setupAndAuth(t)

	// check positions before
	t.Log("positions before order:")
	posResp := callTool(t, env.ctx, env.toolMap, "get_positions", nil)
	t.Logf("  %s", posResp)

	// open LONG ETH: MARKET BUY 0.005 ETH (~$11 at ~$2200)
	t.Log("create_order — MARKET BUY 0.005 PERP_ETH_USDC")

	orderResp := callTool(t, env.ctx, env.toolMap, "create_order", map[string]any{
		"symbol":         "PERP_ETH_USDC",
		"order_type":     "MARKET",
		"side":           "BUY",
		"order_quantity": float64(0.005),
	})
	t.Logf("  order response: %s", orderResp)

	// check positions after
	t.Log("positions after order:")
	posAfter := callTool(t, env.ctx, env.toolMap, "get_positions", nil)
	t.Logf("  %s", posAfter)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func callTool(
	t *testing.T,
	ctx context.Context,
	toolMap map[string]func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error),
	name string,
	args map[string]any,
) string {
	t.Helper()

	handler, ok := toolMap[name]
	if !ok {
		t.Fatalf("tool %q not registered", name)
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args

	result, err := handler(ctx, req)
	if err != nil {
		t.Fatalf("tool %q returned error: %v", name, err)
	}
	if result.IsError {
		t.Fatalf("tool %q failed: %s", name, extractText(result))
	}

	return extractText(result)
}

func extractText(r *mcp.CallToolResult) string {
	var sb strings.Builder
	for _, c := range r.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

func signMessageWithKey(t *testing.T, privKey []byte, msgBase64 string) string {
	t.Helper()
	msgBytes, err := base64.StdEncoding.DecodeString(msgBase64)
	if err != nil {
		t.Fatalf("decode base64 message: %v", err)
	}
	sig := ed25519.Sign(ed25519.PrivateKey(privKey), msgBytes)
	return "0x" + hex.EncodeToString(sig)
}

func mustUnmarshal(t *testing.T, data string, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(data), v); err != nil {
		t.Fatalf("unmarshal tool response: %v\nraw: %s", err, data)
	}
}

func base58Decode(t *testing.T, s string) []byte {
	t.Helper()
	const alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
	result := make([]byte, 0, 64)
	for i := 0; i < len(s); i++ {
		carry := strings.IndexByte(alphabet, s[i])
		if carry < 0 {
			t.Fatalf("invalid base58 character: %c", s[i])
		}
		for j := 0; j < len(result); j++ {
			carry += int(result[j]) * 58
			result[j] = byte(carry & 0xff)
			carry >>= 8
		}
		for carry > 0 {
			result = append(result, byte(carry&0xff))
			carry >>= 8
		}
	}
	for i := 0; i < len(s) && s[i] == '1'; i++ {
		result = append(result, 0)
	}
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result
}

func ed25519PubKeyToBase58(privKey []byte) string {
	const alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
	pub := privKey[32:]
	digits := []byte{0}
	for _, b := range pub {
		carry := int(b)
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
	for _, b := range pub {
		if b != 0 {
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

func loadEnvFile(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if os.Getenv(k) == "" {
			os.Setenv(k, v)
		}
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
