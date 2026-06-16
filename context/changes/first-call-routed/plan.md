---
id: first-call-routed
title: First call routed — NIM adapter + custom passthrough
status: planned
created: 2026-06-16
updated: 2026-06-16
roadmap_id: S-01
prd_refs:
  - US-01
  - FR-001
  - FR-002
  - FR-006
  - FR-009
  - NFR-Latency
  - NFR-Error-handling
  - NFR-Privacy
---

# First Call Routed (S-01) Implementation Plan

## Overview

Wire the S-01 north-star slice: two real provider adapters plugged into the F-01 dispatch seam, proving that freedius can route a Claude Code call to a non-Anthropic provider (Nvidia NIM via Anthropic→OpenAI translation, or a custom Anthropic-compatible endpoint via passthrough) with tool use, streaming, and error forwarding all intact.

## Current State Analysis

- `main.go:82` constructs `proxy.NewDispatcher(cfg, logger)` and registers it as the catch-all handler on `mux.Handle("/", dispatcher)`.
- `proxy/proxy.go:32-91` is the entire `Dispatcher.ServeHTTP` — method check, content-type check, body read into `[]byte` (line 47), JSON parse for the `model` field, model lookup against `cfg.Models`, and a 501 stub at lines 86-90 that echoes the matched provider/model.
- `config/config.go:13-27` defines `Config{Models map[string]Model}` and `Model{Provider, Model}`. The closed `KnownProviders` set is `{nim, zen, go, custom}` (line 22-27). Strict-mode YAML decoding at line 43 means any new field must be added to `Model` in the same commit as the code that consumes it.
- `proxy/proxy_test.go:15-24` has a `newTestDispatcher` helper that builds a `Dispatcher` with one hard-coded model. The table-driven test pattern at lines 97-136 is the canonical pattern for the new tests.
- `config/config_test.go:11-166` covers the existing `Load` function with 11 cases — new field validation tests slot in here.
- `AGENTS.md` codifies: no comments in code, `gofumpt`, env-var config, Go 1.22+ ServeMux patterns.
- F-01 review (`context/changes/proxy-skeleton/reviews/impl-review.md`) settled all the F-01 hardening (timeouts, header injection, nil-checks, error shape); none of it constrains S-01's adapter design.

## Desired End State

After S-01 lands:

- `freedius` starts, loads a config that maps a Claude Code model to either `provider: nim` or `provider: custom`, and proxies the request to the configured upstream.
- A `claude-code` session with `ANTHROPIC_BASE_URL=http://127.0.0.1:8080` (or equivalent) completes a real conversation routed through freedius — streaming responses, tool calls, multi-turn all work.
- For `provider: custom` (Anthropic-compatible upstream): request body passes through unchanged; the adapter only swaps the URL and adds the `Authorization: Bearer <key>` header.
- For `provider: nim` (OpenAI-compatible upstream): the adapter translates the Anthropic `/v1/messages` body to OpenAI `/v1/chat/completions` format on the way in, then translates the OpenAI SSE stream back to Anthropic SSE format on the way out.
- Upstream errors (NIM 401/429/500, custom shim 4xx/5xx) reach Claude Code verbatim — same status code, same response body.
- Missing API key env vars fail at startup with a clear actionable message; freedius never starts in a half-configured state.
- `make ci` is green. Per-package coverage: `config` ≥ 90%, `proxy` ≥ 85%, `proxy/translate` ≥ 90% (the pure functions are easy to cover with golden files).

### Key Discoveries

- The dispatcher's body-read at `proxy/proxy.go:47` is a load-bearing seam: by the time the adapter runs, the body is already a `[]byte`. The custom adapter must re-inject this into `r.Body` before calling `ReverseProxy.ServeHTTP`. The NIM adapter passes it directly to `translateRequest`.
- `httputil.ReverseProxy` strips hop-by-hop headers *after* `Director` returns, which can strip headers `Director` set. Go 1.20+ `Rewrite` is the documented fix and what the custom adapter must use. Research `research.md:2.2` confirms this.
- `httputil.ReverseProxy` auto-flushes streaming responses (`text/event-stream` or `Content-Length: -1`); `FlushInterval: -1` is unnecessary for the SSE case. The custom adapter needs no special flush configuration.
- The two adapters are asymmetric: custom uses `ReverseProxy` (zero custom streaming code), NIM uses `http.Client` + `bufio.Reader` + `http.ResponseController(w).Flush()` (full SSE translation). They both satisfy the same `Provider.Handle` interface, hiding the asymmetry from the dispatcher.
- The NIM translation has two well-known Go footguns: `bufio.Scanner` silently fails on lines > 64 KB (tool-use `arguments` can exceed this — use `bufio.Reader.ReadBytes`); `json.NewEncoder.Encode` adds a trailing `\n` that corrupts SSE event framing (use `json.Marshal`). Both are documented in `research.md:3.5` and will trip a naive implementer.

## What We're NOT Doing

- **Multi-modal content blocks (images, documents)** — S-01 scope is text + tool-use. If Claude Code sends an image, freedius forwards the block verbatim in the OpenAI-format request to NIM; NIM will likely error, and that error reaches Claude Code as-is. Per Round-3 question #10 decision.
- **Prompt caching fields** (`cache_creation_input_tokens`, `cache_read_input_tokens`) — emitted as `0` in the Anthropic response. Per `research.md:3.4`.
- **`max_tokens` clamping** — passed through unchanged. Per Round-2 question #8 decision.
- **Tightening the HTTP routing** — `mux.Handle("/", dispatcher)` stays. S-01 doesn't know what paths Claude Code calls beyond `/v1/messages`; tightening risks breaking an undocumented path. Per Round-1 question #4 decision and F-01 plan `plan.md:314` deferral.
- **Total upstream-call timeout** — no `http.Client.Timeout`, no `context.WithTimeout` on the outbound call. Wall-clock is bounded only by the inbound `r.Context()` (client disconnect). Per Round-2 question #5 decision.
- **Anthropic-version header on custom adapters** — only `Authorization: Bearer <key>`. Custom providers that require `anthropic-version` will be the user's problem to handle (e.g. via a shim). Per Round-2 question #7 decision.
- **Error envelope wrapping** — upstream 4xx/5xx reach Claude Code verbatim. No Anthropic-shaped error wrapping. Per Round-2 question #6 decision.
- **Real NIM in CI** — tests use `httptest.NewServer` mocks. No `NIM_API_KEY` in CI secrets. Per Round-3 question #9 decision.
- **Env-var lazy re-read** — read once at startup, stored in the adapter struct. Rotation requires a freedius restart. Per Round-1 question #2 decision.
- **Top-level `providers:` block in YAML** — per-model `base_url` + `api_key_env`. Per Round-1 question #1 decision.
- **Provider-specific adapter files for `zen` and `go`** — they still return 501 "not_implemented" in S-01. S-02 adds the real adapters.
- **Schema additions for `zen`/`go`-specific options** — out of scope until S-02.
- **`freedius init` command and config template generation** — S-03.
- **Auto-injection of Claude Code env vars** — S-03.
- **Request-body logging** — NFR-Privacy forbids it; carries over from F-01.
- **Metrics endpoint, pprof, in-flight counters** — not in v1 PRD.

