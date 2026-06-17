---
date: 2026-06-17T00:00:00+02:00
researcher: pfrack
git_commit: be4f1f142d2f5193e1ddfddd158d0cc281d51736
branch: zen-go-adapters
repository: pfrack/freedius
topic: "Error hardening + env auto-injection + config template (S-04)"
tags: research, codebase, s-04, error-handling, env-injection, claude-code, init-subcommand
status: complete
last_updated: 2026-06-17
last_updated_by: pfrack
---

# Research: Error hardening + env auto-injection + config template (S-04)

**Date**: 2026-06-17
**Researcher**: pfrack
**Git Commit**: `be4f1f1`
**Branch**: `zen-go-adapters`
**Repository**: `pfrack/freedius`

> Scope: comprehensive dive into the S-04 roadmap slice. Three sub-areas: (1) error-path hardening with clear user-facing messages, (2) Claude Code env auto-injection, (3) `freedius init` config template. Failure-path mapping prioritized per user direction.
> Branch `zen-go-adapters` is local-only (4 commits ahead of `origin/zen-go-adapters`); all file references are local. Permalinks to be added after the next `git push`.

## Research Question

What does `freedius` need to ship for S-04, and what constraints shape the design?

The roadmap slice S-04 ([context/foundation/roadmap.md:34](context/foundation/roadmap.md)) bundles three PRD requirements:

- **NFR-Error-handling** ([prd.md:88](context/foundation/prd.md)) — "provider errors are forwarded to Claude Code as descriptive messages; freedius itself does not crash or drop requests on config or provider errors."
- **Success-Criteria-Primary** ([prd.md:32](context/foundation/prd.md)) — "(freedius) writes a config file (or freedius generates a template)".
- **Success-Criteria-Secondary** ([prd.md:36](context/foundation/prd.md)) — "Freedius auto-injects Claude Code environment variables so the dev doesn't have to manually set `CLAUDE_CODE_API_BASE` or equivalent."

The plan that consumes this research will need to know: what failure paths exist today, where the messages are bad, how Claude Code discovers proxies, and how to add `freedius init` to a single-`run()` `main.go`.

## Summary

1. **Error paths are 70% good, 30% bad.** Config loading and per-entry validation produce clear, descriptive messages with file path + field name + known-values list. Dispatch and adapter errors are where it falls apart: pre-WriteHeader adapter errors are **swallowed** by the dispatcher ([proxy/proxy.go:107](proxy/proxy.go)) into a generic `"upstream error"` body; panics in handlers have **no recovery middleware** ([main.go:97-103](main.go)); three different error JSON shapes exist for morally-the-same condition; the OpenAI HTTP client has **no `Timeout`** ([proxy/openai_compat.go:21](proxy/openai_compat.go)).
2. **The PRD placeholder `CLAUDE_CODE_API_BASE` does not exist.** The canonical variable is `ANTHROPIC_BASE_URL` (with `ANTHROPIC_API_KEY` or `ANTHROPIC_AUTH_TOKEN`). Claude Code reads env from process env *and* from `~/.claude/settings.json` `env` block; shell env wins over settings-file env. Companion var `ENABLE_TOOL_SEARCH=true` is required because `ANTHROPIC_BASE_URL` pointing at a non-first-party host disables MCP tool search.
3. **`main.go` is a 190-line single-`run()` function with zero subcommand parsing.** Today, `freedius init` is silently dropped by stdlib `flag`, then config load fails. Adding `init` / `version` / `serve` is ~40 LoC of stdlib dispatch + a `templates/starter.yaml` loaded via `embed.FS` (~5 LoC) + the starter template content (~50 LoC of YAML).
4. **Cross-cutting concern: adapter error messages leak the inner adapter name**, not the configured provider name. A user with `provider: nim` sees `"openai adapter: env var NIM_API_KEY is not set"`. Fix: pass the original provider name into inner adapters via a wrapper.
5. **Total S-04 LoC estimate: ~700-1000 LoC across 3-4 phases** (error hardening + env injection + init subcommand). Within the MVP budget ([prd.md:13](context/foundation/prd.md) — 2 weeks, after-hours).

## Detailed Findings

### A. Error-path mapping

#### A.1 Config loading — `config/config.go`, `config/defaults.go`, `main.go:66-77`

The config-load surface is in good shape. `Load` returns wrapped errors with file path + line/column for YAML parse failures, and the friendly-hint path for "file not found" is already in `main.go:73-75` ("create one or pass --config <path>"). Per-entry validation messages name the model/mapping, the field, and (for unknown providers) the sorted canonical list of valid values ([config/config.go:94-96](config/config.go)).

**Gap**: when the implicit-search path resolves to a non-existent `~/.config/freedius/config.yaml`, the user sees **two error messages** — the path-resolution `slog.Error` AND the `Load` failure. Should be a single clear "no config found, run `freedius init`" message.

**No startup banner** — the first `slog.Info` is the "listening on" line ([main.go:84](main.go)). Slow config loads show nothing.

**Inconsistent error sinks** — path-resolution errors use `slog.Error` ([main.go:68](main.go)); everything else uses raw `fmt.Fprintf(os.Stderr, ...)` via `failf` ([main.go:131-134](main.go)). After `os.Exit(1)`, deferred writes never run.

#### A.2 Per-entry validation — `config/config.go:87-115`

All validation messages are descriptive. The unknown-provider message is exemplary — it lists the sorted canonical names so the user can self-correct.

**Gap**: `provider=zen` users see `provider=mix` in config errors because `applyEntryDefaults` rewrites ([config/defaults.go:36-43](config/defaults.go)). Per `context/foundation/lessons.md:15-19`, this is a known footgun; tests must use post-rewrite names. The fix is to preserve the original provider name on the `Model` struct (one new field, two rewrite points).

