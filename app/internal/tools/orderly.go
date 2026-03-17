package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"mcp-server/app/internal/clients/orderly"
	"mcp-server/app/internal/service"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func RegisterOrderlyTools(svc *service.Service) []ToolDef {
	return []ToolDef{
		{
			Tool: mcp.NewTool("prepare_orderly_deposit",
				mcp.WithDescription("Build an unsigned Solana transaction that deposits tokens into the Orderly vault via LayerZero. The LayerZero cross-chain fee is fetched automatically via oappQuote simulation. Returns base64-encoded transaction for wallet signing."),
				mcp.WithString("wallet_address", mcp.Required(), mcp.Description("Solana wallet public key (base58)")),
				mcp.WithString("symbol", mcp.Required(), mcp.Description("Token symbol: USDC, USDT, or SOL")),
				mcp.WithNumber("amount", mcp.Required(), mcp.Description("Amount in smallest token units (e.g. lamports for SOL, 1e6 units for USDC)")),
			),
			Handler: prepareOrderlyDeposit(svc),
		},
		{
			Tool: mcp.NewTool("get_withdraw_nonce",
				mcp.WithDescription("Get the current withdraw (settle) nonce from Orderly. Requires authentication. Used for debugging or when prepare_orderly_withdraw fetches it automatically."),
			),
			Handler: getWithdrawNonce(svc),
		},
		{
			Tool: mcp.NewTool("prepare_orderly_withdraw",
				mcp.WithDescription("Build an unsigned Solana memo transaction for withdrawal. Fetches withdraw nonce automatically. Returns base64-encoded transaction for wallet signing."),
				mcp.WithString("wallet_address", mcp.Required(), mcp.Description("Solana wallet public key (base58)")),
				mcp.WithString("token", mcp.Required(), mcp.Description("Token symbol: USDC, USDT, or SOL")),
				mcp.WithNumber("amount", mcp.Required(), mcp.Description("Amount in smallest token units (e.g. 1500000 for 1.5 USDC)")),
			),
			Handler: prepareOrderlyWithdraw(svc),
		},
		{
			Tool: mcp.NewTool("create_order",
				mcp.WithDescription(`Place a new order on Orderly. Requires authentication.

IMPORTANT: All PERP markets use order_quantity in BASE currency (ETH, BTC, etc.), NOT in USDC.
If the user says "buy $100 of ETH", you must convert: order_quantity = desired_usdc / current_price.
Use get_markets to look up the current mark_price for the symbol.

Order types:
  MARKET  — executes immediately at best available price. Only needs: symbol, side, order_quantity.
  LIMIT   — executes at order_price or better. Needs: symbol, side, order_quantity, order_price.
  POST_ONLY — like LIMIT but guaranteed to be maker (no taker fees). Needs same as LIMIT.
  IOC     — fills as much as possible at order_price, cancels the rest.
  FOK     — fills entirely at order_price or cancels entirely.
  ASK/BID — executes at the best ask/bid price.

MARKET order examples:
  Open LONG  0.005 ETH → symbol=PERP_ETH_USDC, order_type=MARKET, side=BUY,  order_quantity=0.005
  Open SHORT 0.001 BTC → symbol=PERP_BTC_USDC, order_type=MARKET, side=SELL, order_quantity=0.001

LIMIT order example:
  Buy 0.01 ETH at $2000 → symbol=PERP_ETH_USDC, order_type=LIMIT, side=BUY, order_quantity=0.01, order_price=2000

Closing positions:
  To close a position, use the OPPOSITE side with reduce_only=true.
  Close LONG  0.005 ETH → order_type=MARKET, side=SELL, order_quantity=0.005, reduce_only=true
  Close SHORT 0.001 BTC → order_type=MARKET, side=BUY,  order_quantity=0.001, reduce_only=true
  Use get_positions to see current position_qty. Absolute value of position_qty is the size to close.`),
				mcp.WithString("symbol", mcp.Required(), mcp.Description("Trading pair symbol (e.g. PERP_BTC_USDC, PERP_ETH_USDC)")),
				mcp.WithString("order_type", mcp.Required(), mcp.Description("Order type: LIMIT, MARKET, IOC, FOK, POST_ONLY, ASK, BID")),
				mcp.WithString("side", mcp.Required(), mcp.Description("Order side: BUY or SELL")),
				mcp.WithNumber("order_quantity", mcp.Required(), mcp.Description("Order size in base currency (e.g. 0.005 for 0.005 ETH, 0.001 for 0.001 BTC)")),
				mcp.WithNumber("order_price", mcp.Description("Order price. Required for LIMIT/IOC/FOK/POST_ONLY orders. Not needed for MARKET.")),
				mcp.WithBoolean("reduce_only", mcp.Description("If true, the order can only reduce an existing position. Use this to close positions.")),
			),
			Handler: createOrder(svc),
		},
		{
			Tool: mcp.NewTool("cancel_order",
				mcp.WithDescription("Cancel a single order by order_id. Requires authentication."),
				mcp.WithString("symbol", mcp.Required(), mcp.Description("Trading pair symbol (e.g. PERP_BTC_USDC)")),
				mcp.WithNumber("order_id", mcp.Required(), mcp.Description("The order_id to cancel")),
			),
			Handler: cancelOrder(svc),
		},
		{
			Tool: mcp.NewTool("get_positions",
				mcp.WithDescription(`Get all open positions with margin, PnL, and liquidation info. Requires authentication.

Present the response to the user as a formatted table:

Account Summary:
| Metric              | Value       |
|---------------------|-------------|
| Total Collateral    | $X,XXX.XX   |
| Free Collateral     | $X,XXX.XX   |
| Margin Ratio        | X.XX%       |
| Total PnL (24h)     | $X.XX       |

Open Positions:
| Symbol          | Side  | Size   | Entry Price | Mark Price | Unreal. PnL | Liq. Price | Leverage |
|-----------------|-------|--------|-------------|------------|-------------|------------|----------|
| PERP_BTC_USDC   | LONG  | 0.500  | $27,908.14  | $27,794.90 | -$354.86    | $117,335.93| 10x      |
| PERP_ETH_USDC   | SHORT | 2.000  | $1,850.00   | $1,842.50  | +$15.00     | $3,200.00  | 5x       |

Side is LONG when position_qty > 0, SHORT when position_qty < 0. Display absolute value for Size.
To close a position, use create_order with the opposite side and reduce_only=true.`),
			),
			Handler: getPositions(svc),
		},
		{
			Tool: mcp.NewTool("set_position_tp_sl",
				mcp.WithDescription(`Set take-profit and stop-loss on an existing position. Requires authentication.
Uses POSITIONAL_TP_SL: max 1 per user per symbol. Closes the full position when triggered.

LONG position: take_profit_price > entry price (profit when price rises), stop_loss_price < entry (limit loss when price falls).
SHORT position: take_profit_price < entry price, stop_loss_price > entry.

Call get_positions first to see current positions and entry prices. Only one active TP/SL order per symbol — use cancel_algo_order first if replacing.`),
				mcp.WithString("symbol", mcp.Required(), mcp.Description("Trading pair (e.g. PERP_ETH_USDC)")),
				mcp.WithNumber("take_profit_price", mcp.Required(), mcp.Description("Price at which to take profit (closes position)")),
				mcp.WithNumber("stop_loss_price", mcp.Required(), mcp.Description("Price at which to stop loss (closes position)")),
			),
			Handler: setPositionTPSL(svc),
		},
		{
			Tool: mcp.NewTool("get_algo_orders",
				mcp.WithDescription("List algo orders (TP/SL, etc.). Requires authentication. Pass symbol to filter by trading pair."),
				mcp.WithString("symbol", mcp.Description("Filter by symbol (e.g. PERP_ETH_USDC). Optional — omit to list all.")),
			),
			Handler: getAlgoOrders(svc),
		},
		{
			Tool: mcp.NewTool("cancel_algo_order",
				mcp.WithDescription("Cancel an algo order (e.g. TP/SL) by algo_order_id. Requires authentication. Use get_algo_orders to find the ID."),
				mcp.WithString("symbol", mcp.Required(), mcp.Description("Trading pair (e.g. PERP_ETH_USDC)")),
				mcp.WithNumber("algo_order_id", mcp.Required(), mcp.Description("The algo_order_id from get_algo_orders")),
			),
			Handler: cancelAlgoOrder(svc),
		},
	}
}