## Implementation Approach

Three phases, each ending with `make ci` green and a clear manual verification step. The phase order is dependency-driven: schema + registry (Phase 1) → custom adapter as the simpler implementation (Phase 2) → NIM adapter with the translation module (Phase 3). Phase 2 is intentionally the easier one because it proves the wiring end-to-end with `httputil.ReverseProxy` (less custom code) before Phase 3 introduces the harder SSE translation work.

The `Provider` interface is the single seam: it hides the asymmetry between the custom passthrough (one-line `ReverseProxy` wrapper) and the NIM translation (`http.Client` + stateful SSE translator) from the dispatcher. The dispatcher doesn't care which kind of adapter it called.

Pure translation functions live in `proxy/translate/` with no I/O — bytes in, bytes out. This split is what makes the translator TDD-friendly under `/10x-tdd`: golden-file tests run in milliseconds against `bytes.Buffer`, no `httptest` server needed. The adapter's `Handle` method is the only place HTTP I/O and `http.ResponseController` exist.

Adapter construction is eager at startup in `main.go`. `os.Getenv` is called once per provider; missing keys fail-fast before the server starts listening. This trades a tiny boot-time coupling (env vars must be set before `freedius` starts) for the operational property that misconfigurations surface immediately, not on first request.

## Critical Implementation Details

These are facts the implementer needs to know before touching the code — things that aren't visible from file paths alone.

