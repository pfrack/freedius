# Error Code Differentiation — Implementation Plan

## Overview

Differentiate HTTP status codes across the proxy's error paths so Claude Code can distinguish transient overload (retryable, 529) from permanent misconfiguration (retry won't help, 500), and preserve original upstream status codes for unrecognized 5xx responses instead of collapsing them to 529.

## Current State Analysis

Every adapter error path converges on HTTP 529 `overloaded_error`, regardless of cause:

| Error condition | File:line | Current status | Retryable? |
|---|---|---|---|
| Missing BaseURL (adapter pre-flight) | `openai_compat.go:65-67`, `anthropic_compat.go:45-47` | 529 | No — config bug |
| Missing API key (adapter pre-flight) | `openai_compat.go:69-74`, `anthropic_compat.go:49-54` | 529 | No — config bug |
| Translation failure (adapter pre-flight) | `openai_compat.go:77-79` | 529 | No — config bug |
| Pre-send hook failure (adapter pre-flight) | `openai_compat.go:82-84` | 529 | No — config bug |
| URL parse failure (adapter pre-flight) | `anthropic_compat.go:57-63`, `mix.go:61-62` | 529 | No — config bug |
| Client.Do transport error | `openai_compat.go:103-106` | 529 | Yes |
| Upstream 502, 504, 505 (translateUpstreamError default) | `errors.go:83-86` | 529 | Unknown |
| ReverseProxy transport error (DNS, TLS, connection refused) | `errors.go:145` | 529 | Varies |

The dispatcher's single catch-all at `proxy/proxy.go:231` lumped all adapter errors together when S-05 replaced the S-04 502 path with 529 for Claude Code retry compatibility.

### Key Discoveries:

- All adapter errors land on `proxy/proxy.go:231` — the **only** classification point upstream of the dispatcher is whether `!ww.wroteHeader`. (`proxy/proxy.go:219`)
- `translateUpstreamError`'s default case (`errors.go:83-86`) was set to 529 when only 503/529 were in the map. With the `case resp.StatusCode == 503 || resp.StatusCode == 529` above it, the default catches true unknowns (502, 504, 505). (`errors.go:64-87`)
- `TestTranslateUpstreamError` at `errors_test.go:155-156` already has a "502" test case that expects 529 — this will need updating once 502 passes through. (`errors_test.go:155-156`)
- MixAdapter delegates pre-flight checks entirely to sub-adapters, except for its own `url.Parse(m.BaseURL)` at `mix.go:61` which also needs wrapping. (`proxy/mix.go:44-70`)
- The Adapter Return Contract (`lessons.md:33-43`) is not affected — all changes happen on the pre-WriteHeader path (returned errors), never after headers are written. (`context/foundation/lessons.md:33-43`)

## Desired End State

After this plan lands:

1. Adapter config errors (missing API key, bad BaseURL, translation failure, URL parse, pre-send hook failure) → **500 Internal Server Error** with appropriate Anthropic `error.type` (`authentication_error` or `invalid_request_error`) and **no retry headers**.
2. Adapter transport errors (connection refused, timeout, DNS failure from `client.Do`) → **529 overloaded_error** with `retryAfter: 15` and `x-should-retry: true` (unchanged from today).
3. Unrecognized upstream 5xx in `translateUpstreamError` → **original upstream status code** preserved (502→502, 504→504, 505→505), `error.type: api_error`, `retryAfter: 15`.
4. ReverseProxy permanent transport errors (DNS resolution failure, TLS certificate/handshake failure) → **502 Bad Gateway**, `error.type: api_error`, no retry headers.
5. ReverseProxy transient transport errors (connection refused, reset, timeout) → **529 overloaded_error**, retry headers (unchanged from today).
6. Error envelope format remains `writeAnthropicError` (Anthropic shape: `{"type":"error","error":{"type":"...","message":"..."}}`) across all adapter/upstream paths — no format drift.

### How to verify:

