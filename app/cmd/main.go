package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"mcp-server/app/internal/service"
	"mcp-server/app/internal/tools"

	"github.com/mark3labs/mcp-go/server"
)

func main() {
	svc := service.NewService(service.Config{
		OrderlyBaseURL:   "https://api.orderly.org",
		PerptoolsBaseURL: "https://app.perptools.ai/api",
		BrokerID:         "dextools",
		SolanaRPCURL:     envOrDefault("SOLANA_RPC_URL", "https://api.mainnet-beta.solana.com"),
	})

	s := server.NewMCPServer(
		"perptools-mcp",
		"1.0.0",
		server.WithToolCapabilities(true),
		server.WithInstructions(`You are connected to the Perptools/Orderly MCP server for Solana perpetual futures trading.

PREREQUISITE — Solana wallet access:
This server requires the ability to sign messages and transactions with a Solana wallet. You MUST have access to either a wallet provider (e.g. connected wallet MCP, wallet extension) or the user's private key. Before starting any auth or trading flow, CHECK if you have a way to sign with the user's Solana wallet. If not, ask the user to provide a connected wallet or configure wallet access for you.

AUTHENTICATION (required before trading AND withdraw):
1. Get the user's Solana wallet address (ask the user if needed).
2. Call prepare_registration with wallet_address → if already_registered=true, skip to step 4.
3. Sign the returned message_base64 with the user's Solana wallet → call complete_registration with the signature.
4. Call prepare_orderly_key with wallet_address.
5. Sign the returned message_base64 with the user's Solana wallet → call complete_orderly_key with the signature.
6. Authentication is complete. All trading tools and prepare_orderly_withdraw are now available.

TRADING TOOLS (require authentication):
- create_order        — place MARKET/LIMIT orders on PERP markets. Use order_quantity in base currency (ETH, BTC), NOT USDC.
- cancel_order       — cancel an open order by order_id.
- get_positions      — view all open positions, collateral, margin info.
- set_position_tp_sl  — set take-profit and stop-loss on existing position (one TP/SL order per symbol).
- get_algo_orders    — list TP/SL and other algo orders. Optional symbol filter.
- cancel_algo_order  — cancel TP/SL by algo_order_id.

MARKET DATA (no auth needed):
- get_markets       — list available trading pairs with prices.
- health            — check API status.

DEPOSIT/WITHDRAW:
- prepare_orderly_deposit  — build deposit transaction (no auth). Sign with user's Solana wallet, then submit.
- prepare_orderly_withdraw — build withdrawal transaction. REQUIRES AUTH — perform steps 1–6 first. If user wants to withdraw, start with authentication.

WORKFLOW EXAMPLE — Open a LONG ETH position:
1. Authenticate (steps 1-6 above).
2. Call get_markets to check current ETH price.
3. Call get_positions to check available collateral.
4. Call create_order with symbol=PERP_ETH_USDC, order_type=MARKET, side=BUY, order_quantity=0.005.
5. Call get_positions to confirm the position was opened.
6. Optionally: set_position_tp_sl with take_profit_price and stop_loss_price to protect the position.

To close a position: use create_order with the OPPOSITE side and reduce_only=true.`),
	)

	for _, t := range tools.RegisterAuthTools(svc) {
		s.AddTool(t.Tool, t.Handler)
	}
	for _, t := range tools.RegisterPerptoolsTools(svc) {
		s.AddTool(t.Tool, t.Handler)
	}
	for _, t := range tools.RegisterOrderlyTools(svc) {
		s.AddTool(t.Tool, t.Handler)
	}

	transport := strings.ToLower(envOrDefault("TRANSPORT", "sse"))

	switch transport {
	case "sse":
		addr := envOrDefault("ADDR", ":8080")
		basePath := envOrDefault("BASE_PATH", "/mcp")
		baseURL := envOrDefault("BASE_URL", "")

		opts := []server.SSEOption{
			server.WithStaticBasePath(basePath),
			server.WithKeepAliveInterval(30 * time.Second),
		}
		if baseURL != "" {
			opts = append(opts, server.WithBaseURL(baseURL))
		}

		sseServer := server.NewSSEServer(s, opts...)
		log.Printf("Starting SSE server on %s (base path: %s)", addr, basePath)
		if err := sseServer.Start(addr); err != nil {
			log.Fatalf("SSE server error: %v", err)
		}

	case "stdio":
		if err := server.ServeStdio(s); err != nil {
			fmt.Fprintf(os.Stderr, "mcp server error: %v\n", err)
			os.Exit(1)
		}

	default:
		log.Fatalf("Unknown transport: %s (supported: sse, stdio)", transport)
	}
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
