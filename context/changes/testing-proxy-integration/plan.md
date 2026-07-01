# Testing Proxy Integration — Implementation Plan

## Overview

Add integration + unit tests proving the proxy core correctly translates formats, routes by config, propagates errors, and doesn't leak API keys. Covers risks #1, #3, #4, #5, #6 from `context/foundation/test-plan.md` Phase 1. Includes implementing API key redaction in `translateUpstreamError` to fix the upstream error body snippet leakage vector identified by research.

## Current State Analysis

The proxy has ~255 test functions across 25 files. Research identified these specific gaps:

- **Risk #1 (translation format)**: No integration test verifies the full Anthropic response envelope through the adapter — existing tests use substring checks (`mix_test.go:91` checks `event: message_start` but not data payload structure).
- **Risk #3 (routing)**: No multi-provider end-to-end test with two real httptest upstreams verifying only the correct one receives the request.
- **Risk #4 (config validation)**: Nearly gap-free. Only missing: empty mapping key, empty behavior string.
- **Risk #5 (error propagation)**: No test for large/malformed upstream error bodies, no test for HTML error pages.
- **Risk #6 (API key leakage)**: Only a source-code comment check exists. `translateUpstreamError()` forwards upstream error body snippets (256 bytes) without API key redaction — `sanitizePrintable` only strips non-printable chars.

### Key Discoveries:

- `translateUpstreamError()` at `proxy/errors.go:71-111` reads 256 bytes of upstream body and includes it in the Anthropic error envelope message field — no redaction of API key patterns
- `forwardUpstreamError()` at `proxy/errors.go:29-38` is dead code — tested but never called by any adapter
- The adapter return contract (return nil after writing response) is enforced by `wroteHeaderResponseWriter` at `proxy/proxy.go:395-415` but not explicitly tested
- Config validation at `config/config.go:196-285` is thorough — 22 table-driven cases already exist
- Best reference test pattern: `proxy/nim_test.go:166` — `TestNIMAdapter_Upstream401_ReturnsAnthropicFormat`

## Desired End State

