package tools_test

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"mcp-server/app/internal/service"
	"mcp-server/app/internal/tools"

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

	ctx := context.Background()
	env := &testEnv{ctx: ctx, toolMap: toolMap, walletAddress: walletAddress, privKey: privKeyBytes}

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
		sig := phantomSignMessage(t, privKeyBytes, msgBase64)

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

	keySig := phantomSignMessage(t, privKeyBytes, keyData["message_base64"])

	t.Log("auth — complete_orderly_key")
	callTool(t, ctx, toolMap, "complete_orderly_key", map[string]any{
		"wallet_address": walletAddress,
		"signature":      keySig,
	})
	t.Log("  authentication complete")

	return env
}

// ---------------------------------------------------------------------------
// TestAuthAndDeposit — full auth + deposit tx preparation
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
	t.Logf("  transaction ready for Phantom sign_transaction (len=%d bytes)",
		len(depositData["transaction_base64"].(string)))
}

// ---------------------------------------------------------------------------
// TestGetWithdrawNonce — auth + get settle nonce only (isolates Orderly API)
// ---------------------------------------------------------------------------

func TestGetWithdrawNonce(t *testing.T) {
	env := setupAndAuth(t)

	t.Log("get_withdraw_nonce — testing Orderly /v1/settle_nonce")
	start := time.Now()
	nonceResp := callTool(t, env.ctx, env.toolMap, "get_withdraw_nonce", nil)
	elapsed := time.Since(start)

	var nonceData map[string]any
	mustUnmarshal(t, nonceResp, &nonceData)
	t.Logf("  withdraw_nonce=%v (took %v)", nonceData["withdraw_nonce"], elapsed)
	if elapsed > 5*time.Second {
		t.Logf("  WARNING: settle_nonce took >5s — Orderly API may be slow from this host")
	}
}

// ---------------------------------------------------------------------------
// TestWithdraw — auth + prepare 1.5 USDC withdraw
// ---------------------------------------------------------------------------

func TestWithdraw(t *testing.T) {
	env := setupAndAuth(t)

	t.Log("withdraw — prepare_orderly_withdraw (1.5 USDC)")
	withdrawResp := callTool(t, env.ctx, env.toolMap, "prepare_orderly_withdraw", map[string]any{
		"wallet_address": env.walletAddress,
		"token":          "USDC",
		"amount":         float64(1_500_000),
	})

	var withdrawData map[string]any
	mustUnmarshal(t, withdrawResp, &withdrawData)
	t.Logf("  transaction ready for Phantom sign_transaction (len=%d bytes)",
		len(withdrawData["transaction_base64"].(string)))
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

func phantomSignMessage(t *testing.T, privKey []byte, msgBase64 string) string {
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
