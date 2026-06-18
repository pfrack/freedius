# /v1/messages/count_tokens Passthrough — Implementation Plan

## Overview

Make `freedius` correctly route Claude Code's `/v1/messages/count_tokens` probe to Anthropic-protocol upstreams (where it already works by accident via `httputil.ReverseProxy`'s path preservation) and reject it with `501 Not Implemented` for OpenAI-protocol upstreams (where it currently silently corrupts the request). This is a proxy-correctness fix — Claude Code already sends these requests, and today the dispatcher's path-agnostic dispatch lets them flow through to broken behavior on every non-Anthropic-compatible upstream.

Local token counting (so OpenAI-protocol upstreams also return a useful `count_tokens` response) is **out of scope** for this plan. It is filed as a follow-up change; the implementation choice (character-based heuristic vs `github.com/pkoukk/tiktoken-go`) will be made when that plan starts.

## Current State Analysis

**Dispatcher** (`proxy/proxy.go:70-244`) — `Dispatcher.ServeHTTP` never inspects `r.URL.Path`. It checks method, content type, reads body, parses `"model"` field, resolves `model → config.Model → Provider`, and forwards. The path is opaque to it.

**`AnthropicCompatibleAdapter`** (`proxy/anthropic_compat.go:39-84`) — uses `httputil.ReverseProxy` with `Rewrite: pr.SetURL(target)` (line 75). The original `r.URL.Path` flows through to upstream unchanged. So `/v1/messages/count_tokens` already reaches upstream Anthropic correctly. **Works by accident.**

**`OpenAICompatibleAdapter`** (`proxy/openai_compat.go:90-127`) — builds `http.NewRequestWithContext(POST, m.BaseURL, ...)` (line 90-95). It posts the translated body to `m.BaseURL` (e.g. `https://integrate.api.nvidia.com/v1/chat/completions`) and runs `translate.Stream` (line 121) which expects OpenAI SSE chunks. **Silently breaks** on count_tokens — wrong endpoint, wrong response shape, no translation exists for the request/response.

**`MixAdapter`** (`proxy/mix.go:44-69`) — dispatches to Anthropic or OpenAI sub-adapter via `m.Protocol` (line 50-56) or URL sniff (`strings.HasSuffix(parsedURL.Path, "/v1/messages")` line 64). count_tokens only works if routed to the anthropic sub-adapter.

**Registered adapter names** (`proxy/adapters_gen.go:49-54`): `nim`, `openai`, `anthropic`, `mix`. By dispatch time, `m.Provider` is always one of these — `custom`/`zen`/`go` are rewritten to `mix` by `config/providers_gen.go:75-93`.

**Reference behavior** — `free-claude-code` (Python/FastAPI) implements local token counting with tiktoken `cl100k_base` and serves count_tokens entirely in the proxy (`core/anthropic/tokens.py`, `api/routes.py:76-84`). This is the pattern for Phase 2; not used in Phase 1.

**Constraint from `context/foundation/lessons.md`** — "Adapter Return Contract": once an adapter has written the response, it must return `nil`. Any count_tokens rejection must happen in the dispatcher **before** `adapter.Handle` is called, not inside an adapter that may have already started writing.

### Key Discoveries:

- `AnthropicCompatibleAdapter` already handles count_tokens correctly — no adapter changes needed. (`proxy/anthropic_compat.go:39-84`)
- `MixAdapter` already has the routing logic for `m.Protocol` + URL sniff that the capability check needs to duplicate. (`proxy/mix.go:50-69`)
- `applyEntryDefaults` rewrites `custom`/`zen`/`go` → `mix` before dispatch. The dispatcher only ever sees `m.Provider ∈ {nim, openai, anthropic, mix}`. (`config/providers_gen.go:71-95`)
- AccessLogMiddleware already logs `r.URL.Path`, so count_tokens requests will be visible in logs without changes. (`proxy/proxy.go:415-435`)

## Desired End State

After this plan lands:

1. `POST /v1/messages/count_tokens` to a `provider: anthropic` (or `provider: mix` with `protocol: anthropic`, or `provider: mix` with `base_url` ending in `/v1/messages`) flows through to the upstream Anthropic endpoint and returns the upstream's `{"input_tokens": N, ...}` response. Verified via integration test with `httptest.NewServer`.
2. `POST /v1/messages/count_tokens` to a `provider: nim` (or `provider: openai`, or `provider: mix` with `protocol: openai`) returns `501 Not Implemented` with the freedius error envelope `{"error":"not_supported","message":"/v1/messages/count_tokens is not supported for provider \"<name>\""}` and `request_id` if present.
3. `POST /v1/messages` (regular messages endpoint) continues to work identically for every provider. Verified by regression test.

### How to verify:

- `go test ./...` — all tests pass, including 6 new cases in `proxy/proxy_test.go`.
- `go vet ./...` — clean.
- `go build -o freedius .` — produces a static binary.
- Manual: `curl -X POST http://localhost:8082/v1/messages/count_tokens -d '{"model":"claude-opus-4","messages":[]}'` against a config with an Anthropic-protocol provider returns the upstream response; against an OpenAI-protocol provider returns 501 with the freedius error envelope.

## What We're NOT Doing

- **Local token counting for OpenAI-protocol upstreams.** Deferred to a follow-up plan. The user has confirmed this is a separate concern; the implementation choice (character-based heuristic vs `github.com/pkoukk/tiktoken-go`) will be made when that plan starts.
- **HEAD/OPTIONS probe support on count_tokens.** free-claude-code handles these with `204 + Allow: POST`. Claude Code does not probe count_tokens, and the dispatcher's current method check (`http.MethodPost` only) returning `405 Method Not Allowed` is acceptable for Phase 1.
- **Translation layer changes.** count_tokens is Anthropic-protocol-only; no `proxy/translate/` changes.
- **New config flags** (e.g. `count_tokens_enabled`). Unnecessary for a single-user local tool.
- **Telemetry / structured logging** beyond what `AccessLogMiddleware` already emits. `path` field in the access log line already distinguishes count_tokens requests.
- **Provider-interface capability methods** (e.g. `SupportsCountTokens() bool`). Hardcoded helper in `proxy/capabilities.go` is sufficient for one endpoint. If a second Anthropic-protocol endpoint appears (e.g. `/v1/messages/batches`), the pattern repeats — a 3-line addition to the helper.

## Implementation Approach

Insert a path-aware capability check between model resolution and adapter dispatch in `Dispatcher.ServeHTTP`. The check has two parts:

1. **Path detection** — exact match on `/v1/messages/count_tokens`. Lives in `proxy/capabilities.go` so it's discoverable as a single capability surface.
2. **Provider capability** — pure function `supportsCountTokens(m config.Model) bool` that encodes the four rules: `m.Provider == "anthropic"` → true; `m.Provider == "mix"` with `m.Protocol == "anthropic"` → true; `m.Provider == "mix"` with no protocol but `m.BaseURL` ending in `/v1/messages` → true (URL sniff, mirroring `MixAdapter.Handle` line 64); everything else → false.

When the path matches but the provider doesn't support it, the dispatcher writes `501 Not Implemented` via the existing `d.writeErrorJSON` helper and returns — never reaching `adapter.Handle`. This respects the Adapter Return Contract lesson.