**Dead code under current flow**: `main.go:137-145` checks `cfg.UsesProvider("zen")` and `cfg.UsesProvider("go")` for required env vars, but `applyEntryDefaults` runs **before** this check and rewrites `zen`/`go` → `mix`, so the branches are unreachable. Only the `nim` branch ever fires. Result: a `provider: zen` user with a missing `OPENCODE_API_KEY` sees `"provider=zen"` in the error (from the **per-model** check at line 147), but a `provider: mix` user sees `"provider=mix"`. The fix is to centralize provider-name lookup through the new `OriginalProvider` field.

#### A.3 Dispatch — `proxy/proxy.go:36-109`

The dispatcher handles 8 distinct failure modes with status codes ranging from 400 to 502. The status codes are correct. The body shapes are inconsistent:

| Failure | Status | Body shape | File:line |
|---|---|---|---|
| Method not POST | 405 | text/plain via `http.Error` | [proxy/proxy.go:37-41](proxy/proxy.go) |
| Wrong content-type | 415 | `{"error":"unsupported content type..."}` | [proxy/proxy.go:43-46](proxy/proxy.go) |
| Body too large (>10 MB) | 413 | `{"error":"request body too large..."}` | [proxy/proxy.go:51-60](proxy/proxy.go) |
| Empty body | 400 | `{"error":"invalid request body: empty"}` | [proxy/proxy.go:62-65](proxy/proxy.go) |
| Malformed JSON | 400 | `{"error":"invalid request body: <err>"}` | [proxy/proxy.go:70-73](proxy/proxy.go) |
| Missing model field | 400 | `{"error":"invalid request body: missing..."}` | [proxy/proxy.go:75-78](proxy/proxy.go) |
| Model not mapped | 404 | `{"status":"no_match"}` | [proxy/proxy.go:80-93](proxy/proxy.go) |
| Provider not registered | 500 | `{"error":"provider not registered: X"}` | [proxy/proxy.go:99-104](proxy/proxy.go) |
| Adapter returned error | 502 | `{"error":"upstream error"}` — **original err logged, lost to client** | [proxy/proxy.go:105-108](proxy/proxy.go) |

