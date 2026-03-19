All neccessary documentation alavailable in https://suzik.gitbook.io/perptools-mcp/

But you can also use this instructions to use MCP.

# Getting Started

Install, configure, and run the Perptools MCP server in a few steps.

# Installation

## Requirements

- Go 1.25+
- (Optional) Docker for containerized deployment

## From source

```bash
git clone <repo-url>
cd mcp-server
go mod download
go build -o mcp-server ./app/cmd
```

## Docker

```bash
docker build -t perptools-mcp .
docker run -p 8080:8080 --env-file .env perptools-mcp
```

## Docker Compose

```bash
cp .env.template .env
# Edit .env with your values
docker-compose up -d
```

The image uses a non-root user and minimal Alpine base.

# Configuration

Configure the server via environment variables.

## Environment Variables

| Variable | Default | Description |
|----------|---------|--------------|
| `TRANSPORT` | `sse` | `sse` or `stdio` |
| `ADDR` | `:8080` | Bind address (SSE) |
| `BASE_PATH` | `/mcp` | URL path prefix |
| `BASE_URL` | (empty) | Public base URL (e.g. for CORS) |
| `SOLANA_RPC_URL` | `https://api.mainnet-beta.solana.com` | Solana RPC endpoint |
| `MCP_PORT` | `8080` | Port (Docker Compose) |

> **Note:** `SOLANA_PRIVATE_KEY` is used only in integration tests, not by the server. Signing is performed by the client (wallet, MCP, or user).

## Example .env

```bash
TRANSPORT=sse
ADDR=:8080
BASE_PATH=/mcp
SOLANA_RPC_URL=https://api.mainnet-beta.solana.com
```

# Quickstart

## 1. Run the server (SSE transport)

```bash
TRANSPORT=sse go run ./app/cmd
```

## 2. Connect your MCP client

Connect to `http://localhost:8080/mcp/sse` (or your deployed URL).

## 3. Authenticate before trading

1. `prepare_registration` → sign → `complete_registration`
2. `prepare_orderly_key` → sign → `complete_orderly_key`

## 4. Place an order

```
create_order: symbol=PERP_ETH_USDC, order_type=MARKET, side=BUY, order_quantity=0.005
```

## Transport Modes

| Mode | Use Case | Endpoint |
|------|----------|----------|
| **SSE** | Web, remote clients | `BASE_PATH/mcp/sse` (default `/mcp/sse`) |
| **stdio** | Local CLI, AI agents | stdin/stdout |

## AI Agent Integration (Cursor, Claude Code, OpenClaw, etc.)

Add to your AI agent's MCP settings. Example for Cursor:

```json
{
  "mcpServers": {
    "perptools": {
      "url": "http://localhost:8080/mcp/sse"
    }
  }
}
```

Replace `localhost:8080` with your deployed URL if applicable.