- `go test ./...` — all tests pass, including new tests for:
  - Config errors → 500 with correct `error.type`
  - Transport errors → 529 (unchanged)
  - `translateUpstreamError` 502 → 502 (not 529)
  - DNS error → 502, connection refused → 529
- `go vet ./...` — clean
- `go build -o freedius .` — produces a static binary
- Manual: Start freedius with a missing API key; Claude Code shows 500 with `authentication_error` (not 529 with `overloaded_error`). Start freedius with a bad BaseURL; Claude Code shows 500 with `invalid_request_error`. Set upstream unreachable; Claude Code still retries on 529 `overloaded_error`.

## What We're NOT Doing

- **Changing error envelope format** — both `writeErrorJSON` and `writeAnthropicError` remain as-is; this plan only adjusts status codes and error types within the Anthropic envelope.
- **Differentiating transport-error subtypes beyond permanent/transient** — we don't distinguish "connection refused" from "connection reset" from "timeout" — all are 529.
- **Adding a `detail` field to the Anthropic error envelope** — verbose-errors mode already logs the full error string at Debug level; no change to the client-facing envelope.
- **Changing the 404 model-not-found path** — the dispatcher's `no_match` 404 is correct and tested. Not touched.

## Implementation Approach

Introduce a lightweight `configError` sentinel type that wraps adapter pre-flight errors with an Anthropic `error.type`. The dispatcher at `proxy.go:231` checks for this type via `errors.As` to decide 500 (config) vs 529 (transport). Two other code sites (`translateUpstreamError` default, `freediusErrorHandler`) get targeted status-code changes without the sentinel pattern — they self-classify based on upstream response codes or Go error types.

## Phase 1: Sentinel Error Type + Adapter Pre-Flight Wrapping

### Overview

Define the `configError` type in `proxy/errors.go` and wrap all adapter pre-flight errors (missing API key, missing BaseURL, translation failure, pre-send hook failure, URL parse failure) with it. Transport errors from `client.Do` remain unwrapped.

### Changes Required:

#### 1. Define `configError` type

**File**: `proxy/errors.go`

**Intent**: Add an unexported `configError` struct that wraps an underlying error with a machine-readable Anthropic error type. The dispatcher detects it with `errors.As`. Must implement `Error()` and `Unwrap()` for Go error-chain convention.

**Contract**: Unported struct `configError` with two fields (`err error`, `errType string`). Implements `Error() string` (returns `e.err.Error()`), `Unwrap() error` (returns `e.err`). Use `errType` values per Anthropic conventions: `"authentication_error"` for missing credentials, `"invalid_request_error"` for bad configuration.

#### 2. Wrap OpenAI adapter pre-flight errors

**File**: `proxy/openai_compat.go`

**Intent**: Each pre-flight error return in `OpenAICompatibleAdapter.Handle` (lines 65-97) is wrapped in a `configError` with the appropriate `errType`. Transport errors from `client.Do` (line 103-106) are NOT wrapped — they remain plain errors for the dispatcher to classify as transport failures.

**Contract**: Five error sites changed:
- `openai_compat.go:66` — `return &configError{err: fmt.Errorf(...), errType: "invalid_request_error"}` (missing BaseURL)
- `openai_compat.go:70-74` — `return &configError{err: fmt.Errorf(...), errType: "authentication_error"}` (missing API key)
- `openai_compat.go:77-78` — `return &configError{err: fmt.Errorf(...), errType: "invalid_request_error"}` (translate failure)
- `openai_compat.go:82-83` — `return &configError{err: fmt.Errorf(...), errType: "invalid_request_error"}` (pre-send hook failure)
- `openai_compat.go:97` — `return &configError{err: fmt.Errorf(...), errType: "invalid_request_error"}` (URL parse / request build failure)
- `openai_compat.go:105` — unchanged (plain `fmt.Errorf` — transport error)

#### 3. Wrap Anthropic adapter pre-flight errors

