# freedius

[![CI](https://github.com/pfrack/freedius/actions/workflows/ci.yml/badge.svg)](https://github.com/pfrack/freedius/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/pfrack/freedius)](https://github.com/pfrack/freedius/releases/latest)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

A local HTTP proxy that routes LLM API requests from AI coding agents
(Claude Code, OpenCode) to upstream providers — with fallback chains,
model-name mapping, and a live web dashboard.

Built for solo developers who want cheaper inference than Anthropic's
direct pricing without spinning up a production gateway. One config
file, one local process, no account to sign up for. For the full
contributor guide, see [AGENTS.md](AGENTS.md).

> **Why freedius?** Your coding agent already speaks the Anthropic API.
> freedius sits in front of any LLM upstream — free tiers (NVIDIA NIM),
> cheaper providers (DeepSeek, Groq), or local models (Ollama, LM
> Studio) — and translates protocols as needed. Configure a fallback
> chain once and your dev loop survives provider outages, rate limits,
> or credit exhaustion without editing the config again.

## Installation

Pre-built static binaries for Linux, macOS, and Windows (amd64/arm64) are
published on every `v*` tag. Grab the latest archive from the
[Releases](https://github.com/pfrack/freedius/releases) page, or install via:

```bash
go install github.com/pfrack/freedius/cmd/freedius@v0.1.0
```

`freedius --version` prints the installed tag.

## Docker

`Dockerfile` (distroless static, `nonroot` user, ports 8082/8083) and
`docker-compose.yml` are real artifacts for local container runs.
`magefiles/mage.go` exposes `mage dockerBuild`, `mage dockerRun`, and
`mage dockerPush` for the workflow.

```bash
mage dockerBuild && mage dockerRun
```

The container expects `FREEDIUS_HOST=0.0.0.0` and `FREEDIUS_UI_HOST=0.0.0.0`
to bind on the Docker network; `docker-compose.yml` already sets them.
No image is published to a registry yet — that pipeline is a separate
future change.

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
copy those lines to point Claude Code at freedius. The hint is
Anthropic-shaped (`ANTHROPIC_BASE_URL`, `ANTHROPIC_API_KEY`, and a few
Claude-Code-specific variables); OpenCode can consume it as well when it
honors Anthropic-compatible env-var overrides. Silence it with
`--no-export-hint`.

### Point Claude Code at freedius

Instead of pasting the snippet, one command wires it up permanently:

```bash
freedius configure          # backs up, then writes ~/.claude/settings.json
freedius configure --restore   # undo: put the newest backup back
```

`configure` first copies `~/.claude/settings.json` to a timestamped
`settings.json.bak.<timestamp>` next to it, shows what it will write, and
asks for confirmation. It then **overwrites** the file with freedius's env
block only — any custom keys you had live on in the backup, and
`freedius configure --restore` brings them back. Use `--dry-run` to preview
without touching anything, `--yes` to skip the prompt in scripts, and
`--config-dir DIR` to target a directory other than `~/.claude`. The
startup snippet remains the manual, shell-only alternative.

freedius accepts Anthropic-format requests on `/v1/messages` and translates to
the upstream provider's protocol (the `MixAdapter` detects from the base URL
suffix). See `Development` for build-from-source instructions.

## Configuration

freedius reads a YAML config file. Resolution order:

1. `--config <path>` (or `-c <path>`) flag
2. `freedius.yaml` or `freedius.yml` in the current directory
3. `~/.config/freedius/config.yaml` on Linux; `~/Library/Application Support/freedius/config.yaml` on macOS; `%AppData%\freedius\config.yaml` on Windows

When no config file is found, freedius loads the embedded starter at
`cmd/freedius/templates/starter.yaml` — NIM-primary for every tier, with a
per-tier `fallback:` chain that steps down through cheaper NIM models, then
Nous Research, then Kilo as the last resort. Only `NVIDIA_NIM_API_KEY` is
required; `NOUS_API_KEY` and `KILO_API_KEY` are optional and only log a
startup warning when unset. The `fallback:` lists under each mapping support
any provider in scope.

### Example config

```yaml
providers:
  nim: { behavior: openai, default_api_key_env: NVIDIA_NIM_API_KEY }

mappings:
  default: { provider_name: nim, model_string: nvidia/nemotron-3-nano-30b-a3b }
  opus:    { provider_name: nim, model_string: nvidia/nemotron-3-ultra-550b-a55b }
  sonnet:  { provider_name: nim, model_string: nvidia/llama-3.3-nemotron-super-49b-v1 }
  haiku:   { provider_name: nim, model_string: nvidia/nemotron-3-nano-30b-a3b }
  auto:    { provider_name: nim, model_string: nvidia/nemotron-3-nano-30b-a3b }
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
        model_string: nvidia/nemotron-3-super-120b-a12b
      - provider_name: nous
        model_string: tencent/hy3:free
      - provider_name: kilo
        model_string: kilo-auto/free
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

## Supported Providers

The full provider list lives in
[`providers.yaml`](providers.yaml) — add a new provider by editing
that file and running `go generate ./...`.

| Provider | Behavior | API key env var |
|----------|----------|------------------|
| NVIDIA NIM | openai | `NVIDIA_NIM_API_KEY` _(free tier)_ |
| Groq | openai | `GROQ_API_KEY` _(free tier)_ |
| Google Gemini | openai | `GEMINI_API_KEY` _(free tier)_ |
| Mistral | openai | `MISTRAL_API_KEY` _(free tier)_ |
| DeepSeek | openai | `DEEPSEEK_API_KEY` |
| Together | openai | `TOGETHER_API_KEY` |
| Fireworks | openai | `FIREWORKS_API_KEY` |
| Cohere | openai | `COHERE_API_KEY` |
| Nous Research | openai | `NOUS_API_KEY` |
| Kilo | openai | `KILO_API_KEY` |
| Ollama (local) | openai | _(no key — local server)_ |
| LM Studio (local) | openai | _(no key — local server)_ |
| Anthropic | anthropic | `ANTHROPIC_API_KEY` |
| OpenCode Zen | mix | `OPENCODE_API_KEY` |
| OpenCode Go | mix | `OPENCODE_API_KEY` |
| OpenAI (BYO endpoint) | openai | _(set in your config)_ |
| mix (passthrough) | mix | _(set in your config)_ |
| custom | mix | _(set in your config)_ |

Behavior classes:
- `openai` — standard OpenAI-format upstreams.
- `anthropic` — Anthropic-format upstreams.
- `mix` — protocol auto-detected from the `base_url` suffix (`/v1/messages` → Anthropic, `/v1/chat/completions` → OpenAI).

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
| `FREEDIUS_UI_PORT` | Override `--ui-port` |
| `FREEDIUS_UI_HOST` | Override `--ui-host` |
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

For the full contributor guide — commit conventions, hook details, and
release process — see [AGENTS.md](AGENTS.md).

```bash
mage test      # tests with race detection and coverage
mage lint      # staticcheck + golangci-lint
mage ci        # full CI check
mage format    # gofmt, goimports, golines, gci
```

### Build from source

Requires [mage](https://magefile.org) (`go install github.com/magefile/mage`):

```bash
mage build         # produce ./freedius (local binary)
mage install       # produce $GOPATH/bin/freedius
mage installHooks  # install pre-commit / pre-push hooks
```

`scripts/pre-commit` runs `mage lint` + `mage generateCheck` before
every commit. `scripts/pre-push` runs `go test -race` on the packages
you changed versus `origin/main`. To skip the push hook in an
emergency, use `git push --no-verify`.

For the full contributor guide — commit conventions, hook details, and
release process — see [AGENTS.md](AGENTS.md).

## Contributing

Bug reports, pull requests, and feature proposals are welcome. The
build, test, and commit conventions live in [AGENTS.md](AGENTS.md);
this README keeps only the user-facing surface. `CONTRIBUTING.md`,
`CHANGELOG.md`, and `SECURITY.md` are not yet present and may land in
future changes.

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