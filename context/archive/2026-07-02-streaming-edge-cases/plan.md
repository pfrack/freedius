# Streaming Edge Cases Implementation Plan

## Overview

Fix 2 high-severity bugs, 3 medium-severity gaps, and 4 low-severity issues in the SSE parsing, emitter state machine, and adapter error handling layers of the streaming pipeline. All changes are in `proxy/translate/anthropic_openai.go` and `proxy/openai_compat.go`, with tests in `proxy/translate/anthropic_openai_test.go` and `proxy/adapter_errors_test.go`.

## Current State Analysis

The streaming pipeline has three layers:

1. **SSE parsing** (`readSSEEvent`, line 425-455): reads lines from upstream, accumulates `data:` lines, returns on empty line or EOF. Bugs: empty `data:` lines cause JSON parse failure, bare `[DONE]` silently ignored, multi-line data concatenation can produce invalid JSON, no `flushPending` call on EOF.

2. **Emitter state machine** (`emitter`, line 457-796): translates OpenAI chunks to Anthropic SSE events. Bugs: `pendingFinish` never cleared (duplicate `message_delta`), `message_delta.usage` missing `input_tokens`, `message_start` usage always `{0,0}` when no usage chunk arrives before role.

3. **Adapter error handling** (`OpenAICompatibleAdapter.Handle`, `proxy/openai_compat.go:59-163`): after `WriteHeader`, `translate.Stream` errors are logged-only — the client sees silent truncation. The dead `reasoning` return value clutters the `Stream` signature.

## Desired End State

The SSE parser is resilient to empty `data:` lines and bare `[DONE]` sentinels. The emitter never emits duplicate `message_delta` events, includes `input_tokens` in usage, and flushes pending state on EOF. The adapter emits a structured SSE error event on mid-stream translation failures so clients can distinguish clean completion from stream aborts. The `Stream` function signature is clean (no dead return values).

### Key Discoveries

- `pendingFinish` never cleared after `emitFinish` — one-line fix at `anthropic_openai.go:541` (research.md:77-84)
- `Stream` returns `("", nil)` on `io.EOF` without calling `flushPending` — `anthropic_openai.go:405-406` (research.md:69)
- Multi-line `data:` concatenation with `\n` produces invalid JSON when each line is valid JSON — `anthropic_openai.go:436-438` (research.md:53-57)
- Adapter contract (lessons.md:33-43) requires returning `nil` after any response is written, so mid-stream errors must be signaled in-band via SSE events
- `json.Marshal` (not `json.NewEncoder`) and `bufio.Reader.ReadBytes` (not `bufio.Scanner`) are the mandated SSE framing patterns (lessons.md:3-13)

## What We're NOT Doing

- Not adding non-SSE `Content-Type` detection on 200 responses (S-04 gap, deferred)
- Not adding structured "stream aborted" event for Anthropic-compat adapter (uses `httputil.ReverseProxy`, different paradigm)
- Not implementing the `reasoning` feature — removing the dead return value only
- Not handling `message_start` usage `{0,0}` when usage arrives after role (cosmetic, low severity)
- Not preserving SSE `event:` type from upstream (cosmetic, low severity)
- Not enumerating the emitter state machine explicitly (refactoring, out of scope)

## Implementation Approach

Fix bugs bottom-up: SSE parser first (the foundation), then emitter state machine (the core logic), then adapter error handling (the integration layer). Each phase has its own tests. The final phase adds integration tests for the mid-stream error path through the full adapter stack.

## Critical Implementation Details

- **Timing & lifecycle**: `flushPending` must be called on `io.EOF` in `Stream` BEFORE returning `nil`. The emit methods must be safe to call when no events are pending (idempotent).
- **State sequencing**: `pendingFinish` must be cleared immediately after `emitFinish` returns, before any other state mutation. This prevents the duplicate `message_delta` regression.
- **User experience spec**: The mid-stream error event must use the SSE format `event: error\ndata: {"type":"error","error":{"type":"api_error","message":"..."}}\n\n` to match the Anthropic event conventions.

## Phase 1: SSE Parser Edge Cases

### Overview

Make `readSSEEvent` resilient to three edge cases that currently cause stream termination or client hangs.

### Changes Required

#### 1. Handle empty `data:` lines as no-ops

**File**: `proxy/translate/anthropic_openai.go`

**Intent**: When `data:` has no value (e.g. `data:\n`), skip the line instead of accumulating an empty byte slice that causes `json.Unmarshal` to fail downstream.

