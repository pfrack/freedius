---
date: 2026-07-02T00:00:00+00:00
researcher: OpenCode
git_commit: 120e5112c11c72ac8cc43c3a2987af0bf519b7fd
branch: testing-proxy-integration
repository: freedius
topic: "Streaming Edge Cases: SSE Parsing, Emitter State Machine, and Adapter Error Handling"
tags: [research, codebase, streaming, SSE, emitter, translation, error-handling, openai-compat, anthropic-compat]
status: complete
last_updated: 2026-07-02
last_updated_by: OpenCode
---

# Research: Streaming Edge Cases

**Date**: 2026-07-02T00:00:00+00:00
**Researcher**: OpenCode
**Git Commit**: 120e5112c11c72ac8cc43c3a2987af0bf519b7fd
**Branch**: testing-proxy-integration
**Repository**: freedius

## Research Question

Comprehensive analysis of streaming edge cases across the freedius proxy pipeline: SSE parsing, the OpenAI→Anthropic emitter state machine, adapter-level error handling, and historical context from prior changes.

## Summary

The streaming pipeline has three layers: **SSE parsing** (`readSSEEvent`), **state-machine translation** (`emitter`), and **adapter error handling** (`OpenAICompatibleAdapter.Handle`). Several bugs and gaps exist:

- **2 high-severity bugs**: (1) Empty `data:` lines cause JSON parse failure terminating the stream; (2) `pendingFinish` is never cleared, causing duplicate `message_delta` events when usage arrives before or with finish_reason.
- **3 medium-severity gaps**: (3) `[DONE]` without `data:` prefix is silently ignored; (4) stream close without `[DONE]` never emits `message_stop`; (5) mid-stream translation errors are logged-only, client sees truncated response.
- **4 low-severity issues**: `message_delta` omits `input_tokens`, `message_start` usage always `{0,0}`, SSE `event:` type lost, dead `reasoning` return value.
- **Historical gaps** from prior research (S-01, S-04, S-05) remain open: non-SSE content-type detection, shutdown timeout truncation, structured "stream aborted" events, and no integration tests for mid-stream errors.

## Detailed Findings

### SSE Parsing (`readSSEEvent`)

**File**: `proxy/translate/anthropic_openai.go:425-455`

#### Bug: Empty `data:` line causes stream termination

`proxy/translate/anthropic_openai.go:431-439, 493`

When a `data:` line has no value (e.g. `data:\n`), `readSSEEvent` returns an empty `[]byte{}` slice. In `consume` at line 493, `json.Unmarshal([]byte{}, &chunk)` fails with `"unexpected end of JSON input"`, terminating the stream. This is untested.

#### Bug: `[DONE]` without `data:` prefix silently ignored

`proxy/translate/anthropic_openai.go:431`

`readSSEEvent` only captures lines with the `data:` prefix. If a provider sends `[DONE]\n\n` (standalone, no `data:` prefix), the line is not captured and the `[DONE]` sentinel is never detected. The stream never terminates. The Anthropic client would hang.

#### Multi-line `data:` concatenation

`proxy/translate/anthropic_openai.go:436-438`

Multiple `data:` lines within the same SSE event are joined with `\n`. If individual lines are valid JSON, the concatenated payload `{"a":1}\n{"b":2}` is invalid JSON, causing parse failure. Test `TestStream_MultilineData` (line 848) uses separate single-line events, not multi-line data.

#### Non-`data:` lines silently dropped

`proxy/translate/anthropic_openai.go:431`

Lines without `data:` prefix (comments `:`, `event:`, `id:`, `retry:`) are silently skipped. SSE `event:` type information is completely lost. If a provider sends `event: error\ndata: {"type":"error"}\n\n`, the event type is discarded and the data is parsed as an `openAIChunk` with no `choices` field, returning `nil, nil` — silently dropped.

#### No empty-line terminator at EOF

`proxy/translate/anthropic_openai.go:443-447`

When EOF is hit without an empty line terminator, any accumulated `dataLine` is returned as a valid event. This is correct. However, `Stream` returns `("", nil)` on `io.EOF` (line 405-406), so `flushPending` is never called and `message_stop` is never emitted. The downstream Anthropic client would hang waiting for stream completion.

### Emitter State Machine

