---
title: Workflows
layout: page
---

# Workflows

Example sequences for common operations.

---

## Open a LONG Position

1. Authenticate (see [Authentication](authentication)).
2. `get_markets` — Check current price for the symbol.
3. `get_positions` — Verify available collateral.
4. `create_order`:
   - `symbol`: `PERP_ETH_USDC` (or `PERP_BTC_USDC`, etc.)
   - `order_type`: `MARKET`
   - `side`: `BUY`
   - `order_quantity`: e.g. `0.005` (0.005 ETH)
5. `get_positions` — Confirm position opened.

**Example:** Buy 0.005 ETH at market:
```json
{
  "symbol": "PERP_ETH_USDC",
  "order_type": "MARKET",
  "side": "BUY",
  "order_quantity": 0.005
}
```

---

## Open a LIMIT Position

```json
{
  "symbol": "PERP_ETH_USDC",
  "order_type": "LIMIT",
  "side": "BUY",
  "order_quantity": 0.01,
  "order_price": 2000
}
```

---

## Close a Position

1. `get_positions` — Get current `position_qty` (absolute value = size to close).
2. `create_order` with **opposite side** and `reduce_only: true`:

**Close LONG 0.005 ETH:**
```json
{
  "symbol": "PERP_ETH_USDC",
  "order_type": "MARKET",
  "side": "SELL",
  "order_quantity": 0.005,
  "reduce_only": true
}
```

**Close SHORT 0.001 BTC:**
```json
{
  "symbol": "PERP_BTC_USDC",
  "order_type": "MARKET",
  "side": "BUY",
  "order_quantity": 0.001,
  "reduce_only": true
}
```

---

## Set Take-Profit and Stop-Loss

1. `get_positions` — Note entry price.
2. `set_position_tp_sl`:
   - **LONG:** `take_profit_price` > entry, `stop_loss_price` < entry
   - **SHORT:** `take_profit_price` < entry, `stop_loss_price` > entry

```json
{
  "symbol": "PERP_ETH_USDC",
  "take_profit_price": 2500,
  "stop_loss_price": 1800
}
```

3. `get_algo_orders` — Verify TP/SL is active.
4. To remove: `cancel_algo_order` with `algo_order_id` from `get_algo_orders`.

---

## Deposit

1. `prepare_orderly_deposit`:
   - `wallet_address`: user's Solana address
   - `symbol`: `USDC`, `USDT`, or `SOL`
   - `amount`: in smallest units (1 USDC = 1_000_000)
2. Decode `transaction_base64`, sign with user's wallet.
3. Submit signed transaction to Solana RPC.

**Example:** Deposit 10 USDC:
```json
{
  "wallet_address": "9QdB7iqThhbQS1w5AddSwc4suGUEmzJh8rUk8kuxxGXz",
  "symbol": "USDC",
  "amount": 10000000
}
```

---

## Withdraw

1. Authenticate (required for withdraw).
2. `prepare_orderly_withdraw`:
   - `wallet_address`: receiver (usually user's own)
   - `token`: `USDC`, `USDT`, or `SOL`
   - `amount`: in smallest units
3. Decode `transaction_base64`, sign with user's wallet.
4. Submit signed transaction to Solana RPC.

**Example:** Withdraw 1.5 USDC:
```json
{
  "wallet_address": "9QdB7iqThhbQS1w5AddSwc4suGUEmzJh8rUk8kuxxGXz",
  "token": "USDC",
  "amount": 1500000
}
```

---

## Convert USD to Order Quantity

User says "buy $50 of ETH":

1. `get_markets` — Get `mark_price` for `PERP_ETH_USDC`.
2. Compute: `order_quantity = 50 / mark_price`.
3. `create_order` with computed `order_quantity`.

Example: If ETH = $2000, then `order_quantity = 50 / 2000 = 0.025`.

---

## Full Session Checklist

| Step | Action |
|------|--------|
| 1 | `prepare_registration` → sign → `complete_registration` (if new) |
| 2 | `prepare_orderly_key` → sign → `complete_orderly_key` |
| 3 | `get_positions` — Check collateral |
| 4 | If low: `prepare_orderly_deposit` → sign → submit |
| 5 | `create_order` — Place trades |
| 6 | `get_positions` — Monitor |
| 7 | Optional: `set_position_tp_sl` for risk management |
| 8 | To close: `create_order` with `reduce_only: true` |
| 9 | To withdraw: `prepare_orderly_withdraw` → sign → submit |
