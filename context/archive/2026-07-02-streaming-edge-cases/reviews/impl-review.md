<!-- IMPL-REVIEW-REPORT -->
# Implementation Review: Streaming Edge Cases

- **Plan**: context/changes/streaming-edge-cases/plan.md
- **Scope**: Phase 1 of 4
- **Date**: 2026-07-02
- **Verdict**: NEEDS ATTENTION → FIXED
- **Findings**: 0 critical, 3 warnings, 1 observation

## Verdicts

| Dimension | Verdict |
|-----------|---------|
| Plan Adherence | PASS (after fixes) |
| Scope Discipline | PASS |
| Safety & Quality | PASS |
| Architecture | PASS |
| Pattern Consistency | PASS |
| Success Criteria | PASS (after fixes) |

## Findings

### F1 — Phase 1 items 1.1 and 1.3 not implemented

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Plan Adherence / Success Criteria
- **Location**: proxy/translate/anthropic_openai.go:442-451, 506-508

- **Detail**:
  Two of the three Phase 1 SSE parser fixes are not implemented:

  1. **Empty `data:` lines** (item 1.1): `readSSEEvent` appends empty byte slices to `dataLine` when `data:` has no value. Downstream `json.Unmarshal` on `[]byte{}` fails with `unexpected end of JSON input`. No guard clause exists to skip empty data values.

  2. **Multi-line JSON fallback** (item 1.3): `consume` returns immediately on `json.Unmarshal` failure (line 507-508). No split-retry logic exists — when multiple `data:` lines within a single SSE event produce concatenated invalid JSON like `{"a":1}\n{"b":2}`, the stream fails instead of falling back to the first valid line.

  Phase 1 progress checkboxes are marked `[x]` but these code changes are absent from the diff. The existing `TestStream_NonDataLine_Ignored` and `TestStream_MultilineData` tests cover different scenarios (non-data lines and separate SSE events, not empty data values or intra-event concatenation).

- **Fix**: Implement the two missing parser fixes as described in the plan:
  1. Add `if len(value) == 0 { continue }` after `bytes.TrimLeft(value, " ")` in `readSSEEvent`.
  2. Add newline-split fallback in `consume` after `json.Unmarshal` fails.
  - Strength: Matches the plan's intent; closes real gaps where providers send empty keepalive lines or multi-line payloads.
  - Tradeoff: A few lines of code; tests needed for both cases.
  - Confidence: HIGH — plan specifies exact contract for both fixes.
  - Blind spot: Haven't tested with real providers that send empty `data:` lines.
  - **Decision**: FIXED

### F2 — Bare `[DONE]` handled via EOF path, not explicit detection

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Adherence
- **Location**: proxy/translate/anthropic_openai.go:436-465

- **Detail**:
  Plan item 1.2 specifies: "In `readSSEEvent`, before checking for `data:` prefix, check if the trimmed line equals `[DONE]`. If so, return `[]byte("[DONE]")`."

  Actual behavior: bare `[DONE]` (no `data:` prefix) is skipped by the parser because it doesn't match `bytes.HasPrefix(trimmed, []byte("data:"))`. The stream ends via the EOF → `flushPending` path, which emits `message_stop`. The outcome is correct (stream terminates cleanly), but the mechanism differs from the plan.

  The test `TestStream_BareDone_DetectsNoPrefix` passes, but the name is misleading — the parser doesn't "detect" bare `[DONE]`; it just ignores it and EOF handles cleanup.

- **Fix**: Add `bytes.Equal(trimmed, []byte("[DONE]"))` check in `readSSEEvent` before the `data:` prefix check, returning `[]byte("[DONE]")` as planned.
  - Strength: Explicit detection matches the plan; makes the parser behavior predictable.
  - Tradeoff: One extra `bytes.Equal` call per non-data line.
  - Confidence: HIGH — exact contract specified in plan.
  - Blind spot: None significant.
  - **Decision**: FIXED for unfixed Phase 1 items

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Success Criteria
- **Location**: N/A (missing test functions)

- **Detail**:
  The plan's Testing Strategy and Phase 4 specify tests that should verify the Phase 1 fixes:
  - `TestStream_EmptyDataLine_Skipped` — not implemented
  - `TestStream_MultilineInvalidJSON_Fallback` — not implemented

  These tests are absent from the diff and don't exist in the codebase. Since the corresponding code fixes (F1) are also missing, the tests would fail if added. Once F1 is fixed, these tests should be added to prevent regression.

- **Fix**: Add the two missing test functions after implementing F1. Tests should cover:
  1. Stream with `data:\n\n` (empty data line) — verify stream completes normally.
  2. Stream with `data: {"a":1}\ndata: {"b":2}\n\n` (concatenated data within single event) — verify first valid line is parsed.
  - Strength: Closes test coverage gap; prevents regression on these edge cases.
  - Tradeoff: Two new test functions.
  - Confidence: HIGH — tests specified in plan.
  - Blind spot: None significant.
  - **Decision**: FIXED (resolved with F1) `EmitError` method exported correctly

- **Severity**: ℹ️ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Pattern Consistency
- **Location**: proxy/translate/anthropic_openai.go:805-818

- **Detail**:
  Plan item 3.1 specifies `emitError` (unexported). Actual implementation is `EmitError` (exported, uppercase) to allow cross-package access from `proxy/openai_compat.go`. This is a reasonable deviation — the method must be exported for the adapter to use it. The adapter correctly creates a new emitter via `NewAnthropicEmitter()` and calls `EmitError(err.Error())`. No issue here; noting for completeness.

- **Fix**: None needed. The export is correct for the usage pattern.
  - **Decision**: ACCEPTED

## Summary

All findings triaged and fixed. Phase 1 SSE parser now handles empty `data:` lines, bare `[DONE]` sentinels, and multi-line JSON fallback. Phase 2 (Emitter State Machine) and Phase 3 (Mid-Stream Error Handling) were already fully implemented.

Tests pass, linter clean. Coverage at 84.9% for `proxy/translate`.
