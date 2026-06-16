---
id: proxy-skeleton
title: Proxy skeleton — HTTP server + config loading + dispatch stub
status: planned
created: 2026-06-16
updated: 2026-06-16
roadmap_id: F-01
prd_refs:
  - FR-001
  - FR-003
  - FR-004
  - FR-005
  - NFR-Multi-agent
  - NFR-Resource-footprint
---

# Proxy Skeleton (F-01) Implementation Plan

## Overview

Wire the F-01 request loop: an HTTP server that listens on a local port, loads and validates a YAML config (model→provider mappings) at startup, and dispatches incoming requests to a no-op handler. The handler is the integration point S-01 will replace with real provider adapters; for F-01 it returns `501 Not Implemented` with the matched provider info so the routing is observable end-to-end.

The goal is the smallest possible loop that proves "request comes in → config gets matched → dispatch gets called". Anything related to actually translating or proxying requests belongs in S-01.

## Current State Analysis

- `go.mod` exists with `module github.com/user/freedius` and `go 1.26.1`. No source files.
- AGENTS.md defines the structural convention: `main.go` + `config/` + `proxy/` packages, `gofumpt` formatting, no comments, env-var config for runtime knobs.
- The bootstrap-verification log shows CI was not scaffolded and `govulncheck` is not installed locally — both are F-01 concerns.
- The roadmap (F-01 row) is explicit: keep this slice minimal. Risk note: "the main risk is over-engineering the foundation".
- PRD FR-004 splits configuration: **credentials live in env vars; model mappings live in the config file**. F-01 only touches the mappings side; credentials are S-01's concern.

## Desired End State

After F-01 lands:

- `make ci` (or `go test ./... && go vet ./... && go build ./...`) is green.
- `./freedius` starts, loads the config, and listens on `127.0.0.1:8080` (or the configured port).
- `curl -X POST http://127.0.0.1:8080/v1/messages -d '{"model":"claude-opus-4",...}' -H 'content-type: application/json'` returns `501` with `X-Freedius-Matched-Provider: nim` and `X-Freedius-Matched-Model: meta/llama-3.1-70b` headers, and a JSON body describing the match.
- Sending a request with an unmapped model returns `501` with `status: "no_match"`.
- Sending a malformed body returns `400` with a clear message.
- Sending a bad config file or unavailable port produces a single actionable stderr line and `os.Exit(1)`.
- GitHub Actions CI runs `go vet`, `go test`, `go build`, and `govulncheck` on every push and PR.

### Key Discoveries