After this plan completes:
- Every risk in test-plan.md §2 Risk Response Guidance (#1, #3, #4, #5, #6) has at least one test proving the protection described
- `translateUpstreamError` redacts API key patterns from upstream error body snippets
- test-plan.md §6 cookbook patterns 6.4 and 6.5 are filled in
- `mage test` passes with race detection

## What We're NOT Doing

- Streaming edge-case tests (Risk #2) — deferred to test-plan.md Phase 2
- Quality gates / CI wiring — deferred to test-plan.md Phase 3
- Removing dead code `forwardUpstreamError()` — out of scope for a testing change
- Testing generated code (`providers_gen.go`, `adapters_gen.go`) — per test-plan.md §7
- Testing TUI visual layout — per test-plan.md §7
- Testing magefile build scripts — per test-plan.md §7

## Implementation Approach

Group tests by code area (3 sub-phases) rather than one-per-risk. Each sub-phase adds tests to existing test files following established patterns (httptest.NewServer for mock providers, table-driven tests, t.Setenv for API keys). Phase 3 additionally implements API key redaction — a code change, not just tests.

## Critical Implementation Details

- **Adapter return contract**: When writing tests that exercise error paths through adapters, the adapter returns nil after writing any response part. Tests that check `err != nil` from `adapter.Handle()` on error paths will fail — check the recorder status/body instead.
- **SSE framing**: Tests that verify streaming response format must use `json.Marshal` (not `json.NewEncoder`) per `context/foundation/lessons.md:3-7`. Existing tests already follow this.
- **Config post-rewrite names**: Per `context/foundation/lessons.md:15-19` (now stale — `custom` is a first-class provider), config validation error tests should use the provider name as-is in the YAML, not a rewritten name.

---

## Phase 1: Translation + Routing Tests

### Overview

Add integration tests proving: (a) Anthropic-format response (headers, body schema, error format) is returned regardless of upstream provider, and (b) configured model mappings route to the correct provider endpoint with clear errors on missing/ambiguous mappings.

### Changes Required:

#### 1. Anthropic response envelope verification

**File**: `proxy/openai_compat_test.go`

**Intent**: Add tests that verify the OpenAI-compat adapter returns a properly structured Anthropic response envelope — not just status codes and substrings, but decoded JSON with correct field names and types.

**Contract**: Test upstream returns a valid OpenAI chat completion response. Assert downstream response has: `Content-Type: application/json` or `text/event-stream`, Anthropic SSE event structure (`event: message_start`, `event: message_stop` with correct `data:` JSON payloads containing `type`, `message.role`, `stop_reason` fields).

#### 2. Anthropic-compat error passthrough

**File**: `proxy/anthropic_compat_test.go`

**Intent**: Add test for Anthropic-compat adapter receiving a non-200 response from upstream — verify the error is forwarded with correct status and body (passthrough, no translation needed for Anthropic-format upstreams).

**Contract**: Mock upstream returns 401 with Anthropic-format error body. Assert adapter returns 401 and body contains the upstream error. This tests the `freediusErrorHandler` path through the Anthropic-compat adapter, which is currently untested.

#### 3. Multi-provider routing

**File**: `proxy/proxy_test.go`

**Intent**: Add test with two real httptest upstreams (provider A and provider B) configured with different model mappings. Send a request matching provider A's mapping. Assert only provider A receives the request and provider B is untouched.

**Contract**: Two `httptest.NewServer` instances. Config with two providers, two mappings. Send request with model matching mapping A. Assert: recorder status 200, provider A handler was called, provider B handler was NOT called.

#### 4. Missing/ambiguous mapping edge cases

**File**: `proxy/proxy_test.go`

**Intent**: Add table-driven tests for edge cases in mapping resolution: model that matches no family and no exact mapping (404), model that matches multiple families (verify priority), empty model string (400 before routing).

**Contract**: `resolveMapping` at `proxy/proxy.go:94-111`. These extend existing `TestServeHTTP` table-driven tests.

### Success Criteria:

#### Automated Verification:

- `mage test` passes — all new and existing tests green
- New Anthropic envelope test decodes JSON and asserts field names/types (not substring)
- Multi-provider test asserts provider B handler was NOT called
- Race detector clean: `mage test` (already enables `-race`)

#### Manual Verification:

- Review test output for clear assertion messages on failure

---

## Phase 2: Error + Config Tests

### Overview

Add tests for error propagation edge cases (large bodies, HTML error pages, body format verification) and config validation gaps (empty mapping key, empty behavior).

### Changes Required:

#### 1. Large/malformed upstream error body

**File**: `proxy/errors_test.go`

**Intent**: Add test cases to the existing `TestTranslateUpstreamError` table for: (a) upstream body larger than 256 bytes (verify truncation), (b) upstream body with binary/non-printable characters (verify sanitization), (c) upstream body with empty string.

**Contract**: `translateUpstreamError()` at `proxy/errors.go:71-111` reads 256 bytes via `io.LimitReader`. The `sanitizePrintable` function strips non-printable chars. Test: upstream returns 1KB error body → `message` field in Anthropic error contains at most 256 bytes of printable chars.

#### 2. HTML error page handling

**File**: `proxy/errors_test.go`

**Intent**: Add test case for upstream returning `Content-Type: text/html` with an HTML error page (e.g., CDN error page). Verify `translateUpstreamError` produces a reasonable `message` field (HTML tags stripped or included as printable text).

**Contract**: Upstream returns 502 with `Content-Type: text/html` and body `<html><body><h1>Bad Gateway</h1></body></html>`. Assert the Anthropic error `message` field contains the HTML body text (since `sanitizePrintable` doesn't strip tags — they're printable chars).

#### 3. Error body format through full dispatcher chain

**File**: `proxy/adapter_errors_test.go`

**Intent**: Add test that verifies the complete error chain: adapter encounters upstream 500 → `translateUpstreamError` → Anthropic error envelope → client receives properly structured JSON. Assert on decoded JSON fields, not just status code.

**Contract**: Mock upstream returns 500 with `{"error":"internal failure"}`. Dispatcher returns Anthropic-format error with `error.type: "api_error"`, `message` containing "internal failure" snippet, `retry-after` and `x-should-retry` headers.

#### 4. Config validation edge cases

**File**: `config/config_test.go`

**Intent**: Add table-driven cases to `TestLoad` for: (a) mapping with empty key (`""`), (b) provider with empty behavior string (`""`).

**Contract**: Empty mapping key → validation error referencing the mapping. Empty behavior → `"invalid behavior"` error message.

### Success Criteria:

#### Automated Verification:

- `mage test` passes — all new and existing tests green
- Large body test asserts message field length ≤ 256
- HTML error test asserts message field contains upstream body content
- Config edge case tests assert specific error substrings
- Race detector clean

#### Manual Verification:

- Review that error messages in tests are descriptive (not just "error occurred")

---

## Phase 3: Privacy — API Key Redaction

### Overview

Implement API key redaction in `translateUpstreamError` and add runtime tests verifying API keys don't appear in logs, error responses, or response headers. This is the only sub-phase that modifies production code.

### Changes Required:

#### 1. API key redaction in translateUpstreamError

**File**: `proxy/errors.go`

**Intent**: Add a `redactSensitive` function that scans a string for common API key patterns (e.g., `sk-`, `sk-ant-`, `Bearer `, long alphanumeric tokens) and replaces them with `***`. Apply this to the upstream body snippet before including it in the Anthropic error envelope message field.

**Contract**: `redactSensitive(s string) string` — returns input with API key patterns replaced by `[REDACTED]`. Called from `translateUpstreamError()` at line ~75, after `sanitizePrintable` and before writing to the message field. Patterns to redact:
- `sk-[a-zA-Z0-9]{20,}` (OpenAI-style keys)
- `sk-ant-[a-zA-Z0-9]{20,}` (Anthropic-style keys)
- `Bearer [a-zA-Z0-9._-]{20,}` (Bearer tokens in error messages)
- Any string matching `[a-zA-Z0-9]{40,}` adjacent to keywords like "key", "token", "secret", "api_key"

#### 2. Redaction unit tests

**File**: `proxy/errors_test.go`

**Intent**: Add `TestRedactSensitive` table-driven test covering: (a) OpenAI key in error body → redacted, (b) Anthropic key → redacted, (c) Bearer token → redacted, (d) normal error message → unchanged, (e) key-like string not adjacent to keyword → unchanged (avoid false positives).

**Contract**: `redactSensitive` function. Table-driven with `input`, `expected`, `description` columns.

#### 3. Redaction integration test

**File**: `proxy/errors_test.go` or `proxy/adapter_errors_test.go`

**Intent**: Add test where mock upstream returns an error body containing a fake API key (e.g., `"Invalid API key: sk-test1234567890abcdef1234567890"`). Assert the client receives an Anthropic error envelope where the `message` field contains `[REDACTED]` instead of the key.

**Contract**: Full dispatcher chain — mock upstream returns 401 with body containing fake key. Assert: status 401, JSON decoded, `message` field contains `[REDACTED]`, does NOT contain `sk-test1234567890abcdef1234567890`.

#### 4. Log output leakage test

**File**: `proxy/adapter_errors_test.go`

**Intent**: Add test that captures log output via `slog.NewTextHandler` with a `bytes.Buffer`, triggers an error path, and asserts the API key string does NOT appear in log output.

**Contract**: Use `slog.SetDefault` or pass a custom logger to the dispatcher. Trigger a transport error (upstream unreachable). Assert log buffer does NOT contain the API key value. Best reference: `proxy/adapter_errors_test.go` — `TestMixAdapter_RoutingDebugLog` pattern.

#### 5. Response header leakage test

**File**: `proxy/errors_test.go`

**Intent**: Add test verifying the `X-Freedius-Error-Message` response header does NOT contain API key values when upstream error body includes them.

**Contract**: Mock upstream returns 401 with body containing fake key. Assert `X-Freedius-Error-Message` header contains `[REDACTED]`.

#### 6. Update test-plan.md §6 cookbook patterns

**File**: `context/foundation/test-plan.md`

**Intent**: Fill in §6.4 (Adding a test for a new provider adapter) and §6.5 (Adding a test for config validation) with the patterns established in Phases 1-2.

**Contract**: §6.4 describes the httptest mock provider pattern (reference: `proxy/nim_test.go:166`). §6.5 describes the table-driven config validation pattern (reference: `config/config_test.go:22`).

### Success Criteria:

#### Automated Verification:

- `mage test` passes — all new and existing tests green
- `TestRedactSensitive` covers ≥5 cases (key patterns + non-key strings)
- Integration test asserts `[REDACTED]` in error message when upstream body contains API key
- Log output test asserts API key absent from captured log buffer
- Header test asserts API key absent from `X-Freedius-Error-Message`
- Race detector clean

#### Manual Verification:

- Verify redaction doesn't produce false positives on normal error messages
- Verify test-plan.md §6 cookbook patterns are filled in

---

## Testing Strategy

### Unit Tests:

- `TestRedactSensitive` — API key pattern redaction (5+ cases)
- `TestTranslateUpstreamError` — extend with large body, HTML, empty body cases
- `TestLoad` — extend with empty mapping key, empty behavior cases

### Integration Tests:

- Anthropic response envelope verification through OpenAI-compat adapter
- Anthropic-compat error passthrough
- Multi-provider routing (two real upstreams)
- Error body format through full dispatcher chain
- API key redaction through full error chain (upstream → translateUpstreamError → client)
- Log output leakage verification

### Manual Testing Steps:

1. Run `mage test` and verify all new tests pass
2. Review test failure messages for clarity
3. Verify redaction doesn't break normal error messages

## References

- Research: `context/changes/testing-proxy-integration/research.md`
- Test plan: `context/foundation/test-plan.md`
- Best adapter test pattern: `proxy/nim_test.go:166` — `TestNIMAdapter_Upstream401_ReturnsAnthropicFormat`
- Best config test pattern: `config/config_test.go:22` — `TestLoad`
- Best log capture pattern: `proxy/adapter_errors_test.go` — `TestMixAdapter_RoutingDebugLog`
- Lessons: `context/foundation/lessons.md` — SSE encoding (§1), adapter return contract (§5)

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles. See `references/progress-format.md`.

### Phase 1: Translation + Routing Tests

#### Automated

- [x] 1.1 Anthropic response envelope test in `proxy/openai_compat_test.go` — decode JSON, assert field names/types
- [x] 1.2 Anthropic-compat error passthrough test in `proxy/anthropic_compat_test.go` — upstream 401 forwarded with body
- [x] 1.3 Multi-provider routing test in `proxy/proxy_test.go` — two httptest upstreams, verify correct one called
- [x] 1.4 Missing/ambiguous mapping edge cases in `proxy/proxy_test.go` — extend table-driven tests
- [x] 1.5 `mage test` passes — all new and existing tests green, race detector clean

#### Manual

- [ ] 1.6 Review test failure messages for clarity

### Phase 2: Error + Config Tests

#### Automated

- [ ] 2.1 Large/malformed upstream error body cases in `proxy/errors_test.go` — truncation, binary, empty
- [ ] 2.2 HTML error page test in `proxy/errors_test.go` — text/html upstream → reasonable message field
- [ ] 2.3 Error body format through full dispatcher chain in `proxy/adapter_errors_test.go` — JSON decode + field assertions
- [ ] 2.4 Config validation edge cases in `config/config_test.go` — empty mapping key, empty behavior
- [ ] 2.5 `mage test` passes — all new and existing tests green, race detector clean

#### Manual

- [ ] 2.6 Review error messages in tests are descriptive

### Phase 3: Privacy — API Key Redaction

#### Automated

- [ ] 3.1 Implement `redactSensitive` function in `proxy/errors.go`
- [ ] 3.2 `TestRedactSensitive` table-driven test in `proxy/errors_test.go` — 5+ cases
- [ ] 3.3 Redaction integration test — upstream error body with fake key → `[REDACTED]` in client response
- [ ] 3.4 Log output leakage test — API key absent from captured log buffer
- [ ] 3.5 Response header leakage test — API key absent from `X-Freedius-Error-Message`
- [ ] 3.6 Update test-plan.md §6 cookbook patterns 6.4 and 6.5
- [ ] 3.7 `mage test` passes — all new and existing tests green, race detector clean

#### Manual

- [ ] 3.8 Verify redaction doesn't produce false positives on normal error messages