**File**: `proxy/translate/anthropic_openai.go:457-796`

#### Bug: `pendingFinish` never cleared — duplicate `message_delta`

`proxy/translate/anthropic_openai.go:541, 554-569`

When usage arrives before or in the same chunk as `finish_reason`:
1. `sawUsage` is set `true` (line 503)
2. `emitFinish` is called immediately (line 543) — emits `message_delta` with stop_reason
3. `[DONE]` arrives: `flushPending` is called. `pendingFinish` is still set (never cleared) → `emitFinish` called a **second time** → duplicate `message_delta`

Only `TestStream_FinishBeforeUsage_UsesPendingFinish` (line 343) checks `strings.Count(out, "event: message_delta") != 1`, but that test has usage absent when finish arrives, so only one emit occurs. The `TestStream_TextStream` test (line 168) has usage in the same chunk as finish_reason but only checks `strings.Contains`, not count.

#### Usage-only chunk doesn't trigger deferred finish

`proxy/translate/anthropic_openai.go:500-508`

When a usage-only chunk arrives after a deferred finish reason:
1. `sawUsage` is set `true` (line 503)
2. `len(chunk.Choices) == 0` → returns `nil, nil` (line 507-508)
3. `emitFinish` is NOT called even though usage is now available

The deferred finish is only emitted when `[DONE]` triggers `flushPending()`. This is correct behavior but means the timing of `message_delta` emission depends on when `[DONE]` arrives, not when usage becomes available.

#### Multiple choices in a single chunk — potential duplicate events

`proxy/translate/anthropic_openai.go:511-551`

The `consume` loop iterates over ALL choices. If a chunk has multiple choices, each triggers role-sending, thinking/text/tool emissions, and finish_reason handling independently. `pendingFinish` is overwritten by the last choice's finish_reason. `emitFinish` could be called multiple times. This is untested.

#### Empty `model` field in `message_start`

`proxy/translate/anthropic_openai.go:497-499, 571-589`

If no chunk provides a non-empty `model` field, `e.model` remains `""` and `message_start` emits `"model":""`. The Anthropic spec requires a non-empty model string. This is untested.

#### ReasoningContent + Content in same delta

`proxy/translate/anthropic_openai.go:519-531`

Both are checked in sequence — thinking first, then text. `emitThinkingDelta` closes the previous block and opens a thinking block. `emitText` sees `openBlock == "thinking"`, closes it, and opens a text block. Two sequential blocks emitted in one chunk. Works correctly but untested.

#### Interleaved tool calls with different indices

`proxy/translate/anthropic_openai.go:676-741`

The `toolToBlock` map preserves the original block index for each tool call. `openBlock` remains `"tool"` throughout. When a new tool index arrives, `emitBlockStop` closes the current block. Should work correctly, but untested for interleaving.

#### `mapFinishReason` defaults unknown reasons to `end_turn`

`proxy/translate/anthropic_openai.go:798-811`

All known OpenAI finish reasons are handled (`stop`, `length`, `tool_calls`, `function_call`, `content_filter`). Unknown reasons silently map to `"end_turn"`. Safe default but could mask new OpenAI finish reasons.

### Adapter Error Handling

#### Mid-stream translation errors — logged only, client sees truncation

`proxy/openai_compat.go:153-162`

After `WriteHeader(http.StatusOK)` (line 153), `translate.Stream` runs. If it returns an error:
1. Logged at `Error` level (line 160)
2. Returns `nil` to dispatcher (line 162)
3. Client sees truncated SSE with no error envelope

The dispatcher at `proxy/proxy.go:297-309` has a post-WriteHeader error branch, but it's currently unreachable because `OpenAICompatibleAdapter` always returns `nil` even on stream errors.

#### Stream timeout — pre-WriteHeader, correctly handled

`proxy/openai_compat.go:113-116, 139-141`

`context.WithTimeout(r.Context(), streamTimeout)` wraps the upstream request. Timer fires before `resp, err := a.client.Do(req)` returns. Error returned to dispatcher → 529 `overloaded_error` with `retry-after: 15`. Tested by `TestOpenAICompat_StreamTimeout_Honored` (adapter_errors_test.go:279).

#### Client disconnect during streaming

`proxy/openai_compat.go:115` (r.Context propagation)

