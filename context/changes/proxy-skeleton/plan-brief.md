# Proxy Skeleton (F-01) — Plan Brief

> Full plan: `context/changes/proxy-skeleton/plan.md`
> Research: `context/changes/proxy-skeleton/research.md` (none — greenfield change)

## What & Why

Wire the F-01 request loop for freedius: an HTTP server that loads a YAML config of model→provider mappings, listens on a local port, and dispatches incoming Claude Code requests to a no-op handler. The handler returns 501 with the matched provider info — proving the routing works end-to-end without yet doing any real proxying. This is the foundation slice the entire north-star (S-01: first-call-routed through NIM) plugs into.

## Starting Point

The codebase is greenfield: `go.mod` exists (module `github.com/user/freedius`, Go 1.26.1) but contains no source files. `AGENTS.md` codifies the conventions: `main.go` + `config/` + `proxy/` packages, `gofumpt` formatting, no comments in code, env-var config for runtime knobs, `govulncheck` for audits. The roadmap's F-01 risk note warns explicitly against over-engineering — keep this slice to the smallest loop that can receive a request, parse a config, and dispatch to a no-op handler.

## Desired End State

After F-01 lands, `make ci` is green and `./freedius` runs as a local HTTP server: it loads a YAML config, listens on `127.0.0.1:8080` (or the configured port), and dispatches each incoming request to a handler that returns 501 with the matched provider/model in the response body and `X-Freedius-*` headers. Sending an unmapped model returns 501 with `status: "no_match"`; sending a malformed body returns 400. Startup errors (missing config, malformed YAML, port already in use) produce one actionable stderr line and a non-zero exit. GitHub Actions runs vet/test/build/govulncheck on every push.

## Key Decisions Made

| Decision                       | Choice            | Why (1 sentence)  | Source           |
| ------------------------------ | ----------------- | ----------------- | ---------------- |
| Config schema                  | Nested `model→{provider, model}` | Provider is a first-class field — no string parsing at dispatch time, easy to extend with per-provider options. | Plan |
| Config file location           | CWD first (`freedius.yaml`/`freedius.yml`), then XDG | Zero-setup for ad-hoc use, idiomatic for installed CLI; both common Linux/macOS conventions covered. | Plan |
| Port configuration             | `--port` flag > `FREEDIUS_PORT` env > `8080` default | Flag overrides env overrides default — the standard 12-factor precedence; ad-hoc overrides easy. | Plan |
| Module layout                  | `main.go` + `config/` + `proxy/` from day one | Matches AGENTS.md structural hint; package boundaries set early so S-01 plugs into `proxy/` rather than creating it. | Plan |
| Dispatch stub response         | `501` + JSON body + `X-Freedius-Matched-Provider`/`-Model` headers | 501 is honest about "not done yet"; matched-provider info proves routing works; same response shape is S-01's integration point. | Plan |
| Startup error behavior         | Fail fast with one actionable stderr line + `os.Exit(1)` | Matches AGENTS.md "Panic only at package init or `main()` entry"; satisfies FR-005 "clear error on port conflict" + the config-error guardrail. | Plan |
| Multi-agent concurrency        | `net/http` default; config read-only at request time | Per-request goroutine isolation + immutable config satisfies the NFR with zero custom code. | Plan |
| CI in F-01                     | Add `.github/workflows/ci.yml` + `Makefile` | Closes the gap from bootstrap ("CI not yet set up"); every subsequent change gets CI for free. | Plan |
| YAML library                   | `github.com/goccy/go-yaml` (strict mode) | Best line+col error messages for "clear error" guardrail; active, popular, well-maintained. | Plan |
| Provider name validation       | Validate against closed set `{nim, zen, go, custom}` | Catches typos at startup instead of silently routing to 501 forever; matches FR-009's "custom providers must be Anthropic-compatible" implicit closed set. | Plan |
| Dispatch stub input handling   | `json.Unmarshal` into `{Model string}`, 400 on failure | Closest to what S-01 needs anyway (Anthropic-format requests are JSON with a `model` field); proves the full request loop. | Plan |
| Test coverage                  | Comprehensive table-driven, 80%+ per package | Locks in the F-01 contract via tests; S-01 can change the dispatch handler without breaking the foundation. | Plan |
| Host binding                   | `127.0.0.1` default, `--host` flag accepts `127.0.0.1`/`0.0.0.0` | Default-secure per NFR-Privacy; explicit opt-in for LAN exposure; other IPs rejected with clear error. | Plan |
| Logging                        | `log/slog` `TextHandler` to stderr at Info, tagged with `component` | Modern stdlib structured logging; TTY/JSON auto-switching deferred (would need `golang.org/x/term`; not worth it for v1). | Plan |

## Scope