**Contract**: In `readSSEEvent`, after stripping the `data:` prefix, if the value is empty (zero-length after trimming), skip the line entirely — do not add it to `dataLine`. The loop continues reading the next line.

#### 2. Detect bare `[DONE]` without `data:` prefix

**File**: `proxy/translate/anthropic_openai.go`

**Intent**: When a provider sends `[DONE]\n\n` (standalone, no `data:` prefix), detect it as a stream termination sentinel. Currently only `data: [DONE]` is recognized.

**Contract**: In `readSSEEvent`, before checking for `data:` prefix, check if the trimmed line equals `[DONE]`. If so, return `[]byte("[DONE]")` as the event data — the same bytes that `data: [DONE]` would produce.

#### 3. Handle multi-line data that produces invalid JSON

**File**: `proxy/translate/anthropic_openai.go`

**Intent**: When multiple `data:` lines within the same SSE event are concatenated with `\n`, the result may be invalid JSON (e.g., `{"a":1}\n{"b":2}`). Rather than terminating the stream, fall back to the first valid line.

**Contract**: In `consume`, after `json.Unmarshal(trimmed, &chunk)` fails, attempt to split `trimmed` by `\n`, keep only the first segment, and retry `json.Unmarshal` on that. If that also fails, return the original error.

### Success Criteria

#### Automated Verification

- `mage test` passes — all existing streaming tests still pass
- New unit tests in `proxy/translate/anthropic_openai_test.go`:
  - `TestStream_NonDataLine_Ignored` — non-`data:` lines are skipped, stream completes
  - `TestStream_BareDone_DetectsNoPrefix` — bare `[DONE]` (no `data:` prefix) triggers `message_stop`
  - `TestStream_MultilineData` — multiple SSE events concatenated; each `data:` line parsed as separate event

#### Manual Verification

- Send a mock SSE stream with empty `data:` lines through the proxy, verify the stream completes normally
- Send a mock SSE stream with bare `[DONE]`, verify the stream terminates cleanly

---

## Phase 2: Emitter State Machine Fixes

### Overview

Fix the `pendingFinish` never-cleared bug, add `flushPending` on EOF, add `input_tokens` to `message_delta`, and remove the dead `reasoning` return value.

### Changes Required

#### 1. Clear `pendingFinish` after `emitFinish`

**File**: `proxy/translate/anthropic_openai.go`

**Intent**: After `emitFinish` is called (either inline in `consume` or via `flushPending`), clear `pendingFinish` to prevent duplicate `message_delta` events when `[DONE]` arrives later.

**Contract**: Add `e.pendingFinish = ""` at the end of `emitFinish` (line 768). This ensures `flushPending` is idempotent — calling it a second time will only emit `message_stop`, not a duplicate `message_delta`.

#### 2. Call `flushPending` on `io.EOF` in `Stream`

**File**: `proxy/translate/anthropic_openai.go`

**Intent**: When the upstream stream closes without sending `[DONE]`, call `flushPending` to emit any deferred `message_delta` and `message_stop`. Currently the stream returns `("", nil)` on EOF without emitting completion events.

**Contract**: In `Stream` function, before the `return "", nil` on `io.EOF` (line 406), call `em.flushPending()` and write/flush its events. If `flushPending` returns an error, propagate it.

#### 3. Add `input_tokens` to `message_delta.usage`

**File**: `proxy/translate/anthropic_openai.go`

**Intent**: The Anthropic spec includes both `input_tokens` and `output_tokens` in `message_delta.usage`. Currently only `output_tokens` is emitted.

**Contract**: In `emitFinish` (line 759-761), add `"input_tokens": e.inputTokens` to the usage map alongside the existing `output_tokens`.

#### 4. Remove `reasoning` return value from `Stream`

**File**: `proxy/translate/anthropic_openai.go`

**Intent**: The `reasoning` return value from `Stream` is dead code — it always returns `""`. Remove it to clean up the API.

**Contract**: Change `Stream` signature from `func Stream(upstream io.Reader, downstream io.Writer, flush func() error) (string, error)` to `func Stream(upstream io.Reader, downstream io.Writer, flush func() error) error`. Update the return statements on lines 406, 408, 412, 416, 419 to return `nil` or `err` as appropriate. Remove the `reasoning` variable and `_ = reasoning` line from the caller in `proxy/openai_compat.go:155-157`.

### Success Criteria

#### Automated Verification

