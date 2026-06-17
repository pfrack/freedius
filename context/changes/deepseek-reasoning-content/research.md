---
date: 2026-06-17T22:19:29+02:00
researcher: opencode
git_commit: f7c8689e5f7f7b5555a6964fce17ec01456cbc65
branch: opencode-nim-fixes
repository: freedius
topic: "DeepSeek reasoning_content must be passed back error"
tags: [research, reasoning, deepseek, thinking, opencode-go]
status: complete
last_updated: 2026-06-17
last_updated_by: opencode
---

# Research: DeepSeek "reasoning_content must be passed back" Error

**Date**: 2026-06-17T22:19:29+02:00
**Researcher**: opencode
**Git Commit**: f7c8689e5f7f7b5555a6964fce17ec01456cbc65
**Branch**: opencode-nim-fixes
**Repository**: freedius

## Research Question

Why does the "The `reasoning_content` in the thinking mode must be passed back to the API" 400 error persist on multi-turn conversations despite all fixes (Phases 2–9), and how does free-claude-code (FCC) avoid this?

## Summary

The error originates from DeepSeek (via opencode.ai/zen/go) when a request contains assistant messages that lack the `reasoning_content` field after a prior response included thinking. Three things must be true for this to not error:

1. The proxy MUST emit thinking blocks in the SSE response when DeepSeek sends `reasoning_content` deltas (Phase 2 ✓)
2. Claude Code MUST preserve those thinking blocks in the subsequent request's message history
3. The proxy MUST convert those thinking blocks back to `reasoning_content` on the OpenAI message (Phase 6 ✓)

The core finding is that **Claude Code likely strips thinking blocks without an Anthropic `signature` field** before sending them back in requests. Our thinking block SSE (`content_block_start(type=thinking)`) omits the `signature` field that Anthropic's API requires for round-tripping. Without the signature, Claude Code may discard the thinking blocks, leaving our Phase 6 conversion with nothing to convert.

## Detailed Findings

### 1. Our Current Implementation

The full round-trip path:

**Response direction** (`proxy/translate/anthropic_openai.go:429-431`):
```go
if ch.Delta.ReasoningContent != "" {
    ev, err := e.emitThinkingDelta(ch.Delta.ReasoningContent)
```
Emits Anthropic SSE: `content_block_start(type=thinking)` + `content_block_delta(type=thinking_delta)`.
No `signature` field is included (`proxy/translate/anthropic_openai.go:547-557`).

**Request direction** (`proxy/translate/anthropic_openai.go:244-248`):
```go
case "thinking":
    thinking, _ := b["thinking"].(string)
    if thinking != "" {
        om.ReasoningContent = thinking
    }
```
Converts `thinking` content blocks → `reasoning_content` on the OpenAI message.

Plus fallback for top-level field (`proxy/translate/anthropic_openai.go:273-275`):
```go
if om.ReasoningContent == "" && m.ReasoningContent != "" {
    om.ReasoningContent = m.ReasoningContent
}
```

**Known missing piece:** Our `anthropicMessage` struct (`proxy/translate/types.go:52-63`) has no `Thinking` field. If Claude Code sends `thinking: {type: "enabled"}` as a top-level request parameter, it's silently dropped by `json.Unmarshal`. This matters because it would prevent us from detecting or controlling the upstream's thinking mode.

### 2. Anthropic Extended Thinking Protocol

