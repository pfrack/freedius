<!-- IMPL-REVIEW-REPORT -->
# Implementation Review: Echo client's original model name in response (stable across fallbacks)

- **Plan**: context/changes/response-model-echo/plan.md
- **Scope**: Phases 1–2 of 2
- **Date**: 2026-07-28
- **Verdict**: NEEDS ATTENTION
- **Findings**: 0 critical, 7 warnings, 1 observation

## Verdicts

| Dimension | Verdict |
|-----------|---------|
| Plan Adherence | WARNING |
| Scope Discipline | PASS |
| Safety & Quality | WARNING |
| Architecture | PASS |
| Pattern Consistency | PASS |
| Success Criteria | WARNING |

## Verification

All automated verification commands listed by both phases passed:

- `go test ./proxy/translate/...` — PASS
- `go test -run TestStream_ModelOverride ./proxy/translate/...` — PASS
- `go test -race ./...` — PASS
- `mage lint` — PASS (`0 issues`)
- `go test ./proxy/...` — PASS
- `go test -run TestAnthropicCompat_ModelOverride ./proxy/...` — PASS
- `go test -race ./...` — PASS
- `mage lint` — PASS (`0 issues`)

Manual criteria remain pending in the plan: Phase 1 item 1.5 (OpenAI-route curl), Phase 2 item 2.5 (Anthropic-route curl), and Phase 2 item 2.6 (fallback-chain model stability).

Git scope review found the planned implementation/test files and plan artifacts in the feature range `2dfed07..HEAD`; no unrelated tracked production scope creep was found. The workspace also contains unrelated/untracked planning artifacts that were not attributed to this review.

## Findings

### F1 — Transformed JSON responses retain the upstream Content-Length

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Safety & Quality
- **Location**: proxy/anthropic_compat.go:150-163
- **Detail**: The adapter copies all upstream headers and calls `WriteHeader` before rewriting an `application/json` body. That includes the upstream `Content-Length`, even though replacing the model can change the body size. A real HTTP client can therefore receive a stale length and a truncated or invalid response. The plan requires a rewritten complete JSON body, so the response headers must describe the rewritten body.
- **Fix**: Perform the JSON rewrite before committing the response, then remove or recompute `Content-Length` for the transformed body before copying headers and writing the status.
  - Strength: Restores the HTTP response contract and directly matches the plan's full-body rewrite behavior.
  - Tradeoff: Requires restructuring the success-path header/write ordering and a small transformed-response branch.
  - Confidence: HIGH — the current code visibly commits headers before transformation.
  - Blind spot: Compression/content-encoding behavior should be checked while changing the header policy.
- **Decision**: FIXED — applied the proposed fix: JSON responses are transformed before headers are committed, and upstream `Content-Length` is omitted for the transformed body.

### F2 — Non-streaming JSON rewriting buffers an unbounded upstream body

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Safety & Quality
- **Location**: proxy/anthropic_compat.go:160
- **Detail**: `io.ReadAll(resp.Body)` allocates memory proportional to any successful `application/json` response. A provider or intermediary can return an unexpectedly large body, creating avoidable memory pressure; the previous passthrough did not require buffering the entire response.
- **Fix**: Add a bounded rewrite buffer with an explicit maximum and a defined oversized-response fallback (passthrough or controlled error) before committing response headers.
  - Strength: Prevents a provider response from causing unbounded memory growth while preserving graceful behavior for normal responses.
  - Tradeoff: Requires choosing and documenting a maximum, and handling the branch where rewriting is skipped.
  - Confidence: HIGH — the unbounded allocation is explicit in the implementation.
  - Blind spot: The repository does not currently establish a response-size limit for Anthropic JSON bodies.
- **Decision**: FIXED — applied the proposed fix with a `MaxBodyBytes`-sized limit; oversized JSON responses are passed through without rewrite.

### F3 — Anthropic SSE forwarding does not flush events to the client

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Safety & Quality
- **Location**: proxy/anthropic_compat.go:170, 245-279
- **Detail**: The new SSE forwarding path writes through an `io.Writer` but has no flush callback. When an upstream sends `message_start` and pauses, the downstream may not receive that event until later buffering or stream completion. This weakens the live-streaming contract that the OpenAI translation path explicitly preserves by flushing each emitted event.
- **Fix**: Pass a `http.ResponseController(w).Flush` callback into the SSE forwarder and flush after each complete forwarded event, including the rewritten `message_start` event.
  - Strength: Preserves first-token/event latency and aligns the changed adapter with the existing streaming pattern.
  - Tradeoff: Requires event-boundary handling rather than flushing arbitrary lines.
  - Confidence: HIGH — the changed path owns SSE forwarding and currently has no flush operation.
  - Blind spot: A live-provider smoke test is still needed to confirm buffering behavior across the supported server/runtime combinations.
- **Decision**: FIXED — applied the proposed fix: Anthropic SSE forwarding now flushes after each complete event via `http.ResponseController`.

