# Perptools MCP Server

MCP server for Solana perpetual futures trading via [Orderly Network](https://orderly.network) and [Perptools](https://app.perptools.ai).

**Full documentation:** See the [`docs/`](docs/) folder (or your GitHub Pages site if configured).

## Quick Start

```bash
TRANSPORT=sse go run ./app/cmd
```

Connect MCP clients to `http://localhost:8080/mcp/sse`.

## Docs

| Page | Description |
|------|-------------|
| [Index](docs/index.md) | Overview, features, architecture |
| [Getting Started](docs/getting-started.md) | Installation, configuration, deployment |
| [Authentication](docs/authentication.md) | Registration and Orderly key flow |
| [Tools Reference](docs/tools-reference.md) | All MCP tools with parameters |
| [Workflows](docs/workflows.md) | Example sequences |
