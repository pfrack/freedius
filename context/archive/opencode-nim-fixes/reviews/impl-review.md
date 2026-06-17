<!-- IMPL-REVIEW-REPORT -->
# Implementation Review: OpenCode Go 401 + NIM SSE Fixes

- **Plan**: `context/changes/opencode-nim-fixes/plan.md`
- **Scope**: Phases 1-8 of 8 (all automated checks complete, manual checks pending)
- **Date**: 2026-06-18
- **Verdict**: NEEDS ATTENTION
- **Findings**: 0 critical, 2 warnings, 1 observation

## Verdicts

| Dimension | Verdict |
|-----------|---------|
| Plan Adherence | WARNING ⚠️ |
| Scope Discipline | PASS ✅ |
| Safety & Quality | PASS ✅ |
| Architecture | PASS ✅ |
| Pattern Consistency | PASS ✅ |
| Success Criteria | PASS ✅ |

## Findings

### F1 — Phase 7 (Merge Thinking into Text) Not Implemented

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Plan Adherence
- **Location**: `proxy/translate/anthropic_openai.go:396`, `proxy/translate/anthropic_openai.go:458`
- **Detail**: Plan specifies Phase 7 adds a `thinkingBuffer` string to the `emitter` struct, accumulates `ReasoningContent` deltas into it instead of emitting separate thinking blocks, and merges the buffer into text content when content arrives. The actual code has no `thinkingBuffer`, and line 458 still calls `e.emitThinkingDelta(ch.Delta.ReasoningContent)`. All 4 reasoning tests still assert separate `"type":"thinking"` / `"type":"thinking_delta"` blocks (Phase 2 behavior). Phase 7 was explicitly added to handle the case where Claude Code strips thinking blocks from conversation history before subsequent requests — without it, reasoning content may not survive round-trips.
- **Fix A ⭐ Recommended**: Implement Phase 7 as designed (buffer reasoning, merge into text on content/tool-call/finish)
  - Strength: Closes the gap between plan and code; addresses the original Claude Code thinking-block-stripping concern.
  - Tradeoff: Claude Code loses native thinking UI (rationale already documented in plan).
  - Confidence: HIGH — plan has detailed implementation contract including exact code.
  - Blind spot: Need to verify current behavior actually triggers the Claude Code stripping issue; may not be reproducible with all providers.
- **Fix B**: Update the plan to deprecate Phase 7, noting that Phase 8's post-pass approach is sufficient
  - Strength: No rework needed; current implementation may be adequate.
  - Tradeoff: Risk of "reasoning_content must be passed back" errors if Claude Code strips thinking blocks in multi-turn conversations.
  - Confidence: LOW — haven't tested multi-turn scenarios without thinking blocks surviving.
  - Blind spot: Phase 8's post-pass only propagates reasoning_content when at least one message has it. If ALL thinking blocks are stripped, it does nothing.
- **Decision**: FIXED via Fix B — will update plan to deprecate Phase 7

### F2 — Phase 8 (Reasoning Cache) Implemented Differently Than Planned

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Plan Adherence
- **Location**: `proxy/translate/anthropic_openai.go:142-176`, `proxy/openai_compat.go:94-96`
- **Detail**: Plan specifies a `sync.Map`-based cross-request reasoning cache with `CacheReasoning`/`LoadCachedReasoning`/`InjectReasoningIntoOpenAI` functions, `TranslateStream` returning accumulated reasoning from responses, and the adapter storing/loading cached reasoning. Actual implementation uses a simpler post-pass in `convertMessages`: if any assistant message in the request has `ReasoningContent`, inject `" "` as reasoning into all tool_call assistant messages that lack it. No `sync.Map` exists; `openai_compat.go:94-96` discards `TranslateStream`'s return value. The actual approach is simpler and avoids cross-request state, but only works when at least one thinking block survives the round-trip in the conversation history.
- **Fix A ⭐ Recommended**: Accept the current implementation as superior — simpler, no cross-request state, and sufficient for the common case. Update the plan to document the chosen approach.
  - Strength: Cleaner architecture; the post-pass in `convertMessages` is the right layer for this transformation. Tests pass.
  - Tradeoff: No protection against the case where ALL thinking blocks are stripped from conversation history.
  - Confidence: HIGH — the approach is well-tested and the `" "` placeholder pattern matches DeepSeek's documented requirement.
  - Blind spot: Haven't verified behavior when thinking blocks are fully stripped (this also affects the planned cache approach if Phase 7 wasn't implemented).
- **Fix B**: Implement the cache as planned, with `CacheReasoning` called after `TranslateStream` and `InjectReasoningIntoOpenAI` called after `TranslateRequest`
  - Strength: Provides cross-request caching for robustness even if thinking blocks are stripped.
  - Tradeoff: Adds sync.Map package-level state, increases complexity, duplicates some logic that `convertMessages` already handles.
  - Confidence: MEDIUM — plan has detailed contracts but `TranslateStream` currently returns `""` so caching infrastructure would need wiring.
  - Blind spot: Not clear whether cross-request caching is actually needed for any real provider scenario.
- **Decision**: FIXED via Fix A — will update plan to document current post-pass approach

### F3 — No-op Reassignment in NIM Sanitizer

- **Severity**: 👁️ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Safety & Quality
- **Location**: `proxy/nim_sanitize.go:47-52`
- **Detail**: The properties-walking loop unconditionally re-assigns each child map to itself (`props[name] = child`), which is a no-op.
- **Fix**: Remove the assignment. The child map values already exist in `props` from the type assertion on the preceding line; no mutation is needed.
- **Decision**: FIXED — removed redundant `properties` iteration loop
