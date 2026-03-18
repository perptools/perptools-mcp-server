---
title: Tools Reference
layout: page
---

# Tools Reference

Complete reference for all MCP tools.

---

## Authentication

### prepare_registration

Prepare Orderly account registration. Returns a base64 message to sign.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `wallet_address` | string | Yes | Solana wallet public key (base58) |

---

### complete_registration

Complete registration by submitting the wallet signature.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `wallet_address` | string | Yes | Solana wallet public key |
| `signature` | string | Yes | Hex signature (0x-prefixed) |

---

### prepare_orderly_key

Generate Orderly key and prepare sign message. Returns base64 message to sign.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `wallet_address` | string | Yes | Solana wallet public key |

---

### complete_orderly_key

Complete Orderly key registration with signature. Stores credentials in memory.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `wallet_address` | string | Yes | Solana wallet public key |
| `signature` | string | Yes | Hex signature (0x-prefixed) |

---

## Vault (Deposit & Withdraw)

### prepare_orderly_deposit

Build unsigned Solana deposit transaction (LayerZero). No auth required.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `wallet_address` | string | Yes | Solana wallet public key |
| `symbol` | string | Yes | `USDC`, `USDT`, or `SOL` |
| `amount` | number | Yes | Amount in smallest units (e.g. 1_000_000 for 1 USDC) |

**Returns:** `transaction_base64` — Sign with wallet, then submit to Solana.

**Token units:** USDC/USDT use 6 decimals (1 USDC = 1_000_000). SOL uses lamports (1 SOL = 1e9).

---

### prepare_orderly_withdraw

Build unsigned Solana memo transaction for withdrawal. **Requires auth.**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `wallet_address` | string | Yes | Solana wallet public key |
| `token` | string | Yes | `USDC`, `USDT`, or `SOL` |
| `amount` | number | Yes | Amount in smallest units |

**Returns:** `transaction_base64` — Sign with wallet, then submit to Solana.

---

### get_withdraw_nonce

Get current withdraw (settle) nonce from Orderly. **Requires auth.** Used for debugging; `prepare_orderly_withdraw` fetches it automatically.

---

## Trading

### create_order

Place a new order. **Requires auth.**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `symbol` | string | Yes | e.g. `PERP_ETH_USDC`, `PERP_BTC_USDC` |
| `order_type` | string | Yes | `MARKET`, `LIMIT`, `IOC`, `FOK`, `POST_ONLY`, `ASK`, `BID` |
| `side` | string | Yes | `BUY` or `SELL` |
| `order_quantity` | number | Yes | Size in **base currency** (e.g. 0.005 ETH, not USDC) |
| `order_price` | number | No | Required for LIMIT/IOC/FOK/POST_ONLY |
| `reduce_only` | boolean | No | If true, only reduces position (closing) |

**Important:** `order_quantity` is in base asset (ETH, BTC), not USDC. For "$100 of ETH", compute: `order_quantity = 100 / mark_price`.

---

### cancel_order

Cancel an open order. **Requires auth.**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `symbol` | string | Yes | Trading pair |
| `order_id` | number | Yes | Order ID to cancel |

---

### get_positions

Get all open positions, margin, PnL. **Requires auth.**

Returns account summary (collateral, margin ratio) and position list (symbol, side, size, entry/mark price, PnL, liquidation price).

---

### set_position_tp_sl

Set take-profit and stop-loss on a position. **Requires auth.** One TP/SL per symbol.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `symbol` | string | Yes | Trading pair |
| `take_profit_price` | number | Yes | Price to close for profit |
| `stop_loss_price` | number | Yes | Price to close for loss |

**LONG:** TP > entry, SL < entry. **SHORT:** TP < entry, SL > entry.

---

### get_algo_orders

List algo orders (TP/SL). **Requires auth.**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `symbol` | string | No | Filter by symbol |

---

### cancel_algo_order

Cancel an algo order (e.g. TP/SL). **Requires auth.**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `symbol` | string | Yes | Trading pair |
| `algo_order_id` | number | Yes | From `get_algo_orders` |

---

## Market Data

### get_markets

Get paginated list of markets and prices. No auth.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `limit` | number | No | Max results (default 10) |
| `offset` | number | No | Pagination offset (default 0) |

---

### health

Check Perptools API health. No auth.

---

## Gamification

### get_user_points

Get user points and distribution. **Requires auth.**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `public_key` | string | Yes | User Solana public key |

---

### get_leaderboard

Get points leaderboard. **Requires auth.**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `public_key` | string | Yes | User Solana public key |
| `limit` | number | No | Max results (default 10) |
| `offset` | number | No | Pagination offset (default 0) |