Client disconnect propagates through `r.Context()` to the upstream request context. The upstream call is cancelled. Error surfaces through `translate.Stream` → logged, returned as `nil`.

#### Anthropic-compat mid-stream failures — silent truncation

`proxy/anthropic_compat.go:86-96`

`httputil.ReverseProxy` handles streaming transparently. If the upstream connection breaks mid-stream, the proxy silently stops copying. The `ErrorHandler` is only called for transport errors during `RoundTrip`, not mid-stream copy failures. `AnthropicCompatibleAdapter.Handle` always returns `nil` (line 97).

#### `freediusErrorHandler` — cancel vs. transport errors

`proxy/errors.go:202-238`

- `context.Canceled` → logged at Debug, no response body, no status code written (line 207-215)
- Permanent transport errors (DNS, TLS cert) → 502 `api_error`, no retry (line 231-233)
- Transient errors (connection refused, reset, timeout) → 529 `overloaded_error`, `retry-after: 15` (line 234-236)

#### `translateUpstreamError` — pre-stream error mapping

`proxy/errors.go:72-113`

Called when upstream returns `>= 400` before streaming starts. Reads up to 256 bytes of body, sanitizes (strip non-printable, redact API keys), drains 4 KiB for connection reuse, and maps to Anthropic error types. Status code mappings: 429→rate_limit_error, 503/529→overloaded_error, 500→api_error, 401/403→authentication_error, other 4xx→invalid_request_error, other 5xx→api_error.

### SSE Framing (lessons.md validated)

`proxy/translate/anthropic_openai.go:789-796`

Uses `json.Marshal` (NOT `json.NewEncoder`) per lessons.md:3-7 to avoid trailing `\n` corruption. Uses `bufio.Reader.ReadBytes('\n')` (NOT `bufio.Scanner`) per lessons.md:9-13 to avoid 64KB token truncation. SSE format: `event: <type>\ndata: <json>\n\n`.

## Code References

- `proxy/translate/anthropic_openai.go:425-455` — `readSSEEvent` SSE parser
- `proxy/translate/anthropic_openai.go:457-471` — `emitter` struct definition
- `proxy/translate/anthropic_openai.go:482-552` — `emitter.consume` main chunk processing
- `proxy/translate/anthropic_openai.go:554-569` — `flushPending` deferred finish logic
- `proxy/translate/anthropic_openai.go:571-796` — all `emit*` methods
- `proxy/translate/anthropic_openai.go:399-423` — `Stream` function entry point
- `proxy/openai_compat.go:59-163` — `OpenAICompatibleAdapter.Handle`
- `proxy/anthropic_compat.go:39-98` — `AnthropicCompatibleAdapter.Handle`
- `proxy/errors.go:72-113` — `translateUpstreamError`
- `proxy/errors.go:202-238` — `freediusErrorHandler`
- `proxy/errors.go:171-194` — `isPermanentTransportError`
- `proxy/proxy.go:268-311` — dispatcher adapter.Handle call with wroteHeader tracking
- `proxy/proxy.go:394-425` — `wroteHeaderResponseWriter` with Flush delegation
- `proxy/translate/anthropic_openai_test.go:168-1291` — 25+ streaming test functions

## Architecture Insights

1. **Three-layer pipeline**: SSE parsing → state-machine translation → adapter error handling. Each layer has independent failure modes.
2. **Post-WriteHeader error swallowing**: The adapter contract (lessons.md:33-43) requires returning `nil` after any response is written. This means mid-stream errors are invisible to the client. The dispatcher's post-WriteHeader branch at `proxy/proxy.go:297-309` is a safety net that's currently unreachable.
3. **State machine invariants are implicit**: `openBlock`, `pendingFinish`, `sawUsage`, `roleSent`, `finished` — these booleans form a state machine without explicit state enumeration. The `pendingFinish` never-cleared bug is a direct consequence of this implicit design.
4. **Two streaming paradigms coexist**: `OpenAICompatibleAdapter` does manual SSE translation (read-chunk-translate-write-flush loop), while `AnthropicCompatibleAdapter` uses `httputil.ReverseProxy` (transparent byte-copy). The error handling differs significantly between them.
5. **The `reasoning` return value from `Stream` is dead code**: The function signature claims to return "assistant text used as input_reasoning" but always returns `""`.

