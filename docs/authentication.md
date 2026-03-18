---
title: Authentication
layout: page
---

# Authentication

Authentication is required for **trading** and **withdrawals**. Deposits do not require auth.

## Overview

1. **Account registration** — Links the Solana wallet to an Orderly account.
2. **Orderly key** — Registers an ed25519 key for signing API requests.

Both steps require the user to **sign messages** with their Solana wallet. The server never stores private keys.

## Flow

```
┌──────────────────┐     ┌──────────────────┐     ┌──────────────────┐
│ prepare_         │     │ User signs        │     │ complete_        │
│ registration     │ ──► │ message_base64    │ ──► │ registration     │
└──────────────────┘     └──────────────────┘     └──────────────────┘
        │                                                      │
        │ (if already_registered: true, skip to step 2)       │
        ▼                                                      ▼
┌──────────────────┐     ┌──────────────────┐     ┌──────────────────┐
│ prepare_orderly_ │     │ User signs       │     │ complete_orderly_│
│ key              │ ──► │ message_base64   │ ──► │ key              │
└──────────────────┘     └──────────────────┘     └──────────────────┘
                                                              │
                                                              ▼
                                                    ┌──────────────────┐
                                                    │ Authenticated    │
                                                    │ (trading &       │
                                                    │  withdraw OK)   │
                                                    └──────────────────┘
```

## Step 1: Account Registration

### prepare_registration

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `wallet_address` | string | Yes | Solana wallet public key (base58) |

**Response (new user):**

```json
{
  "message_base64": "<base64-encoded message>",
  "wallet_address": "...",
  "debug_hash": "...",
  "next_step": "Sign the message_base64 with the user's Solana wallet and call complete_registration with the signature."
}
```

**Response (already registered):**

```json
{
  "already_registered": true,
  "account_id": "...",
  "wallet_address": "...",
  "next_step": "Account already registered. Skip complete_registration and proceed directly to prepare_orderly_key."
}
```

### complete_registration

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `wallet_address` | string | Yes | Same as prepare |
| `signature` | string | Yes | Hex signature with `0x` prefix |

**Signature format:** Decode `message_base64`, sign the raw bytes with the wallet's Ed25519 key, encode as hex and prefix with `0x`.

---

## Step 2: Orderly Key

### prepare_orderly_key

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `wallet_address` | string | Yes | Solana wallet public key (base58) |

**Response:**

```json
{
  "message_base64": "<base64-encoded message>",
  "wallet_address": "...",
  "debug_hash": "..."
}
```

The server generates a random ed25519 Orderly key. The user signs to prove wallet ownership; the server stores the key in memory for subsequent authenticated API calls.

### complete_orderly_key

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `wallet_address` | string | Yes | Same as prepare |
| `signature` | string | Yes | Hex signature with `0x` prefix |

**Response:** `"Orderly key registered. Authentication complete — you can now use all tools."`

---

## Auth-Required Tools

| Tool | Requires Auth |
|------|---------------|
| `create_order` | Yes |
| `cancel_order` | Yes |
| `get_positions` | Yes |
| `set_position_tp_sl` | Yes |
| `get_algo_orders` | Yes |
| `cancel_algo_order` | Yes |
| `prepare_orderly_withdraw` | Yes |
| `get_withdraw_nonce` | Yes |
| `get_user_points` | Yes |
| `get_leaderboard` | Yes |
| `prepare_registration` | No |
| `complete_registration` | No |
| `prepare_orderly_key` | No |
| `complete_orderly_key` | No |
| `prepare_orderly_deposit` | No |
| `get_markets` | No |
| `health` | No |