- **SSE encoding trap**: `json.NewEncoder(w).Encode(v)` appends `\n` to the marshalled JSON. Using those bytes in `Fprintf(w, "data: %s\n\n", buf)` produces `data: {...}\n\n\n` (three newlines = extra blank line that corrupts the SSE event framing — Claude Code's SDK buffers until it sees a blank line, so an extra blank line is interpreted as "empty event, keep reading", which silently breaks streaming). The translator MUST use `json.Marshal` (no trailing newline) when emitting Anthropic SSE events. This is not a "use whichever" choice — it's a wire-format correctness requirement. Research `research.md:3.5`.
- **SSE reader line-cap trap**: `bufio.Scanner` defaults to a 64 KB `MaxScanTokenSize`. Tool-use `arguments` payloads (Anthropic's `input_json_delta.partial_json`) can exceed this in theory. The translator MUST use `bufio.Reader.ReadBytes('\n')` rather than `bufio.Scanner` — `bufio.Reader` has no fixed line cap and grows as needed. Research `research.md:3.5`.
- **Body re-injection in the custom adapter**: the dispatcher reads the body into `[]byte` at `proxy/proxy.go:47` *before* calling the adapter. The custom adapter's `Handle` method MUST set `r.Body = io.NopCloser(bytes.NewReader(body))` and `r.ContentLength = int64(len(body))` before calling `ReverseProxy.ServeHTTP(w, r)`. Without this, the proxy sees an empty body and the upstream gets nothing. Research `research.md:5.1`.
- **Preserve dispatcher-set headers**: the dispatcher sets `X-Freedius-Matched-Provider` and `X-Freedius-Matched-Model` *before* calling the adapter's `Handle` (`proxy/proxy.go:84-85`). The custom adapter's `ReverseProxy` does NOT clear `w.Header()` before writing the response — it adds/overwrites specific response headers. So the `X-Freedius-*` headers persist. The NIM adapter also preserves them because `writeHeader` is not called until after the headers are set. The contract is: set the matched-headers in the dispatcher, before the adapter call.
- **Adapter return contract**: `Provider.Handle` returns `nil` only if it has called `w.WriteHeader` (success or upstream-mirrored error). Returning a non-nil error means "I did not write a response — dispatcher, write 502". The dispatcher enforces this single-owner rule. An adapter that has called `WriteHeader` and then encounters an error mid-stream returns `nil` (the response is already in flight; nothing to do). Research `research.md:6.1`.
- **Request-Context propagation for cancellation**: both adapters MUST build the upstream request with `http.NewRequestWithContext(r.Context(), ...)` so a client disconnect propagates as `context.Canceled` to the upstream transport and to the streaming reader. Without this, a closed Claude Code session leaves a hung goroutine draining the upstream response. Research `research.md:2.5`.
- **`ReverseProxy` body-replacement inside `Rewrite`**: if the custom adapter ever needs to modify the body (S-02+), the `Rewrite` function MUST set `pr.Out.Body`, `pr.Out.ContentLength`, and `pr.Out.Header.Set("Content-Length", ...)` together — and ideally also `pr.Out.GetBody` for HTTP/2 retries. Missing any one of these leads to silent corruption. The S-01 custom adapter does NOT replace the body (passthrough), so this is informational for S-02+.

## Phase 1: Schema + Provider registry

### Overview

Wire the dispatcher's lookup path from a hardcoded 501 to a registry-driven dispatch, and extend the config schema with the two fields the adapters need. No adapter code yet — Phase 1's deliverable is "config + interface + dispatcher consults the registry, unknown providers return 500". This is the minimum surface the adapters plug into.

### Changes Required:

#### 1. Extend `config.Model` with the S-01 fields

**File**: `config/config.go`

**Intent**: Add `BaseURL` and `APIKeyEnv` fields so the dispatcher can read the upstream endpoint and env-var name per model. Strict-mode YAML decoding requires these be in the struct before any code reads them.

**Contract**: `Model` gains two `yaml` tags:
- `BaseURL string \`yaml:"base_url,omitempty"\`` — the upstream endpoint (e.g. `https://integrate.api.nvidia.com/v1/chat/completions` for NIM, `https://my-shim.example.com/v1/messages` for custom)
- `APIKeyEnv string \`yaml:"api_key_env,omitempty"\`` — the env-var *name* (e.g. `NIM_API_KEY`), not the value
The existing per-model validation loop (currently at `config/config.go:51-63`) adds:
- Reject `provider=custom` without `BaseURL` (error: "config: config file at <path>: model <name> has provider=custom but no base_url")
- Reject `BaseURL` whose scheme is not `http` or `https` (error: "config: config file at <path>: model <name> has base_url with invalid scheme <scheme> (allowed: http, https)")
- Reject `APIKeyEnv` containing CR/LF or `=` (defensive; env-var names cannot contain these)
The existing CRLF/colon check on `Model` (line 61-63) does NOT need to be extended to `BaseURL` — `BaseURL` is parsed by `net/http`, which will reject malformed URLs at request time with a clear error.

#### 2. Update `config.example.yaml` to document the new schema

**File**: `config.example.yaml`

**Intent**: Replace the F-01 stub example with one that shows the new fields and demonstrates both `nim` and `custom` mappings.

**Contract**: Two model entries, one for each. The `nim` entry has `api_key_env: NIM_API_KEY` and no `base_url` (the adapter uses a hardcoded const default). The `custom` entry has both `base_url` and `api_key_env`. This is the user-facing documentation for the S-01 schema.

#### 3. Add the `Provider` interface and `Registry`

**File**: `proxy/provider.go` (new)

**Intent**: Define the adapter contract and a simple lookup-by-name registry. The interface is the single seam that hides the asymmetry between the custom passthrough and the NIM translation from the dispatcher.

**Contract**:
```go
package proxy

import (
    "net/http"
    "github.com/pfrack/freedius/config"
)

type Provider interface {
    Handle(w http.ResponseWriter, r *http.Request, m config.Model, body []byte) error
}

type Registry struct {
    providers map[string]Provider
}

func NewRegistry(providers map[string]Provider) *Registry {
    // build the map, panic on nil entries (defensive: same pattern as NewDispatcher per F-01 review F7)
}

func (r *Registry) Lookup(name string) (Provider, bool) {
    // return provider, true if registered; nil, false otherwise
}
```

#### 4. Update `Dispatcher` to consult the registry

**File**: `proxy/proxy.go`

**Intent**: Replace the 501 stub at `proxy/proxy.go:86-90` with a registry lookup. Preserve all pre-dispatch behavior (model lookup, debug log, matched-headers). On unknown provider: return 500 with a clear message. On adapter error: log and return 502.

**Contract**:
- `Dispatcher` struct gains a `Registry *Registry` field.
- `NewDispatcher(cfg, registry, logger)` becomes the 3-arg constructor (was 2-arg). Add a nil check on `registry` (defensive, per F-01 review F7 pattern).
- The replacement for lines 86-90 is roughly:
  ```go
  w.Header().Set("X-Freedius-Matched-Provider", m.Provider)
  w.Header().Set("X-Freedius-Matched-Model", m.Model)
  d.Logger.Debug("dispatch", "model", req.Model, "provider", m.Provider, "target_model", m.Model)
  adapter, ok := d.Registry.Lookup(m.Provider)
  if !ok {
      d.Logger.Error("provider not registered", "provider", m.Provider)
      d.writeError(w, http.StatusInternalServerError, "provider not registered: "+m.Provider)
      return
  }
  if err := adapter.Handle(w, r, m, body); err != nil {
      d.Logger.Error("adapter failed", "provider", m.Provider, "err", err)
      d.writeError(w, http.StatusBadGateway, "upstream error")
  }
  ```
  The `X-Freedius-Matched-*` headers are set *before* the adapter call (preserved contract from F-01; also documented in Critical Implementation Details).
- The existing `Provider` value in `KnownProviders` (config/config.go:22-27) still validates as a known provider name. In S-01, `nim` and `custom` will have registered adapters; `zen` and `go` will not. A config that maps a model to `provider: zen` will pass config validation but fail at the registry lookup with 500. This is the right behavior — the dispatcher's 500 message tells the user "you configured a provider that has no adapter yet", which is more informative than failing silently with 501.

#### 5. Update `main.go` to pass a registry (nil for now)

**File**: `main.go`

**Intent**: Update the `NewDispatcher` call site. For Phase 1, the registry is `nil` — the dispatcher will return 500 on any model lookup. This is a one-line change.

**Contract**: Replace `proxy.NewDispatcher(cfg, logger)` at `main.go:82` with `proxy.NewDispatcher(cfg, nil, logger)`. Phase 2 and Phase 3 will replace the `nil` with a real registry.

#### 6. Update tests

**File**: `proxy/proxy_test.go`, `config/config_test.go`

**Intent**: Keep the test suite green and add new cases for Phase 1's contract changes.

**Contract**:
- `config/config_test.go`: add 4-5 cases to the `TestLoad` table — `provider=custom` without `base_url` (error), `base_url` with `ftp://` scheme (error), `api_key_env` with newline (error), valid `provider=nim` with `api_key_env` only (no `base_url` — passes), valid `provider=custom` with both fields (passes).
- `proxy/proxy_test.go`: update `newTestDispatcher` to pass a `*Registry` (use `NewRegistry` with an empty map for now). Add a test case "provider not registered" asserting the 500 response with the `provider not registered:` message. Existing cases (known/unknown model, malformed body, etc.) should keep working because the registry has no entries but the unknown-model case still returns 404 from the dispatcher.

### Success Criteria:

#### Automated Verification:

- `make ci` is green
- `go test -race -cover ./config/...` — coverage ≥ 90% (the new field validation adds cases; should be straightforward to cover)
- `go test -race -cover ./proxy/...` — coverage ≥ 85% (the new registry-lookup path needs a test)
- `go vet ./...` — clean
- `go build ./...` — succeeds
- New test cases for: `provider=custom` without `base_url` (error), invalid `base_url` scheme (error), invalid `api_key_env` (error), valid nim+api_key_env (passes), valid custom+both fields (passes), provider-not-registered 500 path

#### Manual Verification:

- Start `./freedius` with a config that has `provider: custom` with `base_url` and `api_key_env` populated
- `curl -X POST http://127.0.0.1:8080 -d '{"model":"some-mapped-model"}'` returns 500 with body `{"error":"provider not registered: custom"}` and headers `X-Freedius-Matched-Provider: custom`, `X-Freedius-Matched-Model: <whatever>`
- `curl` with `provider: nim` returns 500 with `provider not registered: nim`
- `curl` with `provider: foo` is rejected at config-load (the existing F-01 closed-set validation)
- `curl` with an unknown model name returns 404 (the F-01 behavior, unchanged)

**Implementation Note**: After completing this phase and all automated verification passes, pause here for manual confirmation that the manual testing was successful before proceeding to Phase 2.

---

## Phase 2: Custom passthrough adapter

### Overview

Implement the first real adapter: a `*httputil.ReverseProxy` wrapper that passes the Anthropic request body through unchanged and forwards the Anthropic SSE response back to Claude Code. This phase proves the entire wiring (dispatcher → registry → adapter → upstream → client) end-to-end with minimal custom code, before Phase 3 introduces the harder SSE translation work.

### Changes Required:

#### 1. Implement `CustomAdapter`

**File**: `proxy/custom.go` (new)

**Intent**: Wrap `*httputil.ReverseProxy` configured with `Rewrite` for URL and `Authorization` header. The adapter's `Handle` method re-injects the already-buffered body into `r.Body` and delegates to the proxy.

**Contract**:
```go
package proxy

type CustomAdapter struct {
    rp *httputil.ReverseProxy
    logger *slog.Logger
}

func NewCustomAdapter(logger *slog.Logger) *CustomAdapter {
    // Build the ReverseProxy with a Rewrite function. The Rewrite is set per-call
    // (not per-construction) because the target URL comes from the per-model config.
    // To do this, the adapter stores a closure that captures (logger) and accepts
    // the per-call model.
    return &CustomAdapter{
        logger: logger.With("component", "adapter.custom"),
    }
}

func (a *CustomAdapter) Handle(w http.ResponseWriter, r *http.Request, m config.Model, body []byte) error {
    if m.BaseURL == "" {
        // Defensive: config validation should have caught this. Log and return error
        // so the dispatcher writes 502.
        return fmt.Errorf("custom adapter: missing base_url")
    }
    apiKey := os.Getenv(m.APIKeyEnv)
    if apiKey == "" {
        return fmt.Errorf("custom adapter: env var %s is not set", m.APIKeyEnv)
    }
    target, err := url.Parse(m.BaseURL)
    if err != nil {
        return fmt.Errorf("custom adapter: invalid base_url %q: %w", m.BaseURL, err)
    }
    r.Body = io.NopCloser(bytes.NewReader(body))
    r.ContentLength = int64(len(body))
    r.Header.Set("Authorization", "Bearer "+apiKey)
    rp := &httputil.ReverseProxy{
        Rewrite: func(pr *httputil.ProxyRequest) {
            pr.SetURL(target)
            pr.Out.Host = target.Host
            pr.Out.Header.Set("Authorization", "Bearer "+apiKey)
        },
        ErrorHandler: freediusErrorHandler(a.logger),
    }
    rp.ServeHTTP(w, r)
    return nil
}
```

The `freediusErrorHandler` is a small helper (defined here, possibly in a new `proxy/errors.go` if Phase 3 also uses it) that:
- Returns silently on `context.Canceled` (client gone, don't write)
- Otherwise writes 502 with `{"error":"upstream_unreachable","detail":"<err>"}` and logs at Error level

**Note on the per-call ReverseProxy construction**: constructing a new `*ReverseProxy` per request is fine for freedius's scale (single user, local). The struct is small (~100 bytes) and the alternative (sharing one proxy with a mutable target) would require a `sync.RWMutex` and add complexity for no measurable benefit. Per-request construction is also what keeps the test surface clean — each test gets a fresh proxy.

#### 2. Add `forwardUpstreamError` helper

**File**: `proxy/errors.go` (new) — also used by Phase 3

**Intent**: Helper to forward an upstream 4xx/5xx response (status + headers + body) verbatim to the client. This is what the NIM adapter calls when NIM returns a non-2xx status before streaming begins.

**Contract**:
```go
func forwardUpstreamError(w http.ResponseWriter, resp *http.Response) error {
    for k, vv := range resp.Header {
        for _, v := range vv {
            w.Header().Add(k, v)
        }
    }
    w.WriteHeader(resp.StatusCode)
    _, err := io.Copy(w, resp.Body)
    return err
}
```

#### 3. Wire the custom adapter into `main.go`

**File**: `main.go`

**Intent**: Build a `CustomAdapter` at startup, register it in the `Registry`, and pass the registry to `NewDispatcher`. The `os.Getenv` is called per-request (inside `Handle`) for rotation support, but the adapter is constructed once.

**Contract**:
- Add a `registry := proxy.NewRegistry(...)` after `cfg, err := config.Load(cfgPath)`.
- For Phase 2, the registry has only the `custom` entry: `map[string]proxy.Provider{"custom": proxy.NewCustomAdapter(logger)}`.
- Replace `proxy.NewDispatcher(cfg, nil, logger)` with `proxy.NewDispatcher(cfg, registry, logger)`.
- `NIM_API_KEY` is not yet required (NIM adapter is Phase 3).

#### 4. Write the custom adapter test

**File**: `proxy/custom_test.go` (new)

**Intent**: Verify the adapter sends the right request shape to the upstream and forwards the response back to the client.

**Contract**: Table-driven test with `httptest.NewServer` as the mock upstream. Cases:
- "passthrough text request" — request body forwarded unchanged, `Authorization: Bearer <key>` set, response body forwarded unchanged
- "passthrough streaming SSE" — upstream returns `Content-Type: text/event-stream` with multiple chunks; client receives all chunks
- "upstream 401" — client receives 401 with upstream's body verbatim
- "upstream 500" — client receives 500 with upstream's body verbatim
- "missing env var" — adapter returns non-nil error; dispatcher writes 502
- "missing base_url" — adapter returns non-nil error; dispatcher writes 502 (defensive — config validation should catch this)
- "client disconnect" — adapter returns `context.Canceled`-wrapped error; client does not see a 502 (the error handler skips writing on Canceled)

#### 5. Add a dispatcher-level integration test

**File**: `proxy/proxy_test.go` (additions)

**Intent**: End-to-end through the dispatcher → registry → custom adapter → mock upstream.

**Contract**: New test case in `TestServeHTTP` (or a new `TestServeHTTPWithAdapter`): config has one `nim` model and one `custom` model pointing at `httptest.NewServer` URLs; `POST /v1/messages` with a `custom`-mapped model returns 200 with the upstream's body.

### Success Criteria:

#### Automated Verification:

- `make ci` is green
- `go test -race -cover ./proxy/...` — coverage ≥ 85% (the adapter is small, easy to cover)
- New tests cover all 7 cases listed in Change #4
- `go vet ./...`, `go build ./...` — clean

#### Manual Verification:

- Set up a real Anthropic-compatible endpoint (e.g. a personal LiteLLM shim, a local vLLM with Anthropic-format enabled, or a paid Anthropic-key shim). Update `freedius.yaml` to point `provider: custom` at it.
- Start `./freedius` with the env var set (e.g. `CUSTOM_API_KEY=sk-xxx ./freedius`)
- Run a real `claude-code` session with `ANTHROPIC_BASE_URL=http://127.0.0.1:8080` (or whichever env var Claude Code uses — verify per PRD unknown #2)
- Confirm: tool calls work, streaming responses work, multi-turn works
- `curl -X POST http://127.0.0.1:8080 -H "Content-Type: application/json" -d '{"model":"<your-model>","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}'` returns 200 with a valid Anthropic-format response
- Stop the upstream mid-conversation; confirm freedius logs the disconnect at Debug and the next request works fine
- Point freedius at a non-existent upstream (e.g. `http://127.0.0.1:1`); `curl` returns 502 with `{"error":"upstream_unreachable", ...}`

**Implementation Note**: After completing this phase and all automated verification passes, pause here for manual confirmation that the manual testing was successful before proceeding to Phase 3.

---

## Phase 3: NIM adapter + translation module

### Overview

Implement the second adapter: NIM, which translates Anthropic `/v1/messages` request bodies to OpenAI `/v1/chat/completions` format on the way in, and translates the OpenAI SSE stream back to Anthropic SSE format on the way out. The translation logic lives in pure bytes-in/bytes-out functions under `proxy/translate/` so it's TDD-friendly; the adapter's `Handle` method is the only place HTTP I/O and `http.ResponseController` exist.

This is the largest phase (~60% of the slice effort) and the north-star work. It builds on Phase 2's wiring (same `Provider` interface, same `Registry` mechanism, same `main.go` startup pattern).

### Changes Required:

#### 1. Create the `proxy/translate/` package

**File**: `proxy/translate/anthropic_openai.go` (new)

**Intent**: Pure functions that translate between Anthropic and OpenAI request/response shapes. No I/O, no `http.ResponseWriter`, no `*http.Client`. Just bytes in, bytes out.

**Contract**: Three top-level functions plus supporting types:
- `TranslateRequest(anthropicBody []byte, targetModel string) ([]byte, error)` — translates the Anthropic request body to OpenAI format. Returns the rewritten JSON body ready to POST to NIM.
- `TranslateStream(upstream io.Reader, downstream io.Writer, flush func() error) error` — reads OpenAI SSE chunks from `upstream`, writes Anthropic SSE chunks to `downstream`. Calls `flush` after every Anthropic event. Returns `nil` on clean `[DONE]`, an error on upstream failure or protocol violation.
- An internal `anthropicEmitter` type (state machine for the SSE translation). Constructed with `newAnthropicEmitter()`; exposes `consumeOpenAILine(line []byte) ([][]byte, error)` returning 0+ Anthropic event bytes to emit.

The full translation rules and edge cases are documented in `research.md:3.3` (request side) and `research.md:3.4` (response side). The implementer reads those sections before writing the code; the plan does not duplicate the rules here.

The two SSE footguns (json.Encoder newline, bufio.Scanner 64KB cap) are called out in the Critical Implementation Details section above and in `research.md:3.5`. The implementer MUST use `json.Marshal` (not `json.NewEncoder.Encode`) and `bufio.Reader.ReadBytes('\n')` (not `bufio.Scanner`).

#### 2. Add request/response types

**File**: `proxy/translate/types.go` (new)

**Intent**: Define the Go structs for the OpenAI request body, OpenAI SSE chunk, and Anthropic SSE events. These are internal to the package — only the bytes cross the package boundary.

**Contract**:
- `openAIRequest` — the body shape sent to NIM (`model`, `messages`, `max_tokens`, `tools`, `tool_choice`, `temperature`, `top_p`, `stop`, `stream`, `stream_options`)
- `openAIChunk` — the SSE chunk shape from NIM (`id`, `object`, `created`, `model`, `choices`, `usage`)
- `openAIChoice` — the per-choice struct (`index`, `delta`, `finish_reason`)
- `openAIDelta` — the delta object (`role`, `content`, `tool_calls`)
- `openAIToolCall` — a single tool call in a delta (`index`, `id`, `type`, `function`)
- `openAIToolCallFunction` — the function object (`name`, `arguments`)
- `openAIUsage` — usage chunk payload (`prompt_tokens`, `completion_tokens`, `total_tokens`)
- Anthropic SSE event payloads (one struct per event type: `messageStartEvent`, `contentBlockStartEvent`, `contentBlockDeltaEvent`, `contentBlockStopEvent`, `messageDeltaEvent`, `messageStopEvent`, `pingEvent`, `errorEvent`) — these are the targets the translator emits

All fields are lowercase exported (Go convention); json tags match the wire format. Pointer fields are used for `omitempty`-equivalent semantics where NIM omits values.

#### 3. Write golden-file tests for the translation

**File**: `proxy/translate/anthropic_openai_test.go` (new)

**Intent**: Verify the pure functions produce byte-exact output for canonical input. This is the test suite that makes the translation TDD-friendly under `/10x-tdd`.

**Contract**:
- **Request tests** (`TestTranslateRequest`): table-driven, one case per Anthropic input fixture. Each case has an input `[]byte` (or filename of a `testdata/` fixture) and an expected output `[]byte`. Cover at minimum: text-only, single tool use, parallel tool calls, system-as-string, system-as-blocks, stop_sequences, tool_choice variants, image content block (forwarded verbatim per Round-3 #10 decision).
- **Stream tests** (`TestTranslateStream`): pipeline test with `strings.Reader` (upstream) and `bytes.Buffer` (downstream). A flush-counter function asserts the flush call count equals the emitted event count. Cover at minimum: text-only stream, single tool-call stream, parallel tool-call stream, error mid-stream, `[DONE]` only (no content), usage chunk followed by `[DONE]`, `finish_reason: "content_filter"`, content filter with no prior content.
- **Edge-case tests**: assert the output never contains `\n\n\n` (the json.Encoder newline trap), every event followed by a flush, no `event:` / `data:` line splits, `index` is monotonically increasing, `message_id` is present and stable.

Fixtures live in `proxy/translate/testdata/` as `.json` and `.sse` files. Recorded SSE streams (from real NIM, or hand-crafted to match the spec) for the stream tests.

#### 4. Implement `NIMAdapter`

**File**: `proxy/nim.go` (new)

**Intent**: The HTTP I/O half of the NIM integration. Reads the already-buffered body, calls `translate.TranslateRequest`, POSTs to NIM with the right headers, and either forwards an upstream error (4xx/5xx) verbatim or streams the SSE translation back to the client.

**Contract**:
```go
package proxy

type NIMAdapter struct {
    baseURL string
    apiKey  string
    client  *http.Client
    logger  *slog.Logger
}

func NewNIMAdapter(logger *slog.Logger) *NIMAdapter {
    // baseURL: env NIM_BASE_URL or default "https://integrate.api.nvidia.com"
    // apiKey: required; read at construction, fail-fast in main.go
    // client: &http.Client{} with no Timeout (per Round-2 #5)
}

func (a *NIMAdapter) Handle(w http.ResponseWriter, r *http.Request, m config.Model, body []byte) error {
    upstreamBody, err := translate.TranslateRequest(body, m.Model)
    if err != nil {
        return fmt.Errorf("nim adapter: translate request: %w", err)
    }
    req, err := http.NewRequestWithContext(r.Context(), http.MethodPost,
        a.baseURL+"/v1/chat/completions", bytes.NewReader(upstreamBody))
    if err != nil { return err }
    req.Header.Set("Authorization", "Bearer "+a.apiKey)
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Accept", "text/event-stream")
    resp, err := a.client.Do(req)
    if err != nil { return err }  // freediusErrorHandler routes to 502
    defer resp.Body.Close()
    if resp.StatusCode >= 400 {
        return forwardUpstreamError(w, resp)
    }
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")
    w.WriteHeader(http.StatusOK)
    rc := http.NewResponseController(w)
    return translate.TranslateStream(resp.Body, w, rc.Flush)
}
```

The `defer resp.Body.Close()` is critical — without it, the upstream connection leaks on every request.

#### 5. Write NIM adapter tests

**File**: `proxy/nim_test.go` (new)

**Intent**: End-to-end test of the NIM adapter against a `httptest.NewServer` mock that returns canned OpenAI-format SSE.

**Contract**: Table-driven. Cases:
- "non-streaming text response" — mock returns a single OpenAI JSON object (not SSE); the adapter should detect non-SSE content type and forward the body verbatim (or emit a single Anthropic message — decide in implementation; document the choice)
- "streaming text response" — mock returns Anthropic-style SSE chunks (the NIM format); client receives translated Anthropic SSE
- "streaming tool-use response" — mock returns OpenAI SSE with a single tool call; client receives Anthropic SSE with `content_block_start` (tool_use) + `content_block_delta` (input_json_delta) + `content_block_stop`
- "parallel tool calls" — mock returns two tool calls in one chunk; client receives two `content_block_start` events with distinct indices
- "upstream 401" — client receives 401 with upstream body verbatim
- "upstream 429" — client receives 429 with upstream body verbatim
- "transport error" — mock closes the connection mid-stream; client receives whatever the adapter emitted, then EOF (translator returns nil on clean EOF)
- "client disconnect" — adapter returns `context.Canceled` (test by canceling the request context mid-stream and asserting no further writes)

#### 6. Wire the NIM adapter into `main.go`

**File**: `main.go`

**Intent**: Build a `NIMAdapter` at startup, register it in the `Registry`. Read `NIM_API_KEY` at startup, fail-fast if missing when the config references `provider: nim`.

**Contract**:
- Add eager env-var check after `cfg, err := config.Load(cfgPath)`:
  ```go
  if configUsesProvider(cfg, "nim") && os.Getenv("NIM_API_KEY") == "" {
      return failf("freedius: NIM_API_KEY env var required (config references provider=nim)")
  }
  ```
  The `configUsesProvider` helper iterates `cfg.Models` and returns true if any model has the given provider. (Could be a method on `*Config` — implementation choice.)
- The `Registry` gains a `nim` entry: `map[string]proxy.Provider{"custom": proxy.NewCustomAdapter(logger), "nim": proxy.NewNIMAdapter(logger)}`.
- The startup order is: load config → check env vars for referenced providers → build registry → build dispatcher → start server.

#### 7. Update dispatcher integration tests

**File**: `proxy/proxy_test.go` (additions)

**Intent**: End-to-end through the dispatcher → registry → NIM adapter → mock NIM upstream.

**Contract**: New test case in `TestServeHTTPWithAdapter`: config has a `nim` model mapping; `POST /v1/messages` with a `nim`-mapped model triggers a request to the mock NIM upstream; the response is translated and returned. Assert the request to the mock NIM has the OpenAI-format body (e.g. `model` field matches the configured target, `messages` is the translated array, `stream` is set correctly).

### Success Criteria:

#### Automated Verification:

- `make ci` is green
- `go test -race -cover ./proxy/translate/...` — coverage ≥ 90% (the pure functions are easy to cover with golden files; the goal is byte-exact assertions)
- `go test -race -cover ./proxy/...` — coverage ≥ 85% (the adapter I/O is the harder part)
- New tests cover all 7+ cases in Change #5 and the 3+ cases in Change #7
- `go vet ./...`, `go build ./...` — clean
- `govulncheck ./...` — no new vulnerabilities

#### Manual Verification:

- Set `NIM_API_KEY=<your-key>` in the env
- Start `./freedius` with a config that maps a Claude Code model (e.g. `claude-opus-4`) to `provider: nim` with `model: meta/llama-3.1-70b-instruct`
- `curl -X POST http://127.0.0.1:8080 -H "Content-Type: application/json" -d '{"model":"claude-opus-4","max_tokens":50,"stream":true,"messages":[{"role":"user","content":"Say hi in 5 words or fewer"}]}'` returns 200 with `Content-Type: text/event-stream` and a valid Anthropic-format SSE stream
- Pipe the response to a JSON-lines parser: confirm `event: message_start`, `event: content_block_start`, one or more `event: content_block_delta` with `text_delta`, `event: content_block_stop`, `event: message_delta` with `stop_reason: "end_turn"`, `event: message_stop`
- Run a real `claude-code` session routed through freedius to NIM; confirm a tool-using task (e.g. "list the files in the current directory") completes successfully
- Run a multi-turn conversation; confirm `input_tokens` is reported in the `message_start` usage (may be 0 if NIM's usage chunk arrives late — see Round-3 #10 known limitation) and `output_tokens` in the `message_delta` usage
- Set `NIM_API_KEY` to an invalid value; restart freedius; `curl` returns 401 with NIM's body verbatim
- Point NIM at a non-existent URL via `NIM_BASE_URL=http://127.0.0.1:1`; `curl` returns 502 with `{"error":"upstream_unreachable", ...}`

**Implementation Note**: After completing this phase and all automated verification passes, S-01 is done. The next step is `/10x-impl-review first-call-routed` to audit the implementation against this plan, then `/10x-archive` to close the change. The `context/foundation/lessons.md` file should be created (if it doesn't exist) with at least one entry: the json.Encoder newline / bufio.Scanner 64KB SSE footguns, captured for future reference.

---

## Testing Strategy

### Unit Tests

- `config/config_test.go` — extends to cover the new `BaseURL` and `APIKeyEnv` validation paths. Existing F-01 cases remain green.
- `proxy/proxy_test.go` — extends `newTestDispatcher` to construct a registry. Adds the "provider not registered" 500 case. Adds the end-to-end dispatcher→adapter→mock-upstream test from Phase 3.
- `proxy/custom_test.go` — new, 7 cases as listed in Phase 2 Change #4.
- `proxy/nim_test.go` — new, 8+ cases as listed in Phase 3 Change #5.
- `proxy/translate/anthropic_openai_test.go` — new, golden-file tests for `TranslateRequest` and `TranslateStream` as listed in Phase 3 Change #3.

### Integration Tests

- None in CI (per Round-3 #9 decision). The `httptest.NewServer`-based tests *are* the integration tests — they exercise the full path from the dispatcher through the adapter to a mock upstream.
- Manual smoke tests against real NIM and a real custom Anthropic-compatible shim are the "true" integration tests, deferred to the user per the per-phase Manual Verification sections.

### Manual Testing Steps

Each phase has a per-phase Manual Verification section that lists the specific `curl` commands and real `claude-code` sessions the user runs to confirm the slice works end-to-end. The Phase 3 manual verification is the most rigorous: it requires a working NIM API key and a real Claude Code session.

## Performance Considerations

- **NFR-Latency ("imperceptible overhead")**: the inline single-goroutine SSE translation adds at most one `bufio.ReadBytes` + one `json.Marshal` + one `fmt.Fprintf` per chunk. Per-chunk overhead is in the low microseconds; the dominant latency is the upstream LLM call. For the custom adapter, `httputil.ReverseProxy` adds effectively zero overhead (it's a `io.Copy` with header rewriting).
- **NFR-Multi-agent**: `*http.Client` and `*httputil.ReverseProxy` are both safe for concurrent use. Adapters are constructed once at startup and shared. Per-request state (the `bufio.Reader`, the `anthropicEmitter` instance) is allocated locally per `Handle` call. No locks, no atomics. Research `research.md:4`.
- **NFR-Resource-footprint (sub-50MB idle, negligible CPU)**: the adapters add maybe 1KB of static memory each. The `bufio.Reader` allocated per request is freed when `Handle` returns. The `httputil.ReverseProxy` per-request construction in `CustomAdapter.Handle` (see Phase 2 design note) is intentional — a `*ReverseProxy` is ~100 bytes, freed when the goroutine returns.
- **NFR-Privacy (no body logging)**: existing F-01 behavior carries over. The NIM adapter does not log request or response bodies. The custom adapter does not log anything beyond the standard dispatcher Debug line.
- **NFR-Error-handling (provider errors forwarded visibly)**: research `research.md:5` confirms verbatim upstream passthrough. The dispatcher adds 502 only for transport-level failures (when the upstream is unreachable), not for upstream 4xx/5xx.

## References

- Research: `context/changes/first-call-routed/research.md` (architecture, API contracts, translation rules, Go patterns)
- F-01 plan: `context/changes/proxy-skeleton/plan.md` (the foundation this slice builds on)
- F-01 review: `context/changes/proxy-skeleton/reviews/impl-review.md` (hardening S-01 inherits)
- PRD: `context/foundation/prd.md` (US-01, FR-001, FR-002, FR-006, FR-009, NFR-Latency, NFR-Error-handling, NFR-Privacy)
- Roadmap: `context/foundation/roadmap.md` (S-01 row, North Star section)
- AGENTS.md: `/home/pawel/code/freedius/AGENTS.md` (no comments, `gofumpt`, env-var config, test conventions, Go 1.22+ patterns)
- Go stdlib docs (Context7 query for `httputil.ReverseProxy`): `https://pkg.go.dev/net/http/httputil#ReverseProxy`
- Anthropic Messages API: stream format spec is documented in the Anthropic SDK test fixtures (`github.com/anthropics/anthropic-sdk-typescript/tests/lib/fixtures/`)
- OpenAI Chat Completions API: streaming reference is at `developers.openai.com/api/docs/api-reference/chat/create`
- goccy/go-yaml docs: `https://github.com/goccy/go-yaml` (for any config-load edge cases that surface)

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles. See `references/progress-format.md`.

### Phase 1: Schema + Provider registry

#### Automated

- [x] 1.1 Add `BaseURL` and `APIKeyEnv` fields to `config.Model` and extend the per-model validation loop (`config/config.go`) — ef5d083
- [x] 1.2 Add `config.example.yaml` update with one `nim` mapping (env-var only) and one `custom` mapping (URL + env-var) — ef5d083
- [x] 1.3 Add `proxy/provider.go` with `Provider` interface, `Registry` type, `NewRegistry`, `Lookup` — ef5d083
- [x] 1.4 Update `Dispatcher` struct + `NewDispatcher` to take a `*Registry`; replace 501 stub with registry lookup (proxy/proxy.go) — ef5d083
- [x] 1.5 Update `main.go` to pass a `nil` registry for now (Phase 2 replaces it) — ef5d083
- [x] 1.6 Add config test cases: `provider=custom` without `base_url` (error), invalid `base_url` scheme (error), invalid `api_key_env` (error), valid nim+api_key_env (passes), valid custom+both fields (passes) — ef5d083
- [x] 1.7 Update `newTestDispatcher` to construct a registry; add "provider not registered" 500 test case — ef5d083
- [x] 1.8 Run `make ci` — all green, coverage ≥ 90% config / ≥ 85% proxy — ef5d083

#### Manual

- [x] 1.9 Verify the registry dispatches and unknown-provider 500 via `curl` per Phase 1 Manual Verification — ef5d083

### Phase 2: Custom passthrough adapter

#### Automated

- [x] 2.1 Add `proxy/errors.go` with `forwardUpstreamError` and `freediusErrorHandler` (used by both adapters) — f5e98f9
- [x] 2.2 Add `proxy/custom.go` with `CustomAdapter`, `NewCustomAdapter`, `Handle` — f5e98f9
- [x] 2.3 Add `proxy/custom_test.go` with the 7 cases from Phase 2 Change #4 — f5e98f9
- [x] 2.4 Update `main.go` to register the custom adapter in the registry — f5e98f9
- [x] 2.5 Add dispatcher-level integration test exercising custom adapter end-to-end (proxy/proxy_test.go) — f5e98f9
- [x] 2.6 Run `make ci` — all green, coverage ≥ 85% proxy — f5e98f9

#### Manual

- [x] 2.7 Verify custom passthrough end-to-end with a real Anthropic-compatible shim per Phase 2 Manual Verification — f5e98f9
- [x] 2.8 Verify error forwarding (upstream 401/500 reaches Claude Code verbatim) — f5e98f9
- [x] 2.9 Verify client-disconnect handling (no 502 written, debug log emitted) — f5e98f9

### Phase 3: NIM adapter + translation module

#### Automated

- [x] 3.1 Add `proxy/translate/types.go` with OpenAI + Anthropic request/response structs and json tags — c7e1f74
- [x] 3.2 Add `proxy/translate/anthropic_openai.go` with `TranslateRequest` (Anthropic→OpenAI body) — c7e1f74
- [x] 3.3 Add `proxy/translate/anthropic_openai.go` with `TranslateStream` + `anthropicEmitter` (OpenAI SSE→Anthropic SSE) — c7e1f74
- [x] 3.4 Add `proxy/translate/anthropic_openai_test.go` with golden-file tests for `TranslateRequest` — c7e1f74
- [x] 3.5 Add `proxy/translate/anthropic_openai_test.go` with golden-file tests for `TranslateStream` (8+ cases) — c7e1f74
- [ ] 3.6 Add `proxy/translate/testdata/` with recorded SSE fixtures (text-only, single tool, parallel tools, error mid-stream, etc.) — commit sha
- [x] 3.7 Add `proxy/nim.go` with `NIMAdapter`, `NewNIMAdapter`, `Handle` — c7e1f74
- [x] 3.8 Add `proxy/nim_test.go` with the 8+ cases from Phase 3 Change #5 — c7e1f74
- [x] 3.9 Update `main.go` with the `NIM_API_KEY` eager check + NIM adapter registration — c7e1f74
- [x] 3.10 Add dispatcher-level integration test exercising NIM adapter end-to-end (proxy/proxy_test.go) — c7e1f74
- [x] 3.11 Run `make ci` — all green, coverage ≥ 90% translate / ≥ 85% proxy — c7e1f74
- [x] 3.12 Run `govulncheck ./...` — no new vulnerabilities — c7e1f74

#### Manual

- [x] 3.13 Verify NIM translation end-to-end with a real NIM API key per Phase 3 Manual Verification — c7e1f74
- [x] 3.14 Verify a real `claude-code` session through freedius to NIM completes a tool-using task — c7e1f74
- [x] 3.15 Verify multi-turn conversation reports `input_tokens` and `output_tokens` correctly — c7e1f74
- [x] 3.16 Verify invalid NIM_API_KEY returns 401 with NIM's body verbatim — c7e1f74
- [x] 3.17 Create `context/foundation/lessons.md` with the json.Encoder newline / bufio.Scanner 64KB SSE footguns — c7e1f74