## Historical Context (from prior changes)

### S-01 (`first-call-routed`) — Foundation
- `context/archive/first-call-routed/research.md:188-311` — Original SSE translation pipeline specification with 8 Anthropic event types and state machine design
- `context/archive/first-call-routed/research.md:270` — `bufio.Reader.ReadBytes('\n')` over `bufio.Scanner` decision
- `context/archive/first-call-routed/research.md:311` — `json.Marshal` over `json.NewEncoder.Encode` decision
- `context/archive/first-call-routed/research.md:407-408` — Known limitation: output token accounting without usage chunk

### S-04 (`error-hardening`) — Streaming error gaps
- `context/archive/error-hardening/research.md:103-104` — Identified: no `http.Client.Timeout` on OpenAI compat adapter (now fixed with `stream-timeout`)
- `context/archive/error-hardening/research.md:107-113` — Identified: non-SSE Content-Type on 200 not detected, mid-stream errors logged-only, no per-request timeout
- `context/archive/error-hardening/research.md:152` — Shutdown timeout (5s) may truncate SSE streams
- `context/archive/error-hardening/research.md:444` — Client disconnect propagation gap (now fixed via context propagation)

### S-05 (`opencode-nim-fixes`) — Translation gaps
- `context/archive/opencode-nim-fixes/research.md:38-40` — NIM SSE gaps: dropped reasoning_content, no sanitization, no retry logic
- `context/archive/opencode-nim-fixes/research.md:83-105` — `openAIDelta` struct missing `ReasoningContent` field (now fixed)
- `context/archive/opencode-nim-fixes/research.md:137-149` — SSE response comparison: 8 gaps identified between freedius and free-claude-code

### `deepseek-reasoning-content` — Thinking block edge cases
- `context/archive/deepseek-reasoning-content/research.md:197-203` — Signature theory was wrong; Claude Code doesn't strip unsigned thinking blocks
- `context/archive/deepseek-reasoning-content/research.md:230-256` — OpenCode pattern: always set `reasoning_content` on assistant messages (post-pass at `proxy/translate/anthropic_openai.go:169-186`)

### `testing-proxy-integration` — Test gaps
- `context/archive/2026-07-02-testing-proxy-integration/research.md:78-79` — No test for Anthropic-compat adapter receiving upstream error, no test for mid-stream translation errors through adapter

### `improve-mixed-providers-config` — Active gap
- `context/changes/improve-mixed-providers-config/research.md:49-65` — URL-path sniffing fragility in `MixAdapter` routing; `protocol` field approved but never merged to main

## Related Research

- `context/archive/first-call-routed/research.md` — Original SSE translation pipeline design
- `context/archive/error-hardening/research.md` — Streaming error handling gaps
- `context/archive/opencode-nim-fixes/research.md` — NIM-specific SSE translation gaps
- `context/archive/deepseek-reasoning-content/research.md` — Thinking block streaming edge cases
- `context/archive/2026-07-02-testing-proxy-integration/research.md` — Test gap analysis
- `context/changes/improve-mixed-providers-config/research.md` — Mix adapter routing fragility

## Open Questions

1. **Should `readSSEEvent` handle `[DONE]` without `data:` prefix?** The SSE spec allows `[DONE]` as a comment line, but OpenAI sends it as `data: [DONE]`. Adding standalone `[DONE]` detection would be a safety net.
2. **Should `flushPending` be called on `io.EOF` in `Stream`?** Currently, stream close without `[DONE]` never emits `message_stop`. Some providers may not send `[DONE]`.
3. **Should `pendingFinish` be cleared after `emitFinish`?** The duplicate `message_delta` bug is a one-line fix.
4. **Should empty `data:` lines be handled gracefully?** Currently they cause stream termination. Could be treated as no-ops.
5. **Should the adapter emit a structured "stream aborted" SSE event on mid-stream errors?** This was identified in S-04 as an open gap — the client currently sees silent truncation.
6. **Should the `reasoning` return value from `Stream` be removed or implemented?** It's dead code that signals an incomplete feature.
7. **Should `message_delta.usage` include `input_tokens`?** The Anthropic spec includes both `input_tokens` and `output_tokens`.