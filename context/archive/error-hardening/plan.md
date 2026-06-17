# Error hardening + env auto-injection + config template — Implementation Plan

## Overview

S-04 is the hardening and developer-experience slice. It fixes every silent failure in the proxy pipeline, unifies error-response shapes, adds panic recovery and request-ID correlation, lands the `OriginalProvider` fix so error messages name the user's configured provider (not the inner adapter), and ships the `freedius init` subcommand with a starter config template and optional Claude Code environment auto-injection — including opt-in `~/.claude/settings.json` writing and opt-in shell-rc appending. One plan, four phases, with a `--verbose-errors` flag gating leakier error detail.

## Current State Analysis

The proxy has 70% good error handling (config loading + per-entry validation are descriptive) and 30% that violates NFR-Error-handling:

- **Pre-WriteHeader adapter errors are swallowed** (`proxy/proxy.go:107` writes generic `"upstream error"` regardless of cause).
- **No panic recovery middleware** (`main.go:97-103` — `http.Server.Handler` is a bare `mux`). A panic anywhere produces a connection reset, not a 500.
- **Three different error JSON shapes** coexist for morally-the-same condition: `{"status":"no_match"}` vs `{"error":"upstream error"}` vs `{"error":"upstream_unreachable","detail":"..."}`.
- **Adapter error messages leak the inner adapter name** (`nim` → `"openai adapter: …"`; `custom` → `"anthropic adapter: …"`) because `applyEntryDefaults` rewrites `custom` → `anthropic` and `zen`/`go` → `mix` before error templates evaluate `m.Provider`.
- **No request-ID or correlation ID** is generated — multiple concurrent Claude Code sessions produce interleaved, unsortable log lines.
- **`http.Client{}` with no `Timeout`** in `OpenAICompatibleAdapter` (`proxy/openai_compat.go:21`) — a hanging upstream holds the goroutine forever.
- **`freedius init` does not exist** — `main.go` is a single `run()` function with zero subcommand dispatch; unknown positional args are silently dropped by stdlib `flag` and fall through to a config-load failure.
- **No env auto-injection** — the dev must manually discover and set `ANTHROPIC_BASE_URL`, `ANTHROPIC_API_KEY`, `ENABLE_TOOL_SEARCH` before `claude` can use freedius.
- **Privacy is satisfied by omission, not by policy** — no body logging exists, but no comment documents the rule for future contributors.

### Key Discoveries:

