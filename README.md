# freedius

A local HTTP proxy that routes LLM API requests from AI coding agents to
upstream providers — with fallback chains, model-name mapping, and a live
dashboard for the solo-dev maintainer. Single static binary, zero external
runtime dependencies.

## What it does

freedius sits between a coding agent (Claude Code, OpenCode) and many LLM
upstreams. The agent sends a normal `POST` with a `model` field; freedius
resolves it against config, forwards to the matching upstream, and on failure
walks an ordered fallback chain.

## Reading the system state

The web dashboard (`http://localhost:8083/`, default) is the primary way to read
what the system is doing right now:

- **Mapping cards** — routing shape (primary + fallback chain) plus provenance:
  when added (`added_at`), a green/amber dot for whether the API key is in the
  environment right now, and a family badge (opus/sonnet/haiku).
- **Last-used responder** — the highlighted step shows which fallback fired on
  the last request.
- **Live logs** — streaming via SSE with level and provider/mapping filtering.

Access at `http://localhost:8083/` (default). Set `FREEDIUS_UI_TOKEN` to require
bearer authentication on all dashboard routes (useful for LAN/Docker exposure).

## Quickstart

```bash
go build -o freedius ./cmd/freedius
./freedius
curl -X POST http://127.0.0.1:8082/v1/messages \
  -H 'Content-Type: application/json' \
  -d '{"model": "opus", "messages": [{"role": "user", "content": "hi"}]}'
```

On first run, freedius loads an embedded default config so it serves requests
immediately — but upstream API keys are still required for any provider you
actually use.

## Configuration

freedius reads a YAML config file. Resolution order:

1. `--config <path>` (or `-c <path>`) flag
2. `freedius.yaml` or `freedius.yml` in the current directory
3. `~/.config/freedius/config.yaml`

### Example config

```yaml
providers:
  nim:  { behavior: openai }
  zen:  { behavior: mix, default_base_url: https://opencode.ai/zen/v1/messages, default_api_key_env: OPENCODE_API_KEY }
  go:   { behavior: mix, default_base_url: https://opencode.ai/zen/go/v1/chat/completions, default_api_key_env: OPENCODE_API_KEY }

mappings:
  default: { provider_name: nim, model_string: step-3.5 }
  opus:    { provider_name: go,  model_string: deepseek-v4-pro }
  sonnet:  { provider_name: go,  model_string: minimax-m3 }
  haiku:   { provider_name: zen, model_string: claude-sonnet-4-6 }
```

### Mapping resolution

When a request arrives, freedius resolves the `model` field against:

1. Exact match in `mappings` map
2. Family prefix match (e.g. `claude-sonnet-4-6-20250908` matches
   `claude-sonnet-4-6`)

### Fallback chains

Mappings support ordered fallback entries. When the primary fails (config
error, transport failure, or upstream 4xx/5xx), freedius tries each fallback
in order:

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

### Provenance annotation

Mappings accept an optional `added_at` free-form string shown on the card in
the dashboard. Blank means unknown.

```yaml
mappings:
  opus:
    provider_name: go
    model_string: deepseek-v4-pro
    added_at: 2026-07-06
```

## CLI & Environment Variables

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

| Variable | Description |
|----------|-------------|
| `FREEDIUS_PORT` | Listen port (overridden by `--port`) |
| `FREEDIUS_HOST` | Listen host (overridden by `--host`) |
| `FREEDIUS_LOG` | Log format: `text` or `json` |
| `FREEDIUS_VERBOSE_ERRORS` | Set to `1` for verbose errors |
| `FREEDIUS_STREAM_TIMEOUT` | Per-request upstream timeout duration |
| `FREEDIUS_FALLBACK_TIMEOUT_MULTIPLIER` | Scales per-attempt fallback budget (default `2`) |
| `FREEDIUS_UI_PORT` | Dashboard port (overridden by `--ui-port`) |
| `FREEDIUS_UI_HOST` | Dashboard bind address (overridden by `--ui-host`) |
| `FREEDIUS_UI_TOKEN` | Bearer token for dashboard auth (opt-in) |
| `NVIDIA_NIM_API_KEY` | API key for NVIDIA NIM |
| `ANTHROPIC_API_KEY` | API key for Anthropic |
| `GEMINI_API_KEY` | API key for Google Gemini |
| `MISTRAL_API_KEY` | API key for Mistral |
| `DEEPSEEK_API_KEY` | API key for DeepSeek |
| `GROQ_API_KEY` | API key for Groq |
| `TOGETHER_API_KEY` | API key for Together |
| `FIREWORKS_API_KEY` | API key for Fireworks |
| `COHERE_API_KEY` | API key for Cohere |
| `OPENCODE_API_KEY` | API key for OpenCode Go/Zen |

## Development

```bash
mage test      # tests with race detection
mage lint      # staticcheck + golangci-lint
mage ci        # full CI check
mage format    # goimports, golines, gci
```

## Reference

The full provider table lives in
[`providers.yaml`](providers.yaml) as the single source of truth — run
`go generate ./...` after adding an entry. Each entry declares behavior class,
default base URL, and the env var holding the API key.

Response headers:
- `X-Freedius-Request-ID` — unique request identifier
- `X-Freedius-Matched-Provider` — the provider that handled the request
- `X-Freedius-Matched-Model` — the upstream model name

Built-in endpoints:
- `HEAD /` — health check, returns 200
- `GET /health` — health check, returns 200 with JSON body
