# freedius

A local HTTP proxy that routes LLM API requests from AI coding agents to
upstream providers — with fallback chains, model-name mapping, and a live
dashboard for the solo-dev maintainer. Compiles to a single static binary;
the optional web dashboard loads its web font from a third-party CDN.

## What it does

freedius sits between a coding agent (Claude Code, OpenCode) and many LLM
upstreams. The agent sends a normal `POST` with a `model` field; freedius
resolves it against config, forwards to the matching upstream, and on failure
walks an ordered fallback chain. The embedded web dashboard (see below)
shows which mapping handled the last request and which provider's key is
set, live.

## Quickstart

```bash
go install github.com/pfrack/freedius/cmd/freedius@v0.1.0   # install
export NVIDIA_NIM_API_KEY=nvapi-...                        # set one key
freedius                                                   # start (listens on :8082)
curl -X POST http://127.0.0.1:8082/v1/messages \
  -H 'Content-Type: application/json' \
  -d '{"model": "default", "messages": [{"role": "user", "content": "hi"}]}'
```

On first run, freedius loads an embedded default config (see
`cmd/freedius/templates/starter.yaml`) so it serves requests immediately.
The binary prints a shell snippet to stderr (the `env-inject` hint) —
copy those lines to point Claude Code or OpenCode at freedius. The hint
includes `ANTHROPIC_BASE_URL`, `ANTHROPIC_API_KEY`, and a few optional
Claude-Code-specific variables. Silence it with `--no-export-hint`.

freedius accepts Anthropic-format requests on `/v1/messages` and translates to
the upstream provider's protocol (the `MixAdapter` detects from the base URL
suffix). See `Installation` for the binary path and `Development` for build-
from-source instructions.

## Installation

