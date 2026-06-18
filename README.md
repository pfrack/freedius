# freedius

A local HTTP proxy that routes LLM API requests from AI coding agents (Claude Code, OpenCode) to multiple upstream providers — NVIDIA NIM, OpenCode Go/Zen, OpenAI, Anthropic, or any custom OpenAI/Anthropic-compatible endpoint.

Single static binary. Zero external runtime dependencies.

## Quickstart

```bash
# Build
go build -o freedius .

# Generate a starter config + Claude Code integration
./freedius init --shell-install

# Start the proxy (defaults to 127.0.0.1:8082)
./freedius serve
```

The proxy will auto-write a default config to `~/.config/freedius/config.yaml` on first run if none is found.

## Configuration

freedius reads a YAML config file. Resolution order:

1. `--config <path>` flag
2. `freedius.yaml` or `freedius.yml` in the current directory
3. `~/.config/freedius/config.yaml`

### Providers

Supported providers (defined in `providers.yaml` as the single source of truth):

| Provider   | Protocol | Default Base URL |
|------------|----------|-------------------|
| `nim`      | OpenAI   | `https://integrate.api.nvidia.com/v1/chat/completions` |
| `openai`   | OpenAI   | — (required) |
| `anthropic`| Anthropic| — (required) |
| `go`       | Mix*     | — (required) |
| `zen`      | Mix*     | — (required) |
| `custom`   | Mix*     | — (required) |

\* Mix adapter auto-detects protocol from the URL path (`/v1/messages` → Anthropic, else OpenAI) or the explicit `protocol` field.

### Example config

```yaml
mappings:
  default: { provider: nim,   model: step-3.5 }
  auto:    { provider: nim,   model: step-3.5 }
  opus:    { provider: go,    model: deepseek-v4-pro,  base_url: https://opencode.ai/zen/go/v1/chat/completions }
  sonnet:  { provider: go,    model: minimax-m3,       base_url: https://opencode.ai/zen/go/v1/messages }
  haiku:   { provider: zen,   model: claude-sonnet-4-6, base_url: https://opencode.ai/zen/v1/messages }
```

### Mapping resolution

When a request arrives, freedius resolves the `model` field against:
1. Exact match in `models` map
2. Exact match in `mappings` map
3. Family prefix match in `mappings` (e.g. `claude-sonnet-4-6-20250908` matches `claude-sonnet-4-6`)

Each mapping specifies the upstream `provider`, `model`, `base_url`, and optionally `api_key_env` and `protocol`.

## CLI

```
freedius [<subcommand>] [flags]

  serve     Start the proxy server (default)
  init      Generate a starter config file
  version   Print the binary version
  help      Show help
```

### `serve` flags

| Flag | Default | Description |
|------|---------|-------------|
| `--config` | auto-resolved | Path to config file |
| `--port` | `8082` | Port to listen on |
| `--host` | `127.0.0.1` | Host to bind (`127.0.0.1` or `0.0.0.0`) |
| `--log-format` | `text` | Log format: `text` or `json` |
| `--verbose-errors` | `false` | Include upstream error detail in responses |
| `--stream-timeout` | `5m` | Per-request upstream timeout |
| `--no-export-hint` | `false` | Suppress env-export hint on startup |

### Environment variables

| Variable | Description |
|----------|-------------|
| `FREEDIUS_PORT` | Listen port (overridden by `--port`) |
| `FREEDIUS_LOG` | Log format: `text` or `json` |
| `FREEDIUS_VERBOSE_ERRORS` | Set to `1` for verbose errors |
| `FREEDIUS_STREAM_TIMEOUT` | Per-request upstream timeout duration |
| `NVIDIA_NIM_API_KEY` | API key for NVIDIA NIM provider |
| `ANTHROPIC_API_KEY` | API key for Anthropic provider |
| `OPENCODE_API_KEY` | API key for OpenCode Go/Zen providers |

## Features

- **Multi-provider routing** — dispatch requests to different upstreams based on the `model` field
- **Protocol auto-detection** — mix adapter sniffs URL path to choose OpenAI vs Anthropic format
- **Family-based matching** — `claude-sonnet-4-6-20250908` falls back to `claude-sonnet-4-6` mapping
- **Request IDs** — every request gets a unique ID, returned in `X-Freedius-Request-ID` header
- **Panic recovery** — catches panics, logs stack traces, returns 500 JSON errors
- **Structured access logs** — logs method, path, status, duration, matched provider/model (never request/response bodies)
- **Token counting** — local BPE-based token counting for providers that don't support it natively
- **NIM sanitization** — auto-strips unsupported fields from requests routed to NVIDIA NIM
- **Graceful shutdown** — drains connections on SIGINT/SIGTERM

## Development

```bash
# Run tests with race detection
make test

# Lint (vet + staticcheck + golangci-lint)
make lint

# Full CI check (vet, generate-check, test, build)
make ci

# Format code (requires goimports, golines, gci)
make format

# Install git pre-commit hook
make install-hooks
```

## API

The proxy accepts `POST` requests with `Content-Type: application/json` at any path. The request body must contain a `model` field. The proxy resolves the model against configuration and forwards to the matching upstream provider.

Response headers:
- `X-Freedius-Request-ID` — unique request identifier
- `X-Freedius-Matched-Provider` — the provider that handled the request
- `X-Freedius-Matched-Model` — the upstream model name

Built-in endpoints:
- `HEAD /` — health check, returns 200
- `GET /health` — health check, returns 200 with JSON body