func prepareOrderlyDeposit(svc *service.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		wallet, err := req.RequireString("wallet_address")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		symbol, err := req.RequireString("symbol")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		amount := uint64(optNumber(req, "amount", 0))
		if amount == 0 {
			return mcp.NewToolResultError("amount is required and must be > 0"), nil
		}

		result, err := svc.PrepareOrderlyDeposit(ctx, wallet, symbol, amount)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("prepare deposit failed: %v", err)), nil
		}

		out, _ := json.Marshal(result)
		return mcp.NewToolResultText(string(out)), nil
	}
}

func createOrder(svc *service.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		symbol, err := req.RequireString("symbol")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		orderType, err := req.RequireString("order_type")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		side, err := req.RequireString("side")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		qty := optNumber(req, "order_quantity", 0)
		if qty == 0 {
			return mcp.NewToolResultError("order_quantity is required and must be > 0"), nil
		}

		orderReq := orderly.CreateOrderRequest{
			Symbol:        symbol,
			OrderType:     orderType,
			Side:          side,
			OrderQuantity: qty,
		}

		if v := optNumber(req, "order_price", 0); v != 0 {
			orderReq.OrderPrice = v
		}
		if req.GetBool("reduce_only", false) {
			orderReq.ReduceOnly = true
		}

		result, err := svc.CreateOrder(ctx, orderReq)
		if err != nil {
			return mcp.NewToolResultError(formatOrderError(err, orderReq)), nil
		}

		out, _ := json.Marshal(result)
		return mcp.NewToolResultText(string(out)), nil
	}
}