**In scope:**
- HTTP server (`net/http`) listening on `127.0.0.1:<port>` with graceful shutdown
- YAML config loader (strict mode, provider set validation) with full error coverage
- Dispatch handler stub: parse request body, look up model in config, return 501 with matched info
- Flag/env parsing for `--port`, `--host`, `--config`
- Config path resolution (CWD → XDG)
- `slog` setup with `component` tags
- GitHub Actions CI workflow + Makefile
- Table-driven tests for both packages (80%+ coverage)
- `config.example.yaml` checked in as schema documentation

**Out of scope:**
- Real provider translation (Anthropic→OpenAI, etc.) — S-01
- Provider adapters (NIM, Zen, Go, custom) — S-01 and S-02
- Credential loading from env vars — S-01
- `freedius init` command or config template generation — S-03
- Request-body logging (NFR-Privacy forbids it)
- Metrics endpoint, pprof, in-flight counters
- TTY/JSON auto-switching for slog
- Windows-specific path handling (roadmap "Parked" — Linux + macOS only)
- Auto-injection of Claude Code env vars — S-03

## Architecture / Approach

Three small packages and one entry point. Config is loaded once at startup into an immutable struct; the dispatch handler closes over it. `net/http` provides per-request goroutine isolation for free. The dispatch handler's response shape (`{matched_provider, matched_model, status}` + `X-Freedius-*` headers) is the contract S-01 will preserve — just change the status code and body to a real proxied response.

```
                          ┌─────────────────┐
   Claude Code ──POST──▶  │  net/http       │
                          │  ServeMux       │
                          │  (catch-all /)  │
                          └────────┬────────┘
                                   │
                                   ▼
                          ┌─────────────────┐
                          │  Dispatcher     │
                          │  (proxy/)       │
                          │  - parse body   │
                          │  - lookup model │
                          │  - return 501   │
                          └────────┬────────┘
                                   │ reads
                                   ▼
                          ┌─────────────────┐
                          │  Config         │
                          │  (config/)      │  ← loaded once at startup
                          │  Models map     │
                          └─────────────────┘
                                   ▲
                          ┌────────┴────────┐
                          │  freedius.yaml  │  ← CWD first, then XDG
                          └─────────────────┘
```

## Phases at a Glance

| Phase     | What it delivers       | Key risk                  |
| --------- | ---------------------- | ------------------------- |
| 1. Project scaffolding | go.mod updated, .gitignore, .github/workflows/ci.yml, Makefile, config.example.yaml; green `make ci` | None — pure file additions; nothing can fail at runtime |
| 2. config/ package | YAML loader with strict mode, provider set validation, full error coverage; 80%+ test coverage | Wrong error message format makes debugging config issues painful — must use `yaml.FormatError` with line+col |
| 3. proxy/ package | Dispatch handler: parse body, lookup model, return 501 with matched-provider info; 80%+ test coverage | Tempting to over-implement (real request transformation, retries) — keep it strictly the 501 stub; S-01 owns the real proxying |
| 4. main.go wiring | Flag/env parsing, config path resolution, slog setup, server with graceful shutdown; end-to-end smoke tests | Wiring bugs are easy to introduce; mitigated by the manual-verification checklist in Phase 4 (12 specific scenarios) |

**Prerequisites:** None — greenfield.
**Estimated effort:** ~4-6 hours of focused work across 4 phases. Each phase is small enough to land as a single commit; the whole slice can ship in one session.

## Open Risks & Assumptions

- **goccy/go-yaml strict-mode behavior**: the goccy docs reference `yaml.Strict()` as an option; the exact error message format on unknown fields should be confirmed in Phase 2 by running a failing test against an actual typo'd config. If the format is less actionable than expected, we wrap it.
- **10 MiB request body limit**: chosen as a reasonable LLM-request ceiling. If real Claude Code requests exceed this, it's a one-line constant change in `proxy/proxy.go`. Not a blocker for F-01.
- **govulncheck not installed locally** (per bootstrap-verification): `make ci` excludes govulncheck; CI runs it. If a developer needs it locally, the README/AGENTS.md should document `go install golang.org/x/vuln/cmd/govulncheck@latest`.
- **Empty `models:` block treated as an error**: an empty config file currently produces a confusing "yaml: unmarshal empty" error. Phase 2 adds a post-parse check that converts this to "config file at <path> contains no model mappings" — a better error.
- **Module path `github.com/user/freedius`**: AGENTS.md flags this as a placeholder to update before publishing. F-01 doesn't change it; the implementer should not touch it either — that's a separate housekeeping change.

## Success Criteria (Summary)

- `make ci` is green on every push (PR + main branch).
- `./freedius` starts, loads a config, and serves `501 Not Implemented` with matched-provider info for every known model, `status: no_match` for unknown models, and `400` for malformed bodies.
- Every startup error path (missing config, malformed YAML, unknown provider, invalid port/host, port already in use) produces one actionable stderr line and a non-zero exit.
- SIGINT/SIGTERM trigger a clean shutdown within 5 seconds.
- `go test -race -cover ./...` shows ≥ 80% coverage for `config/` and `proxy/`.
