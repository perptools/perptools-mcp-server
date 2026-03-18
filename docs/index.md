---
title: Perptools MCP Server
layout: page
---

# Perptools MCP Server

An **MCP (Model Context Protocol)** server for Solana perpetual futures trading via [Orderly Network](https://orderly.network) and [Perptools](https://app.perptools.ai). Enables AI assistants and chat interfaces to manage deposits, withdrawals, and trading operations through a standardized tool interface.

## Features

| Category | Capabilities |
|----------|--------------|
| **Authentication** | Account registration, Orderly key setup, wallet-based auth |
| **Trading** | Market/limit orders, position management, take-profit/stop-loss |
| **Vault** | Deposit (LayerZero) and withdraw from Orderly vault |
| **Market Data** | Live markets, prices, health checks |
| **Gamification** | User points, leaderboard |

## Prerequisites

- **Solana wallet** — The server requires signing messages and transactions. You must have access to either:
  - A connected wallet MCP or browser extension
  - The user's Solana private key (for automated flows)
- **Orderly Network** — Accounts are registered and traded on Orderly mainnet.

## Quick Start

1. **Run the server** (SSE transport):
   ```bash
   TRANSPORT=sse go run ./app/cmd
   ```

2. **Connect your MCP client** to `http://localhost:8080/mcp/sse` (or your deployed URL).

3. **Authenticate** before trading or withdrawing:
   - `prepare_registration` → sign → `complete_registration`
   - `prepare_orderly_key` → sign → `complete_orderly_key`

4. **Place an order**:
   - `create_order` with `symbol=PERP_ETH_USDC`, `order_type=MARKET`, `side=BUY`, `order_quantity=0.005`

## Transport Modes

| Mode | Use Case | Endpoint |
|------|----------|----------|
| **SSE** | Web, remote clients | `BASE_PATH/mcp/sse` (default `/mcp/sse`) |
| **stdio** | Local CLI, Cursor | stdin/stdout |

## Documentation

- [Getting Started](getting-started) — Installation, configuration, deployment
- [Authentication](authentication) — Registration and Orderly key flow
- [Tools Reference](tools-reference) — Full API for all tools
- [Workflows](workflows) — Example sequences (open/close position, deposit, withdraw)

## Architecture

```
┌─────────────────┐     MCP Tools      ┌──────────────────┐
│  AI Assistant   │ ◄────────────────► │  Perptools MCP  │
│  (Cursor, etc) │                     │     Server       │
└─────────────────┘                    └────────┬─────────┘
                                                │
                    ┌───────────────────────────┼───────────────────────────┐
                    ▼                           ▼                           ▼
            ┌───────────────┐           ┌───────────────┐           ┌───────────────┐
            │ Orderly API   │           │ Perptools API  │           │ Solana RPC    │
            │ (trading)    │           │ (markets)     │           │ (tx build)   │
            └───────────────┘           └───────────────┘           └───────────────┘
```

## License

See repository.