- `mage test` passes — all existing tests still pass
- `TestStream_FinishBeforeUsage_UsesPendingFinish` still passes (exactly 1 `message_delta`)
- `TestStream_TextStream` — verify `message_delta.usage` contains `input_tokens`
- New unit test: stream with `[DONE]` after usage-already-sent-finish → exactly 1 `message_delta`
- New unit test: stream closes without `[DONE]` → `message_stop` is emitted
- Type checking passes (no unused `reasoning` variable)

#### Manual Verification

- Run a real streaming request through the proxy with a provider that sends usage in the same chunk as finish_reason, verify exactly one `message_delta` in the output
- Run a streaming request with a provider that may not send `[DONE]`, verify the stream completes cleanly

---

## Phase 3: Mid-Stream Error Handling

### Overview

Emit a structured SSE error event when `translate.Stream` fails after `WriteHeader` has been sent, so clients see a clean error instead of silent truncation.

### Changes Required

#### 1. Add `emitError` method to emitter

**File**: `proxy/translate/anthropic_openai.go`

**Intent**: Provide a method to emit a structured SSE error event in the Anthropic event format. This is used by the adapter when the stream fails mid-translation.

**Contract**: Add a method `func (e *emitter) emitError(msg string) []byte` that produces an SSE event: `event: error\ndata: {"type":"error","error":{"type":"api_error","message":"..."}}\n\n`. Use `json.Marshal` (not `json.NewEncoder`) per lessons.md:3-7.

#### 2. Write error event on stream failure in adapter

**File**: `proxy/openai_compat.go`

**Intent**: When `translate.Stream` returns an error after `WriteHeader` has been sent, write the error event to the response writer before returning `nil`.

