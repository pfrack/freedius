# Streaming Edge Cases — Plan Brief

> Full plan: `context/changes/streaming-edge-cases/plan.md`
> Research: `context/changes/streaming-edge-cases/research.md`

## What & Why

Fix 2 high-severity bugs, 3 medium-severity gaps, and 4 low-severity issues in the freedius streaming pipeline. The SSE parser crashes on empty `data:` lines, the emitter sends duplicate `message_delta` events, and mid-stream translation errors cause silent truncation. Clients experience hangs, corrupted streams, and opaque failures.

## Starting Point

The streaming pipeline has three layers: `readSSEEvent` (SSE line-by-line parser), `emitter` (state machine translating OpenAI chunks to Anthropic events), and `OpenAICompatibleAdapter.Handle` (adapter that wires them together). The research at `context/changes/streaming-edge-cases/research.md` traced root causes for all bugs with exact line references.

## Desired End State

The SSE parser is resilient to empty `data:` lines and bare `[DONE]`. The emitter never emits duplicate `message_delta` events, includes `input_tokens` in usage, and flushes pending state on EOF. The adapter emits a structured SSE error event on mid-stream failures. The `Stream` function signature is clean (no dead `reasoning` return value).

## Key Decisions Made

| Decision | Choice | Why (1 sentence) | Source |
| --- | --- | --- | --- |
| Empty `data:` lines | Skip as no-op | Prevents stream termination from provider keepalives; minimal code change. | Plan |
| Bare `[DONE]` | Detect as safety net | Protects against non-OpenAI providers that send `[DONE]` without `data:` prefix. | Plan |
| EOF without `[DONE]` | Always flush pending | Guarantees `message_stop` emission; clients never hang on stream close. | Plan |
| Mid-stream errors | Emit structured error event | Clients get a clean `event: error` instead of silent truncation. | Plan |
| Dead `reasoning` return | Remove from signature | Cleans up dead code; no current use case. | Plan |
| `input_tokens` in `message_delta` | Add to usage map | Matches Anthropic API spec; clients that validate schema expect it. | Plan |
| `pendingFinish` clear | After `emitFinish` | One-liner prevents duplicate `message_delta` when `[DONE]` arrives after deferred finish. | Research |
| Multi-line data concat | Fall back to first valid line | Prevents parse failure when each `data:` line is valid JSON but concatenated is not. | Plan |

## Scope

**In scope:** SSE parser resilience (empty data, bare [DONE], multi-line fallback), emitter state machine fixes (pendingFinish clear, EOF flush, input_tokens), mid-stream error SSE event, dead `reasoning` removal, comprehensive tests

**Out of scope:** Non-SSE Content-Type detection on 200, structured error for Anthropic-compat adapter, `message_start` usage `{0,0}` fix, SSE `event:` type preservation, explicit state machine enumeration

## Architecture / Approach

Fix bottom-up: SSE parser first (foundation), then emitter state machine (core logic), then adapter error handling (integration layer). Each phase has its own tests. All changes are in `proxy/translate/anthropic_openai.go` and `proxy/openai_compat.go` — no new files, no config changes.

## Phases at a Glance

| Phase | What it delivers | Key risk |
| --- | --- | --- |
| 1. SSE Parser Edge Cases | Empty data: skip, bare [DONE] detection, multi-line fallback | Regression in existing SSE parsing tests |
| 2. Emitter State Machine Fixes | pendingFinish clear, EOF flush, input_tokens, dead reasoning removal | Duplicate message_delta regression test needs careful ordering |
| 3. Mid-Stream Error Handling | Structured error SSE event on stream failure | Edge case: error event after partial stream data |
| 4. Integration & Edge-Case Tests | 7 new unit tests + 1 integration test | Test setup for mid-stream error integration test |

**Prerequisites:** None — all fixes are self-contained in two files.
**Estimated effort:** ~2-3 sessions across 4 phases

## Open Risks & Assumptions

- The `emitError` method assumes the response writer is still writable after stream failure — if the upstream connection is broken both ways, the error event may not reach the client (acceptable, same as current behavior)
- The multi-line data fallback (keep first valid line) assumes the first line is the meaningful one — this is a best-effort heuristic
- Removing the `reasoning` return value is a breaking change to the `Stream` function signature, but only `OpenAICompatibleAdapter.Handle` calls it

## Success Criteria (Summary)

- All existing streaming tests pass with no regressions
- Empty `data:` lines, bare `[DONE]`, and EOF without `[DONE]` are handled gracefully
- Exactly one `message_delta` per stream, `input_tokens` included in usage
- Mid-stream errors produce an SSE `error` event, not silent truncation
- `mage test -race` and `mage lint` pass cleanly