Pre-built static binaries for Linux, macOS, and Windows (amd64/arm64) are
published on every `v*` tag. Grab the latest archive from the
[Releases](https://github.com/pfrack/freedius/releases) page, or install via:

```bash
go install github.com/pfrack/freedius/cmd/freedius@v0.1.0
```

`freedius --version` prints the installed tag.

## Configuration

freedius reads a YAML config file. Resolution order:

1. `--config <path>` (or `-c <path>`) flag
2. `freedius.yaml` or `freedius.yml` in the current directory
3. `~/.config/freedius/config.yaml` on Linux; `~/Library/Application Support/freedius/config.yaml` on macOS; `%AppData%\freedius\config.yaml` on Windows

When no config file is found, freedius loads the embedded starter at
`cmd/freedius/templates/starter.yaml` (the canonical NIM-only default — one
provider, one key, four model tiers — and the file this README's
Configuration example is abridged from). For cross-provider fallback, set
additional keys and add the corresponding `providers:` entries; the
`fallback:` lists under each mapping support any provider in scope.

### Example config

```yaml
providers:
  nim: { behavior: openai, default_api_key_env: NVIDIA_NIM_API_KEY }

mappings:
  default: { provider_name: nim, model_string: deepseek-ai/deepseek-v4-flash }
  opus:    { provider_name: nim, model_string: nvidia/nemotron-3-ultra-550b-a55b }
  sonnet:  { provider_name: nim, model_string: deepseek-ai/deepseek-v4-pro }
  haiku:   { provider_name: nim, model_string: deepseek-ai/deepseek-v4-flash }
  auto:    { provider_name: nim, model_string: deepseek-ai/deepseek-v4-flash }
```

### Mapping resolution

freedius resolves the `model` field against an exact match in `mappings`, then
a family prefix match (e.g. `claude-sonnet-4-6-...` → `claude-sonnet-4-6`).

### Fallback chains

When the primary fails (config error, transport failure, or upstream 4xx/5xx),
freedius tries each fallback in order:

```yaml
mappings:
  opus:
    provider_name: nim
    model_string: nvidia/nemotron-3-ultra-550b-a55b
    fallback:
      - provider_name: nim
        model_string: deepseek-ai/deepseek-v4-pro
      - provider_name: groq
        model_string: openai/gpt-oss-120b
```

### Provenance annotation

Mappings accept an optional `added_at` free-form string. Stored in the
mapping config and accessible from the dashboard's edit dialog; rendering on
the mapping card is tracked separately in the `mapping-first-ui-refactor`
change. Blank means unknown.

```yaml
mappings:
  opus:
    provider_name: nim
    model_string: nvidia/nemotron-3-ultra-550b-a55b
    added_at: 2026-07-06
```

## Web Dashboard

The embedded dashboard provides:

- **Live logs** — SSE streaming with level and provider/mapping filtering
- **Provider management** — add, edit, delete providers through the UI
- **Mapping management** — add, edit, delete mappings with fallback chains
- **Mapping cards** — routing shape plus provenance: an Active / Key Missing
  badge for whether the API key is in the environment right now, and a family
  badge (opus/sonnet/haiku). The highlighted step shows the last-used
  responder.
- **Health check** — `GET /health` returns `{"status":"ok"}`

Access at `http://localhost:8083/` (default). Set `FREEDIUS_UI_TOKEN` to require
bearer authentication on all dashboard routes (useful for LAN/Docker exposure).

## CLI & Environment Variables

| Flag | Default | Description |
|------|---------|-------------|
| `-c`, `--config <path>` | auto-resolve | Config file path |
| `--host` | `127.0.0.1` | Bind host (`0.0.0.0` to expose) |
| `--log-format` | `text` | Log output: `text` or `json` |
| `--no-export-hint` | | Suppress env-export hint on startup |
| `--port` | `8082` | Listen port |
| `--stream-timeout` | `5m` | Per-request upstream timeout |
| `--verbose-errors` | | Include upstream error detail in responses |
| `--ui-port` | `8083` | Dashboard port |
| `--ui-host` | `127.0.0.1` | Dashboard bind address |

| Variable | Description |
|----------|-------------|
| `FREEDIUS_PORT` | Override `--port` |
| `FREEDIUS_HOST` | Override `--host` |
| `FREEDIUS_LOG` | Override `--log-format` |
| `FREEDIUS_VERBOSE_ERRORS` | Set to `1` for verbose errors |
| `FREEDIUS_STREAM_TIMEOUT` | Override `--stream-timeout` |
| `FREEDIUS_FALLBACK_TIMEOUT_MULTIPLIER` | Scales the total fallback-chain timeout as a multiple of the per-attempt stream timeout (default `2`) |
| `FREEDIUS_UI_PORT` | Override `-ui-port` |
| `FREEDIUS_UI_HOST` | Override `-ui-host` |
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
mage test      # tests with race detection and coverage
mage lint      # staticcheck + golangci-lint
mage ci        # full CI check
mage format    # gofmt, goimports, golines, gci
```

## Reference

The full provider table lives in
[`providers.yaml`](providers.yaml) as the single source of truth — run
`go generate ./...` after adding an entry. Each entry declares a behavior
class (`openai` | `anthropic` | `mix`), an optional `default_base_url`, an
optional `default_api_key_env`, an optional `require_base_url` flag, an
optional `manual` flag (skip codegen), and a behavior-specific sub-block
(e.g. `openai.no_stream_usage`, `openai.pre_send_hook`).

Response headers:
- `X-Freedius-Request-ID` — unique request identifier
- `X-Freedius-Matched-Provider` — the provider that handled the request
- `X-Freedius-Matched-Model` — the upstream model name
- `X-Freedius-Error-Type` — set on 4xx/5xx responses; error category
- `X-Freedius-Error-Message` — set on 4xx/5xx responses; human-readable message

Built-in endpoints:
- `GET /` — health check, returns 200 with `{"status":"ok"}` JSON body
- `HEAD /` — health check, returns 200 with no body
- `GET /health` — health check, returns 200 with `{"status":"ok"}` JSON body
- `HEAD /health` — health check, returns 200 with no body