**Critical gap** — the adapter-failure 502 swallows the original error message. NFR-Error-handling requires "descriptive messages." The fix is to forward the error to the client for **pre-WriteHeader** failures (per `lessons.md:33-43` — adapters return nil post-WriteHeader; the dispatcher is the only place that can write the body in that state, and it shouldn't).

**Three different shapes for morally-the-same condition**: `{"status":"no_match"}` vs `{"error":"upstream error"}` vs `{"error":"upstream_unreachable","detail":"..."}` (from `proxy/errors.go:32-35`). Should be unified to `{"error":"<machine_code>","message":"<human>","detail":"<optional context>"}`.

**No request-ID / correlation ID** is generated. Multiple concurrent Claude Code sessions produce interleaved log lines with no way to separate them.

#### A.4 Adapter Handle contract — per-adapter failure matrix

| Adapter | Pre-WriteHeader errors returned? | Post-WriteHeader errors returned? | Error message template |
|---|---|---|---|
| `AnthropicCompatibleAdapter` | yes (line 25-31) | no (correct per `lessons.md`) | `"anthropic adapter: ..."` |
| `OpenAICompatibleAdapter` | yes (line 27-33) | no (correct) | `"openai adapter: ..."` |
| `NIMAdapter` | inherited | inherited | inner `"openai adapter: ..."` |
| `CustomAdapter` | inherited | inherited | inner `"anthropic adapter: ..."` |
| `MixAdapter` | inherited | inherited | `"mix adapter: ..."` for URL parse, inner otherwise |

**Cross-cutting pattern** — all four production adapters duplicate the same two checks (`base_url` + `api_key_env`) with nearly identical error templates. The error messages leak the inner adapter name, not the configured provider name. The `custom` → `anthropic` rewrite means a user with `provider: custom` sees `"anthropic adapter: env var X is not set"` even though they configured `custom`. The `zen`/`go` → `mix` rewrite (per `lessons.md:15-19`) means `provider: zen` users see `"openai adapter: ..."` or `"anthropic adapter: ..."` depending on URL suffix.

**Per-adapter gaps**:

- `OpenAICompatibleAdapter.Handle` — `http.Client{}` with **no `Timeout`** ([proxy/openai_compat.go:21](proxy/openai_compat.go)). A hanging upstream holds the goroutine forever. The Anthropic compat adapter is bounded by `r.Context()` (httputil.ReverseProxy honors cancellation); OpenAI compat is not.
- `OpenAICompatibleAdapter.Handle` — non-SSE `Content-Type` on a 200 response (e.g., a misconfigured upstream returning JSON instead of `text/event-stream`) is **not detected**; the translator tries to parse SSE, fails on the first chunk, and the user sees a truncated Anthropic-format SSE.
- `MixAdapter.Handle` — URL routing is suffix-based ([proxy/mix.go:30](proxy/mix.go) — `strings.HasSuffix(parsedURL.Path, "/v1/messages")`). A typo like `/v1/messagess` silently routes to the OpenAI adapter. Should log the routing decision at Debug (or Info on mismatch) and reject unknown suffixes.

#### A.5 SSE streaming — `proxy/translate/anthropic_openai.go:272-296`

`TranslateStream` reads via `bufio.Reader.ReadBytes('\n')` (per `lessons.md:9-13` — unbounded line length, no Scanner truncation). `emitter.consume` parses one OpenAI chunk per call; JSON unmarshal errors are returned and surfaced as a `slog.Error` from `OpenAICompatibleAdapter` ([openai_compat.go:65](proxy/openai_compat.go)).

**Error recovery** is logged-only after the SSE headers are sent. The user sees a truncated stream with no explanation. Per `lessons.md:33-43`, returning nil is correct, but the client has no way to distinguish "model stopped" from "freedius gave up." A structured `stream aborted: <reason>` event before close would help.

**No per-request timeout** on the stream — an upstream that hangs between events holds the goroutine indefinitely (combines with the `http.Client` timeout gap above).

#### A.6 Panic surfaces — `main.go:97-103`

`http.Server` is constructed with `mux` as `Handler`. There is **no panic recovery middleware** anywhere in production code (the only `recover()` calls are in tests). A panic in any handler, adapter, or translator will:

1. unwind the goroutine serving the request
2. log the panic + stack trace to stderr (Go runtime default)
3. close the client connection (no response sent)
4. leave the server running (the listener keeps accepting)

The client sees a connection reset, **not a 500**. A `recoverMiddleware` wrapping `mux.Handle("/", dispatcher)` would write a structured 500 (if headers not yet sent) and log at Error.

#### A.7 Logging — `slog.NewTextHandler(os.Stderr, LevelInfo)` at `main.go:62-63`

| Location | Level | Keys |
|---|---|---|
| `main.go:68` | Error | `err` |
| `main.go:84,121,127` | Info | `host, port` / — / — |
| `proxy/proxy.go:90,95` | Debug | `model` / `model, provider, target_model` |
| `proxy/proxy.go:101,106,115,123` | Error | `provider` / `provider, err` / `err` / `err` |
| `proxy/openai_compat.go:65` | Error | `err` |
| `proxy/errors.go:26,29` | Debug / Error | `path` / `err, path` |

**Observations**:

- **No request ID** is generated or threaded. Multi-session log correlation is impossible (per NFR-Multi-agent).
- **No body logging** — NFR-Privacy is satisfied by omission, not by policy. Add a `// DO NOT log request/response bodies (PRD NFR-Privacy)` comment at the top of `proxy/proxy.go` and `proxy/translate/anthropic_openai.go`.
- **No HTTP access log** — debugging "what requests did freedius see" requires adding trace middleware or running with `FREEDIUS_LOG=debug`.
- **Text format, not JSON** — the user has no way to opt into JSON output for log aggregation. `FREEDIUS_LOG=json` would be a natural flag.

#### A.8 Process exit semantics — `main.go:36-38, 105-128`

`main()` calls `os.Exit(run())` ([main.go:37](main.go)). `os.Exit` skips deferred functions, so `defer stop()` and `defer cancel()` never run on the exit path. Today this is harmless (no pending writes), but if a future `freedius init` does buffered writes before exiting, this becomes a leak.

**Only `SIGINT` and `SIGTERM`** are handled ([main.go:105](main.go)). `SIGHUP` (from `nohup`/`disown`) doesn't trigger graceful shutdown.

**No `recover` in `run()`** — a panic in setup (e.g., a future `freedius init` subcommand) bypasses cleanup. Wrap `run()` in `defer recover()`.

**Shutdown timeout** is 5 seconds ([main.go:25](main.go)). SSE streams that may not be enough — the user could see truncated responses on shutdown.

### B. Claude Code env auto-injection

#### B.1 The PRD placeholder does not exist

`prd.md:36` and `roadmap.md:75` both reference `CLAUDE_CODE_API_BASE`. **There is no `CLAUDE_CODE_API_BASE` variable in Claude Code** (verified against the official [env-vars doc](https://docs.claude.com/en/docs/claude-code/env-vars), [CLI reference](https://docs.claude.com/en/docs/claude-code/cli-reference), and [LLM-gateway doc](https://docs.claude.com/en/docs/claude-code/llm-gateway)). The canonical name is `ANTHROPIC_BASE_URL`.

→ S-04 implementation should use `ANTHROPIC_BASE_URL`. A separate PRD-amendment change should update the PRD + roadmap to reflect the real variable name. This is a one-line copy fix; out of scope for the implementation itself.

#### B.2 Variables that matter

| Variable | Purpose | Default if unset | Freedius impact |
|---|---|---|---|
| `ANTHROPIC_BASE_URL` | Override the API endpoint to route through a proxy/gateway | `https://api.anthropic.com` | **Required.** This is what tells Claude Code to talk to freedius. |
| `ANTHROPIC_API_KEY` | API key sent as `X-Api-Key` header | OAuth subscription token | **Required.** Can be any string (freedius accepts the dummy key); `freedius-dummy` is conventional. |
| `ANTHROPIC_AUTH_TOKEN` | Bearer token for `Authorization` header (LLM gateway convention) | unset | Optional; alternative to `ANTHROPIC_API_KEY`. |
| `ENABLE_TOOL_SEARCH` | Re-enable MCP tool search on non-first-party `ANTHROPIC_BASE_URL` | disabled | **Required.** Without this, MCP tool search is disabled when pointing at freedius. |
| `DISABLE_TELEMETRY` | Opt out of telemetry events | on | **Recommended** — aligns with NFR-Privacy ([prd.md:90](context/foundation/prd.md)). |
| `DISABLE_ERROR_REPORTING` | Opt out of Sentry | on | **Recommended** — same. |
| `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC` | Composite kill-switch | unset | Optional shortcut for the two above. |
| `CLAUDE_CONFIG_DIR` | Override config dir | `~/.claude` | N/A unless the dev runs multiple accounts. |

#### B.3 The settings.json mechanism (key finding)

Claude Code reads env from process env *and* from a `settings.json` file **at startup, regardless of how `claude` was launched** ([env-vars doc](https://docs.claude.com/en/docs/claude-code/env-vars), [settings doc](https://docs.claude.com/en/docs/claude-code/settings)).

Locations:

- `~/.claude/settings.json` — user scope, every project
- `.claude/settings.json` — project scope, checked into source control
- `.claude/settings.local.json` — local scope, just this project, gitignored

Format:

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://127.0.0.1:8080",
    "ANTHROPIC_API_KEY": "freedius-dummy",
    "ENABLE_TOOL_SEARCH": "true",
    "DISABLE_TELEMETRY": "1",
    "DISABLE_ERROR_REPORTING": "1"
  }
}
```

**Precedence** ([env-vars doc](https://docs.claude.com/en/docs/claude-code/env-vars)): shell env > settings-file env. A user who already has `ANTHROPIC_API_KEY` set in their shell wins over the injected value. **Desirable** — see edge cases below.

**Caveat — `CLAUDE_CODE_PROVIDER_MANAGED_BY_HOST`**: if a host platform (Claude Desktop, IDE extension) sets this flag, settings-file `ANTHROPIC_BASE_URL`/`ANTHROPIC_API_KEY` are ignored. Only shell env still works. Out of scope for the target persona (terminal-native developer per `prd.md:26`).

#### B.4 Scope coverage

| Component | Uses `ANTHROPIC_BASE_URL`? | Notes |
|---|---|---|
| Main session | yes | reads `~/.claude/settings.json` at startup |
| Sub-agents | yes | inherit parent process env, also re-read settings.json on launch |
| MCP servers (their URLs) | no | configured in `.mcp.json` independently |
| MCP tool calls (model side) | yes | tool-use requests flow through freedius |
| Skills | yes for model calls | skills are prompt bundles, but tool calls triggered by skills go through freedius |
| Background tasks (`--bg`, `claude remote-control`) | yes | inheriting shell env / settings.json |

**One write to `~/.claude/settings.json` covers everything.**

#### B.5 Mechanism design space

| Option | Mechanism | Pros | Cons | LoC | Verdict |
|---|---|---|---|---|---|
| **A: Wrapper script** | `freedius init` writes `~/.local/bin/freedius-claude` that exports then `exec`s `claude` | Zero Claude Code changes; user sees wrapper in `PATH` | User must type `freedius-claude`; breaks `claude` muscle memory; shebang/script-language fragmentation | ~80 | **Defer to v2.** Most invasive UX; least automatic. |
| **B: Print eval-snippet on startup** | Stderr message: `export ANTHROPIC_BASE_URL=...; export ANTHROPIC_API_KEY=...` with comment hints | Zero coupling with settings file; respects shell-env precedence; works for direnv/dotenv users | User must copy-paste or add to rc | ~30 | **Always-on baseline.** Minimal, zero surprise, zero file mutation. |
| **C: Shell-rc append** | `freedius init --shell-install` appends marker-delimited block to `~/.zshrc`/`~/.bashrc`/fish | Persistent across shell restarts; reversible in one delete | Conflicts with existing `ANTHROPIC_API_KEY`; per-shell syntax | ~120 | **Opt-in flag, with explicit warning.** The existing-key conflict is the killer. |
| **D: systemd/launchd unit** | `freedius init` writes `~/.config/systemd/user/freedius.service` | Clean separation; service starts on login | **Doesn't help `claude`** — unit env doesn't propagate to interactive shells | ~150 | **Reject.** Category error. |
| **E: Write `~/.claude/settings.json`** | `freedius init` writes the `env` block (B.3) | Officially supported; survives shell restarts; shell-env wins (existing-key safe); reversible; cross-IDE | Mutates a user-owned file; `CLAUDE_CODE_PROVIDER_MANAGED_BY_HOST` disables it | ~80 | **v1 primary mechanism.** Opt-in via `freedius init`. |

**Recommended combination for v1**:

- **Always-on**: B (print eval-snippet on every freedius startup, ~30 LoC, `--no-export-hint` to silence)
- **Opt-in via `freedius init`**: E (write `~/.claude/settings.json`, ~80 LoC)
- **Opt-in via `freedius init --shell-install`**: C (shell-rc append, ~120 LoC, requires explicit warning about existing-key conflict)

Total: ~230 LoC. Well within slice budget.

### C. `freedius init` subcommand

#### C.1 Current `main.go` architecture

`main.go` is 190 lines, one `run()` function, zero subcommands ([main.go:36-38](main.go) and [main.go:40-129](main.go)).

**Today's behavior for subcommand-like invocations** (verified against the built binary):

| `os.Args` | Behavior today | Source |
|---|---|---|
| `--help` | stdlib `flag` prints `Usage of ./freedius:` with the 3 flags, exits 2 | [main.go:44](main.go) (default `flag.CommandLine.Usage`) |
| `--version` | `flag.Parse` returns `flag.ErrHelp`, prints usage + "flag provided but not defined: -version" | [main.go:44](main.go) |
| `version` (positional) | `flag.Parse` silently drops unknown positional args; `run()` tries config load and fails with "config: config file not found" | stdlib `flag` semantics |
| `init` (positional) | identical to `version`: positional dropped, then config-not-found | same path |
| `--config=init` | `flagConfig = "init"`; `resolveConfigPath("init")` returns `"init"` as explicit; `os.ReadFile("init")` fails | [main.go:41,66,171-173](main.go) |

There is no `version` variable anywhere in the binary. `os.Args[0]` is never referenced. Wrapper scripts and `argv[0]` rename are not currently supported.

**Refactor cut-point**: between `flag.Parse()` ([main.go:44](main.go)) and resolve port/host ([main.go:49-60](main.go)). Rename `run()` → `runServe()`, add `runInit()` and `runVersion()`, change `main()` to peek `os.Args[1]` and dispatch.

#### C.2 Subcommand parser options

| Option | Mechanism | Pros | Cons | LoC | Verdict |
|---|---|---|---|---|---|
| **A: stdlib `flag` + dispatcher** | Peek `os.Args[1]`, build per-subcommand `flag.NewFlagSet`, set `fs.Usage` per subcommand | Zero deps; idiomatic; aligns with AGENTS.md "no external HTTP router" / "no config library"; per-subcommand `--help` "just works" | No auto top-level help; dispatcher is hand-written | ~40 | ✅ **Use this.** |
| **B: cobra / urfave/cli** | `cobra.Command`, etc. | Auto-help, banners, nested subcommands | Adds deps (cobra pulls `pflag` + transitive); violates "single static binary, zero external runtime deps" | 5+ boilerplate | ❌ Reject. |
| **C: Manual argv parsing** | `if os.Args[1] == "init"` | 5 LoC | No auto help; breaks on `freedius --config=foo init`; can't grow into flags cleanly | 5-10 | ❌ Reject. |
| **D: Separate binaries via `//go:build ignore`** | `cmd/freedius-init/main.go` | Cleanest mental model | Forces `internal/` move; Go tooling doesn't ship multi-binary repos well; one binary name already in user scripts | 100+ restructure | ❌ Reject. |

#### C.3 Template shape

**Source-of-truth**: `config.example.yaml` (10 lines, in repo root) — already demonstrates the S-02 family-mapping conventions with `nim`/`zen`/`go` providers and `auto`/`default` fallthrough.

**Recommended template content**: 5 mappings mirroring `config.example.yaml:1-10` + 1 explicit `models:` example showing exact-match. The four most-useful providers to demo:

1. `nim` — NIM with built-in defaults (no `base_url`/`api_key_env` needed) — `config/defaults.go:17`
2. `go` — Opencode Go OpenAI-format (DeepSeek/GLM/Kimi)
3. `go` — Opencode Go Anthropic-format (MiniMax/Qwen)
4. `zen` — Opencode Zen Anthropic-format
5. `custom` — passthrough with explicit `base_url` + `api_key_env` (per FR-009, [prd.md:83](context/foundation/prd.md)); `custom` → `anthropic` rewrite ([config/defaults.go:46-48](config/defaults.go))

**Comments to include**: top-of-file purpose; per-mapping provider explanation (matching `config.example.yaml:2,4,6,8` style); note that `api_key_env` is the env-var name not the value (NFR-Privacy); note that `mappings` uses family names (`opus`/`sonnet`/`haiku`/`auto`/`default` per `proxy/families.go:10-16`) while `models` is exact-match.

**Out-of-box validity**: the template will parse YAML, but `config.Load` will fail at server start until the user exports `NIM_API_KEY` / `OPENCODE_API_KEY` (which the loader auto-injects from `knownProviderDefaults` at [config/defaults.go:53-58](config/defaults.go)). This is **correct** behavior — the template should mention "set the env vars listed in `api_key_env` before running `freedius`".

**Docs URL**: `README.md:1` is one word; no docs URL is established. Defer the URL mention in the template — recommend a comment pointing at `config.example.yaml` as the canonical example instead.

#### C.4 Template storage options

| Option | Mechanism | Pros | Cons | LoC | Verdict |
|---|---|---|---|---|---|
| **A: Hardcoded Go const** | `const initTemplate = "# freedius..."` in `init.go` | No embed; zero path logic | YAML in Go string is ugly; escape rules for `{}` and `:`; updates require recompile | ~80-100 | ❌ Reject. |
| **B: `embed.FS`** | `//go:embed templates/starter.yaml` in `init.go`; `embed.FS.ReadFile(...)` | Normal YAML file (editor highlighting, lint, test); no runtime path resolution; works for `go install` / `go run` / static binary | Updates need rebuild (acceptable) | ~5 | ✅ **Use this.** |
| **C: External file next to binary** | `os.ReadFile(filepath.Join(filepath.Dir(os.Args[0]), "templates/starter.yaml"))` | User can edit without rebuild | Breaks for `go run`, `go install`, Homebrew, `go test`; `os.Args[0]` is unreliable (can be relative) | ~10 + fallback | ❌ Reject. |
| **D: Download from GitHub** | `http.Get("https://raw.githubusercontent.com/.../starter.yaml")` | Template updates ship without binary release | Network dep at init time; HTTPS + certs + retries; NFR-Privacy concerns; version skew | ~15 + error handling | ❌ Reject. |

#### C.5 Idempotency + edge cases

| Scenario | Recommended behavior |
|---|---|
| Target file exists | **Refuse to overwrite** by default; print "use `--force`"; exit 1 |
| `--force` | Back up to `<path>.bak` (if absent); overwrite |
| Target dir absent | `os.MkdirAll(filepath.Dir(path), 0o755)` |
| `--output <path>` | Override default (`./freedius.yaml`) |
| Idempotent rerun | Refuse because file exists; user opts into re-run with `--force` |
| `--dry-run` | Print template to stdout; do not write |
| Parent of target is a file | `MkdirAll` fails; surface error verbatim |
| Target is symlink | Follow; treat as exists if points to file |
| User lacks write permission | `WriteFile` returns EACCES; surface error |

No tests today exist for `init`. The planner should add a `TestInit_*` family in `main_test.go` (existing 99-line structure).

#### C.6 Integration with env-injection

Two design choices:

- **Single command vs. flag**: **single command, two flags**. `freedius init` does both by default; `--no-env` skips the `settings.json` write.
- **Shared code**: env-injection's `WriteSettingsJSON` should live in a new `internal/envinject/` package so `init` can import it. The only cross-cutting concern; no other module touches `~/.claude/`.
- **Order**: write config first, then `settings.json`. If `settings.json` write fails, leave config in place and exit non-zero (so user can re-run with `--no-env` to recover the env step).
- **Idempotency for `settings.json`**: `--merge` (default — read existing JSON, add `env` block, write back) and `--overwrite` (destructive). Merge is naturally idempotent for a fixed key set.

### D. Cross-cutting patterns

1. **Adapter error messages leak inner name.** A user with `provider: nim` and no `NIM_API_KEY` set gets `"openai adapter: env var NIM_API_KEY is not set"`. Affects all 4 production adapters via delegation. **Fix**: preserve original provider name on `Model` (`Model.OriginalProvider` field); pass through inner adapter constructors; use in error templates.

2. **Pre-WriteHeader errors are swallowed by the dispatcher.** [proxy/proxy.go:107](proxy/proxy.go) writes only `"upstream error"` regardless of the original `err`. Direct violation of NFR-Error-handling. **Fix**: forward the adapter's error to the client body for pre-WriteHeader failures; keep nil-return for post-WriteHeader (per `lessons.md`).

3. **Inconsistent error JSON shapes.** `{"status":"no_match"}` (404) vs `{"error":"upstream error"}` (502) vs `{"error":"upstream_unreachable","detail":"..."}` (transport 502). Should unify to `{"error":"<code>","message":"<human>","detail":"<optional>"}`.

4. **No request-recovery middleware.** A panic in `translate/anthropic_openai.go` or any adapter crashes the connection without producing a 500. Wrap mux with `recoverMiddleware`.

5. **No request ID.** Multiple concurrent Claude Code sessions log into the same stream with no correlation. Add a `requestID` middleware that generates a UUID, sets `X-Freedius-Request-ID` response header, attaches to `r.Context()`, threads into logger via `logger.With("request_id", id)`.

6. **`http.Client{}` with no `Timeout`** in [proxy/openai_compat.go:21](proxy/openai_compat.go). The Anthropic compat adapter is bounded by `r.Context()`; the OpenAI one is not. Add a generous timeout (e.g., 5 minutes) or per-request deadline via `context.WithTimeout(r.Context(), ...)`.

7. **`applyDefaults` rewrite affects error messages and test expectations.** Per `lessons.md:15-19`. The user sees `provider=mix` in config errors but `provider=zen` in env errors (the second is dead code — see A.2). Fix: `OriginalProvider` field.

8. **No HTTP access log.** No way to debug "what requests did freedius see today." Add a minimal access log middleware.

9. **Privacy-by-omission.** NFR-Privacy is satisfied because no one added body logging. Should be a documented policy comment at the top of `proxy/proxy.go` and `proxy/translate/anthropic_openai.go`.

10. **Inconsistent startup error sinks.** `slog.Error` for path resolution, raw `fmt.Fprintf` for everything else (via `failf`). Unify behind slog.

11. **Text format, not JSON for logs.** `slog.NewTextHandler` is human-readable; `FREEDIUS_LOG=json` would be a natural opt-in.

12. **`MixAdapter` URL routing is fragile.** A typo in the path suffix silently routes to the wrong adapter. Log the routing decision at Debug, reject unknown suffixes explicitly.

## Code References

### Error-handling surface (Phase 1 candidates)

- `config/config.go:32-57` — `Load` function; file-not-found + empty + YAML parse + schema errors
- `config/config.go:87-115` — `validateModel`; per-entry error messages
- `config/defaults.go:65-74` — `readConfigFile`; `os.ErrNotExist` wrapping
- `config/defaults.go:45-63` — `applyEntryDefaults`; `custom`→`anthropic` and `zen`/`go`→`mix` rewrites
- `main.go:40-129` — `run()`; full startup sequence
- `main.go:131-134` — `failf`; stderr writer
- `main.go:136-157` — `checkRequiredEnvVars`; env var presence check (note: dead code for `zen`/`go` branches)
- `main.go:159-189` — `resolveInt` + `resolveConfigPath`
- `main.go:97-103` — `http.Server` construction; **no recovery middleware**
- `proxy/proxy.go:36-109` — `ServeHTTP`; dispatch + 8 failure modes + 3 inconsistent body shapes
- `proxy/proxy.go:111-125` — `writeJSON` + `writeError`; body writers
- `proxy/anthropic_compat.go:25-35` — pre-WriteHeader error returns
- `proxy/openai_compat.go:21` — `http.Client{}` with no `Timeout`
- `proxy/openai_compat.go:27-49` — pre-WriteHeader error returns
- `proxy/openai_compat.go:50-66` — post-WriteHeader behavior (returns nil per `lessons.md`)
- `proxy/mix.go:25-33` — URL-suffix routing; fragile to typos
- `proxy/nim.go:18-19`, `proxy/custom.go:18-19` — thin wrappers
- `proxy/errors.go:23-37` — `freediusErrorHandler`; ReverseProxy transport errors
- `proxy/translate/anthropic_openai.go:272-296` — `TranslateStream`; SSE error handling
- `proxy/translate/anthropic_openai.go:355-418` — `emitter.consume`; chunk parse errors

### Env-injection surface (Phase 2 candidates)

- `main.go:42-49` — `FREEDIUS_PORT` env var resolution (the only env var freedius reads today, besides provider keys)
- `main.go:185-189` — `os.UserConfigDir` for config file resolution
- `config/defaults.go:15-26` — `knownProviderDefaults`; env var per provider
- `proxy/anthropic_compat.go:28`, `proxy/openai_compat.go:30` — per-request `os.Getenv(m.APIKeyEnv)`

### Init-subcommand surface (Phase 3 candidates)

- `main.go:36-38` — `main()`; current exit-code passthrough
- `main.go:41-44` — flag definitions + `flag.Parse()`; the cut-point for subcommand dispatch
- `main.go:171-189` — `resolveConfigPath`; default `./freedius.yaml` for `init`'s `--output`
- `config.example.yaml:1-10` — existing template, source-of-truth for the starter
- `proxy/families.go:10-16` — family regex list (`opus`/`sonnet`/`haiku`/`auto`/`default`) referenced in template comments

### Tests to seed for S-04

- `config/config_test.go:11-427` (`TestLoad`) — extend with per-model `api_key_env` validation, base_url scheme check, mapping-vs-model precedence edge cases
- `proxy/proxy_test.go:34-145` (`TestServeHTTP` + family tests) — extend with panic recovery (inject panicking stub, assert 500), pre-WriteHeader error forward (assert `detail` reaches client), request ID propagation
- `proxy/mix_test.go:21-159` — extend with bad-JSON streaming, client-disconnect mid-stream, non-SSE `Content-Type` on 200, routing-suffix typo behavior
- `main_test.go:10-99` (`TestCheckRequiredEnvVars_*`) — extend with original-provider-name assertions, exit-code (1) assertion
- `proxy/errors_test.go:44-79` (`TestFreediusErrorHandler_*`) — extend with panic-in-handler test, partial-write-after-headers test
- New `main_test.go:TestInit_*` — covers default write, refuse-on-exists, `--force`, `--output`, `--dry-run`, `MkdirAll` parent creation

## Architecture Insights

1. **The codebase prefers stdlib + thin internal layering** (`main.go:7-19` imports only stdlib + `config` + `proxy`; `goccy/go-yaml` is the only non-stdlib dep per `go.mod:5`). S-04 should follow this — no cobra, no urfave/cli, no Logrus, no validator libraries.

2. **The "Adapter Return Contract" (`lessons.md:33-43`) is the cornerstone of all adapter error handling.** Any S-04 error-handling change must respect: pre-WriteHeader errors returned to dispatcher; post-WriteHeader errors logged and returned nil. The S-04 dispatcher fix (D.2 above) lives on the dispatcher side and never breaks this contract.

3. **The `applyDefaults` rewrite (per `lessons.md:15-19`) is a load-bearing pattern.** All error messages and validation rules are downstream of this. The `OriginalProvider` field addition is the right way to make the user-facing provider name accessible to error templates without breaking the rewrite invariant.

4. **Privacy is satisfied by omission, not by policy.** The team has not added body logging, but there's no explicit "do not log bodies" comment to guide future contributors. S-04 is a good moment to add the policy comment as part of the privacy hardening.

5. **`embed.FS` is the Go-idiomatic way to ship small text assets in a static binary.** S-04 should use it for `templates/starter.yaml` — no path resolution, no network, no install-path fragility.

6. **`httputil.ReverseProxy.ErrorHandler` is the abstraction layer for transport errors.** `freediusErrorHandler` ([proxy/errors.go:23-37](proxy/errors.go)) is the right hook for unifying transport-error body shape with dispatch errors.

7. **Multi-agent concurrency is an explicit NFR ([prd.md:89](context/foundation/prd.md))**. The current logger has no per-request scoping — `requestID` middleware is the right primitive to enable log correlation, panic attribution, and (later) request-scoped metrics.

8. **The PRD/roadmap reference a placeholder env-var name (`CLAUDE_CODE_API_BASE`) that doesn't exist in Claude Code.** This is an upstream-doc drift, not a research error. The implementation should use `ANTHROPIC_BASE_URL` and surface this in the implementation notes (and as a small PRD amendment change, separately).

## Historical Context (from prior changes)

- **`context/foundation/lessons.md:3-7` (SSE encoding)** — `json.Marshal` over `json.NewEncoder` to avoid trailing-newline corruption. Relevant when shaping new SSE-emitting paths in error recovery.
- **`context/foundation/lessons.md:9-13` (SSE reader)** — `bufio.Reader.ReadBytes('\n')` over `bufio.Scanner` to avoid 64 KB token truncation. Apply to any future per-request stream readers in `init` if it spawns subprocesses.
- **`context/foundation/lessons.md:15-19` (`custom`→`anthropic` rewrite)** — tests must use post-rewrite name in expected error substrings. Directly applicable to S-04 tests for `mix` (post-rewrite of `zen`/`go`).
- **`context/foundation/lessons.md:21-29` (Embrace extra tests)** — when S-04 implementation naturally produces extra tests beyond the planned list (e.g., `panic_recovery_test.go` or `request_id_test.go`), keep them.
- **`context/foundation/lessons.md:33-43` (Adapter Return Contract)** — pre-WriteHeader errors returned; post-WriteHeader errors logged and returned nil. **Cornerstone of all adapter error handling in S-04.**
- **`context/archive/provider-and-mapping/`** — S-02 archive (the just-archived slice that produced `KnownProviders`, `applyDefaults`, and the family-mapping conventions). S-04 builds directly on top of S-02's schema. Review `archive/provider-and-mapping/research.md` if any schema-design questions arise.
- **`context/changes/zen-go-adapters/plan.md`** — S-03 plan (the just-completed slice). Notable decisions: zen/go/mix multi-format routing via `MixAdapter`; per-entry validation extended to require `base_url` for `mix`; `config.example.yaml` demonstrates 4 family mappings. S-04 should reuse `config.example.yaml` as the source for the starter template.
- **`context/changes/first-call-routed/` (S-01, archived)** — the north-star slice. Established the NIM/custom passthrough adapters and the SSE translation pipeline. The dispatcher in `proxy/proxy.go` and the `Provider` interface in `proxy/provider.go` are S-01 artifacts.

## Related Research

- `context/changes/error-hardening/change.md` — the stub change folder created during this research
- `context/archive/provider-and-mapping/` — S-02 research; the schema + dispatch + config-load architecture S-04 inherits
- `context/changes/zen-go-adapters/plan.md` — S-03 plan; the just-completed slice S-04 must integrate with (via `OriginalProvider` field, via `~/.claude/settings.json` writing in `init`)
- `context/foundation/lessons.md` — team's accepted recurring rules; S-04 must shape every implementation choice around them

## Open Questions

1. **Where should the pre-WriteHeader adapter error be forwarded?** Three options:
   - In response body (privacy risk if it contains upstream URLs; risk of leaking internal structure to Claude Code UI)
   - In response body minus the URL (lose info)
   - In a custom response header (machine-readable but invisible to Claude Code UI)
   Recommend: **body with optional `detail` field**, gated by a `--verbose-errors` flag. The default is the structured `{"error":"<code>","message":"<human>"}`; with the flag, the upstream error text is included in `detail`. The dispatcher has the original `err` already; this is a 5-line change.

2. **Should `Model.OriginalProvider` be added now or in S-05?** It's the cleanest fix for the inner-adapter-name leakage (A.4 cross-cutting), but it touches the `Model` struct used by every adapter, every test, and the schema. S-04 is the natural home; defer would mean living with misleading error messages for another slice. **Recommend: add in S-04.** Cost: 1 new field, 2 rewrite points in `defaults.go`, +1 line per adapter constructor that wants to use it, ~20 test assertions to update.

3. **Should adapter constructors accept a request-scoped logger?** Today they're constructed once at startup. A request-scoped logger (with request ID) would require either a per-request clone of the adapter (expensive) or passing the logger through `Handle(w, r, ...)` (signature change). The current `Handle` signature has no logger parameter. **Recommend: keep constructor-time logger for v1**, add `request_id` to per-request log via `r.Context()` lookup. The constructor-time logger carries the adapter name; per-request logs add `request_id` from context.

4. **What should the panic recovery middleware report?** Three options:
   - `{"error":"internal_error"}` (opaque)
   - `{"error":"internal_error","detail":"<panic message>"}` (informative)
   - stderr only, no body (current behavior)
   Recommend: **opaque body, full stderr log**. NFR-Privacy argues for opaque; user-friendliness argues for informative. The compromise: log the panic with stack trace and request_id at Error level; respond with opaque body. The dev sees it in stderr; Claude Code sees a stable 500.

5. **What's the policy for partial SSE truncation?** If the client disconnects mid-stream, should the server close the upstream connection immediately, or let it finish (and waste tokens)? `httputil.ReverseProxy` handles this for Anthropic compat (context propagation); OpenAI compat does not (separate `http.Client.Do`). **Recommend: propagate `r.Context()` to the OpenAI `http.NewRequestWithContext`** (already done at [proxy/openai_compat.go:38](proxy/openai_compat.go)) **and add `http.Client.Transport.DisableKeepAlives`** so the connection closes on cancel. ~5 LoC.

6. **Should `freedius init` write the `~/.claude/settings.json` env block or just print the eval-snippet?** Per B.5 recommendation: **both, with `--no-env` to opt out of the settings.json write**. The eval-snippet is also always-on on startup (Option B). Three surfaces:
   - Always-on: stderr eval-snippet
   - Opt-in via `init`: write `settings.json`
   - Opt-in via `init --shell-install`: write shell-rc (with explicit warning)

7. **How do we test request-recovery without a real connection?** `httptest.ResponseRecorder` is enough for status code assertion. For "panic in handler" we need a stub adapter that panics and an assertion that the response is well-formed (or the connection closes cleanly).

8. **What's the right `http.Client.Timeout` for OpenAI compat?** Long streams can run for minutes. A `Timeout` of 5 minutes is generous; a per-request `context.WithTimeout(r.Context(), 5*time.Minute)` is more precise. **Recommend: per-request context timeout, 5 min default, configurable via `--stream-timeout` flag.**

9. **What does the S-04 implementation do about `CLAUDE_CODE_API_BASE` (the PRD placeholder)?** Two options:
   - Update PRD + roadmap in a follow-up amendment change (low risk; ~5 LoC of Markdown)
   - Ignore the PRD text and use `ANTHROPIC_BASE_URL` everywhere; let the inconsistency surface in code review
   Recommend: **option 1** — file a small PRD-amendment change alongside the implementation.

10. **Should the `OriginalProvider` field be added at all, or just passed through `Handle(w, r, m, body)`?** Two options:
    - New field on `Model` — touches schema, validation, tests
    - New parameter on `Handle` — touches adapter interface, all 4 adapters, dispatcher
    Recommend: **new field** — `Handle` signature is already 4 params; adding a 5th is uglier than adding a struct field. Plus the field is useful in non-error contexts (logging, metrics).

## Estimated Phase Breakdown

For the S-04 plan that will consume this research:

| Phase | Title | LoC | Critical path |
|---|---|---|---|
| 1 | Error-path hardening — panic recovery, dispatcher error forwarding, request ID, JSON shape unification | ~250 | `recoverMiddleware` + dispatcher body change + `requestID` middleware + adapter error-message fix using `OriginalProvider` |
| 2 | `freedius init` subcommand + starter template | ~150 | subcommand dispatcher + `embed.FS` template + `--output`/`--force`/`--dry-run` flags |
| 3 | Env auto-injection — settings.json + shell hint | ~230 | `internal/envinject` package + `WriteSettingsJSON` (merge, idempotent) + always-on stderr eval-snippet + shell-rc opt-in flag |
| 4 | Quality — privacy policy comments, `--verbose-errors` flag, `FREEDIUS_LOG=json`, `http.Client.Timeout`, access log | ~150 | cross-cutting; can be folded into phases 1-3 |

**Total**: ~780 LoC + ~300 LoC of new tests. Within the 2-week MVP budget.

### Cross-phase risks

- **Adapter `OriginalProvider` field** touches every adapter constructor. Migration: add field as optional (empty string = current behavior), populate in `applyEntryDefaults`, use in error templates one adapter at a time. Could be split into a sub-phase of its own.
- **`requestID` middleware** touches every code path that logs. Migration: add middleware early; update loggers to pull from context; existing logs without request_id are valid (just uncorrelated).
- **`init` subcommand** breaks the `freedius --help` flow if the dispatcher doesn't fall through correctly. Migration: keep `serve` as the default subcommand (no positional = run serve); add explicit `serve` for clarity.

### Prerequisite for S-04 planning

- The planner should resolve Q1-Q3 from Open Questions before writing the plan (they affect phase boundaries).
- The PRD-amendment change for `CLAUDE_CODE_API_BASE` → `ANTHROPIC_BASE_URL` should be filed as a tiny follow-up change (1-2 LoC of Markdown).