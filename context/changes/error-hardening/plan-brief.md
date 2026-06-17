# Error hardening + env auto-injection + config template — Plan Brief

> Full plan: `context/changes/error-hardening/plan.md`
> Research: `context/changes/error-hardening/research.md`

## What & Why

S-04 is the hardening and developer-experience slice. It fixes every silent failure in the proxy pipeline, unifies three inconsistent error JSON shapes into one, adds panic recovery and request-ID correlation, lands the `OriginalProvider` fix so error messages name the user's configured provider, and ships `freedius init` — a subcommand that writes a starter config template and can auto-inject Claude Code environment variables via `~/.claude/settings.json` or shell rc.

## Starting Point

The proxy has 70% good error handling — config loading and per-entry validation produce descriptive messages. The other 30% is broken: pre-WriteHeader adapter errors are swallowed into a generic `"upstream error"` (`proxy/proxy.go:107`), there's no panic recovery (`main.go:97-103` gives bare connection resets), adapter error messages leak inner delegation names (`provider: nim` users see `"openai adapter: …"`), three different error JSON shapes coexist, `http.Client{}` in OpenAI compat has no `Timeout`, and `freedius init` doesn't exist — `main.go` is a single monolithic `run()` function.

## Desired End State

- Every error response is `{"error":"<code>","message":"…","detail":"…","request_id":"32-hex"}` — with `detail` gated by `--verbose-errors`.
- A panic anywhere produces a logged stack trace (with `request_id`) and a structured 500 — never a connection reset.
- Adapter errors name the user's provider: `"nim adapter (openai-compat): env var NVIDIA_NIM_API_KEY is not set"` instead of `"openai adapter: …"`.
- `freedius init` writes a valid config; `--force` backs up then overwrites; `--dry-run` previews.
- On startup, a copy-paste `export` block prints to stderr. `freedius init` optionally writes `~/.claude/settings.json` and/or shell rc.
- `--stream-timeout=5m` bounds upstream calls; `FREEDIUS_LOG=json` opts into structured logging.

## Key Decisions Made

| Decision | Choice | Why (1 sentence) | Source |
| -------- | ------ | ---------------- | ------ |
| Slice scope | Single plan, 4 sequential phases | Roadmap bundles all three concerns; splitting would need more change folders and review cycles without benefit. | Plan |
| `Model.OriginalProvider` | Add field now, set before rewrites | Fixes adapter error-message leakage in one structural change; deferring would leave misleading messages for another slice. | Research / Plan |
| Pre-WriteHeader error detail | Opt-in via `--verbose-errors` flag (default off) | Default protects against leaking internal URLs/structure; dev can opt in for debugging. | Plan |
| Env-injection mechanisms | stderr eval-snippet (always-on) + settings.json (`init` opt-in) + shell-rc (`init --shell-install` opt-in) | Three surfaces cover all dev personas: shell-rc users get the hint, settings.json users get auto-config, shell-rc users get persistent install. | Plan |
| `freedius init` flags | `--output` + `--force` (with `.bak`) + `--dry-run` + `--no-env` + `--shell-install`; refuse-on-exists by default | Refuse-by-default protects from clobbering; backup-on-force is reversible; dry-run lets users preview. | Plan |
| Panic recovery body | Opaque 500 `{"error":"internal_error","request_id":"…"}` with full stderr log + stack trace | Privacy-safe (no internal state leaks to Claude Code); `request_id` lets dev cross-reference log for the full trace. | Plan |
| PRD/roadmap doc drift (`CLAUDE_CODE_API_BASE`) | Separate follow-up amendment change | Keeps S-04 implementation-focused; PRD/roadmap edits go through their own change lifecycle. | Plan |

## Scope

**In scope:**
- Unified error JSON contract with machine-readable codes across all 8 dispatch failure modes + transport errors
- Request-ID middleware with context threading and log correlation
- Panic recovery middleware producing structured 500
- `Model.OriginalProvider` field and adapter error-template rewrites
- Pre-WriteHeader error forwarding (gated by `--verbose-errors`)
- Per-request stream timeout for OpenAI-compat adapter (default 5 min)
- Access-log middleware (method, path, status, duration, request_id)
- Startup banner; unified error sinks (slog everywhere)
- `FREEDIUS_LOG=json` / `--log-format=json`
- Privacy-policy comments at body-touching code surfaces
- Subcommand dispatch: `freedius serve` (default), `init`, `version`, `help`
- `embed.FS`-backed `templates/starter.yaml` with 5 mappings + 1 model example
- `freedius init` with `--output`, `--force` (backup), `--dry-run`
- `internal/envinject/` package: `EvalSnippet`, `WriteSettingsJSON`, `WriteShellRC`
- `freedius init` wires settings.json merge; `--no-env` skips it
- `freedius init --shell-install` for zsh/bash/fish rc files
- Startup eval-snippet (silenceable with `--no-export-hint`)

