package tools_test

import (
	"fmt"
	"strings"
	"testing"
)

// TestSmokeAllReadOnly exercises every read-only / prepare tool and asserts each
// returns without a handler error. These move no funds.
//
// Run: go test ./app/internal/tools/ -run TestSmokeAllReadOnly -v
func TestSmokeAllReadOnly(t *testing.T) {
	env := setupAndAuth(t)
	w := env.walletAddress
	agentID := envOr("AGENT_ID", "1b5d9ef1-562e-41fe-be09-ee2002550e6b")
	txID := envOr("WITHDRAW_TX_ID", "fa8010f2-ddbe-46c1-baa5-4271c70072d4")

	cases := []struct {
		tool string
		args map[string]any
	}{
		{"health", map[string]any{}},
		{"get_markets", map[string]any{}},
		{"get_positions", map[string]any{}},
		{"get_algo_orders", map[string]any{}},
		{"get_balances", map[string]any{"wallet_address": w}},
		{"get_master_wallet", map[string]any{"wallet_address": w}},
		{"list_agents", map[string]any{"wallet_address": w}},
		{"get_agent_details", map[string]any{"wallet_address": w, "agent_id": agentID}},
		{"get_agent_positions", map[string]any{"wallet_address": w, "agent_id": agentID}},
		{"get_my_agent_stats", map[string]any{"wallet_address": w, "agent_id": agentID}},
		{"get_my_portfolio", map[string]any{"wallet_address": w}},
		{"get_agent_transactions", map[string]any{"wallet_address": w, "agent_id": agentID}},
		{"get_agent_withdrawal_status", map[string]any{"wallet_address": w, "transaction_id": txID}},
		{"get_agent_deposit_status", map[string]any{"wallet_address": w, "transaction_id": txID}},
		{"get_user_points", map[string]any{"public_key": w}},
		{"get_leaderboard", map[string]any{"public_key": w}},
		{"get_agent_chat_history", map[string]any{"wallet_address": w, "agent_id": agentID}},
		// auth-dependent prepares FIRST (need the live orderly key from setupAndAuth)
		{"prepare_agreement", map[string]any{"wallet_address": w}},
		{"prepare_main_deposit", map[string]any{"wallet_address": w, "amount": 1.0}},
		{"prepare_orderly_deposit", map[string]any{"wallet_address": w, "symbol": "USDC", "amount": 1_000_000.0}},
		{"prepare_orderly_withdraw", map[string]any{"wallet_address": w, "token": "USDC", "amount": 1_000_000.0}},
		// stateful auth-flow prepares LAST (they reset pending registration/key state)
		{"prepare_registration", map[string]any{"wallet_address": w}},
		{"prepare_orderly_key", map[string]any{"wallet_address": w}},
	}

	var pass, fail int
	var summary []string
	for _, c := range cases {
		text, isErr := callToolSoft(t, env.ctx, env.toolMap, c.tool, c.args)
		snippet := strings.ReplaceAll(text, "\n", " ")
		if len(snippet) > 90 {
			snippet = snippet[:90]
		}
		if isErr {
			fail++
			summary = append(summary, fmt.Sprintf("FAIL %-28s %s", c.tool, snippet))
		} else {
			pass++
			summary = append(summary, fmt.Sprintf("OK   %-28s %s", c.tool, snippet))
		}
	}
	for _, s := range summary {
		t.Log(s)
	}
	t.Logf("READ-ONLY SMOKE: %d OK, %d FAIL", pass, fail)
	if fail > 0 {
		t.Fatalf("%d read-only tools failed", fail)
	}
}

// TestSmokeMutations exercises the state-changing Orderly trading tools with
// valid-shaped args. A clean Orderly business response (success OR a proper
// rejection like min_notional / order-not-found) means the tool round-trips
// correctly. Logs every response for inspection.
func TestSmokeMutations(t *testing.T) {
	env := setupAndAuth(t)
	agentID := envOr("AGENT_ID", "1b5d9ef1-562e-41fe-be09-ee2002550e6b")

	type call struct {
		label string
		tool  string
		args  map[string]any
	}
	calls := []call{
		// far-from-market limit buy, tiny size: either rests (cancelable) or a clean margin/min_notional reject
		{"create_order (limit, far)", "create_order", map[string]any{
			"symbol": "PERP_BTC_USDC", "order_type": "LIMIT", "side": "BUY",
			"order_quantity": 0.0001, "order_price": 10000.0,
		}},
		{"cancel_order (bogus id)", "cancel_order", map[string]any{
			"symbol": "PERP_BTC_USDC", "order_id": 1,
		}},
		{"set_position_tp_sl (no pos)", "set_position_tp_sl", map[string]any{
			"symbol": "PERP_BTC_USDC", "take_profit_price": 200000.0, "stop_loss_price": 10000.0,
		}},
		{"cancel_algo_order (bogus id)", "cancel_algo_order", map[string]any{
			"symbol": "PERP_BTC_USDC", "algo_order_id": 1,
		}},
		{"send_agent_message (help)", "send_agent_message", map[string]any{
			"wallet_address": env.walletAddress, "agent_id": agentID, "message": "help",
		}},
	}
	for _, c := range calls {
		text, isErr := callToolSoft(t, env.ctx, env.toolMap, c.tool, c.args)
		t.Logf("[%s] isErr=%v\n%s\n", c.label, isErr, text)
	}
}