### F4 — SSE rewrite does not handle multiline events or raw-copy the remainder

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Plan Adherence
- **Location**: proxy/anthropic_compat.go:245-279
- **Detail**: The helper parses each `data:` line independently instead of collecting all data fields through the blank event delimiter. A valid multiline `message_start` event can therefore pass through with the upstream model unchanged. After a successful rewrite, it also continues `ReadBytes`/`Write` processing for the entire remaining stream, whereas the plan explicitly calls for piping the remainder through unchanged with `io.Copy` and no long-stream rewrite overhead.
- **Fix**: Read and rewrite one complete SSE event using accumulated data fields, preserve its framing, then call `io.Copy` for the untouched remainder.
  - Strength: Handles valid SSE framing and fulfills the plan's stated performance contract for long streams.
  - Tradeoff: The first-event parser is more involved and must preserve event/comment/line-ending details.
  - Confidence: HIGH — the translator already contains a complete data-line accumulation pattern in `proxy/translate/anthropic_openai.go`.
  - Blind spot: Provider-specific deviations from standard SSE framing need compatibility tests.
- **Decision**: FIXED — applied the proposed fix: complete SSE events now accumulate multiline data, preserve framing, and raw-copy the remainder after rewriting.

### F5 — SSE write failures are discarded

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Safety & Quality
- **Location**: proxy/anthropic_compat.go:284-312
- **Detail**: `tryRewriteSSEEvent` ignores errors from every write. A disconnected client can make the helper report a successful rewrite after a partial write; the adapter then keeps reading/draining the upstream and returns nil instead of stopping promptly.
- **Fix**: Return write errors from `tryRewriteSSEEvent` and propagate them through `forwardSSEWithModelRewrite`, stopping the upstream read when the downstream writer fails.
- **Decision**: FIXED — resolved as part of F4: the replacement SSE helper checks and propagates all downstream write errors.

### F6 — Fallback success leaves matched-provider/model headers stale

- **Severity**: ⚠️ WARNING
- **Impact**: 🔬 HIGH — architectural stakes; think carefully before deciding
- **Dimension**: Plan Adherence
- **Location**: proxy/proxy.go:274-275, 296-341
- **Detail**: `X-Freedius-Matched-Provider` and `X-Freedius-Matched-Model` are set once from the primary mapping before the fallback loop. If the primary fails and a fallback succeeds, the response still advertises the primary provider/model, contradicting the plan's requirement that these headers show what actually handled the request and undermining fallback diagnostics.
- **Fix**: Set or overwrite both matched headers immediately before each adapter attempt, after resolving that attempt's target and before it can commit the response.
  - Strength: Makes the successful response's diagnostics reflect the actual fallback target without changing the Provider interface.
  - Tradeoff: Requires deciding what headers should mean on an all-fallbacks-failed response, where no successful target exists.
  - Confidence: HIGH — the current one-time header assignment is visible, and the plan explicitly requires real upstream values.
  - Blind spot: A fallback test with distinct provider/model values is needed to validate the final wire headers.
- **Decision**: FIXED — applied the proposed fix: matched headers are overwritten per adapter attempt, with regression coverage using distinct fallback values.

### F7 — End-to-end and fallback model stability remain unverified

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Success Criteria
- **Location**: proxy/anthropic_compat_test.go:150-250; proxy/fallback_test.go:22-309; plan.md Progress 1.5, 2.5, 2.6
- **Detail**: The new tests exercise adapters directly, while existing fallback tests assert only that a fallback provider was called and returned success. No test verifies dispatcher → adapter model echo, distinct fallback response headers/model values, or fallback-chain response-model stability. The plan's three manual checks (OpenAI curl, Anthropic curl, and fallback stability) are still unchecked, so the change is not fully verified despite `status: implemented`.
- **Fix**: Add dispatcher-level OpenAI, Anthropic, and fallback tests with distinct upstream model/provider values, then run the three curl checks and record their evidence in the plan's Progress section.
  - Strength: Verifies the public contracts at the boundary where context propagation, fallback selection, headers, and response rewriting interact.
  - Tradeoff: Requires test fixtures for both adapter behaviors and access to configured upstreams for the manual checks.
  - Confidence: HIGH — the plan explicitly lists these integration/manual scenarios and current tests do not assert them.
  - Blind spot: The exact production provider/mapping setup for the curl checks is not captured in the plan.
- **Decision**: FIXED — added dispatcher-level OpenAI, Anthropic, and fallback stability tests with distinct upstream model/provider values. Provider-backed manual curl checks remain pending because no configured live upstream was available in this review environment.

### F8 — Graceful-degradation edge cases lack regression tests

- **Severity**: ℹ️ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Success Criteria
- **Location**: proxy/anthropic_compat_test.go:150-291
- **Detail**: The implementation attempts passthrough for malformed JSON, unexpected content types, SSE without `message_start`, malformed SSE data, and downstream write failures, but the test suite covers only well-formed non-streaming/streaming rewrites and empty-context passthrough. These untested branches are where response corruption and graceful fallback regressions are most likely.
- **Fix**: Add focused tests for malformed/unexpected JSON and SSE, multiline `data:` events, unrecognized content types, and writer failures.
- **Decision**: FIXED — added regression coverage for malformed JSON, unrecognized content types, unexpected/malformed SSE events, and downstream write errors.

## Triage Summary

- **Fixed**: F1, F2, F3, F4, F5, F6, F7, F8 (8)
- **Manual verification still pending**: plan Progress items 1.5, 2.5, and 2.6 require configured live-provider curl checks.