**Out of scope:**
- PRD/roadmap amendment for `CLAUDE_CODE_API_BASE` → `ANTHROPIC_BASE_URL` (separate change)
- Wrapper script (`freedius-claude`) — deferred to v2
- Claude Desktop / IDE flows
- Metrics, Prometheus, dashboards
- Body logging (ever)
- Per-request logger injection into adapters

## Architecture / Approach

The plan layers middleware and new subsystems onto the existing `main → proxy/dispatcher → adapter → upstream` pipeline without restructuring it.

```
HTTP request
  → RecoverMiddleware (panics → 500)
    → AccessLogMiddleware (log method, status, duration)
      → RequestIDMiddleware (32-hex header + context)
        → Dispatcher.ServeHTTP (validation → model lookup → adapter dispatch)
          → Adapter.Handle (anthropic/compat, openai/compat, mix, nim, custom)
```

`main.go` gains a `dispatch(os.Args)` subcommand router that hands off to `runServe`, `runInit`, or `runVersion`. `runInit` writes from an `embed.FS` template and optionally calls `internal/envinject/` to write settings.json or shell rc.

**Key integration points:**
- `OriginalProvider` touches `config.Model`, `config/defaults.go` (set before rewrite), and every adapter's error template.
- `--verbose-errors` touches the Dispatcher constructor and the `freediusErrorHandler` closure.
- `--stream-timeout` touches `OpenAICompatibleAdapter` + its wrapper constructors (`NIMAdapter`, `MixAdapter`).
- Request-ID middleware sets context value read by `writeErrorJSON` and `freediusErrorHandler`.

## Phases at a Glance

| Phase | What it delivers | Key risk |
| ----- | ---------------- | -------- |
| 1. Foundation | Middleware stack + unified error contract + `OriginalProvider` + `--verbose-errors` + privacy comments | Middleware ordering wrong (recovery must be outermost); `OriginalProvider` ordering in `applyDefaults` must be before rewrites. |
| 2. Adapter hardening | Pre-WriteHeader error forwarding + stream timeout + error-template rewrites + mixer routing log | Adapter Return Contract violation if dispatcher writes after `WriteHeader`; stream timeout interaction with HTTP/1.1 keep-alive. |
| 3. `init` subcommand | Subcommand dispatch + `embed.FS` template + `--output`/`--force`/`--dry-run` | Subcommand dispatch breaks `freedius --help` flow if fallthrough isn't right; `embed` path must match the repo layout. |
| 4. Env auto-injection | `internal/envinject/` + settings.json merge + shell-rc install + eval-snippet | Settings.json mutation is destructive-by-merge; shell-rc detection must refuse unknown shells with a clear message; `init` host/port defaults must match `serve`. |

**Prerequisites:** S-01 (first-call-routed) — the dispatcher, Provider interface, and all adapter implementations this plan modifies.
**Estimated effort:** ~780 LoC implementation + ~300 LoC tests, across ~4 sessions (one per phase).

## Open Risks & Assumptions

- **Assumption: `ANTHROPIC_BASE_URL` remains the canonical Claude Code env var.** Mitigation: a separate follow-up change amends the PRD/roadmap; the implementation uses `ANTHROPIC_BASE_URL` regardless.
- **Risk: Settings.json mutation may surprise users who have hand-edited their `~/.claude/settings.json`.** Mitigation: merge preserves unknown keys; `--no-env` skips the write entirely; `--dry-run` previews the result.
- **Risk: Shell-rc install may conflict with existing `ANTHROPIC_API_KEY` exports in the user's rc file.** Mitigation: the marker block is clearly delimited; shell env wins over settings.json (safe); `--force` replaces rather than appends. A warning is printed about the precedence.
- **Assumption: The OpenAI-compat upstream honors stream cancellation via context deadline.** Mitigation: Go's `http.NewRequestWithContext` handles `context.DeadlineExceeded` correctly on the client side; the upstream may continue processing but freedius closes the connection.
- **Risk: `OriginalProvider` field rollout touches every adapter and ~20 test assertions.** Mitigation: fallback to `Provider` when `OriginalProvider` is empty (backwards-compatible for existing test structs); update tests incrementally, adapter by adapter.

## Success Criteria (Summary)

- Every proxy failure produces a well-structured, JSON-encoded error with a stable machine-readable code — not `"upstream error"`, not a connection reset, not three different shapes.
- Claude Code users see adapter errors in their own provider's name, not the inner delegation target's name.
- `freedius init` works end-to-end: writes config, optionally writes env, refuses on overwrite unless `--force`.
- A developer can run `freedius init --shell-install` in their zsh/bash/fish setup, source their rc, and immediately use `claude` through freedius without manually setting any env vars.
