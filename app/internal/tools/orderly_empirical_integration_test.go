package tools_test

import (
	"os"
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

// TestEmpiricalState is a READ-ONLY snapshot before any fund movement: wallet
// USDC + SOL, Main Account USDC, and the agent portfolio. It tells us what is
// available to fund the empirical Orderly deposit/withdraw test.
//
// Run: go test ./app/internal/tools/ -run TestEmpiricalState -v
func TestEmpiricalState(t *testing.T) {
	env := setupAndAuth(t)

	bal, _ := callToolSoft(t, env.ctx, env.toolMap, "get_balances", map[string]any{
		"wallet_address": env.walletAddress,
	})
	t.Logf("get_balances:\n%s", bal)

	port, _ := callToolSoft(t, env.ctx, env.toolMap, "get_my_portfolio", map[string]any{
		"wallet_address": env.walletAddress,
	})
	t.Logf("get_my_portfolio:\n%s", port)

	agentID := envOr("AGENT_ID", "1b5d9ef1-562e-41fe-be09-ee2002550e6b")
	stats, _ := callToolSoft(t, env.ctx, env.toolMap, "get_my_agent_stats", map[string]any{
		"wallet_address": env.walletAddress,
		"agent_id":       agentID,
	})
	t.Logf("get_my_agent_stats:\n%s", stats)

	// on-chain SOL (needed to pay LayerZero native fee + tx fees for a deposit)
	rpcURL := os.Getenv("SOLANA_RPC_URL")
	if rpcURL == "" {
		rpcURL = "https://api.mainnet-beta.solana.com"
	}
	cl := rpc.New(rpcURL)
	wallet := solana.MustPublicKeyFromBase58(env.walletAddress)
	solBal, err := cl.GetBalance(env.ctx, wallet, rpc.CommitmentFinalized)
	if err != nil {
		t.Logf("SOL balance error: %v", err)
	} else {
		t.Logf("wallet SOL lamports=%d (%.6f SOL)", solBal.Value, float64(solBal.Value)/1e9)
	}
}
