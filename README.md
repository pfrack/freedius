# freedius

A local HTTP proxy that routes LLM API requests from AI coding agents (Claude Code, OpenCode) to multiple upstream providers — NVIDIA NIM, OpenCode Go/Zen, OpenAI, Anthropic, Google, Mistral, DeepSeek, Groq, Together, Fireworks, Cohere, Ollama, LM Studio, or any custom OpenAI/Anthropic-compatible endpoint.

Single static binary. Zero external runtime dependencies.

## Quickstart

```bash
# Build
go build -o freedius ./cmd/freedius

# Start the proxy + web dashboard (defaults to 127.0.0.1:8082, dashboard at :8083)
./freedius

# Send a request to see it appear on the dashboard:
curl -X POST http://127.0.0.1:8082/v1/messages \
  -H 'Content-Type: application/json' \
  -d '{"model": "opus", "messages": [{"role": "user", "content": "hi"}]}'
```

On first run, freedius uses an embedded default config so no setup is required. Open `http://localhost:8083/` in a browser to view live logs, providers, and mappings.

## Web Dashboard

The embedded web dashboard provides:
- **Live logs** — streaming via SSE with level filtering
- **Request events** — see proxy requests in real-time
- **Provider management** — add, edit, delete providers through the UI
- **Mapping management** — add, edit, delete model mappings (with fallback chains)
- **Health check** — `GET /health` returns `{"status":"ok"}`

Access at `http://localhost:8083/` (default). Configure via `--ui-port` and `--ui-host` flags or `FREEDIUS_UI_PORT` / `FREEDIUS_UI_HOST` env vars.

Set `FREEDIUS_UI_TOKEN` to require authentication on all dashboard routes (useful for LAN/Docker exposure).

## Docker

```bash
# Build and run
docker compose up

# Or manually
docker build -t freedius .
docker run -p 8082:8082 -p 8083:8083 -e OPENCODE_API_KEY freedius
```

The Docker image uses a distroless base with a nonroot user. Set `FREEDIUS_HOST=0.0.0.0` and `FREEDIUS_UI_HOST=0.0.0.0` to expose ports to the container network.

## Configuration

freedius reads a YAML config file. Resolution order:

1. `--config <path>` (or `-c <path>`) flag
2. `freedius.yaml` or `freedius.yml` in the current directory
3. `~/.config/freedius/config.yaml`

### Providers

Supported providers (defined in `providers.yaml` as the single source of truth):

| Provider    | Protocol  | Default Base URL |
|-------------|-----------|-------------------|
| `nim`       | OpenAI    | `https://integrate.api.nvidia.com/v1/chat/completions` |
| `openai`    | OpenAI    | — (required) |
| `anthropic` | Anthropic | `https://api.anthropic.com/v1/messages` |
| `google`    | OpenAI    | `https://generativelanguage.googleapis.com/v1beta/openai/chat/completions` |
| `mistral`   | OpenAI    | `https://api.mistral.ai/v1/chat/completions` |
| `deepseek`  | OpenAI    | `https://api.deepseek.com/chat/completions` |
| `groq`      | OpenAI    | `https://api.groq.com/openai/v1/chat/completions` |
| `together`  | OpenAI    | `https://api.together.xyz/v1/chat/completions` |
| `fireworks` | OpenAI    | `https://api.fireworks.ai/inference/v1/chat/completions` |
| `cohere`    | OpenAI    | `https://api.cohere.com/compatibility/v1/chat/completions` |
| `ollama`    | OpenAI    | `http://localhost:11434/v1/chat/completions` |
| `lmstudio`  | OpenAI    | `http://localhost:1234/v1/chat/completions` |
| `go`        | Mix*      | — (required) |
| `zen`       | Mix*      | — (required) |
| `custom`    | Mix*      | — (required) |
| `mix`       | Mix*      | — (required) |

\* Mix adapter auto-detects protocol from the URL path (`/v1/messages` → Anthropic, else OpenAI). Set `protocol: openai` or `protocol: anthropic` to override — the adapter appends the correct endpoint suffix (`/v1/messages` or `/v1/chat/completions`) automatically.

### Example config

```yaml
providers:
  nim:  { behavior: openai }
  zen:  { behavior: mix, default_base_url: https://opencode.ai/zen/v1/messages, default_api_key_env: OPENCODE_API_KEY }
  go:   { behavior: mix, default_base_url: https://opencode.ai/zen/go/v1/chat/completions, default_api_key_env: OPENCODE_API_KEY }

mappings:
  default: { provider_name: nim, model_string: step-3.5 }
  auto:    { provider_name: nim, model_string: step-3.5 }
  opus:    { provider_name: go,  model_string: deepseek-v4-pro }
  sonnet:  { provider_name: go,  model_string: minimax-m3 }
  haiku:   { provider_name: zen, model_string: claude-sonnet-4-6 }
```

### Mapping resolution

