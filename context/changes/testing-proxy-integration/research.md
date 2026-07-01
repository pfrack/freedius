---
date: 2026-07-02T00:00:00Z
researcher: opencode
git_commit: 9cf6dda
branch: main
repository: freedius
topic: "Ground rollout Phase 1 of test-plan.md — risks #1, #3, #4, #5, #6"
tags: [research, proxy, config, translation, error-handling, testing]
status: complete
last_updated: 2026-07-02
last_updated_by: opencode
---

# Research: Proxy Integration — Translation, Routing, Errors

**Date**: 2026-07-02
**Researcher**: opencode
**Git Commit**: 9cf6dda
**Branch**: main
**Repository**: freedius

## Research Question

Ground rollout Phase 1 of `context/foundation/test-plan.md`. For each of the 5 risks (#1, #3, #4, #5, #6), locate the real failure path in code, verify or correct the risk response guidance, identify existing tests, and flag gaps.

## Summary

The proxy core is **well-structured with strong test coverage** for translation, routing, config validation, and error propagation. The test suite has ~255 test functions across 25 files. Key findings:

- **Risk #1 (translation format)**: Translation is thorough (60 unit tests in `proxy/translate/`), but **no integration test verifies the full Anthropic response envelope** through the adapter — tests use substring checks, not schema validation.
- **Risk #3 (routing)**: Routing is deterministic (exact > family > 404), no silent fallback. Existing tests are good but **no multi-provider end-to-end test** exists.
- **Risk #4 (config validation)**: Strongest coverage — 22 table-driven cases with error message assertions. Nearly gap-free.
- **Risk #5 (error propagation)**: Well-tested (4 dedicated test files, 28 tests). **Gap**: no test for large/malformed upstream error bodies.
- **Risk #6 (API key leakage)**: **Weakest coverage** — only a source-code comment check exists. No runtime test verifies keys absent from logs, error bodies, or TUI output. **One real vector identified**: upstream error body snippets forwarded without API key redaction.

**Test plan corrections recommended**: Risk #6 response guidance should explicitly call out the upstream error body snippet vector. Risk #5's "must challenge" is accurate. Risk #3's assumption about "no silent fallback" is validated — the `default` family catch-all is explicit.

## Detailed Findings

### Risk #1 — Translation Format Confusion

#### Translation Architecture

The translation layer (`proxy/translate/anthropic_openai.go`) handles Anthropic↔OpenAI format conversion:

- **Request translation**: `translate.Request()` (line 32-72) converts Anthropic messages to OpenAI chat completions format — system messages, tool_use, tool_result, thinking blocks, temperature, stop sequences.
- **Stream translation**: `translate.Stream()` (line 399-423) reads OpenAI SSE events and emits Anthropic SSE events via `emitter.consume()` (line 482-552).
- **SSE framing**: `emitter.emit()` (line 789-796) uses `json.Marshal` (not `json.NewEncoder`) per the lesson in `context/foundation/lessons.md:3-7` — avoids trailing newline corrupting SSE framing.
- **SSE reader**: `readSSEEvent()` (line 425-455) uses `bufio.Reader.ReadBytes('\n')` (not `Scanner`) per `lessons.md:9-13` — avoids 64KB truncation of tool-use arguments.

#### Adapter Implementations

Three adapters implement the `Provider` interface:

| Adapter | File | Handle method | Upstream format | Response translation |
|---------|------|---------------|-----------------|---------------------|
| `AnthropicCompatibleAdapter` | `proxy/anthropic_compat.go:39-98` | ReverseProxy passthrough | Anthropic (x-api-key, anthropic-version) | None — passthrough |
| `OpenAICompatibleAdapter` | `proxy/openai_compat.go:59-163` | Translates request, proxies, translates response | OpenAI (Authorization: Bearer) | Anthropic SSE via translate.Stream() |
| `MixAdapter` | `proxy/mix.go:49-79` | Routes to Anthropic or OpenAI sub-adapter based on Protocol field or URL path sniffing | Depends on routing | Depends on sub-adapter |

**Format guarantee per adapter**:
- Anthropic-compat: Upstream receives `x-api-key` + `anthropic-version: 2023-06-01`, body passthrough. `Authorization` header stripped (`proxy/anthropic_compat.go:84-90`). Response forwarded verbatim — **no format translation needed**.
- OpenAI-compat: Request translated to OpenAI format. Response translated back to Anthropic SSE format via `translate.Stream()`.
- Mix: Delegates to sub-adapter. Protocol field (`"openai"` / `"anthropic"`) selects explicitly; URL path sniffing as fallback (`proxy/mix.go:61-78`).

#### Existing Tests

- `proxy/translate/anthropic_openai_test.go`: **60 test functions** — comprehensive unit coverage for request and stream translation including edge cases (thinking blocks, tool calls, finish reasons, mid-stream errors, flush errors, downstream write errors, multiline data, EOF on partial data).
- `proxy/mix_test.go`: **15 tests** — protocol routing, URL sniffing, passthrough, upstream 401 forwarding. `TestMixAdapter_OpenAITranslation` verifies `event: message_start` and `event: message_stop` substring presence but **does NOT parse individual SSE event data payloads**.
- `proxy/anthropic_compat_test.go`: **3 tests** — passthrough headers verified, missing base URL, missing env var. **Does NOT verify response body content for the passthrough test**.
- `proxy/openai_compat_test.go`: **3 tests** — upstream 401 status forwarded, missing env var, stream:true present in upstream body. **Does NOT verify downstream response format**.
- `proxy/nim_test.go`: **9 tests** — NIM→OpenAI→Anthropic full pipeline including tool_use, parallel tool calls, 401, 429, transport errors. Best integration coverage.
- `proxy/error_propagation_test.go`: **3 tests** — 429 with Anthropic format, timeout, transport error.

#### Gaps

1. **No integration test verifies the full Anthropic response envelope** (non-streaming) through the OpenAI-compat adapter — tests check status codes and substrings, not JSON schema structure (`content` array, `stop_reason`, `usage` fields).
2. **No test for Anthropic-compat adapter receiving an error** from upstream — the passthrough test (`anthropic_compat_test.go:20`) only tests 200 responses. Transport errors through Anthropic-compat go to `freediusErrorHandler` but this path is untested through the adapter.
3. **No test for mid-stream translation errors through the adapter** — the translate package tests this (`TestStream_ErrorMidStream`, line 1123), but the adapter just logs and returns nil (`openai_compat.go:158-161`) — no test verifies the client sees a partial/corrupt response.
4. **`forwardUpstreamError()` is dead code** (`proxy/errors.go:29-38`) — tested but never called by any adapter. Only `translateUpstreamError()` (which rewrites body to Anthropic format) is used.

#### Test Plan Guidance Assessment

**Risk #1 guidance is accurate.** The plan's assumption that "Anthropic-format headers + body" is the target holds for the Anthropic-compat and Mix/Anthropic paths. For OpenAI-compat, the response IS translated back to Anthropic format — the guidance correctly states "regardless of upstream provider." The anti-pattern ("assertion copied from production logic") is well-chosen — existing tests use substring checks rather than independent schema validation.

---

### Risk #3 — Silent Misrouting

#### Routing Architecture

`resolveMapping()` at `proxy/proxy.go:94-111`:

1. **Exact match first** (line 97): `d.Cfg.Mappings[model]`
2. **Family fallback** (lines 99-101): `extractFamily(model)` runs regex patterns in priority order: `opus` > `sonnet` > `haiku` > `auto` > `default` (`proxy/families.go:10-16`). The `default` pattern matches everything.
3. **Provider lookup** (lines 106-110): Looks up `mapping.ProviderName` in `d.Cfg.Providers`.
4. **Not found** (lines 103-104): Returns 404 `"no configured mapping for model %q"`.

**No silent fallback exists.** Routing is deterministic: exact > family > 404. The `default` catch-all is explicit — it must be configured by the user. Missing provider returns 500, not a fallback.

**Mix adapter routing** (`proxy/mix.go:49-79`):
- `Protocol` set → explicit sub-adapter selection + URL normalization
- `Protocol` empty → URL path sniffing: `/v1/messages` → Anthropic, everything else → OpenAI
- A URL without either suffix and no Protocol **falls through to OpenAI** — this is the default, not a silent bug.

#### Existing Tests

- `proxy/proxy_test.go`: **12 tests** — exact match, family match, family priority, default catch-all, unknown model (404), missing provider (500), concurrent map access, count_tokens routing.
- `proxy/families_test.go`: **1 test (12 cases)** — family extraction patterns.
- `proxy/mix_test.go`: **15 tests** — protocol routing, URL sniffing.

#### Gaps

1. **No multi-provider end-to-end test** — no test configures two real httptest upstreams and verifies only the correct one receives the request.
2. **No test for the "behavior not registered" branch** (`proxy.go:251-267`) with a non-empty registry — existing test uses empty registry which hits "provider not registered" instead.

#### Test Plan Guidance Assessment

**Risk #3 guidance is accurate.** The "must challenge" (default mapping covers all cases) is correct — the `default` family is the one case where routing could surprise users. The guidance correctly identifies the need to test explicit + missing + ambiguous mappings.

---

### Risk #4 — Config Validation Crash

#### Config Architecture

- `config/config.go:64` — `Load(path)` → `readConfigFile` → `yamlUnmarshalStrict` (strict mode rejects unknown fields) → `applyDefaults` → `validate`
- `config/defaults.go:16-44` — `applyDefaults` auto-injects providers with `DefaultBaseURL`, fills missing fields from `providerDefaults`
- `config/config.go:196-251` — `validateProvider`: checks behavior, URL scheme, env var chars, RequireBaseURL, Protocol
- `config/config.go:253-285` — `validateMapping`: checks provider_name exists, model_string non-empty, no unsafe chars (CR, LF, colon)

**No panic paths in config loading.** Maps initialized before use. The only panics are intentional startup-time guards (`NewDispatcher`, `NewRegistry`).

**Relevant lesson from `context/foundation/lessons.md:15-19`**: The `custom` → `mix` rewriting lesson is **stale** — the current codebase has `custom` as a first-class provider with `Behavior: "mix"` directly (`providers_gen.go:10-11`). No alias rewriting at load time.

#### Existing Tests

`config/config_test.go`: **16 test functions, 22 table-driven cases in TestLoad** covering:
- Empty file, empty providers, malformed YAML
- Invalid behavior, unknown YAML field (strict mode), non-string behavior
- Missing model_string, missing provider_name, unknown provider reference
- Header-unsafe model_string, missing default_base_url, invalid URL scheme
- Invalid default_api_key_env (newline, equals)
- Valid/invalid protocol
- Round-trip marshal/unmarshal, save backup, theme handling

Each error case asserts `errSubstr` on the error message content.

#### Gaps

Minimal. Missing: empty mapping key, behavior as empty string, multiple validation errors (Go map iteration is random — only first error returned).

#### Test Plan Guidance Assessment

**Risk #4 guidance is accurate.** The "must challenge" (config validation exists) is correct — validation is thorough. The guidance correctly identifies the need to verify error message quality and the no-panic guarantee. The unit layer is the cheapest test — config tests don't need httptest.

---

### Risk #5 — Error Swallowed

#### Error Propagation Architecture

Four distinct flows:

**Flow A: Pre-WriteHeader adapter error** (`proxy/proxy.go:269-296`):
- `configError` → `writeAnthropicError(w, 500, ce.errType, err.Error(), 0)` (line 283)
- Generic error → `writeAnthropicError(w, 529, "overloaded_error", "upstream provider not reachable", 15)` (line 294)
- Error message goes into response body AND `X-Freedius-Error-Message` header

**Flow B: Post-WriteHeader adapter error** (`proxy/proxy.go:297-308`):
- Error logged only; client already received partial response. Intentional — cannot rewrite headers.
- `wroteHeaderResponseWriter` (line 395-415) tracks whether headers were written.

**Flow C: Transport error** (`proxy/errors.go:162-198`):
- Client disconnect → Debug log, no response (line 167-176)
- Permanent (DNS, TLS) → 502 "api_error" (line 192-193)
- Transient → 529 "overloaded_error" with retry-after: 15 (line 195-196)

**Flow D: Upstream HTTP error** (`proxy/errors.go:71-111`):
- `translateUpstreamError()` reads up to 256 bytes of upstream body, sanitizes for printable chars
- Maps status codes: 401→authentication_error, 403→permission_error, 404→not_found_error, 429→rate_limit_error, 500-599→api_error
- Sets `retry-after` and `x-should-retry` headers

**Key finding**: `forwardUpstreamError()` (`proxy/errors.go:29-38`) preserves upstream format (headers + body + status) but is **dead code** — never called by any adapter. Only `translateUpstreamError()` (which rewrites to Anthropic format) is used.

#### Existing Tests

- `proxy/errors_test.go`: **6 tests** — `translateUpstreamError` (10 status code cases), `forwardUpstreamError`, transport error, client cancel, writeAnthropicError.
- `proxy/adapter_errors_test.go`: **11 tests** — dispatcher-level error translation, configError types, DNS/connection errors, error templates with provider names, stream timeout.
- `proxy/error_propagation_test.go`: **3 tests** — 429 Anthropic format, timeout, transport error.
- `proxy/error_contract_test.go`: **8 tests** — JSON error shape contract, detail gating, request_id.

All error tests verify **body content** via JSON decoding, not just status codes.

#### Gaps

1. **No test for large/malformed upstream error body** — `forwardUpstreamError` uses `io.ReadAll` with no size limit. While it's dead code, `translateUpstreamError` reads 256 bytes — no test verifies this truncation works correctly for oversized or binary bodies.
2. **No test for upstream returning `Content-Type: text/html`** (CDN error page) — `translateUpstreamError` reads body as plain text but no test verifies HTML content produces a reasonable `message` field.
3. **`writeAnthropicError` uses `json.NewEncoder(w).Encode()`** (`errors.go:57`) which adds trailing `\n` — fine for JSON (not SSE) but no test verifies exact bytes.

#### Test Plan Guidance Assessment

**Risk #5 guidance is accurate.** The "must challenge" (error forwarding is implemented) is validated — error forwarding IS implemented and well-tested. The guidance correctly identifies the need to verify the actual error body, not just status code. The anti-pattern ("mocking provider to return error but only asserting freedius status code") directly addresses the gap in `openai_compat_test.go:21` which checks status 401 but not body format.

---

### Risk #6 — API Key Leakage

#### Where API Keys Are Used (Never Logged)

- `proxy/openai_compat.go:136` — `req.Header.Set("Authorization", "Bearer "+apiKey)`
- `proxy/anthropic_compat.go:84` — `r.Header.Set("x-api-key", apiKey)`
- `proxy/anthropic_compat.go:90` — `pr.Out.Header.Set("x-api-key", apiKey)`

API key values are resolved at runtime via `os.Getenv()` — never stored in config structs.

#### Logging Paths

All `slog` calls in `proxy/` were inspected. **No call includes actual API key values.** The access log middleware (`proxy.go:478-501`) logs request_id, method, path, status, duration_ms, matched_provider, matched_model — no request/response bodies or headers.

The privacy policy is enforced by source-code comments (`proxy.go:1-5`: "DO NOT log request or response bodies") and a test (`proxy/privacy_test.go`) that checks for the comment's existence.

#### TUI Data Flow

The TUI displays: provider name, behavior, base_url, `api_key_env` (env var **name** only, not value), mapping name, provider_name, model_string. The provider edit form (`proxy/tui/model.go:853-860`) shows and edits the env var name, never the actual key.

#### Potential Leakage Vectors

**VECTOR 1 (MEDIUM RISK): Upstream error body snippets in client responses**
- `translateUpstreamError()` (`proxy/errors.go:71-110`) reads 256 bytes of upstream body, sanitizes for printable chars only
- Some LLM providers include API key prefixes in error messages (e.g., "Invalid API key provided: sk-abc...xyz")
- This snippet goes to: client HTTP response body, `X-Freedius-Error-Message` header, EventBus `ErrorMessage`
- **No redaction of API key patterns** — `sanitizePrintable` only strips non-printable chars

**VECTOR 2 (LOW RISK): configError messages**
- `proxy/proxy.go:283` — `err.Error()` includes env var **names** (e.g., "env var NVIDIA_NIM_API_KEY is not set") but never values
- Tested in `adapter_errors_test.go:140-236`

**VECTOR 3 (LOW RISK): Verbose error detail**
- `proxy/errors.go:184-189` — when `verboseErrors=true`, full transport error string logged at Debug
- Transport errors from `http.Client.Do()` could theoretically include URLs with embedded credentials (not the case for current adapters)

**VECTOR 4 (INFORMATIONAL): Config via IPC**
- `cmd/freedius/ipc_unix.go:218-221` — serializes full Config struct including `DefaultAPIKeyEnv` (names only)
- IPC socket is chmod 0600 (line 80)

#### Existing Tests

- `proxy/privacy_test.go`: **1 test** — checks source-code comment exists. Not a runtime behavioral test.
- `proxy/middleware_test.go`: `TestRecoverMiddleware_500WithOpaqueBody` — verifies panic body doesn't contain `"detail"`, `"panic"`, `"stack"`.

#### Gaps (Significant)

1. **No runtime test verifies API keys absent from log output** — no test sets up an adapter with a known key, triggers a request, and asserts the key string doesn't appear in captured log output.
2. **No test verifies API keys absent from error response bodies** — no test checks that upstream error body snippets don't contain sensitive patterns.
3. **No test verifies API keys absent from `X-Freedius-Error-Message` header** — could carry upstream body snippets.
4. **No test for `forwardUpstreamError` header forwarding safety** — forwards ALL upstream headers unfiltered (dead code, but tested).

#### Test Plan Guidance Assessment

**Risk #6 guidance needs correction.** The plan's "must challenge" ("we don't log keys") is accurate for the logging path, but **misses the upstream error body snippet vector** which is the real leakage risk. The guidance should be updated to:

- **What would prove protection**: API keys and sensitive config never appear in logs, error response bodies, or response headers — including upstream error body snippets forwarded via `translateUpstreamError`.
- **Must challenge**: "We don't log keys" — must also verify that upstream error body snippets don't contain API key patterns. The `sanitizePrintable` function strips non-printable chars but does NOT redact sensitive patterns.
- **Anti-pattern to avoid**: Only checking happy-path (no errors = no leakage); not testing error paths where upstream error bodies might contain key prefixes.

---

## Code References

- `proxy/translate/anthropic_openai.go:32-72` — `translate.Request()`
- `proxy/translate/anthropic_openai.go:399-423` — `translate.Stream()`
- `proxy/translate/anthropic_openai.go:482-552` — `emitter.consume()`
- `proxy/translate/anthropic_openai.go:789-796` — `emitter.emit()` (SSE framing)
- `proxy/anthropic_compat.go:39-98` — `AnthropicCompatibleAdapter.Handle()`
- `proxy/openai_compat.go:59-163` — `OpenAICompatibleAdapter.Handle()`
- `proxy/openai_compat.go:145-148` — upstream error path
- `proxy/openai_compat.go:158-161` — stream error (logged only)
- `proxy/mix.go:49-79` — `MixAdapter.Handle()`
- `proxy/mix.go:84-102` — `normalizeBaseURL()`
- `proxy/proxy.go:94-111` — `resolveMapping()`
- `proxy/proxy.go:194-230` — dispatcher routing (no match, provider not registered)
- `proxy/proxy.go:268-310` — adapter error handling (pre/post WriteHeader)
- `proxy/proxy.go:395-415` — `wroteHeaderResponseWriter`
- `proxy/proxy.go:430-458` — `RecoverMiddleware`
- `proxy/proxy.go:478-501` — `AccessLogMiddleware`
- `proxy/errors.go:21-27` — `configError` type
- `proxy/errors.go:29-38` — `forwardUpstreamError()` (dead code)
- `proxy/errors.go:43-64` — `writeAnthropicError()`
- `proxy/errors.go:71-111` — `translateUpstreamError()`
- `proxy/errors.go:135-154` — `isPermanentTransportError()`
- `proxy/errors.go:162-199` — `freediusErrorHandler()`
- `proxy/families.go:10-16` — family priority order
- `config/config.go:64-77` — `Load()` flow
- `config/config.go:196-251` — `validateProvider()`
- `config/config.go:253-285` — `validateMapping()`
- `config/defaults.go:16-44` — `applyDefaults()`

## Architecture Insights

1. **The proxy has a clean layered architecture**: Dispatcher → Adapter → Translate. Each layer has its own error handling contract.
2. **Adapter return contract** (from `lessons.md`): Once an adapter writes any response part, it must return `nil`. Post-WriteHeader errors are logged and discarded. Both adapters comply.
3. **Error format is always Anthropic**: `translateUpstreamError()` rewrites ALL upstream errors into Anthropic format. `forwardUpstreamError()` (which preserves upstream format) exists but is dead code.
4. **Config is validated at load time, not request time**: All validation happens in `Load()` → `validate()`. The dispatcher panics on nil cfg/registry/logger by design (startup guard).
5. **The `custom` → `mix` rewriting lesson is stale**: The current codebase has `custom` as a first-class provider with `Behavior: "mix"` directly. No alias rewriting.

## Historical Context (from prior changes)

- `context/archive/first-call-routed/plan.md` — Original proxy architecture decisions
- `context/archive/error-hardening/plan.md` — Error handling patterns established
- `context/archive/custom-to-mix-protocol/plan.md` — The `custom` → `mix` migration that made the lessons.md entry stale
- `context/archive/opencode-nim-fixes/plan.md` — Auth header fix (x-api-key instead of Bearer)
- `context/foundation/lessons.md` — 7 lessons; SSE encoding, adapter return contract, and `custom` → `mix` rewriting are most relevant

## Related Research

- `context/archive/first-call-routed/research.md` — Original translation layer research
- `context/archive/error-hardening/research.md` — Error handling patterns research

## Open Questions

1. Should `translateUpstreamError` redact API key patterns from upstream error body snippets? (Currently: no redaction. Risk: medium — depends on what upstream providers include in error messages.)
2. Should the dead code `forwardUpstreamError()` be removed? It's tested but never called.
3. Should there be an integration test verifying the full Anthropic non-streaming response schema through the OpenAI-compat adapter? (Current tests use substring checks.)