- The codebase is greenfield — no prior patterns to follow beyond `AGENTS.md`.
- Go 1.22+ ServeMux pattern matching is preferred (per AGENTS.md) but F-01 only needs a single catch-all route (Claude Code may call any path; the proxy doesn't yet know which paths are valid).
- `log/slog` (Go 1.21+) gives structured logging with zero deps. TTY/JSON auto-switching is deferred (would need `golang.org/x/term`) — F-01 always uses `TextHandler` to stderr.
- `goccy/go-yaml`'s `yaml.FormatError` produces `[line:col] message` style errors — matches the "clear error message" guardrail in the PRD.
- `http.Server` + `signal.NotifyContext` + `server.Shutdown(ctx)` is the canonical graceful-shutdown pattern in Go 1.22+.

## What We're NOT Doing

- No real provider translation (Anthropic→OpenAI, etc.) — that is S-01.
- No provider adapters (NIM, Zen, Go, custom) — S-01 and S-02.
- No credential loading from env vars — S-01.
- No `freedius init` command or config template generation — S-03.
- No request-body logging (NFR-Privacy forbids it).
- No metrics endpoint, pprof, or in-flight counters — over-scope for a foundation.
- No TTY/JSON auto-switching for slog — use plain text in F-01; revisit if S-01/S-02 need it.
- No Windows-specific path handling — Linux + macOS only, per the roadmap "Parked" section.

## Implementation Approach

Single linear path through 4 phases. Each phase ends with `go test ./... && go vet ./... && go build ./...` green. Phases 1-3 add files without cross-package dependencies; Phase 4 wires everything in `main.go`.

Config schema is locked at planning time so S-01 can plug in adapters without a schema migration:

```yaml
# freedius.yaml
models:
  claude-opus-4:
    provider: nim
    model: meta/llama-3.1-70b-instruct
  claude-sonnet-4:
    provider: custom
    model: my-sonnet-shim
```

The `provider` field is validated against the closed set `{nim, zen, go, custom}` at startup. Adding a 5th provider in v2 is a one-line edit in `config/config.go`.

The dispatch handler's response shape is the contract S-01 will preserve (just change the status code and body to a real proxied response):

```json
{ "matched_provider": "nim", "matched_model": "meta/llama-3.1-70b-instruct", "status": "not_implemented" }
```

with `X-Freedius-Matched-Provider` and `X-Freedius-Matched-Model` response headers.

## Critical Implementation Details

- **Strict YAML mode**: the config parser must reject unknown fields. A typo like `provder: nim` should fail at startup with a line-numbered error, not silently leave the provider empty. Use `yaml.UnmarshalWithOptions(..., yaml.Strict())` from goccy/go-yaml.
- **Precedence at startup**: `--port` flag > `FREEDIUS_PORT` env var > `8080` default. Same shape for `--config` (no env equivalent) and `--host` (no env equivalent).
- **Config-path resolution order**: `./freedius.yaml` → `./freedius.yml` → XDG path. First match wins; do not error if multiple exist (use the first).
- **Config-path resolution is silent on lookup**: only the final "not found" error is reported. Use slog.Debug for the search steps so a curious dev can opt in with `FREEDIUS_LOG=debug` (a follow-up; F-01 logs at Info).
- **Empty config is an error**: a YAML file that parses to zero model mappings should fail with "config file at <path> contains no model mappings". An empty *file* is the same case.
- **`--host` accepts only `127.0.0.1` and `0.0.0.0`**: any other value is rejected with a clear error. The NFR-Privacy goal of "data lives in-flight only" means we should not silently bind to arbitrary interfaces.
- **Graceful shutdown timeout**: 5 seconds. SIGINT and SIGTERM both trigger it.
- **slog level**: Info by default. Component tags (`config`, `proxy`, `server`) on every log line.

## Phase 1: Project scaffolding

### Overview

Lay down the non-source scaffolding: dependency manifest, ignore file, CI workflow, Makefile, and an example config that documents the schema. No Go code is written in this phase; the goal is a green `go build` and a CI workflow that runs on push.

### Changes Required:

#### 1. Add goccy/go-yaml dependency

**File**: `go.mod`

**Intent**: Add the YAML library we picked for parsing the config file. One direct dependency.

**Contract**: `go.mod` gains a `require` line for `github.com/goccy/go-yaml` (latest stable). `go.sum` is populated by `go mod tidy`.

#### 2. .gitignore

**File**: `.gitignore`

**Intent**: Prevent the build artifact and editor/OS cruft from being committed.

**Contract**: Lines covering: the `freedius` binary, any `.bootstrap-scaffold/` remnants, `.DS_Store`, `*.swp`, `*.swo`, `*~`, IDE files (`.idea/`, `.vscode/`). Keep it short — the AGENTS.md style prefers minimal config files.

#### 3. GitHub Actions CI workflow

**File**: `.github/workflows/ci.yml`

**Intent**: Run the checks AGENTS.md specifies (`go vet`, `go test`, `go build`) plus `govulncheck` on every push and PR. Match the `ci_provider: github-actions` and `auto-deploy-on-merge` hints from tech-stack.md — for F-01, deploy is out of scope; CI runs checks only.

**Contract**: Workflow triggers on `push` and `pull_request` to all branches. Single job `test` on `ubuntu-latest` with Go 1.26 setup. Steps: `go mod download` → `go vet ./...` → `go test -race -coverprofile=coverage.out ./...` → `go build ./...` → `govulncheck ./...`. Caches `~/go/pkg/mod` keyed on `go.sum`.

#### 4. Makefile

**File**: `Makefile`

**Intent**: Single command for "run all checks locally" that matches CI exactly. Devs run `make ci` before pushing.

**Contract**: Targets:
- `make test` → `go test -race -cover ./...`
- `make vet` → `go vet ./...`
- `make build` → `go build -o freedius .`
- `make ci` → `make vet && make test && make build` (govulncheck excluded locally — not installed per bootstrap-verification)
- `make tidy` → `go mod tidy`
- `.PHONY` declaration for all targets

#### 5. Example config

**File**: `config.example.yaml`

**Intent**: Document the YAML schema with a working example. Also serves as the canonical fixture for tests in Phase 2.

**Contract**: Comments-free YAML (per AGENTS.md) that lists two model mappings — one for `nim` and one for `custom` — covering both "use a free provider" and "use a custom Anthropic-compatible endpoint" cases from the PRD. Two-space indentation.

### Success Criteria:

#### Automated Verification:

- `go mod tidy` exits 0 with `go.mod` and `go.sum` written
- `go build ./...` exits 0 (no source files yet, so this is a no-op — confirms tooling works)
- `go vet ./...` exits 0
- `make` runs without "no targets" error
- `make ci` exits 0
- `yamllint` (if available) reports no syntax errors in `config.example.yaml`; manual: open in editor and confirm structure is valid YAML

#### Manual Verification:

- Open `.github/workflows/ci.yml` and confirm the workflow is well-formed (Actions will validate it on the first push, but eyeball it first to catch obvious YAML mistakes)
- Open `config.example.yaml` and confirm it documents the schema we agreed on

---

## Phase 2: config/ package

### Overview

Implement the YAML loader: types for the config, validation against the known provider set, and table-driven tests covering every error path.

### Changes Required:

#### 1. Config types and Load function

**File**: `config/config.go`

**Intent**: Define the schema, parse the file with strict mode, and validate. The package is the single source of truth for "is this config valid?".

**Contract**:
- `type Config struct { Models map[string]Model ` + "`yaml:\"models\"`" + ` }`
- `type Model struct { Provider string ` + "`yaml:\"provider\"`" + `; Model string ` + "`yaml:\"model\"`" + ` }`
- `var KnownProviders = map[string]struct{}{"nim": {}, "zen": {}, "go": {}, "custom": {}}`
- `func Load(path string) (*Config, error)` reads the file, calls `yaml.UnmarshalWithOptions(data, &cfg, yaml.Strict())`, validates every `Provider` against `KnownProviders`, returns `&Config{Models: parsed.Models}` or a wrapped error.
- Error messages must include the file path and (for YAML parse errors) the line+col from `yaml.FormatError(err, true, false)`.
- All errors are wrapped with `fmt.Errorf("config: %w", err)` for traceability.

#### 2. Config tests

**File**: `config/config_test.go`

**Intent**: Table-driven tests for every code path. 80%+ coverage target.

**Contract**: Test cases (all using `t.TempDir()` for fixture files):
- Valid config with 1 model → loads, no error, mapping correct
- Valid config with 2 models → loads, both mappings present
- Empty file (zero bytes) → error: "config file at <path> contains no model mappings"
- File with `models: {}` → same error as above
- Missing file → error: "config file not found at <path>" (wraps `os.ErrNotExist`)
- Malformed YAML (bad indentation) → error contains line+col, e.g. `[3:6]`
- Unknown provider (`provider: foo`) → error: "config file at <path>: model \"claude-opus-4\" uses unknown provider \"foo\" (known: nim, zen, go, custom)"
- Unknown field (`provder: nim` typo) → strict mode error: "config file at <path>: unknown field \"provder\"" (with line+col)
- Missing `model` subfield → error: "config file at <path>: model \"claude-opus-4\" has no \"model\" field"
- Missing `provider` subfield → error: "config file at <path>: model \"claude-opus-4\" has no \"provider\" field"
- Non-string `provider` (e.g. `provider: 42`) → YAML unmarshal error
- `KnownProviders` is exported and contains exactly `{nim, zen, go, custom}` (a small unit test on the set itself)

### Success Criteria:

#### Automated Verification:

- `go test -race -cover ./config/...` exits 0 with coverage ≥ 80%
- `go vet ./...` exits 0
- `go build ./...` exits 0
- `make ci` exits 0

#### Manual Verification:

- None — the package is pure logic; tests cover the behavior.

---

## Phase 3: proxy/ package — dispatch stub

### Overview

Implement the dispatch handler that takes a request, extracts the model name, looks it up in the config, and returns `501 Not Implemented` with the matched provider info. This is the integration point S-01 will replace with real provider adapters.

### Changes Required:

#### 1. Dispatch handler

**File**: `proxy/proxy.go`

**Intent**: Define the handler that closes over the config and produces the F-01 response shape.

**Contract**:
- `type Dispatcher struct { Cfg *config.Config; Logger *slog.Logger }`
- `func NewDispatcher(cfg *config.Config, logger *slog.Logger) *Dispatcher`
- `func (d *Dispatcher) ServeHTTP(w http.ResponseWriter, r *http.Request)`
- Request body is read fully (use `io.ReadAll` with a reasonable limit — 10 MiB matches typical LLM request sizes; document the limit).
- Body is unmarshaled into `struct { Model string ` + "`json:\"model\"`" + ` }`. Unmarshal failure → `400 Bad Request` with `{"error": "invalid request body: <err>"}` (do not log the body — NFR-Privacy).
- Model lookup in `d.Cfg.Models`. Found → 501 with body `{"matched_provider": "<x>", "matched_model": "<y>", "status": "not_implemented"}` and headers `X-Freedius-Matched-Provider: <x>`, `X-Freedius-Matched-Model: <y>`. Not found → 501 with body `{"status": "no_match"}`.
- All 501 responses have `Content-Type: application/json`.
- Logger is used for the `component: "proxy"` tag and to log the dispatch decision at Debug level (model name + match result, no body content).
- The body size limit is exposed as a constant in the file for testability and future tuning.

A non-obvious detail: the response body for a match is JSON-typed `map[string]string` constructed in code, not marshaled from a struct — keeps the JSON keys exactly as the contract specifies without struct-tag drift.

#### 2. Dispatch handler tests

**File**: `proxy/proxy_test.go`

**Intent**: Table-driven tests for the dispatch contract.

**Contract**: Test cases (all using `httptest.NewRecorder`):
- POST with valid body and known model → 501, body has matched_provider/matched_model, headers present
- POST with valid body and unknown model → 501, body has `status: "no_match"`, no X-Freedius headers
- POST with malformed JSON (`{not json`) → 400, body has `error: "invalid request body: ..."`
- POST with valid JSON missing `model` field → 400 (model is empty string; treat empty as "no model field")
- POST with empty body → 400
- POST with body larger than the limit → 400 with a clear "request body too large" error
- GET request → 405 Method Not Allowed (Claude Code always uses POST; defend against accidental misuse)
- The dispatcher does not panic on a nil `*config.Config` — handled by the constructor pattern (`NewDispatcher` is the only entry point)

### Success Criteria:

#### Automated Verification:

- `go test -race -cover ./proxy/...` exits 0 with coverage ≥ 80%
- `go vet ./...` exits 0
- `go build ./...` exits 0
- `make ci` exits 0

#### Manual Verification:

- None — the package is pure logic; tests cover the behavior.

---

## Phase 4: main.go — wire it together

### Overview

The entry point: parse flags and env, resolve the config path, set up slog, build the mux, start the HTTP server, and handle signals for graceful shutdown. This is the only phase that needs manual end-to-end verification.

### Changes Required:

#### 1. main.go

**File**: `main.go`

**Intent**: Wire config + proxy + http.Server + signals. All decisions are pre-made; this is the integration phase.

**Contract**:
- `func main()` (the only exported function; AGENTS.md: "Panic only at package init or `main()` entry" — fine here).
- Flags: `--config string` (path to config; empty = auto-resolve), `--port int` (default 8080, overridden by `FREEDIUS_PORT` env if flag not set), `--host string` (default `127.0.0.1`).
- Flag-precedence logic: parse flags first, then for each setting, `flag-provided || env-provided || default`. Implement as a small helper `resolveInt(flagVal int, flagSet bool, envKey string, def int) int`.
- Config path resolution: `resolveConfigPath(explicit string) string` helper:
  1. If `explicit != ""` → return it
  2. Try `./freedius.yaml` (relative to `os.Getwd()`); exists → return
  3. Try `./freedius.yml`; exists → return
  4. Return XDG path: `os.UserConfigDir()` + `/freedius/config.yaml`. (On Linux this is `$XDG_CONFIG_HOME` or `~/.config`; on macOS it's `~/Library/Application Support`. `os.UserConfigDir()` is the stdlib helper.)
  5. Caller receives the path and calls `config.Load(path)`; if Load fails with `os.ErrNotExist`, wrap with "config file not found at <path> — create one or pass --config <path>".
- slog setup: `slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))` then create a `*slog.Logger` tagged `component: "server"`.
- Build `mux := http.NewServeMux()`. Register `mux.Handle("/", dispatcher.ServeHTTP)` (catch-all — Claude Code's exact paths are not yet known; S-01 will tighten the routing).
- Build `server := &http.Server{Addr: net.JoinHostPort(host, port), Handler: mux, ReadHeaderTimeout: 5 * time.Second}` (ReadHeaderTimeout is the slowloris defense; cheap to add).
- `ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)`; defer `stop()`. Goroutine that calls `server.ListenAndServe()` and `server.Shutdown(ctx)` on signal. 5-second shutdown timeout via `context.WithTimeout` on a derived context if needed for draining.
- `--host` validation: only `127.0.0.1` and `0.0.0.0` accepted; any other value → `os.Exit(1)` with "invalid --host value: <x> (allowed: 127.0.0.1, 0.0.0.0)".
- Startup log line at Info: "freedius listening on http://<host>:<port>" with attrs `component: server, host, port`.
- Dispatch decision is logged at Debug (the proxy package handles this; main.go only logs server lifecycle).
- On `ListenAndServe` error other than `http.ErrServerClosed`, log and `os.Exit(1)`.

The function should be small — under 150 lines per AGENTS.md style.

### Success Criteria:

#### Automated Verification:

- `go build -o freedius .` exits 0 and produces a runnable binary
- `go vet ./...` exits 0
- `go test -race -cover ./...` exits 0 (all package tests pass)
- `make ci` exits 0
- `./freedius --help` exits 0 and prints the flags

#### Manual Verification:

- Start `./freedius` in one terminal with a valid `config.example.yaml` copied to `freedius.yaml` in CWD
- `curl -X POST http://127.0.0.1:8080/v1/messages -H 'content-type: application/json' -d '{"model":"claude-opus-4"}'` returns 501 with `X-Freedius-Matched-Provider: nim` header
- `curl -X POST http://127.0.0.1:8080/v1/messages -H 'content-type: application/json' -d '{"model":"unknown"}'` returns 501 with `status: no_match` body
- `curl -X POST http://127.0.0.1:8080/v1/messages -H 'content-type: application/json' -d 'not json'` returns 400 with clear error
- Start `./freedius --port 99999` → fails fast with "invalid --port value" (out of range)
- Start `./freedius --host 10.0.0.1` → fails fast with the host validation error
- Start `./freedius` with no config file present and no `--config` flag → fails fast with "config file not found at <xdg path> — create one or pass --config <path>"
- Start `./freedius` with a malformed YAML in CWD → fails fast with `[line:col] <yaml error>` and exits non-zero
- Start `./freedius` in one terminal; in another, `kill -TERM <pid>` → process exits cleanly within 5 seconds
- Start two `./freedius` instances on the same port → second one fails fast with "listen tcp 127.0.0.1:8080: bind: address already in use"
- Start `./freedius` and watch stderr — only the "listening on" line is logged at Info; no per-request log spam (privacy + signal)

---

## Testing Strategy

### Unit Tests

- `config/config_test.go` — every code path in `Load`: valid, missing file, malformed YAML, unknown provider, unknown field, missing subfield, empty file, non-string types.
- `proxy/proxy_test.go` — every code path in `ServeHTTP`: known model, unknown model, malformed body, missing field, empty body, oversize body, wrong method.
- `main.go` is not unit-tested in F-01 — its logic is flag/env/path resolution, all of which gets end-to-end manual verification in Phase 4.

### Integration Tests

- None in F-01. The end-to-end flow is one `curl` invocation; full integration tests come in S-01 when the proxy actually proxies to a real provider.

### Manual Testing Steps

See "Manual Verification" under Phase 4 — every startup error path, every response shape, and the signal handling.

## Performance Considerations

- **Idle memory**: Go stdlib `net/http` + `log/slog` + a single `map[string]Model` config comfortably fits under 50 MB (PRD NFR-Resource-footprint). A quick `runtime.ReadMemStats` print at startup could verify this, but it's not necessary in F-01.
- **Latency**: the dispatch handler is one map lookup + one JSON marshal — sub-microsecond overhead. The 501 response adds an artificial "I haven't done this yet" tax but no real latency. PRD NFR-Latency ("imperceptible overhead") is trivially satisfied.
- **Concurrency**: `net/http` serves each request in its own goroutine; the config is read-only after `Load` returns. The dispatch handler closes over the config pointer, so concurrent Claude Code sessions (PRD NFR-Multi-agent) get isolated handler invocations with no shared mutable state. No explicit locking or per-session storage in F-01.
- **Request body limit**: 10 MiB cap in the dispatch handler prevents a single misbehaving client from exhausting memory. The constant lives in `proxy/proxy.go` so it can be tuned or moved to config in a later slice.

## References

- PRD: `context/foundation/prd.md` (FR-001, FR-003, FR-004, FR-005, NFR-Multi-agent, NFR-Resource-footprint, NFR-Privacy)
- Roadmap: `context/foundation/roadmap.md` (F-01 row, Baseline section, "Parked" section for the OS-scope confirmation)
- Tech stack: `context/foundation/tech-stack.md` (Go stdlib + httputil.ReverseProxy, GitHub Actions CI)
- AGENTS.md: `AGENTS.md` (coding style: no comments, `gofumpt`, env-var config, module layout)
- goccy/go-yaml docs (Context7): `yaml.UnmarshalWithOptions(..., yaml.Strict())` for strict mode, `yaml.FormatError(err, true, false)` for line+col errors

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles. See `references/progress-format.md`.

### Phase 1: Project scaffolding

#### Automated

- [x] 1.1 Add goccy/go-yaml to go.mod and run `go mod tidy` — 81f0888
- [x] 1.2 Create `.gitignore` covering the binary, `.bootstrap-scaffold/`, editor/OS cruft — 81f0888
- [x] 1.3 Create `.github/workflows/ci.yml` running vet, test, build, govulncheck — 81f0888
- [x] 1.4 Create `Makefile` with test/vet/build/ci/tidy targets — 81f0888
- [x] 1.5 Create `config.example.yaml` documenting the schema with one nim and one custom mapping — 81f0888
- [x] 1.6 Run `make ci` and confirm it exits 0 — 81f0888

#### Manual

- [x] 1.7 Eyeball `.github/workflows/ci.yml` for YAML correctness before first push
- [x] 1.8 Eyeball `config.example.yaml` to confirm it documents the agreed schema

### Phase 2: config/ package

#### Automated

- [x] 2.1 Implement `Config`, `Model`, `KnownProviders`, and `Load` in `config/config.go` with strict mode and provider validation — 162e9e9
- [x] 2.2 Write table-driven tests in `config/config_test.go` covering valid, missing, malformed, unknown provider, unknown field, missing subfield, empty file — 162e9e9
- [x] 2.3 Run `go test -race -cover ./config/...` — all pass, coverage ≥ 80% — 162e9e9
- [x] 2.4 Run `go vet ./...` and `make ci` — both exit 0 — 162e9e9

#### Manual

### Phase 3: proxy/ package — dispatch stub

#### Automated

- [x] 3.1 Implement `Dispatcher` and `NewDispatcher` in `proxy/proxy.go` with body parsing, model lookup, and 501/400 responses — d030fce
- [x] 3.2 Write table-driven tests in `proxy/proxy_test.go` covering known model, unknown model, malformed body, missing field, empty body, oversize body, wrong method — d030fce
- [x] 3.3 Run `go test -race -cover ./proxy/...` — all pass, coverage ≥ 80% — d030fce
- [x] 3.4 Run `go vet ./...` and `make ci` — both exit 0 — d030fce

#### Manual

### Phase 4: main.go — wire it together

#### Automated

- [x] 4.1 Implement `main.go` with flag/env parsing, config path resolution, slog setup, mux, server, signal handling, host validation — 975001c
- [x] 4.2 Run `go build -o freedius .` — exits 0 and produces a runnable binary — 975001c
- [x] 4.3 Run `./freedius --help` — exits 0 and prints the flags — 975001c
- [x] 4.4 Run `go vet ./...`, `go test -race -cover ./...`, `make ci` — all exit 0 — 975001c

#### Manual

- [x] 4.5 End-to-end smoke: start with a valid `freedius.yaml` in CWD; `curl POST` known model → 501 with `X-Freedius-*` headers
- [x] 4.6 End-to-end smoke: `curl POST` unknown model → 501 with `status: no_match`
- [x] 4.7 End-to-end smoke: `curl POST` malformed body → 400 with clear error
- [x] 4.8 Startup error: `--port 99999` → fails fast with "invalid --port value"
- [x] 4.9 Startup error: `--host 10.0.0.1` → fails fast with the host validation error
- [x] 4.10 Startup error: no config file present and no `--config` flag → fails fast with "config file not found at <xdg path>"
- [x] 4.11 Startup error: malformed YAML in CWD → fails fast with `[line:col] <yaml error>` and non-zero exit
- [x] 4.12 Graceful shutdown: `kill -TERM <pid>` → process exits cleanly within 5 seconds
- [x] 4.13 Port conflict: start a second instance on the same port → fails fast with "bind: address already in use"
- [x] 4.14 Log volume: stderr shows only the "listening on" line at Info; no per-request log spam