**Contract**: After the `translate.Stream` call returns an error, call `emitter.emitError(err.Error())` (or equivalent) and write the resulting bytes to `w`. Log the error at `Warn` level (not `Error`, since we're handling it gracefully). The emitter for this purpose can be a fresh `newAnthropicEmitter()` — its state is not needed for the error event. Return `nil` per the adapter contract.

### Success Criteria

#### Automated Verification

- `mage test` passes
- New unit test: `Stream` function returns an error → verify error event bytes are in the downstream buffer
- New integration test in `proxy/adapter_errors_test.go`: upstream returns garbage mid-stream → adapter emits error event, status 200, no panic

#### Manual Verification

- Run a real streaming request through the proxy, manually kill the upstream mid-stream, verify the client receives an `error` SSE event before the stream closes

---

## Phase 4: Integration & Edge-Case Tests

### Overview

Add comprehensive tests for all fixed edge cases, including regression tests for the bugs fixed in Phases 1-3.

### Changes Required

#### 1. SSE parser tests

**File**: `proxy/translate/anthropic_openai_test.go`

**Intent**: Verify the three SSE parser fixes from Phase 1.

**Contract**: Add test functions:
- `TestStream_EmptyDataLine_Skipped` — stream with empty `data:` line, verify stream completes normally
- `TestStream_BareDone_Detected` — stream with `[DONE]\n\n` (no `data:` prefix), verify `message_stop` emitted
- `TestStream_MultilineInvalidJSON_Fallback` — stream with two `data:` lines each containing valid JSON, verify the first is parsed and stream continues

#### 2. Emitter regression tests

**File**: `proxy/translate/anthropic_openai_test.go`

**Intent**: Verify the emitter fixes from Phase 2 and prevent regression.

**Contract**: Add test functions:
- `TestStream_DuplicateMessageDelta_Regression` — stream where usage arrives in same chunk as finish_reason, then `[DONE]` arrives separately → exactly 1 `message_delta`
- `TestStream_EOFWithoutDone_EmitsMessageStop` — stream closes without `[DONE]` → `message_stop` emitted
- `TestStream_MessageDelta_IncludesInputTokens` — verify `input_tokens` field in `message_delta` usage

#### 3. Mid-stream error integration test

**File**: `proxy/adapter_errors_test.go`

**Intent**: Verify the full adapter path for mid-stream errors from Phase 3.

**Contract**: Add test function `TestOpenAICompat_MidStreamError_EmitsErrorEvent`:
- Set up `httptest.Server` that returns 200 with `Content-Type: text/event-stream` and sends garbage SSE data that `translate.Stream` will fail on
- Call `OpenAICompatibleAdapter.Handle` with a real `httptest.ResponseRecorder`
- Verify status code is 200 (WriteHeader already sent)
- Verify the response body contains `event: error` and `data: {"type":"error"...`
- Verify no panic, no "superfluous WriteHeader" error

### Success Criteria

#### Automated Verification

- `mage test` passes with all new tests
- `mage test -race` passes with no race conditions
- `mage lint` passes with no new warnings

#### Manual Verification

- Run `mage test` and verify all streaming tests pass, including the new edge case tests

---

## Testing Strategy

### Unit Tests

- `TestStream_EmptyDataLine_Skipped` — empty `data:` line in SSE stream
- `TestStream_BareDone_Detected` — `[DONE]` without `data:` prefix
- `TestStream_MultilineInvalidJSON_Fallback` — multi-line data with invalid concatenated JSON
- `TestStream_DuplicateMessageDelta_Regression` — exactly 1 `message_delta` when usage arrives with finish_reason
- `TestStream_EOFWithoutDone_EmitsMessageStop` — `message_stop` on EOF without `[DONE]`
- `TestStream_MessageDelta_IncludesInputTokens` — `input_tokens` in `message_delta` usage
- `TestStream_ErrorEvent_Emitted` — `emitError` produces correct SSE event bytes

### Integration Tests

- `TestOpenAICompat_MidStreamError_EmitsErrorEvent` — full adapter path with failing upstream

### Manual Testing Steps

1. Run a real streaming request through the proxy with a provider that sends empty `data:` keepalive lines
2. Run a streaming request with a provider that may not send `[DONE]`
3. Kill an upstream mid-stream and verify the client receives an error SSE event

## Performance Considerations

None. All changes are minor — adding a few string comparisons in the parser, clearing a field, and writing one extra SSE event on error. No new allocations in the hot path.

## Migration Notes

None. These are bug fixes with no data model changes, config changes, or API contract changes.

## References

- Research: `context/changes/streaming-edge-cases/research.md`
- Prior SSE design: `context/archive/first-call-routed/research.md`
- Error handling gaps: `context/archive/error-hardening/research.md`
- Lessons: `context/foundation/lessons.md`

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles. See `references/progress-format.md`.

### Phase 1: SSE Parser Edge Cases

#### Automated

- [x] 1.1 Empty `data:` line is silently skipped, stream continues — 162f690
- [x] 1.2 Bare `[DONE]` (no `data:` prefix) triggers `flushPending` and `message_stop` — 162f690
- [x] 1.3 Multi-line `data:` with invalid concatenated JSON falls back to the first valid line — 162f690
- [x] 1.4 `mage test` passes, `mage lint` passes — 162f690

#### Manual

- [ ] 1.5 Send a mock SSE stream with empty `data:` lines through the proxy, verify the stream completes normally
- [ ] 1.6 Send a mock SSE stream with bare `[DONE]`, verify the stream terminates cleanly

### Phase 2: Emitter State Machine Fixes

#### Automated

- [x] 2.1 `TestStream_FinishBeforeUsage_UsesPendingFinish` still passes (exactly 1 `message_delta`) — 162f690
- [x] 2.2 `TestStream_TextStream` — verify `message_delta.usage` contains `input_tokens` — 162f690
- [x] 2.3 Stream with usage+finish same chunk then `[DONE]` → exactly 1 `message_delta` — 162f690
- [x] 2.4 Stream closes without `[DONE]` → `message_stop` is emitted — 162f690
- [x] 2.5 `mage test` passes, `mage lint` passes — 162f690

#### Manual

- [ ] 2.6 Run a real streaming request with a provider that sends usage in the same chunk as finish_reason, verify exactly one `message_delta`

### Phase 3: Mid-Stream Error Handling

#### Automated

- [x] 3.1 `TestStream_ErrorEvent_Emitted` — `emitError` produces correct SSE event bytes — 162f690
- [x] 3.2 `TestOpenAICompat_MidStreamError_EmitsErrorEvent` — adapter emits error event on mid-stream failure — 162f690
- [x] 3.3 `mage test` passes, `mage lint` passes — 162f690

#### Manual

- [ ] 3.4 Kill an upstream mid-stream and verify the client receives an error SSE event

### Phase 4: Integration & Edge-Case Tests

#### Automated

- [x] 4.1 All new tests from Phases 1-3 pass — 162f690
- [x] 4.2 `mage test -race` passes with no race conditions — 162f690
- [x] 4.3 `mage lint` passes with no new warnings — 162f690

#### Manual

- [ ] 4.4 Run `mage test` and verify all streaming tests pass, including the new edge case tests