func formatOrderError(err error, req orderly.CreateOrderRequest) string {
	apiErr, ok := orderly.IsAPIError(err)
	if !ok {
		return fmt.Sprintf("create order failed: %v", err)
	}

	base := fmt.Sprintf("Order rejected by Orderly (code %d): %s", apiErr.Code, apiErr.Message)

	switch {
	case contains(apiErr.Message, "not enough", "insufficient", "balance", "margin", "collateral", "free_collateral"):
		return fmt.Sprintf("%s\n\nThe account does not have enough collateral to place this order. "+
			"The user needs to deposit more funds first using prepare_orderly_deposit. "+
			"Tell the user their balance is insufficient and ask if they want to deposit.", base)

	case contains(apiErr.Message, "quantity too small", "min_notional", "minimum"):
		return fmt.Sprintf("%s\n\nThe order_quantity is below the minimum allowed for %s. "+
			"Try a larger order_quantity.", base, req.Symbol)

	case contains(apiErr.Message, "price", "price_range", "price limit"):
		return fmt.Sprintf("%s\n\nThe order_price is outside the allowed range for %s. "+
			"Check current market price with get_markets and adjust.", base, req.Symbol)

	case contains(apiErr.Message, "reduce_only", "reduce only"):
		return fmt.Sprintf("%s\n\nThe reduce_only order failed — the position may already be closed "+
			"or the order_quantity exceeds the current position size. Check with get_positions.", base)

	default:
		return base
	}
}

func contains(s string, substrs ...string) bool {
	lower := strings.ToLower(s)
	for _, sub := range substrs {
		if strings.Contains(lower, strings.ToLower(sub)) {
			return true
		}
	}
	return false
}

func cancelOrder(svc *service.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		symbol, err := req.RequireString("symbol")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		orderID, err := req.RequireInt("order_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		result, err := svc.CancelOrder(ctx, symbol, orderID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("cancel order failed: %v", err)), nil
		}

		out, _ := json.Marshal(result)
		return mcp.NewToolResultText(string(out)), nil
	}
}

func getPositions(svc *service.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := svc.GetPositions(ctx)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("get positions failed: %v", err)), nil
		}

		out, _ := json.MarshalIndent(result, "", "  ")
		return mcp.NewToolResultText(string(out)), nil
	}
}

func setPositionTPSL(svc *service.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		symbol, err := req.RequireString("symbol")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		tpPrice := optNumber(req, "take_profit_price", 0)
		slPrice := optNumber(req, "stop_loss_price", 0)
		if tpPrice == 0 || slPrice == 0 {
			return mcp.NewToolResultError("take_profit_price and stop_loss_price are required and must be > 0"), nil
		}

		result, err := svc.SetPositionTPSL(ctx, symbol, tpPrice, slPrice)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("set position TP/SL failed: %v", err)), nil
		}

		out, _ := json.Marshal(map[string]any{
			"algo_order_id":   result.Data.AlgoOrderID,
			"symbol":          symbol,
			"take_profit":     tpPrice,
			"stop_loss":       slPrice,
			"message":         "TP/SL order placed. Use get_algo_orders to check status.",
		})
		return mcp.NewToolResultText(string(out)), nil
	}
}

func getAlgoOrders(svc *service.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		symbol := optString(req, "symbol")

		result, err := svc.GetAlgoOrders(ctx, symbol)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("get algo orders failed: %v", err)), nil
		}

		out, _ := json.MarshalIndent(result, "", "  ")
		return mcp.NewToolResultText(string(out)), nil
	}
}

func cancelAlgoOrder(svc *service.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		symbol, err := req.RequireString("symbol")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		algoOrderID, err := req.RequireInt("algo_order_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		if err := svc.CancelAlgoOrder(ctx, symbol, algoOrderID); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("cancel algo order failed: %v", err)), nil
		}

		return mcp.NewToolResultText("Algo order cancelled successfully."), nil
	}
}

func getWithdrawNonce(svc *service.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		resp, err := svc.GetSettleNonce(ctx)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("get withdraw nonce failed: %v", err)), nil
		}
		out, _ := json.Marshal(map[string]any{
			"withdraw_nonce": resp.Data.SettleNonce,
		})
		return mcp.NewToolResultText(string(out)), nil
	}
}

func prepareOrderlyWithdraw(svc *service.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		wallet, err := req.RequireString("wallet_address")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		token, err := req.RequireString("token")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		amount := uint64(optNumber(req, "amount", 0))
		if amount == 0 {
			return mcp.NewToolResultError("amount is required and must be > 0"), nil
		}

		result, err := svc.PrepareOrderlyWithdraw(ctx, wallet, token, amount)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("prepare withdraw failed: %v", err)), nil
		}

		out, _ := json.Marshal(result)
		return mcp.NewToolResultText(string(out)), nil
	}
}