**File**: `proxy/anthropic_compat.go`

**Intent**: Same pattern as OpenAI adapter — wrap pre-flight errors but keep transport-path errors bare (Anthropic adapter's transport errors go through `freediusErrorHandler`, not this return path, so there's nothing to leave unwrapped here).

**Contract**: Three error sites changed:
- `anthropic_compat.go:46` — `return &configError{err: fmt.Errorf(...), errType: "invalid_request_error"}` (missing BaseURL)
- `anthropic_compat.go:50-54` — `return &configError{err: fmt.Errorf(...), errType: "authentication_error"}` (missing API key)
- `anthropic_compat.go:58-62` — `return &configError{err: fmt.Errorf(...), errType: "invalid_request_error"}` (URL parse failure)

#### 4. Wrap MixAdapter URL parse error

**File**: `proxy/mix.go`

**Intent**: The `url.Parse(m.BaseURL)` failure at line 61-62 returns a plain error today. Wrap it as a config error.

**Contract**:
- `mix.go:62` — `return &configError{err: fmt.Errorf(...), errType: "invalid_request_error"}`

### Success Criteria:

#### Automated Verification:

- `go test ./proxy/...` — `TestOpenAICompat_MissingBaseURL_UsesOriginalProvider` still passes (error wraps `configError`, `Error()` returns same text)
- `go test ./proxy/...` — `TestAnthropicCompat_MissingBaseURL_UsesOriginalProvider` still passes
- `go test ./proxy/...` — `TestAdapter_ErrorTemplate_UsesOriginalProvider` still passes (table-driven tests check `err.Error()` which is forwarded)
- `go vet ./...` — clean
- `gofumpt -l proxy/` — no formatting issues

#### Manual Verification:

- None required for this phase (pure internal wrapping, behavior confirmed by existing tests + Phase 5 new tests).

---

## Phase 2: Dispatcher Classification + Log Level

### Overview

Modify the dispatcher's adapter-error branch (`proxy/proxy.go:219-232`) to check for `configError` via `errors.As`. Config errors get 500 + Warn log; transport errors keep 529 + Error log. The Anthropic error.message includes the underlying error text for operator debugging. No retry headers on config errors (`retryAfter=0`).

### Changes Required:

#### 1. Dispatcher adapter-error classification

**File**: `proxy/proxy.go`

**Intent**: Replace the single `writeAnthropicError(w, 529, "overloaded_error", "upstream provider not reachable", 15)` call at line 231 with a branch: `errors.As(err, &configError)` → 500 (Warn log, no retry headers), else → 529 (Error log, retry headers). The `configError.errType` drives the Anthropic error type. The existing post-WriteHeader error branch (lines 234-244) is unchanged.

**Contract**: At `proxy/proxy.go:219-246`, replace the `!ww.wroteHeader` block (lines 220-232) with:

```
if !ww.wroteHeader {
    var ce *configError
    if errors.As(err, &ce) {
        d.Logger.Warn("adapter config error", ...)
        writeAnthropicError(w, 500, ce.errType, err.Error(), 0)
    } else {
        d.Logger.Error("adapter transport error", ...)
        writeAnthropicError(w, 529, "overloaded_error", "upstream provider not reachable", 15)
    }
}
```