From [Anthropic's extended-thinking docs](https://docs.anthropic.com/en/docs/build-with-claude/extended-thinking#extended-thinking-with-tool-use):

> **"During tool use, you must pass `thinking` blocks back to the API for the last assistant message. Include the complete unmodified block back to the API to maintain reasoning continuity."**

Thinking blocks in the Anthropic API have:
```json
{
  "type": "thinking",
  "thinking": "the reasoning text",
  "signature": "hex-encoded-signature"  // required for round-trip
}
```

The `signature` is an encryption artifact that the API uses to verify and reconstruct the thinking state. Without it, the API cannot verify the thinking continuity and **the client may not include the thinking block in subsequent requests**.

**Key implication for our proxy**: We emit thinking blocks without `signature`. If Claude Code checks for `signature` before including thinking blocks in the next request, it **strips them** because they lack the required field. This would explain why Phase 6 finds no thinking blocks to convert.

### 3. free-claude-code's Approach

FCC uses an identical round-trip pattern:

**Response** -- emits thinking blocks from `reasoning_content` deltas:
```python
# providers/openai_compat.py:164-170
reasoning = getattr(delta, "reasoning_content", None)
if thinking_enabled and reasoning:
    for event in hold_events(sse.ensure_thinking_block()):
        yield event
    for event in hold_event(sse.emit_thinking_delta(reasoning)):
        yield event
```

**Request** -- reconstructs `reasoning_content` from thinking blocks:
```python
# core/anthropic/conversion.py:273-276
if reasoning_replay == ReasoningReplayMode.REASONING_CONTENT:
    replay_reasoning = reasoning_content or "\n".join(thinking_parts)
    if replay_reasoning:
        msg["reasoning_content"] = replay_reasoning
```

FCC also does NOT include `signature` in thinking blocks.

**If FCC also doesn't include signatures, and it works**, then the `signature` theory is wrong, or FCC works differently in practice.

### 4. FCC's Key Difference: Configurable Thinking

FCC has a **config-level `enable_thinking: bool`** that defaults to `True` in `ProviderConfig` (`providers/base.py:21`). When set to `False`:

- Response: `reasoning_content` deltas are **silently dropped** from the SSE output
- Request: `ReasoningReplayMode.DISABLED` strips all thinking blocks
- Result: the upstream's thinking mode is effectively ignored

FCC users can disable thinking per-provider in the Admin UI. If a user disables thinking for OpenCode Go, the "reasoning_content must be passed back" error disappears entirely because:
1. No thinking blocks are emitted → Claude Code's responses have no thinking
2. No `reasoning_content` is sent in requests → DeepSeek doesn't see a mismatch

FCC also checks the Anthropic request's `thinking` parameter:
```python
# providers/base.py:42-56
thinking_type = thinking.get("type") if isinstance(thinking, dict) else ...
if thinking_type == "disabled":
    request_enabled = False
```

So a client can disable thinking per-request by sending `thinking: {type: "disabled"}`.

### 5. The opencode.ai Upstream

The upstream `https://opencode.ai/zen/go/v1/chat/completions` is an OpenAI-compatible endpoint. It forwards requests to DeepSeek. DeepSeek's models natively support reasoning/thinking and return `reasoning_content` in streaming deltas.

Three possible upstream behaviors:
1. **Upstream always enables thinking for deepseek-v4-flash**: DeepSeek always returns reasoning_content and always requires it back → we can't avoid the requirement
2. **Upstream enables thinking based on request params**: If we control what gets sent, we might be able to disable it
3. **Upstream strips `reasoning_content` from requests**: Even if we include it, the upstream might not forward it to DeepSeek → the field is always missing on DeepSeek's end

### 6. What We Know For Sure

| Aspect | Status | Evidence |
|--------|--------|----------|
| Thinking blocks in SSE response | ✓ Works | Phase 2 implemented and tested |
| Thinking → reasoning_content in request | ✓ Works | Phase 6 implemented and tested (both content-block and top-level field) |
| Top-level reasoning_content on msgItem | ✓ Works | Phase 9 added to anthropicMsgItem |
| Claude Code preserves thinking | ✗ Likely NOT | Error persists after Phase 6 ensures conversion is correct |
| signature field in thinking blocks | ✗ MISSING | Neither Phase 2 nor FCC includes it |
| Configurable thinking control | ✗ MISSING | No enable_thinking config in freedius |

## Code References

- `proxy/translate/anthropic_openai.go:429-431` — Reasoning content delta handling in consume
- `proxy/translate/anthropic_openai.go:547-588` — emitThinkingDelta (no signature)
- `proxy/translate/anthropic_openai.go:244-248` — Thinking block → reasoning_content conversion
- `proxy/translate/anthropic_openai.go:273-275` — Top-level reasoning_content fallback
- `proxy/translate/types.go:52-63` — anthropicMessage struct (no Thinking field)
- `proxy/translate/types.go:65-69` — anthropicMsgItem struct (ReasoningContent added in Phase 9)
- `proxy/openai_compat.go:83-87` — Upstream 400 error forwarding
- `proxy/openai_compat.go:55` — TranslateRequest call (where conversion happens)

## Architecture Insights

1. **Thinking mode is a session-level property**: Once a model is in thinking mode, ALL assistant messages need `reasoning_content`. This is enforced by DeepSeek, not by our proxy or the upstream.

2. **Two failure modes**: When reasoning_content is missing from the request:
   - If the upstream forwards to DeepSeek → DeepSeek returns 400 with "must be passed back"
   - If the upstream rejects at its own layer → returns 200 with empty body (seen with Phase 8 injection)

3. **The Anthropic extended thinking signature**: The `signature` field in thinking blocks is Anthropic-specific and designed for Claude models. Its absence in our thinking blocks may cause Claude Code to discard them. However, FCC also omits it and reportedly works.

4. **Config-based escape hatch**: FCC's `enable_thinking` config option allows users to opt out of thinking entirely, bypassing the requirement. Freedius has no equivalent option.

## Historical Context

- `context/changes/opencode-nim-fixes/plan.md` — Phase 2 (thinking emission), Phase 6 (request conversion), Phase 7 (merge into text, reverted), Phase 8 (cache injection, reverted), Phase 9 (top-level reasoning_content)
- The fix attempt progression shows increasing understanding: from "just emit thinking" (P2) → "convert back" (P6) → "merge into text to avoid round-trip" (P7, reverted) → "inject from cache" (P8, reverted due to upstream stripping) → "capture top-level field" (P9, current)

## Open Questions

1. **Does Claude Code actually require `signature` on thinking blocks?** If yes, we need to add it. If no, the error comes from elsewhere.
2. **Does the upstream (opencode.ai) forward `reasoning_content` to DeepSeek?** Phase 8's 200-with-empty-body suggests it strips or rejects the field internally.
3. **Does FCC actually avoid this error in practice?** Or do FCC users also see it but accept it as a limitation of certain providers?
4. **Could we disable thinking mode by not forwarding the `thinking` parameter from Claude Code's request?** We don't capture this parameter, so we can't control it.
5. **Would adding an `enable_thinking: false` config option fix the issue?** If the upstream's thinking mode is triggered by request parameters we control, we could disable it. But if the upstream always enables it for the model, we can't.
