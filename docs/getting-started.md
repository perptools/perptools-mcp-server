---
title: Getting Started
layout: page
---

# Getting Started

Install, configure, and run the Perptools MCP server.

## Requirements

- Go 1.25+
- (Optional) Docker for containerized deployment

## Installation

### From source

```bash
git clone <repo-url>
cd mcp-server
go mod download
go build -o mcp-server ./app/cmd
```

### Docker

```bash
docker build -t perptools-mcp .
docker run -p 8080:8080 --env-file .env perptools-mcp
```

### Docker Compose

```bash
cp .env.template .env
# Edit .env with your values
docker-compose up -d
```

## Configuration

Environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `TRANSPORT` | `sse` | `sse` or `stdio` |
| `ADDR` | `:8080` | Bind address (SSE) |
| `BASE_PATH` | `/mcp` | URL path prefix |
| `BASE_URL` | (empty) | Public base URL (e.g. for CORS) |
| `SOLANA_RPC_URL` | `https://api.mainnet-beta.solana.com` | Solana RPC endpoint |
| `MCP_PORT` | `8080` | Port (Docker Compose) |

> **Note:** `SOLANA_PRIVATE_KEY` is used only in integration tests, not by the server. Signing is performed by the client (wallet, MCP, or user).

## Running

### SSE (Server-Sent Events)

For web clients and remote connections:

```bash
TRANSPORT=sse ADDR=:8080 go run ./app/cmd
```

Server starts at `http://localhost:8080`. MCP SSE endpoint: `http://localhost:8080/mcp/sse`.

### stdio

For local MCP clients (e.g. Cursor):

```bash
TRANSPORT=stdio go run ./app/cmd
```

Communication is over stdin/stdout.

## Health Check

```bash
curl http://localhost:8080/mcp/sse
```

Returns SSE stream (connection will stay open). A 200 response indicates the server is running.

## Deployment

### Behind a reverse proxy

Set `BASE_URL` to the public base URL (e.g. `https://mcp.example.com`). Ensure:

- Proxy forwards `/mcp/*` to the MCP server
- SSE connections are not buffered or closed by the proxy
- CORS is configured if the client is browser-based

### Docker production

```yaml
services:
  mcp-server:
    build: .
    env_file: .env
    ports:
      - "8080:8080"
    restart: unless-stopped
```

The image uses a non-root user and minimal Alpine base.

## Cursor Integration

Add to Cursor MCP settings:

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