Key details:
- Log key `"adapter config error"` / `"adapter transport error"` replaces `"adapter failed"` — improves log observability.
- `err.Error()` is used as the Anthropic error `message` field (not a hardcoded string) so the operator sees the config error text in Claude Code's error display.
- `retryAfter=0` suppresses `retry-after` and `x-should-retry` headers for config errors (Claude Code won't retry).

### Success Criteria:

#### Automated Verification:

- `go test ./proxy/...` — `TestDispatcher_AdapterError_TranslatedAsAnthropicOverloaded` still passes on the transport-error sub-case (plain error → 529)
- `go test ./proxy/...` — new test passes: config error → 500 with correct error.type and no retry headers (added in Phase 5)
- `go vet ./...` — clean

#### Manual Verification:

- None required for this phase (tests added in Phase 5 confirm behavior).

---

## Phase 3: `translateUpstreamError` Default Pass-Through

### Overview

Change the `default` case in `translateUpstreamError`'s switch statement to pass through the original upstream HTTP status code instead of collapsing to 529. The `error.type` becomes `"api_error"` with `retryAfter: 15`.

### Changes Required:

#### 1. Default case status code pass-through

**File**: `proxy/errors.go`

**Intent**: Lines 83-86 currently map all unrecognized 5xx to 529. Change `status = 529` to `status = resp.StatusCode` so the original upstream status (502, 504, 505, etc.) is preserved. The 503/529 case above (line 69-72) is unchanged — those explicitly mean overload.

**Contract**: In `errors.go:83-86`, change:
```
default: // other 5xx
    status = resp.StatusCode  // was: 529
    errType = "api_error"
    retryAfter = 15
```

The `503/529` case at line 69-72 remains unchanged — those codes still map to 529 `overloaded_error`.

### Success Criteria:

#### Automated Verification:

- `go test ./proxy/...` — `TestTranslateUpstreamError` "502" case now expects 502 (not 529) — test updated in Phase 5
- `go test ./proxy/...` — new "504" test case passes: 504→504, error.type "api_error", retryAfter 15
- All existing `TestTranslateUpstreamError` cases (429, 503, 529, 500, 401, 403, 404) still pass unchanged
- `go vet ./...` — clean

#### Manual Verification:

- None required for this phase (unit test coverage sufficient).

---

## Phase 4: `freediusErrorHandler` DNS/TLS Detection

### Overview

Add permanent transport error detection to `freediusErrorHandler`. DNS resolution failures and TLS certificate/handshake errors emit 502 (no retry); all other transport errors keep 529 (retry). Detection uses `errors.As` for `*net.DNSError` and a helper for TLS error chain inspection.

### Changes Required:

#### 1. Permanent transport error detection + 502 branch

**File**: `proxy/errors.go`

**Intent**: After the existing `freediusErrorHandler` log lines (lines 131-144) and before the `writeAnthropicError` call (line 145), insert a branch: if the error is a permanent transport failure, write 502 via `writeAnthropicError` with no retry headers; otherwise keep the existing 529 path.

**Contract**: In `freediusErrorHandler` (lines 116-148), replace the single `writeAnthropicError(w, 529, ...)` at line 145 with:

```
if isPermanentTransportError(err) {
    writeAnthropicError(w, 502, "api_error",
        "upstream not reachable", 0)
} else {
    writeAnthropicError(w, 529, "overloaded_error",
        "upstream not reachable", 15)
}
```

#### 2. New `isPermanentTransportError` helper

**File**: `proxy/errors.go`

**Intent**: Add an unexported helper that walks the error chain checking for permanent transport failures. DNS resolution failure (`*net.DNSError`) and TLS certificate/handshake errors are permanent; connection refused, connection reset, and I/O timeouts are transient.

**Contract**: Function signature `func isPermanentTransportError(err error) bool`. Uses `errors.As(err, &net.DNSError{})` for DNS failures, and walks the error chain for TLS-level errors (`crypto/tls` and `crypto/x509` types). Returns `false` if no permanent error is found — falling through to 529 with retry.

**Implementation Note**: Some TLS error types in Go's standard library are not easily matched with `errors.As` (they may be `*net.OpError` wrapping opaque `crypto/tls` errors). The implementation may use `errors.As` for `*net.DNSError`, and for the general TLS case, inspect the error chain for TLS/x509 errors. If no chain match, treat as transient. The test in Phase 5 will exercise real system errors for coverage.

### Success Criteria:

#### Automated Verification:

- `go test ./proxy/...` — new `TestFreediusErrorHandler_DNSError_Returns502` passes
- `go test ./proxy/...` — new `TestFreediusErrorHandler_ConnectionRefused_Returns529` passes
- `go test ./proxy/...` — existing `TestFreediusErrorHandler_TransportError` still passes (plain error → 529)
- `go test ./proxy/...` — existing `TestFreediusErrorHandler_ClientCanceled` still passes
- `go vet ./...` — clean

#### Manual Verification:

- Configure freedius with a non-existent hostname as BaseURL. Send a request. Verify Claude Code shows 502 (not 529) and does NOT show retry UI.
- Configure freedius with a live host that refuses connections. Verify Claude Code shows 529 with retry UI (retries should fail but the transient classification is correct).

---

## Phase 5: Tests

### Overview

Update existing tests for new status codes and add table-driven tests for dispatcher classification, translateUpstreamError pass-through, and transport error differentiation. No new test files — all tests added to existing `_test.go` files.

### Changes Required:

#### 1. Update `TestTranslateUpstreamError` "502" case

**File**: `proxy/errors_test.go`

**Intent**: The "502" test case at line 155-156 currently expects `wantStatus: 529`. Change to `wantStatus: 502` with `wantErrType: "api_error"`, `wantRetry: "15"`.

**Contract**: At `errors_test.go:155-156`, update:
```
{"502", 502, "", 502, "api_error", "15", false},
```

#### 2. Add "504" test case to `TestTranslateUpstreamError`

**File**: `proxy/errors_test.go`

**Intent**: Add a "504" entry to the table to cover the pass-through for other 5xx codes.

**Contract**: Add to the cases slice:
```
{"504", 504, "", 504, "api_error", "15", false},
```

#### 3. Add config vs transport dispatcher error tests

**File**: `proxy/adapter_errors_test.go`

**Intent**: Two new test cases extending `TestDispatcher_AdapterError_TranslatedAsAnthropicOverloaded`: one that returns a `configError`-wrapped error → 500, and one that returns a plain error → 529. The existing test (plain error → 529) continues to pass.

**Contract**: Two test functions (or sub-tests):
- `TestDispatcher_ConfigError_Returns500` — `preWriteHeaderErrProvider` returns `&configError{err: errors.New("missing API key"), errType: "authentication_error"}`. Assert: status 500, body `error.type` is `"authentication_error"`, body `message` contains `"missing API key"`, no `retry-after` header, no `x-should-retry` header.
- `TestDispatcher_ConfigError_InvalidRequest` — same pattern but `errType: "invalid_request_error"` with a bad-base-url error. Assert: status 500, body `error.type` is `"invalid_request_error"`, no retry headers.

The existing `TestDispatcher_AdapterError_TranslatedAsAnthropicOverloaded` (plain error → 529) continues to pass unchanged — it validates the transport-error path.

#### 4. Add DNS/TLS transport error tests

**File**: `proxy/adapter_errors_test.go` or `proxy/errors_test.go`

**Intent**: Two new tests for `freediusErrorHandler`: one with a real DNS error → 502, one with connection refused → 529. Use `*net.DNSError` from the standard library (construct with `&net.DNSError{Err: "no such host", Name: "nonexistent.example"}`) for the DNS case; use a plain `"connection refused"` error for the transient case.

**Contract**: Two test functions:
- `TestFreediusErrorHandler_DNSError_Returns502` — call `freediusErrorHandler` with a `*net.DNSError`. Assert: status 502, no `retry-after` header, no `x-should-retry` header, body `error.type` is `"api_error"`.
- `TestFreediusErrorHandler_ConnectionRefused_Returns529` — call `freediusErrorHandler` with `errors.New("dial tcp: connection refused")`. Assert: status 529, `retry-after: 15`, `x-should-retry: true`, body `error.type` is `"overloaded_error"`.

#### 5. Regression check for `TestFreediusErrorHandler_AnthropicFormat`

**File**: `proxy/adapter_errors_test.go:103-133`

**Intent**: `TestFreediusErrorHandler_AnthropicFormat` (line 103) uses `errors.New("dial tcp: connection refused")` which is transient and should still get 529. No changes needed — but verify the test assertion matches after Phase 4 lands.

#### 6. Regression check for `TestAnthropicCompat_TransportError_ReturnsAnthropicOverloaded`

**File**: `proxy/error_propagation_test.go:98-127`

**Intent**: This test passes a plain `errors.New("dial tcp 10.0.0.1:443: connection refused")` error — transient, 529 stays correct. No changes needed.

### Success Criteria:

#### Automated Verification:

- `go test ./proxy/...` — all existing tests pass; new tests pass
- `go test ./...` — full module test suite passes
- `go test -cover ./proxy/...` — coverage maintained or improved
- `go vet ./...` — clean
- `go build -o freedius .` — binary builds
- `gofumpt -l proxy/` — no formatting issues

#### Manual Verification:

1. Start freedius with a provider that has no API key set. Curl a request. Verify `curl -v` shows HTTP 500, body has `"type":"error"`, `"error":{"type":"authentication_error","message":"..."}` and no `retry-after` header.
2. Start freedius with a provider pointing to a non-existent hostname (`http://nonexistent.example:1`). Curl a request. Verify `curl -v` shows HTTP 502 (from DNS failure in `freediusErrorHandler` for Anthropic, or 500 from adapter `client.Do` error path for OpenAI — both appropriate).
3. Start freedius with a working provider on an unreachable port (connection refused). Verify `curl -v` shows HTTP 529 with `retry-after: 15` and `x-should-retry: true`.
4. All existing freedius functionality continues to work — conversations, streaming, count_tokens, tool use — no regressions.

---

## Testing Strategy

### Unit Tests:

- `proxy/errors_test.go` — `TestTranslateUpstreamError`: updated "502" case (502→502), new "504" case
- `proxy/adapter_errors_test.go` — `TestDispatcher_ConfigError_Returns500`, `TestDispatcher_ConfigError_InvalidRequest`
- `proxy/adapter_errors_test.go` — `TestFreediusErrorHandler_DNSError_Returns502`, `TestFreediusErrorHandler_ConnectionRefused_Returns529`
- `proxy/adapter_errors_test.go` — `TestAdapter_ErrorTemplate_UsesOriginalProvider` — verifies `configError.Error()` forwards to original error text (if assertion checks `err.Error()`)

### Regression:

- `TestDispatcher_AdapterError_TranslatedAsAnthropicOverloaded` — plain error → 529 (unchanged)
- `TestFreediusErrorHandler_TransportError` — plain error → 529 (unchanged)
- `TestFreediusErrorHandler_ClientCanceled` — no response (unchanged)
- `TestFreediusErrorHandler_AnthropicFormat` — connection refused → 529 (unchanged)
- `TestAnthropicCompat_TransportError_ReturnsAnthropicOverloaded` — plain error → 529 (unchanged)
- `TestOpenAICompat_Upstream429_ReturnsAnthropicFormat` — 429 pass-through (unchanged)
- `TestWriteAnthropicError` — body/header shape (unchanged)

### Manual Testing Steps:

1. Config: missing API key → 500 / `authentication_error` / no retry headers
2. Config: bad BaseURL → 500 / `invalid_request_error` / no retry headers
3. Transport: unreachable host (connection refused) → 529 / `overloaded_error` / retry headers
4. Transport: non-existent hostname (DNS failure) → 502 / `api_error` / no retry headers
5. Upstream: 502 response → 502 / `api_error` / retry headers
6. Upstream: 504 response → 504 / `api_error` / retry headers
7. All existing proxy paths (messages, count_tokens, streaming) work without regression

## Performance Considerations

- `errors.As(err, &ce)` is a single error-chain walk on the adapter error path — negligible overhead.
- `isPermanentTransportError` walks the error chain once for the already-uncommon transport-failure path — no hot-path impact.
- No new allocations in the streaming or success paths.

## References

- Frame brief: `context/changes/error-code-collapse/frame.md`
- Prior error unification: `context/archive/error-hardening/plan.md` (S-04 — intended 502 for adapter errors)
- Anthropic error format design: `context/archive/count-tokens-passthrough/plan.md` (S-05 — changed 502 to 529 for retry compat)
- Adapter Return Contract: `context/foundation/lessons.md:33-43`
- Source: `proxy/errors.go`, `proxy/proxy.go`, `proxy/openai_compat.go`, `proxy/anthropic_compat.go`, `proxy/mix.go`

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles.

### Phase 1: Sentinel Error Type + Adapter Pre-Flight Wrapping

#### Automated

- [x] 1.1 `go test ./proxy/...` — `TestOpenAICompat_MissingBaseURL_UsesOriginalProvider` passes — 8aadd62
- [x] 1.2 `go test ./proxy/...` — `TestAnthropicCompat_MissingBaseURL_UsesOriginalProvider` passes — 8aadd62
- [x] 1.3 `go test ./proxy/...` — `TestAdapter_ErrorTemplate_UsesOriginalProvider` passes — 8aadd62
- [x] 1.4 `go vet ./...` — clean — 8aadd62
- [x] 1.5 `gofumpt -l proxy/` — no formatting issues — 8aadd62

### Phase 2: Dispatcher Classification + Log Level

#### Automated

- [x] 2.1 `go test ./proxy/...` — `TestDispatcher_AdapterError_TranslatedAsAnthropicOverloaded` passes (transport → 529)
- [ ] 2.2 `go test ./proxy/...` — new `TestDispatcher_ConfigError_Returns500` passes (from Phase 5)
- [x] 2.3 `go vet ./...` — clean

### Phase 3: `translateUpstreamError` Default Pass-Through

#### Automated

- [ ] 3.1 `go test ./proxy/...` — `TestTranslateUpstreamError` "502" case passes (502→502)
- [ ] 3.2 `go test ./proxy/...` — new "504" case passes (504→504)
- [ ] 3.3 `go test ./proxy/...` — all other `TestTranslateUpstreamError` cases pass unchanged
- [ ] 3.4 `go vet ./...` — clean

### Phase 4: `freediusErrorHandler` DNS/TLS Detection

#### Automated

- [ ] 4.1 `go test ./proxy/...` — `TestFreediusErrorHandler_DNSError_Returns502` passes
- [ ] 4.2 `go test ./proxy/...` — `TestFreediusErrorHandler_ConnectionRefused_Returns529` passes
- [ ] 4.3 `go test ./proxy/...` — existing `TestFreediusErrorHandler_TransportError` passes (plain → 529)
- [ ] 4.4 `go test ./proxy/...` — existing `TestFreediusErrorHandler_ClientCanceled` passes
- [ ] 4.5 `go vet ./...` — clean

### Phase 5: Tests

#### Automated

- [ ] 5.1 `go test ./...` — full module test suite passes
- [ ] 5.2 `go test -cover ./proxy/...` — coverage maintained or improved
- [ ] 5.3 `go vet ./...` — clean
- [ ] 5.4 `go build -o freedius .` — binary builds
- [ ] 5.5 `gofumpt -l proxy/` — no formatting issues

#### Manual

- [ ] 5.6 missing API key → 500 `authentication_error`, no retry headers
- [ ] 5.7 bad BaseURL → 500 `invalid_request_error`, no retry headers
- [ ] 5.8 connection refused → 529 `overloaded_error`, retry headers
- [ ] 5.9 DNS failure → 502 `api_error`, no retry headers
- [ ] 5.10 upstream 502 response → 502 `api_error`, retry headers
- [ ] 5.11 upstream 504 response → 504 `api_error`, retry headers
- [ ] 5.12 all existing paths (messages, count_tokens, streaming) work without regression