When a request arrives, freedius resolves the `model` field against:
1. Exact match in `mappings` map
2. Family prefix match in `mappings` (e.g. `claude-sonnet-4-6-20250908` matches `claude-sonnet-4-6`)

Each mapping specifies the upstream `provider`, `model`, `base_url`, and optionally `api_key_env` and `protocol`.

### Fallback chains

Mappings support ordered fallback entries. When the primary provider fails (config error, transport failure, or upstream HTTP 4xx/5xx), freedius automatically tries each fallback entry in order:

```yaml
mappings:
  opus:
    provider_name: go
    model_string: deepseek-v4-pro
    fallback:
      - provider_name: zen
        model_string: claude-sonnet-4-6
      - provider_name: nim
        model_string: step-3.5
```

A shared timeout budget (default `2 × stream-timeout`) covers the entire chain. The web dashboard shows which fallback responder was last used per mapping.

## CLI

```
freedius [flags]

Flags:
  -c string            Shorthand for --config
  -config string       Path to config file (auto-resolved if empty)
  -host string         Host to bind (127.0.0.1 or 0.0.0.0; default 127.0.0.1)
  -log-format string   Log output format: text, json (default text)
  -no-export-hint      Suppress the env-export hint on startup
  -port int            Port to listen on (default 8082)
  -stream-timeout      Per-request upstream timeout (default 5m)
  -verbose-errors      Include upstream error detail in error responses
  -ui-port int         Web UI port (default 8083)
  -ui-host string      Web UI bind address (default 127.0.0.1)
  -help                Show help
  -version             Print version
```

### Environment variables

| Variable | Description |
|----------|-------------|
| `FREEDIUS_PORT` | Listen port (overridden by `--port`) |
| `FREEDIUS_HOST` | Listen host (overridden by `--host`) |
| `FREEDIUS_LOG` | Log format: `text` or `json` |
| `FREEDIUS_VERBOSE_ERRORS` | Set to `1` for verbose errors |
| `FREEDIUS_STREAM_TIMEOUT` | Per-request upstream timeout duration |
| `FREEDIUS_FALLBACK_TIMEOUT_MULTIPLIER` | Scales per-attempt timeout to derive fallback chain budget (default `2`) |
| `FREEDIUS_UI_PORT` | Web dashboard port (overridden by `--ui-port`) |
| `FREEDIUS_UI_HOST` | Web dashboard bind address (overridden by `--ui-host`) |
| `FREEDIUS_UI_TOKEN` | Bearer token for dashboard auth (opt-in) |
| `NVIDIA_NIM_API_KEY` | API key for NVIDIA NIM provider |
| `ANTHROPIC_API_KEY` | API key for Anthropic provider |
| `GEMINI_API_KEY` | API key for Google Gemini provider |
| `MISTRAL_API_KEY` | API key for Mistral provider |
| `DEEPSEEK_API_KEY` | API key for DeepSeek provider |
| `GROQ_API_KEY` | API key for Groq provider |
| `TOGETHER_API_KEY` | API key for Together provider |
| `FIREWORKS_API_KEY` | API key for Fireworks provider |
| `COHERE_API_KEY` | API key for Cohere provider |
| `OPENCODE_API_KEY` | API key for OpenCode Go/Zen providers |

## Features

- **Multi-provider routing** — dispatch requests to different upstreams based on the `model` field
- **Fallback chains** — ordered fallback entries with shared timeout budget; web dashboard shows last-used responder
- **Protocol auto-detection** — mix adapter sniffs URL path to choose OpenAI vs Anthropic format
- **Explicit protocol control** — set `protocol: openai` or `protocol: anthropic` on mix providers; the adapter appends the correct endpoint suffix automatically
- **Family-based matching** — `claude-sonnet-4-6-20250908` falls back to `claude-sonnet-4-6` mapping
- **Local count_tokens** — BPE-based token counting for upstreams that don't support `/v1/messages/count_tokens` natively
- **Models cache** — fetches and caches upstream `/v1/models` responses for the web dashboard
- **Web dashboard** — live logs, request events, provider/mapping management via browser
- **Request IDs** — every request gets a unique ID, returned in `X-Freedius-Request-ID` header
- **Panic recovery** — catches panics, logs stack traces, returns 500 JSON errors
- **Structured access logs** — logs method, path, status, duration, matched provider/model (never request/response bodies)
- **Token counting** — local BPE-based token counting for providers that don't support it natively
- **NIM sanitization** — auto-strips unsupported fields from requests routed to NVIDIA NIM
- **Sensitive data redaction** — API keys and tokens in upstream error messages are automatically redacted
- **Graceful shutdown** — drains connections on SIGINT/SIGTERM

## Development

```bash
# Run tests with race detection
mage test

# Lint (staticcheck + golangci-lint)
mage lint

# Full CI check (vet, mod-verify, tidy-check, generate-check, format-check, test, lint, build, govulncheck)
mage ci

# Format code (requires goimports, golines, gci)
mage format

# Docker build and run
mage dockerBuild
mage dockerRun
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