When the path matches and the provider does support it, control falls through to the existing adapter dispatch — `AnthropicCompatibleAdapter` (whether called directly or via `MixAdapter`'s anthropic branch) handles the request via `httputil.ReverseProxy` exactly like a regular `/v1/messages` request.

No other code paths change. The mix adapter's routing logic is duplicated in ~5 lines inside `supportsCountTokens`; the duplication is acceptable because the URL sniff rule is small and stable, and a unit test on `supportsCountTokens` will catch any future drift if mix's rule changes.

## Critical Implementation Details

- **Mix routing duplication** — `supportsCountTokens` duplicates `MixAdapter.Handle`'s protocol-and-URL-sniff rule (`proxy/mix.go:50-69`). If mix's rule changes (e.g. a third protocol), both must update. A comment on the helper pointing to `MixAdapter.Handle` and a unit test covering each mix branch keeps them in sync.
- **Capability check runs after model resolution** — placed at `proxy/proxy.go:177` (after the `dispatch` debug log, before `X-Freedius-Matched-*` headers and adapter lookup). Requests with unknown models still get `404 no_match`; requests with count_tokens to a model that resolves but routes to OpenAI get `501 not_supported`.
- **Path check is exact, not suffix** — `r.URL.Path == "/v1/messages/count_tokens"`. Anthropic's API surface has exactly this one path; suffix matching risks false positives (`/v2/something/count_tokens`). Query strings don't affect the match (they live in `r.URL.RawQuery`, not `r.URL.Path`).

## Phase 1: Path-aware count_tokens routing

### Overview

Add `proxy/capabilities.go` with `isCountTokensPath` and `supportsCountTokens` helpers. Wire them into `Dispatcher.ServeHTTP` between model resolution and adapter dispatch. Add a focused table-driven test covering the Anthropic/OpenAI/mix routing matrix plus a regression test for the regular `/v1/messages` path.

### Changes Required:

#### 1. New file: `proxy/capabilities.go`

**File**: `proxy/capabilities.go`

**Intent**: Encapsulate the path-detection and provider-capability rules for Anthropic-protocol endpoints (currently just count_tokens) in a single, discoverable location. Keeps dispatcher logic uncluttered and makes the capability surface testable in isolation.

**Contract**: Two exported-from-package functions (lowercase, package-internal — no external consumers yet):
- `func isCountTokensPath(p string) bool` — returns `p == "/v1/messages/count_tokens"`.
- `func supportsCountTokens(m config.Model) bool` — returns true iff `m.Provider == "anthropic"`, or `m.Provider == "mix"` with `m.Protocol == "anthropic"`, or `m.Provider == "mix"` with no protocol set and `m.BaseURL` parseable + path ending in `/v1/messages`. All other combinations return false.

Doc comment on `supportsCountTokens` points at `MixAdapter.Handle` as the source of truth for mix routing and notes the duplication.

#### 2. Modify `proxy/proxy.go` — dispatcher integration

**File**: `proxy/proxy.go`

**Intent**: Reject unsupported count_tokens requests with 501 before any adapter is invoked. The check runs after model resolution (so we know which provider the request would route to) and before adapter lookup, so we never call `adapter.Handle` for a request we'll reject.

**Contract**: Insert a block at `proxy/proxy.go:177` (immediately after the `d.Logger.Debug("dispatch", ...)` log statement at lines 177-187, before the `X-Freedius-Matched-*` headers at lines 188-189):

```
if isCountTokensPath(r.URL.Path) && !supportsCountTokens(m) {
    d.writeErrorJSON(
        w, r,
        http.StatusNotImplemented,
        "not_supported",
        fmt.Sprintf("/v1/messages/count_tokens is not supported for provider %q", originalOr(m)),
    )
    return
}
```

The existing `writeErrorJSON` (line 266-298) already handles the envelope shape (`error`, `message`, `request_id`, optional `detail`); it sets `Content-Type: application/json` and writes the status code. No new error-writing code needed.

`originalOr(m)` (line 246-251) returns `m.OriginalProvider` if set, else `m.Provider` — gives a user-friendly provider name in the error message even after the `custom→mix` rewrite.

#### 3. Add tests to `proxy/proxy_test.go`

**File**: `proxy/proxy_test.go`

**Intent**: Cover the Anthropic/OpenAI/mix routing matrix with a table-driven test, plus a regression test confirming regular `/v1/messages` is unaffected.

**Contract**: One new table-driven test function `TestServeHTTPCountTokens` with six sub-cases. Each sub-case builds a dispatcher (using existing `newTestDispatcherWithAdapter` helper at line 27), posts a request to `/v1/messages/count_tokens` (or `/v1/messages` for the regression case) with a JSON body `{"model":"<test-model>"}`, and asserts on status code, response body substring, and (for success cases) the X-Freedius-Matched-* headers.

Sub-cases:
1. `anthropic` provider + count_tokens path → success (mock returns 200, response forwarded; assert `X-Freedius-Matched-Provider: anthropic`).
2. `nim` provider + count_tokens path → 501, body contains `"error":"not_supported"` and the provider name.
3. `mix` provider + `Protocol: "anthropic"` + count_tokens path → success (delegates to anthropic sub-adapter).
4. `mix` provider + `Protocol: "openai"` + count_tokens path → 501.
5. `mix` provider + no protocol + `BaseURL` ending in `/v1/messages` + count_tokens path → success (URL sniff routes to anthropic sub-adapter).
6. Regular `/v1/messages` path with `nim` provider → success (regression — proves the check doesn't accidentally fire on non-count_tokens paths).

Existing test fixtures (`mockProvider` line 436-451, `recordingProvider` line 453-468) cover the success paths. For case 3 and 5 (mix + anthropic sub-routing), use a minimal `mockProvider` registered as `"mix"` since the test only verifies the dispatcher doesn't reject — the actual mix → anthropic delegation is exercised by existing `proxy/mix_test.go` tests.

Also add a unit test for `supportsCountTokens` covering each branch (anthropic=true, mix+anthropic-protocol=true, mix+openai-protocol=false, mix+/v1/messages-url=true, mix+other-url=false, nim/openai=false) — placed in a new `proxy/capabilities_test.go` next to the helper.

### Success Criteria:

#### Automated Verification:

- `go test ./proxy/...` — all existing tests pass; 6 new sub-cases in `TestServeHTTPCountTokens` pass; new `TestSupportsCountTokens` unit test passes.
- `go test ./...` — full module test suite passes.
- `go test -cover ./...` — coverage of `proxy/proxy.go` and `proxy/capabilities.go` is at or above the existing module average (no new uncovered branches).
- `go vet ./...` — clean.
- `go build -o freedius .` — static binary builds.
- `gofumpt -l proxy/` — no formatting issues (matches repo's CI enforcement from `AGENTS.md`).

#### Manual Verification:

- Run `freedius` against a config with an Anthropic-protocol provider (e.g. `provider: anthropic` or `provider: custom` with Anthropic-compatible base_url). Send `curl -X POST http://localhost:8082/v1/messages/count_tokens -H 'Content-Type: application/json' -d '{"model":"claude-opus-4","messages":[]}'`. Verify the upstream's `{"input_tokens":N,...}` response reaches curl.
- Run `freedius` against a config with `provider: nim`. Send the same curl. Verify the response is HTTP 501 with `{"error":"not_supported","message":"...provider \"nim\"..."}` and that no upstream call was made (check `NVIDIA_NIM_API_KEY` is unset so a misrouted call would fail loudly).
- Send `curl -X POST http://localhost:8082/v1/messages -H 'Content-Type: application/json' -d '{"model":"claude-opus-4","messages":[]}'` and verify the regular messages endpoint still works identically to before this change.

**Implementation Note**: After completing this phase and all automated verification passes, pause here for manual confirmation from the human that the manual testing was successful before proceeding to any next phase. Phase blocks use plain bullets — the corresponding `- [ ]` checkboxes for these items live in the `## Progress` section at the bottom of the plan.

---

## Testing Strategy

### Unit Tests:

- `proxy/capabilities_test.go` — `TestSupportsCountTokens` covers each routing branch: `anthropic`, `mix`+`protocol=anthropic`, `mix`+`protocol=openai`, `mix`+no-protocol+`/v1/messages` URL, `mix`+no-protocol+other URL, `nim`, `openai`, unparseable BaseURL.
- `proxy/capabilities_test.go` — `TestIsCountTokensPath` covers exact match (positive), trailing slash (negative), query string (positive — query strings are in `RawQuery`, not `Path`), unrelated paths (negative).

### Integration Tests:

- `proxy/proxy_test.go` — `TestServeHTTPCountTokens` table-driven with 6 sub-cases covering the Anthropic/OpenAI/mix matrix and a `/v1/messages` regression case. Uses existing `httptest.NewRequest`, `httptest.NewRecorder`, `mockProvider`, `recordingProvider` fixtures.

### Manual Testing Steps:

1. With a config containing a working Anthropic-protocol provider (e.g. `provider: anthropic`, `ANTHROPIC_API_KEY` set), `curl -X POST http://localhost:8082/v1/messages/count_tokens -H 'Content-Type: application/json' -d '{"model":"claude-opus-4-6","messages":[]}'` → verify upstream Anthropic response received.
2. With a config containing `provider: nim` (Nvidia NIM), same curl → verify 501 + freedius error envelope.
3. With `provider: custom` and `base_url: https://api.minimax.io/anthropic` (Anthropic-compatible), same curl → verify upstream response (mix's URL sniff routes to anthropic sub-adapter).
4. Send a regular `/v1/messages` request to any of the above configs → verify it works identically to before the change.

## Performance Considerations

- The capability check is two function calls (path equality + provider capability) per request, both O(1) (URL parsing in the mix branch is the only non-trivial op, and it's only reached when `m.Provider == "mix"`). Negligible overhead on the hot path.
- The check fires only on POST requests that already passed model resolution — the 99% case for `/v1/messages` is unaffected.
- For `/v1/messages/count_tokens` routed to OpenAI-protocol upstreams: instead of a slow failing upstream call, the user gets an immediate 501. Net latency win on the failure path.

## Migration Notes

- No config schema changes. No new env vars. No new files in user-visible locations.
- Existing `provider: custom` configs that point at Anthropic-compatible URLs (the most common case after `custom → mix` rewrite) automatically work for count_tokens without any config change.
- Existing `provider: nim` configs that previously silently sent count_tokens to NIM's chat-completions endpoint now correctly return 501 — a **behavior improvement**, not a regression. No user action required.

## References

- Research: `context/changes/count-tokens-passthrough/research.md`
- Reference pattern: [free-claude-code `core/anthropic/tokens.py`](https://github.com/Alishahryar1/free-claude-code) — local token counting pattern used by Phase 2 (not this plan).
- `proxy/proxy.go:70-244` — Dispatcher.ServeHTTP (insertion point for capability check).
- `proxy/proxy.go:266-298` — `writeErrorJSON` helper (used for 501 response).
- `proxy/mix.go:50-69` — MixAdapter routing logic (duplicated in `supportsCountTokens`).
- `proxy/anthropic_compat.go:39-84` — AnthropicCompatibleAdapter (pass-through works by default; no changes needed).
- `proxy/openai_compat.go:59-127` — OpenAICompatibleAdapter (would silently break count_tokens; dispatcher rejects before reaching it).
- `config/providers_gen.go:71-95` — `applyEntryDefaults` rewriting `custom`/`zen`/`go` → `mix`.
- `context/foundation/lessons.md` — Adapter Return Contract (justifies dispatcher-level rejection, not adapter-internal).

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles. See `references/progress-format.md`.

### Phase 1: Path-aware count_tokens routing

#### Automated

- [x] 1.1 `go test ./proxy/...` — all existing tests pass
- [x] 1.2 `go test ./proxy/...` — `TestIsCountTokensPath` passes
- [x] 1.3 `go test ./proxy/...` — `TestSupportsCountTokens` passes
- [x] 1.4 `go test ./proxy/...` — `TestServeHTTPCountTokens` 6 sub-cases pass
- [x] 1.5 `go test ./...` — full module test suite passes
- [x] 1.6 `go test -cover ./...` — coverage at or above module average
- [x] 1.7 `go vet ./...` — clean
- [x] 1.8 `go build -o freedius .` — static binary builds
- [x] 1.9 `gofumpt -l proxy/` — no formatting issues

#### Manual

- [ ] 1.10 `provider: anthropic` + curl count_tokens → upstream response
- [ ] 1.11 `provider: nim` + curl count_tokens → 501 with freedius error envelope
- [ ] 1.12 `provider: custom` (Anthropic-compatible URL) + curl count_tokens → upstream response
- [ ] 1.13 Regular `/v1/messages` request → works identically to before