- **`lessons.md:33-43`** — Adapter Return Contract: pre-WriteHeader errors are returned; post-WriteHeader errors are logged and returned nil. All hardening changes must honor this.
- **`lessons.md:15-19`** — `custom`→`anthropic` rewrite affects error messages; tests must use post-rewrite names. The `OriginalProvider` field preserves the user-facing name through the rewrite.
- **`ANTHROPIC_BASE_URL`** is the canonical Claude Code variable, not `CLAUDE_CODE_API_BASE` (PRD placeholder that doesn't exist in Claude Code). A separate follow-up change will amend the PRD/roadmap.
- **`~/.claude/settings.json` `env` block** is the official Claude Code mechanism for auto-injection; shell env takes precedence (safe for users with existing `ANTHROPIC_API_KEY`).
- **`embed.FS`** is the Go-idiomatic way to ship the starter template in a static binary — no path resolution, no network fragility.
- **`checkRequiredEnvVars`** (`main.go:136-157`) has unreachable `zen`/`go` branches because `applyDefaults` rewrites them to `mix` first. The `OriginalProvider` field lets us fix this.

## Desired End State

- Every HTTP error path returns a consistent JSON body: `{"error":"<code>","message":"<human text>","detail":"<optional upstream error>","request_id":"<32-hex>"}`. The `detail` field requires `--verbose-errors` (or `FREEDIUS_VERBOSE_ERRORS=1`).
- A panic anywhere in the handler/adapter/translator stack produces a logged stack trace (with `request_id` and path) and a structured 500 response — not a connection reset.
- Every response carries an `X-Freedius-Request-ID` header, and every log line for that request includes the same `request_id`.
- Adapter error messages name the user's configured provider (e.g., `"nim adapter (openai-compat): env var NVIDIA_NIM_API_KEY is not set"`), not the inner delegation target.
- `freedius init` writes a valid `freedius.yaml` template; `freedius init --force` backs up then overwrites; `--dry-run` previews; `--output <path>` targets a custom path. Refuse-on-exists by default.
- On startup, freedius prints a copy-paste-ready `export` block to stderr (silenceable with `--no-export-hint`).
- `freedius init` optionally writes a merged `env` block into `~/.claude/settings.json` (skippable via `--no-env`).
- `freedius init --shell-install` appends a marker-delimited env block to the user's shell rc (zsh/bash/fish); idempotent re-runs are no-ops; `--force` replaces the existing block.
- `freedius --stream-timeout=5m` (or `FREEDIUS_STREAM_TIMEOUT=5m`) sets a per-request deadline on upstream calls; default 5 minutes for the OpenAI compat adapter. Anthropic compat is already bounded by `httputil.ReverseProxy` honoring `r.Context()`.
- `FREEDIUS_LOG=json` (or `--log-format=json`) opts into JSON log output.
- `proxy/proxy.go` and `proxy/translate/anthropic_openai.go` carry a privacy-policy comment at the top.

## What We're NOT Doing

- **PRD/roadmap amendment for `CLAUDE_CODE_API_BASE` → `ANTHROPIC_BASE_URL`** — filed as a separate follow-up change (one-line fix in `prd.md:36` and `roadmap.md:75`).
- **Wrapper script (`freedius-claude`)** — deferred to v2; research scored it most invasive and least automatic.
- **Claude Desktop / IDE `PROVIDER_MANAGED` flows** — out of scope for the terminal-native persona.
- **Metrics, Prometheus, or structured dashboards** — request-ID is the correlation primitive; nothing beyond it.
- **Body logging, ever** — the privacy comment documents the prohibition; no tiered-log mode is introduced.
- **Per-request logger injection into adapters** — adapters keep their constructor-time logger; per-request metadata (request_id) is pulled from context at log time.

## Implementation Approach

Four sequential phases. Each lands a green CI (lint + vet + test + build) and includes manual verification before proceeding.

| Phase | Component       | Core deliverables                                                                                   |
| ----- | --------------- | --------------------------------------------------------------------------------------------------- |
| 1     | Foundation      | Request-ID + panic recovery + error JSON contract + `--verbose-errors` + `OriginalProvider` + `FREEDIUS_LOG=json` + privacy comments |
| 2     | Adapter hardening | Pre-WriteHeader forwarding + stream timeout + Mix log + `freediusErrorHandler` unification + error-template rewrites |
| 3     | `init` subcommand | Subcommand dispatch (`serve`/`init`/`version`) + `embed.FS` template + `--output`/`--force`/`--dry-run` flags |
| 4     | Env auto-injection | `internal/envinject/` package + `settings.json` merge + shell-rc install + always-on eval-snippet |

## Critical Implementation Details

**Adapter Return Contract (lessons.md:33-43)**: The panic recovery middleware must check whether the response has already been written before emitting the structured 500. If `WriteHeader` has been called (post-WriteHeader panic), log the panic and close the connection — do NOT attempt a body write, which would panic again with "superfluous response.WriteHeader". The dispatcher's pre-WriteHeader error forwarding has the same boundary: never write after `WriteHeader`.

**`applyDefaults` ordering**: Set `m.OriginalProvider = m.Provider` *before* the `custom`→`anthropic` and `zen`/`go`→`mix` rewrites in `config/defaults.go:36-43`. If `OriginalProvider` is empty (backwards compat for directly-constructed `Model` structs), fall back to the existing `Model.Provider` behavior in error templates.

**Per-request stream timeout**: Use `context.WithTimeout(r.Context(), streamTimeout)` wrapping the upstream request context — not `http.Client.Timeout`. A client disconnecting should propagate via `r.Context()` cancellation; wrapping preserves this while also bounding the deadline. Add a `Timeout: 0, Transport: &http.Transport{DisableKeepAlives: false}` to the OpenAI client struct so idle connections are reused but cancellation is clean.

**Idempotent settings.json merge**: `WriteSettingsJSON` reads the existing JSON (if any), unmarshals into `map[string]any`, sets/replaces the `env` key, and writes back. Unknown top-level keys are preserved. File is written atomically (write to `<path>.tmp` + `os.Rename`).

**Shell-rc detection**: Inspect `os.Getenv("SHELL")` only — do not guess based on file existence. Map: `zsh` → `~/.zshrc`, `bash` → `~/.bashrc`, `fish` → `~/.config/fish/config.fish`, `sh` → refuse with clear message. Use `os.UserHomeDir()` for `~` expansion.

**`--verbose-errors` propagation**: The flag is read in `main.go` and threaded through the `Dispatcher` constructor. The dispatcher passes it to `writeErrorJSON` and to the adapter via `Handle(w, r, m, body)` — wait, no, adapters don't need it. The dispatcher captures the adapter's error and decides whether to include `detail` based on the verbose flag. The `freediusErrorHandler` (transport errors from httputil.ReverseProxy) gets the flag via closure.

---

## Phase 1: Foundation — middleware stack + error contract

### Overview

Lands the three server-wide middleware primitives (request-ID, panic recovery, access log), unifies all error JSON output into a single `writeErrorJSON` helper with machine-readable codes, adds the `--verbose-errors` flag, the `OriginalProvider` field, the `FREEDIUS_LOG=json` opt-in, and the privacy-policy comments. No adapter behavior changes yet — only the dispatcher and server infrastructure.

### Changes Required:

#### 1. Request-ID middleware

**File**: `proxy/proxy.go` (new `requestIDMiddleware` type or function)

**Intent**: Generate a 32-hex-char request ID via `crypto/rand`, set an `X-Freedius-Request-ID` response header, store in `r.Context()`, and attach to the logger for all downstream log calls.

**Contract**: Exported function `RequestIDMiddleware(next http.Handler) http.Handler` (no logger param — the middleware does not log directly). Uses a `context.WithValue` with an unexported `contextKey` type. Helper `RequestIDFromContext(ctx) string` for extraction. Every log call in this file picks up `request_id` from context or falls back to `""`.

#### 2. Panic recovery middleware

**File**: `proxy/proxy.go` (new `recoverMiddleware` function)

**Intent**: Recover panics anywhere in the handler stack. After recovery: log the panic value as `%v` and a full stack trace (via `runtime/debug.Stack()`) with `request_id` and `path` keys at Error level. If `WriteHeader` has NOT been called yet, write a structured 500 body `{"error":"internal_error","message":"freedius encountered an internal error","request_id":"<id>"}`. If `WriteHeader` has been called, close the connection.

**Contract**: Exported function `RecoverMiddleware(logger *slog.Logger, verboseErrors bool, next http.Handler) http.Handler`. Uses a `wroteHeaderResponseWriter` wrapper (unexported, `code int` field set on `WriteHeader` call) to track whether headers were already written.

#### 3. Access log middleware

**File**: `proxy/proxy.go` (new `accessLogMiddleware` function)

**Intent**: After every request completes, log at Info level: `request_id`, `method`, `path`, `status` (response code), `duration_ms` (millisecond integer), `matched_provider`, `matched_model` (empty if no match). Wraps the final handler.

**Contract**: Exported function `AccessLogMiddleware(logger *slog.Logger, next http.Handler) http.Handler`. Log call happens after the inner handler returns (the `http.ResponseWriter` wrapper captures the status code).

#### 4. Unified error JSON helper

**File**: `proxy/proxy.go` (replace `writeError` + `writeJSON` with `writeErrorJSON`)

**Intent**: Single helper producing `{"error":"<code>","message":"<human>","detail":"<optional>","request_id":"<id>"}`. Every failure path in `ServeHTTP` calls this instead of ad-hoc `writeError` or `writeJSON`. Replace the `{"status":"no_match"}` 404 shape; the code becomes `"no_match"`.

**Contract**: `func (d *Dispatcher) writeErrorJSON(w http.ResponseWriter, r *http.Request, status int, code string, message string, opts ...ErrorOption)` where `ErrorOption` is `WithDetail(string)`. The `request_id` is pulled from `r.Context()` if available, otherwise omitted. The `detail` field is included only when `WithDetail` is provided AND `d.VerboseErrors` is true. (Deviation from draft: added `r *http.Request` param to avoid context-passing boilerplate; replaced planned `WithVerbose(error)` with a `VerboseErrors` field on the Dispatcher combined with `WithDetail(string)` for a simpler API.)

#### 5. `--verbose-errors` flag

**File**: `main.go:41-43` (add flag definition), `proxy/proxy.go:Dispatcher` (add `VerboseErrors bool` field)

**Intent**: When set (flag or `FREEDIUS_VERBOSE_ERRORS=1`), adapter error details are forwarded to the client in the `detail` field. When unset (default), the `detail` field is omitted for adapter/upstream errors.

**Contract**: `Dispatcher.VerboseErrors` field. `main.go` resolves the flag value and passes it to `NewDispatcher`.

#### 6. `Model.OriginalProvider` field

**File**: `config/config.go:16-20` (add field), `config/defaults.go:36-43` (set before rewrites)

**Intent**: Preserve the user's configured provider name before `applyEntryDefaults` rewrites it. Adapters use this for error messages; the dispatcher uses this for the "provider not registered" and "upstream error" responses.

**Contract**: New field `OriginalProvider string` on `Model` struct (`yaml:"-"` — never serialized, optional). In `applyEntryDefaults`, set `m.OriginalProvider = m.Provider` at the top, before any `m.Provider` mutation. When empty (backwards compat), fall back to `m.Provider` in error templates.

#### 7. `FREEDIUS_LOG=json` (or `--log-format=json`)

**File**: `main.go:62-63` (flag + env resolution)

**Intent**: When `FREEDIUS_LOG=json` is set (or `--log-format=json`), use `slog.NewJSONHandler` instead of `slog.NewTextHandler`. Default stays text.

**Contract**: New flag `--log-format` (values: `text`, `json`; default `text`). Env var `FREEDIUS_LOG` overridden by flag. Handler is created once, before config loading.

#### 8. Privacy-policy comments

**Files**: `proxy/proxy.go` (after imports), `proxy/translate/anthropic_openai.go` (after imports)

**Intent**: Document the privacy posture at the code surface closest to body processing.

**Contract**: Multi-line comment following the `package proxy` declaration:

```
// DO NOT log request or response bodies in this file.
// freedius NFR-Privacy (prd.md): no request or response payload is logged
// to disk or transmitted beyond the target provider. Metadata (model name,
// provider, status code) is acceptable; message content, tool arguments,
// tool results, and API responses are not.
```

Same comment in `proxy/translate/anthropic_openai.go`.

#### 9. Middleware wiring in `main.go`

**File**: `main.go:93-96` (replace `mux.Handle("/", dispatcher)` with middleware stack)

**Intent**: Wire the middleware stack outermost-to-innermost: `requestIDMiddleware` → `accessLogMiddleware` → `recoverMiddleware` → dispatcher. Request-ID middleware is outermost so every downstream handler, including the recover handler, can read the request ID from context.

**Contract**: The `mux.Handle("/", ...)` call passes the dispatcher through three wrappers. `RequestIDMiddleware` wraps `AccessLogMiddleware` wraps `RecoverMiddleware` wraps dispatcher.

### Success Criteria:

#### Automated Verification:

- `go vet ./...` passes
- `go build ./...` passes
- `go test ./...` passes with new assertions: panic-in-stub-handler → 500, request-ID header in every response, error JSON contains `"error"` + `"message"` fields, `"detail"` absent when `verboseErrors=false`
- `OriginalProvider` set on parsed `Model` after defaults applied (new test in `config/config_test.go`)
- `FREEDIUS_LOG=json` produces JSON lines (new test in `main_test.go` or verification script)
- Privacy comment present in both files (grep assertion in test)

#### Manual Verification:

- Start server, POST malformed JSON; confirm response has `X-Freedius-Request-ID` header and body has `request_id` field matching the header
- Inject panic in dispatcher (e.g., temp `panic("test")`); confirm 500 with opaque body + full stack trace in stderr + `request_id` in both
- Set `FREEDIUS_LOG=json`; confirm structured log output

---

## Phase 2: Adapter hardening

### Overview

Fixes pre-WriteHeader error forwarding in the dispatcher (replacing generic `"upstream error"` with the adapter's actual error, gated by `--verbose-errors`), adds a per-request stream timeout to `OpenAICompatibleAdapter`, adds a Debug routing log to `MixAdapter`, unifies `freediusErrorHandler` to use `writeErrorJSON`, and rewrites adapter error templates to use `OriginalProvider`.

### Changes Required:

#### 1. Pre-WriteHeader error forwarding

**File**: `proxy/proxy.go:105-108` (modify adapter failure branch)

**Intent**: When an adapter returns a non-nil error AND no response headers have been written yet, forward the original error through `writeErrorJSON` with code `"upstream_error"` and the original error text in `detail` (gated by `--verbose-errors`). When `--verbose-errors` is off, include only the `"message"` field with `"request to upstream provider failed"`.

**Contract**: The dispatcher tracks whether the response was written via an `http.ResponseWriter` wrapper (reuse `wroteHeaderResponseWriter` from Phase 1). After `adapter.Handle()` returns error, check `if !wroteHeader`: call `writeErrorJSON(http.StatusBadGateway, "upstream_error", "request to upstream provider failed", WithVerbose(err))`. If `wroteHeader` was set, log the error and return nil (per Adapter Return Contract — response is already in flight).

#### 2. Per-request stream timeout

**File**: `proxy/openai_compat.go:14-24` (add `streamTimeout` field, set in constructor), `proxy/openai_compat.go:46` (wrap context), `main.go` (add `--stream-timeout` flag)

**Intent**: Wrap the upstream request context with `context.WithTimeout(r.Context(), a.streamTimeout)` to bound hanging upstreams. Replace the bare `&http.Client{}` with a client carrying a configured `Transport`.

**Contract**:
- `OpenAICompatibleAdapter` struct gains `streamTimeout time.Duration` and `client *http.Client` fields (the latter now has a proper `Transport`).
- Constructor signature: `NewOpenAICompatibleAdapter(logger *slog.Logger, streamTimeout time.Duration)`.
- The `http.NewRequestWithContext` call wraps `r.Context()`: `ctx, cancel := context.WithTimeout(r.Context(), a.streamTimeout); defer cancel()`.
- The client is constructed with `&http.Client{Timeout: 0, Transport: &http.Transport{DisableKeepAlives: false}}`.
- `main.go` resolves `--stream-timeout` flag (default `5m`) and passes to `NewOpenAICompatibleAdapter`.
- `NIMAdapter` and `MixAdapter` signatures update to accept and forward `streamTimeout`.

#### 3. `freediusErrorHandler` shape unification

**File**: `proxy/errors.go:23-37` (rewrite body)

**Intent**: Use the same `{"error":"<code>","message":"<human>","detail":"…","request_id":"…"}` shape as the dispatcher for transport errors from `httputil.ReverseProxy`.

**Contract**: The handler receives `request_id` from `r.Context()`. When `context.Canceled`, log Debug (unchanged). Otherwise, write `{"error":"upstream_unreachable","message":"upstream not reachable","detail":"<err.Error()>","request_id":"<id>"}` with 502. The `request_id` is pulled from context via `proxy.RequestIDFromContext(ctx)`.

#### 4. Adapter error-template rewrites

**Files**: `proxy/openai_compat.go:27-33,37,48`, `proxy/anthropic_compat.go:24-31`, `proxy/mix.go:28`

**Intent**: Every `fmt.Errorf("openai adapter: …")` becomes `fmt.Errorf("…")` where the error message uses `m.OriginalProvider` (with fallback to `m.Provider` when empty) and includes the inner format family in parens: `"<orig> adapter (openai-compat): env var %s is not set"`.

**Contract**: Add a helper `originalOr(m config.Model) string` in `proxy/proxy.go` that returns `m.OriginalProvider` if non-empty, else `m.Provider`. Adapter error templates use this helper. Example outputs:
- `provider: nim`, no `NVIDIA_NIM_API_KEY` → `"nim adapter (openai-compat): env var NVIDIA_NIM_API_KEY is not set"`
- `provider: custom`, no `CUSTOM_KEY` → `"custom adapter (anthropic-compat): env var CUSTOM_KEY is not set"`
- `provider: go`, OpenAI path → `"go adapter (openai-compat): env var OPENCODE_API_KEY is not set"`
- `provider: zen`, missing base URL → `"zen adapter (anthropic-compat): missing base_url"`

#### 5. `MixAdapter` routing Debug log

**File**: `proxy/mix.go:30` (add log call)

**Intent**: Log the routing decision at Debug level so typos like `/v1/messagess` can be traced.

**Contract**: After suffix-matching, before delegating: `a.logger.Debug("mix routing", "path", parsedURL.Path, "selected", "anthropic"/"openai")`. Unrecognized paths still route to OpenAI (unchanged behavior) but are logged.

#### 6. Startup banner + unified error sinks

**File**: `main.go:66-77,131-134`

**Intent**: Fix the two-message config-path error, replace `failf` with slog, add a startup banner before config load (so slow loads don't look frozen).

**Contract**:
- Add `slog.Info("freedius starting")` as the first log line after handler creation (before config load).
- `resolveConfigPath` failure: single `slog.Error` message (not two).
- `failf` remains a thin stderr writer only (no `slog.Error` call), because it is used before the logger is fully initialized (early flag parse failures). The logger-based error path is handled by the caller separately when the logger is available.

#### 7. `checkRequiredEnvVars` fix (dead code)

**File**: `main.go:136-157` (rewrite to use `OriginalProvider`)

**Intent**: The `zen`/`go` branchs in `checkRequiredEnvVars` are unreachable because `applyDefaults` rewrites them to `mix` before the function runs. Use `OriginalProvider` for the pre-rewrite name lookup if available.

**Contract**: Instead of hardcoded `[]string{"nim","zen","go"}`, iterate over all model/mapping entries and check `m.APIKeyEnv` is set. Use `originalOr(m).Provider` for the error message. Or simpler: check `m.APIKeyEnv` per-entry (which the second loop already does). Remove the first `for _, name := range ...` loop entirely — it's subsumed by the `for name, m := range cfg.Models` + `cfg.Mappings` loops below it. Note: the pre-rewrite provider name is now available via `m.OriginalProvider`, so the func can report `provider=zen` even when `m.Provider == "mix"`.

### Success Criteria:

#### Automated Verification:

- Test: stub adapter returns pre-WriteHeader error; default (no `--verbose-errors`) → 502 body has `"error":"upstream_error"` and `"message"` but no `"detail"`; with `--verbose-errors` → body has `"detail":"<original error>"`.
- Test: stub upstream hangs; with `--stream-timeout=1s`, context deadline exceeded and client gets a well-formed error response (if headers not yet written) or connection error (if mid-stream).
- Test: `provider=zen` config (post-rewrite `provider=mix`) with missing `OPENCODE_API_KEY`; adapter error template says `"zen adapter (anthropic-compat): …"` not `"anthropic adapter: …"`.
- Test: `provider=nim` config with missing `NVIDIA_NIM_API_KEY`; error says `"nim adapter (openai-compat): …"` not `"openai adapter: …"`.
- Test: `freediusErrorHandler` body matches unified shape: `{"error":"upstream_unreachable","message":"upstream not reachable","detail":"…","request_id":"…"}`.
- Test: Mix routing log emitted with correct `selected` value.
- Test: `checkRequiredEnvVars` catches missing env for `provider=zen` after rewrites (verified via `OriginalProvider`).
- `go vet ./...` passes; `go build ./...` passes; `go test ./...` passes.

#### Manual Verification:

- Configure freedius with a real misconfigured upstream (wrong API key); hit the proxy with claude-code-equivalent POST; confirm error message names the configured provider and is descriptive.
- Run with `--stream-timeout=5s` and pause an upstream mid-stream; confirm stream cuts and error is informative.
- Confirm startup banner appears before config load.

---

## Phase 3: `freedius init` subcommand + starter template

### Overview

Adds subcommand dispatch to `main.go` (`serve`, `init`, `version`), a `templates/starter.yaml` shipped via `embed.FS`, and the `freedius init` command with flags `--output`, `--force`, `--dry-run`. `init` writes a valid config file from the embedded template.

### Changes Required:

#### 1. Subcommand dispatch + `serve` refactor

**File**: `main.go:36-38` (rewrite `main`), `main.go:40-129` (rename `run` → `runServe`)

**Intent**: `main()` peeks `os.Args[1]` to dispatch subcommands. `serve` is the default (no positional arg = serve). Added subcommands: `serve` (explicit or default), `init`, `version`, `help`/`-h`/`--help`.

**Contract**:

```
func main() {
    os.Exit(dispatch(os.Args))
}

func dispatch(argv []string) int {
    sub := "serve"
    args := argv[1:]
    if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
        sub = args[0]
        args = args[1:]
    }
    switch sub {
    case "serve":
        return runServe(args)
    case "init":
        return runInit(args)
    case "version":
        fmt.Printf("freedius %s\n", version)
        return 0
    case "help", "-h", "--help":
        printTopLevelHelp()
        return 0
    default:
        fmt.Fprintf(os.Stderr, "freedius: unknown subcommand %q\nRun 'freedius help' for usage.\n", sub)
        return 2
    }
}
```

`runServe` takes `[]string` args and builds its own `flag.NewFlagSet`. Per-subcommand help via `fs.Usage`.

`version` is a git-version string set via `-ldflags` at build time; if unset, defaults to `"dev"`.

#### 2. `templates/starter.yaml` + `embed.FS`

**File**: New `templates/starter.yaml` (repo root), new `init.go` (package main) with `//go:embed` directive

**Intent**: The starter template is a separate YAML file (editable, lintable) compiled into the binary via `embed.FS`.

**Contract**:

`templates/starter.yaml` content (5 mappings + 1 explicit model, with doc comments):

```yaml
# freedius starter config — edit and save as freedius.yaml
# For more examples, see: config.example.yaml
#
# Model families recognized in requests:
#   opus, sonnet, haiku, auto, default
# Provider options: nim, zen, go, custom, openai, anthropic, mix
#
# Export the env vars listed in api_key_env before running freedius
# (freedius reads keys from environment, not from this file).

mappings:
  # Opencode Go (OpenAI-format base_url)
  opus:    { provider: go, model: deepseek-v4-pro,   base_url: https://opencode.ai/zen/go/v1/chat/completions }
  # Opencode Go (Anthropic-format base_url)
  sonnet:  { provider: go, model: minimax-m3,        base_url: https://opencode.ai/zen/go/v1/messages }
  # Opencode Zen (Anthropic-format base_url)
  haiku:   { provider: zen, model: claude-sonnet-4-6, base_url: https://opencode.ai/zen/v1/messages }
  # NVIDIA NIM (built-in defaults for base_url and api_key_env)
  auto:    { provider: nim, model: step-3.5 }
  # Catch-all for any Claude Code model not matched above
  default: { provider: nim, model: step-3.5 }

# Exact-match entries override family-based mappings.
# Uncomment and edit:
# models:
#   claude-sonnet-4-5-20250929: { provider: zen, model: claude-sonnet-4-6, base_url: https://opencode.ai/zen/v1/messages }
```

`init.go`:

```go
package main

import "embed"

//go:embed templates/starter.yaml
var starterTemplate string  // or []byte; embed.FS is for directories
```

Wait — `embed` for a single file needs the file to exist. `//go:embed templates/starter.yaml` with type `string` or `[]byte` or `embed.FS`. For a single file, `string` or `[]byte` is simplest. Use `var starterTemplate string` with `//go:embed templates/starter.yaml` — one variable in `init.go`.

#### 3. `freedius init` implementation

**File**: `main.go` (new `runInit` function), `init.go` (embed directive + helper)

**Intent**: Write the starter template to `<output>` (default `./freedius.yaml`). Refuse if target exists (exit 1 with hint about `--force`). With `--force`, backup to `<output>.bak` and overwrite. With `--dry-run`, print to stdout and exit 0.

**Contract**:

New `flag.FlagSet` for `init`: `--output <path>` (default `freedius.yaml`), `--force` (bool), `--dry-run` (bool), `--no-env` (bool, consumed in Phase 4), `--shell-install` (bool, consumed in Phase 4).

`runInit(args []string) int`:
1. Parse flags with `fs.Usage` showing init-specific help.
2. Resolve `--output` to absolute path (relative to cwd).
3. If `--dry-run`: print `starterTemplate` to stdout; return 0.
4. If target exists and `!force`: `fmt.Fprintf(os.Stderr, "freedius: %s already exists (use --force to overwrite)\n", output)`; return 1.
5. If `--force` and target exists: `os.Rename(target, target+".bak")`. On error, surface and exit 1.
6. `os.MkdirAll(filepath.Dir(target), 0o755)`. On error, surface and exit 1.
7. `os.WriteFile(target, []byte(starterTemplate), 0o644)`. On error, surface and exit 1.
8. Print "wrote <target>" to stdout.
9. Return 0.

#### 4. `help` banner

**File**: `main.go` (new `printTopLevelHelp` function)

**Intent**: Print a short top-level usage message listing the subcommands.

**Contract**: `printTopLevelHelp()` prints:

```
freedius — local Claude Code proxy

Usage: freedius [<subcommand>] [<flags>]

  serve     Start the proxy server (default)
  init      Generate a starter config file
  version   Print the binary version
  help      Show this help

Run 'freedius <subcommand> --help' for subcommand-specific flags.
```

### Success Criteria:

#### Automated Verification:

- `go test ./...` passes with new tests: `TestRunInit_WritesFile`, `TestRunInit_RefusesExisting`, `TestRunInit_ForceWithBackup`, `TestRunInit_DryRun`, `TestRunInit_OutputCustomPath`, `TestRunInit_OutputParentCreated`
- Test: `freedius init --dry-run` output parses with `config.Load` (round-trip test — new in `main_test.go`).
- Test: Template embedded variable is non-empty.
- Test: `freedius version` prints a non-empty string; exit code 0.
- Test: Default invocation (no subcommand) still runs serve (regression — `TestRunServe_StartsServer`).
- `go vet ./...` passes. `go build ./...` passes.

#### Manual Verification:

- Run `freedius init` in a fresh directory; confirm file contents match expectations; `less freedius.yaml` looks right.
- Run `freedius init` again in same directory; confirm "already exists" refusal with `--force` hint.
- Run `freedius init --force`; confirm backup created (`freedius.yaml.bak`), main file overwritten.
- Run `freedius init --output /tmp/test-config.yaml`; confirm file written to custom path.
- Run `freedius version`; confirm version string output.
- Run `freedius serve` (explicit) and `freedius` (default) — both start the server.

---

## Phase 4: Env auto-injection

### Overview

Adds the `internal/envinject/` package with three capabilities: a stderr eval-snippet printed on every `serve` startup, an opt-in `~/.claude/settings.json` writer (merge, idempotent) triggered by `freedius init`, and an opt-in shell-rc appender triggered by `freedius init --shell-install`.

### Changes Required:

#### 1. `internal/envinject/` package

**File**: New `internal/envinject/envinject.go` (package, exports), `internal/envinject/snippet.go`, `internal/envinject/settings.go`, `internal/envinject/shellrc.go`

##### 1a. `EvalSnippet`

**File**: `internal/envinject/snippet.go`

**Intent**: Generate a copy-paste-ready `export` block for the dev to source in their shell.

**Contract**: `func Snippet(host string, port int) string`. Returns:

```
# Paste these into your shell to route Claude Code through freedius:
export ANTHROPIC_BASE_URL="http://<host>:<port>"
export ANTHROPIC_API_KEY="freedius-dummy"
export ENABLE_TOOL_SEARCH="true"
# Optional: disable telemetry and error reporting
export DISABLE_TELEMETRY="1"
export DISABLE_ERROR_REPORTING="1"
# Or the kill-switch for all non-essential traffic:
# export CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC="1"
# Silence this hint with --no-export-hint
```

No dynamic values beyond host and port.

##### 1b. `WriteSettingsJSON`

**File**: `internal/envinject/settings.go`

**Intent**: Merge an `env` block into `~/.claude/settings.json`, preserving existing keys. Write atomically via temp file + rename.

**Contract**: `func WriteSettingsJSON(configDir string, host string, port int, dryRun bool) error`. Steps:
1. Resolve path: `filepath.Join(configDir, "settings.json")`. Default `configDir` is `~/.claude` (resolved via `os.UserHomeDir()`).
2. If `dryRun`: read existing (if any), merge, print result to stdout, return nil.
3. Read existing file; unmarshal into `map[string]any`. If file doesn't exist, start with empty map.
4. Set `m["env"]` to the fixed env block (marshal `map[string]string{...}`).
5. Marshal with `json.MarshalIndent(m, "", "  ")`.
6. `os.MkdirAll(filepath.Dir(path), 0o700)`.
7. Write to `<path>.tmp`, then `os.Rename(<path>.tmp, path)`.

The env block written:

```json
{
  "ANTHROPIC_BASE_URL": "http://<host>:<port>",
  "ANTHROPIC_API_KEY": "freedius-dummy",
  "ENABLE_TOOL_SEARCH": "true",
  "DISABLE_TELEMETRY": "1",
  "DISABLE_ERROR_REPORTING": "1"
}
```

**Edge case**: If `m["env"]` is not a `map[string]any` (malformed existing settings), replace it entirely and log a warning.

##### 1c. `WriteShellRC`

**File**: `internal/envinject/shellrc.go`

**Intent**: Append or update a marker-delimited block to the user's shell rc file. Detect shell from `$SHELL`. Refuse if shell is unrecognized. Refuse if marker already present, unless `force` replaces the block.

**Contract**: `func WriteShellRC(homeDir string, shell string, host string, port int, force bool, dryRun bool) (string, error)`. Returns the rc path written to.

1. Detect shell lang from `shell` env value (last segment of path):
   - `zsh` → lang: `zsh`, rc: `filepath.Join(homeDir, ".zshrc")`
   - `bash` → lang: `bash`, rc: `filepath.Join(homeDir, ".bashrc")`
   - `fish` → lang: `fish`, rc: `filepath.Join(homeDir, ".config", "fish", "config.fish")`
   - `sh` → return `"", fmt.Errorf("unsupported shell %q: use zsh, bash, or fish", shell)`
   - unknown → same error
2. Generate marker block:

For `zsh`/`bash`:
```
# >>> freedius env >>>
# Auto-generated by freedius init --shell-install.
# Remove everything between the markers to uninstall.
export ANTHROPIC_BASE_URL="http://<host>:<port>"
export ANTHROPIC_API_KEY="freedius-dummy"
export ENABLE_TOOL_SEARCH="true"
export DISABLE_TELEMETRY="1"
export DISABLE_ERROR_REPORTING="1"
# <<< freedius env <<<
```

For `fish`:
```
# >>> freedius env >>>
# Auto-generated by freedius init --shell-install.
# Remove everything between the markers to uninstall.
set -gx ANTHROPIC_BASE_URL "http://<host>:<port>"
set -gx ANTHROPIC_API_KEY "freedius-dummy"
set -gx ENABLE_TOOL_SEARCH "true"
set -gx DISABLE_TELEMETRY "1"
set -gx DISABLE_ERROR_REPORTING "1"
# <<< freedius env <<<
```

3. Read existing rc file. If marker `# >>> freedius env >>>` found:
   - If `!force`: return path, `nil` (idempotent — already installed). Print "already installed (use --shell-install --force to replace)".
   - If `force`: remove existing marker block (everything from start marker to end marker inclusive). Append new block.
4. If marker not found: append block to end of file (prepend `\n` if file doesn't end with newline).
5. If `dryRun`: print would-be content to stdout; do not write; return path, nil.
6. Write to temp + rename (same atomic pattern as settings.json).

#### 2. Wire stderr eval-snippet into `serve` startup

**File**: `main.go:83-84` (after the "listening on" log line)

**Intent**: After the server starts, print the eval-snippet to stderr. Silencable with `--no-export-hint`.

**Contract**: New flag `--no-export-hint` (bool, default false). After server startup log, `fmt.Fprintln(os.Stderr, envinject.Snippet(host, port))` unless the flag is set.

#### 3. Wire into `freedius init`

**File**: `main.go` (`runInit` function — extend)

**Intent**: `freedius init` calls `envinject.WriteSettingsJSON` and optionally `envinject.WriteShellRC` after writing the config file.

**Contract**:
1. After config file write succeeds:
2. If `!noEnv`: call `envinject.WriteSettingsJSON("~/.claude", host, port, dryRun)`. Print "wrote ~/.claude/settings.json" on success.
3. If `shellInstall`: call `envinject.WriteShellRC(home, shell, host, port, dryRun)`. Print result. Exit non-zero on error (settings.json independent — a failed shell-rc write leaves settings.json in place).
4. `init` needs `host` and `port` for the injected values. Resolve defaults the same way `serve` does: `host = "127.0.0.1"`, `port = 8080`, with `--host` and `--port` flags also available for `init`.
5. `--host` and `--port` are shared flags for both `serve` and `init`; the `init` `flag.NewFlagSet` includes them.

### Success Criteria:

#### Automated Verification:

- New `internal/envinject/envinject_test.go`: `TestSnippet_ContainsAllVars`, `TestWriteSettingsJSON_MergePreservesKeys`, `TestWriteSettingsJSON_CreatesNew`, `TestWriteSettingsJSON_DryRunNoWrite`, `TestWriteSettingsJSON_MalformedEnvReplaced`, `TestDetectShellRC_Zsh/Bash/Fish/Unknown`, `TestWriteShellRC_AppendsBlock`, `TestWriteShellRC_IdempotentReturn`, `TestWriteShellRC_ForceReplaces`, `TestWriteShellRC_DryRunNoWrite`, `TestWriteShellRC_UnknownShellError`
- New `main_test.go`: `TestRunInit_WritesSettingsJSON`, `TestRunInit_SkipsSettingsJSONWithNoEnv`, `TestRunInit_ShellInstall_Appends`, `TestRunInit_ShellInstall_Idempotent`, `TestRunInit_ShellInstall_RefusesUnknownShell`
- Test: startup eval-snippet appears in stderr buffer; suppressed by `--no-export-hint`
- Test: `freedius init --shell-install` with `$SHELL=zsh` writes to correct rc path
- `go vet ./...` passes; `go build ./...` passes; `go test ./...` passes

#### Manual Verification:

- Run `freedius init` in a fresh environment; confirm settings.json env block created; launch `claude` in a new terminal and confirm it routes through freedius.
- Run `freedius init --shell-install` in zsh; `source ~/.zshrc`; confirm `echo $ANTHROPIC_BASE_URL` outputs the proxy URL.
- Re-run `freedius init --shell-install`; confirm "already installed" message and no double-append.
- Run `freedius init --shell-install --force`; confirm block replaced (not appeded twice).
- Run `freedius serve`; confirm eval-snippet appears; run `freedius serve --no-export-hint`; confirm snippet suppressed.
- Run `freedius init --no-env`; confirm only config file written, no settings.json mutation.

---

## Testing Strategy

### Unit Tests:

- **Phase 1**: request-ID middleware (header + context propagation), panic recovery (500 body + log), `writeErrorJSON` all code paths (with/without detail, with/without request_id), `OriginalProvider` set correctly through `applyDefaults`, `OriginalProvider` preserved through `custom`→`anthropic` and `zen`/`go`→`mix` rewrites, JSON log handler produces valid JSON.
- **Phase 2**: pre-WriteHeader error forwarding with/without `--verbose-errors`, stream timeout context deadline behavior, adapter error template strings for all 4 providers + inner delegates, `freediusErrorHandler` unified shape, Mix routing Debug log emitted, `checkRequiredEnvVars` uses `OriginalProvider`.
- **Phase 3**: `runInit` writes file, refuses exists, `--force` with backup, `--dry-run`, `--output` creates parent, template parses with `config.Load`, subcommand dispatch routes correctly, `version` exits 0 with non-empty output, default invocation == serve.
- **Phase 4**: `EvalSnippet` content, `WriteSettingsJSON` merge preserves unknown keys, creates new, dry-run, malformed env replacement, `WriteShellRC` appends, idempotent returns, force replaces, unknown shell errors, init wiring (settings.json write on init, skip with `--no-env`, shell-rc install success/idempotent/refuse).

### Integration Tests:

- Full middleware stack: request-ID → panic recovery → dispatch; verify panic produces consistent log + 500.
- `init` + config round-trip: `freedius init --dry-run` output fed to `config.Load` → succeeds with valid known providers.
- Env-injection round-trip: init writes settings.json; verify file content is valid JSON with expected env keys.

### Manual Testing Steps:

1. Start server; trigger each of the 8 dispatch failure modes with `curl`; confirm consistent JSON shape + `request_id`.
2. Inject panic in handler; confirm 500 (not connection reset) + stack trace in stderr.
3. Configure a real misconfigured upstream; confirm Claude Code sees a descriptive error (not `"upstream error"` or a crash).
4. Run `freedius init` in fresh dir; inspect output; run `freedius init` again to confirm refuse.
5. Run `freedius init --shell-install` in zsh; source rc; run `claude` and confirm it routes through freedius.
6. Set `FREEDIUS_VERBOSE_ERRORS=1`; repeat step 3; confirm `detail` field is present and helpful.
7. Set `FREEDIUS_LOG=json`; verify structured log lines.

## Performance Considerations

- **Request-ID generation**: `crypto/rand` is a CSPRNG reading from the OS. For a single-user proxy at tens of QPS, this is imperceptible. No caching needed.
- **Middleware overhead**: Three thin wrappers (request-ID, access log, panic recovery) add ~2 allocs + 1 map access per request. Imperceptible relative to upstream round-trip latency.
- **Stream timeout**: The per-request `context.WithTimeout` allocates a timer channel. Go's runtime pools timers; overhead < 1 microsecond.
- **Shell-rc write**: Read + write + rename of a ~2KB file. O(1ms). No performance concern.
- **Settings.json merge**: Read + unmarshal + marshal + write of a ~500B file. Same order. No concern.

## Migration Notes

- **`OriginalProvider` field is additive** — empty string in existing structs means "use `Provider` as before." No migration needed for existing config files. Tests that construct `Model` literals without `OriginalProvider` continue to pass with the fallback logic.
- **`--verbose-errors` defaults to off** — existing behavior (opaque errors) is preserved for non-explicit users. Only the JSON shape changes (from three inconsistent shapes to one). Existing API consumers parsing `{"status":"no_match"}` will break on the `{"error":"no_match",...}` shape — but the only consumer is Claude Code, which treats 4xx status codes uniformly.
- **Subcommand dispatch** — `freedius` with no args still runs serve (unchanged). `freedius --config path` still works because unrecognized positional args fall through to `runServe`'s `flag.FlagSet`. `freedius --help` still prints help (top-level help now).
- **Shell-rc install is fully contained** — marker delimiters make it visible and reversible. Delete the block to uninstall. No hooks, no daemons, no systemd units.
- **Rollback** — revert to any prior commit; `OriginalProvider` is YAML-ignored so config files don't change. Settings.json merge is idempotent so no cleanup needed on downgrade.

## References

- Research: `context/changes/error-hardening/research.md` (comprehensive error-path mapping, adapter failure matrix, env-injection mechanism analysis)
- Lessons: `context/foundation/lessons.md` (SSE encoding, SSE reader, `custom`→`anthropic` rewrite, extra tests, Adapter Return Contract)
- PRD: `context/foundation/prd.md` (FR-004, Success-Criteria-Secondary, NFR-Error-handling, NFR-Multi-agent, NFR-Privacy)
- Roadmap: `context/foundation/roadmap.md:34` (S-04 definition)
- Adapter Return Contract: `context/foundation/lessons.md:33-43`
- `custom`→`anthropic` rewrite: `config/defaults.go:46-48`
- Dispatch failure matrix: `proxy/proxy.go:36-109`
- Claude Code env vars reference: https://docs.claude.com/en/docs/claude-code/env-vars
- Claude Code settings reference: https://docs.claude.com/en/docs/claude-code/settings

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles. See `references/progress-format.md`.

### Phase 1: Foundation — middleware stack + error contract

#### Automated

- [x] 1.1 — 2e96014 `go vet ./...` passes
- [x] 1.2 — 2e96014 `go build ./...` passes
- [x] 1.3 — 2e96014 `go test ./...` passes with new middleware + error contract tests
- [x] 1.4 — 2e96014 Request-ID header present in all responses
- [x] 1.5 — 2e96014 Panic recovery middleware returns 500 with opaque body
- [x] 1.6 — 2e96014 `writeErrorJSON` produces unified shape with and without detail
- [x] 1.7 — 2e96014 `OriginalProvider` set correctly through `applyDefaults` rewrites
- [x] 1.8 — 2e96014 `FREEDIUS_LOG=json` produces JSON log lines
- [x] 1.9 — 2e96014 Privacy comment present in `proxy/proxy.go` and `proxy/translate/anthropic_openai.go`

#### Manual

- [x] 1.10 — 2e96014 Server responds with `request_id` matching header for malformed requests
- [x] 1.11 — 2e96014 Injected panic produces 500 opaque body + stderr stack trace with `request_id`
- [x] 1.12 — 2e96014 `FREEDIUS_LOG=json` confirmed with structured log lines in terminal

### Phase 2: Adapter hardening

#### Automated

- [x] 2.1 Pre-WriteHeader error forwarded with `detail` only when `--verbose-errors` — bb65298
- [x] 2.2 Stream timeout context deadline honored (stub hanging upstream) — bb65298
- [x] 2.3 Adapter error templates use `OriginalProvider` for all 4 providers — bb65298
- [x] 2.4 `freediusErrorHandler` body matches unified shape — bb65298
- [x] 2.5 Mix routing Debug log emitted on suffix match — bb65298
- [x] 2.6 `checkRequiredEnvVars` uses `OriginalProvider` correctly — bb65298
- [x] 2.7 `go vet ./...` passes; `go build ./...` passes; `go test ./...` passes — bb65298

#### Manual

- [x] 2.8 Real misconfigured upstream produces descriptive error via Claude Code — bb65298
- [x] 2.9 `--stream-timeout=5s` + paused upstream cuts stream cleanly — bb65298
- [x] 2.10 Startup banner appears before config load — bb65298

### Phase 3: `freedius init` subcommand + starter template

#### Automated

- [x] 3.1 `freedius init` writes file when none exists; exits 0 — 1d82761
- [x] 3.2 `freedius init` refuses when target exists; exits 1 with `--force` hint — 1d82761
- [x] 3.3 `freedius init --force` backs up to `.bak` and overwrites — 1d82761
- [x] 3.4 `freedius init --dry-run` prints to stdout without writing — 1d82761
- [x] 3.5 `freedius init --output <path>` writes to custom path; creates parent — 1d82761
- [x] 3.6 Template output parses with `config.Load` (round-trip) — 1d82761
- [x] 3.7 `freedius version` prints version and exits 0 — 1d82761
- [x] 3.8 Default invocation (no subcommand) runs serve (regression) — 1d82761
- [x] 3.9 `go vet ./...` passes; `go build ./...` passes; `go test ./...` passes — 1d82761

#### Manual

- [x] 3.10 `freedius init` in fresh directory produces valid, readable config — 1d82761
- [x] 3.11 Re-run `freedius init` shows helpful refusal message — 1d82761

### Phase 4: Env auto-injection

#### Automated

- [x] 4.1 `EvalSnippet` contains all required env vars — fe56264
- [x] 4.2 Settings.json merge preserves unknown top-level keys — fe56264
- [x] 4.3 Settings.json creates new file when none exists — fe56264
- [x] 4.4 Settings.json dry-run prints without writing — fe56264
- [x] 4.5 Shell-rc appends marker block to zsh/bash/fish — fe56264
- [x] 4.6 Shell-rc idempotent re-run returns "already installed" — fe56264
- [x] 4.7 Shell-rc `--force` replaces existing marker block — fe56264
- [x] 4.8 Shell-rc refuses unknown shell with clear error — fe56264
- [x] 4.9 `freedius init` writes settings.json by default — fe56264
- [x] 4.10 `freedius init --no-env` skips settings.json write — fe56264
- [x] 4.11 `freedius init --shell-install` appends to detected rc file — fe56264
- [x] 4.12 Startup eval-snippet emitted; suppressed by `--no-export-hint` — fe56264
- [x] 4.13 `go vet ./...` passes; `go build ./...` passes; `go test ./...` passes — fe56264

#### Manual

- [~] 4.14 `freedius init` in fresh env writes settings.json env block (script passes); `claude` routing check requires manual `claude` run
- [x] 4.15 `freedius init --shell-install` in zsh; source rc; `echo $ANTHROPIC_BASE_URL` shows proxy URL
- [x] 4.16 Re-run `--shell-install` shows "already installed"; rc has single marker block
- [x] 4.17 `freedius init --shell-install --force` replaces block (not doubled)
- [x] 4.18 `freedius serve` prints eval-snippet; `freedius serve --no-export-hint` suppresses it — fe56264
- [x] 4.19 `freedius init --no-env` writes config only, no settings.json mutation

## Post-Review Addenda

The following changes were shipped with this commit but were not in the original plan. They are documented here for completeness.

### A. Auto-write starter config when none found

When the default config path does not exist and no explicit `--config` was given, freedius now auto-writes the embedded starter template and starts the server instead of failing with "config not found". This improves first-run developer experience.

**File**: `main.go:155-172`

### B. Rename NIM_API_KEY → NVIDIA_NIM_API_KEY

The env var for NIM API key was renamed from `NIM_API_KEY` to `NVIDIA_NIM_API_KEY` across all source, tests, manual scripts, and context docs for clarity and consistency with NVIDIA's naming conventions.

### C. Default port 8080 → 8082

The default listen port changed from 8080 to 8082 to reduce conflicts with common local services.

### D. Starter template uses go/zen providers with OPENCODE_API_KEY

The auto-written starter template now uses `go`/`zen` providers (which delegate to `openai`/`anthropic`/`mix` internally) with `api_key_env: OPENCODE_API_KEY` instead of `nim` with `NVIDIA_NIM_API_KEY`, since the template targets opencode.ai users.

### E. Starter model mapping updates

Mappings were adjusted: `sonnet` → `deepseek-v4-fresh`, `haiku` → `nim step-3.5`, `auto` → `deepseek-v4-fresh`, with corrected model IDs (`deepseek-ai/deepseek-v4-pro`, etc.). All `go` entries use `/v1/messages` or `/v1/chat/completions` based on the upstream format.

### F. Support system message role in Anthropic→OpenAI conversion

Claude Code sends system prompts as messages with `role: system`. The converter previously returned "unsupported message role" and a 502. Now maps `system` role directly to OpenAI's system message role.

**File**: `proxy/translate/anthropic_openai.go:303-322`
