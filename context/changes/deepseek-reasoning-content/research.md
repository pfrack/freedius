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
last_updated_note: "Added follow-up research from opencode, free-claude-code, claude-code-switch, and SDK analysis"
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

## Follow-up Research 2026-06-17T22:23Z — Open-Source Project Analysis

### Key Finding: The Signature Theory is WRONG

**Claude Code does NOT strip thinking blocks that lack a `signature` field.** It sends them back as-is. The research confirms:

1. Claude Code does NOT validate signatures client-side — it passes them through to the API
2. FCC also emits thinking blocks WITHOUT signatures, and its approach works
3. The real issue is different (see below)

### The Actual Solution: Two Viable Approaches

Based on analyzing all three projects plus Anthropic/OpenAI SDKs:

---

#### Approach A: Strip thinking from history (FCC's primary approach)

FCC's `sanitize_native_messages_thinking_policy` **removes unsigned thinking blocks** from the message history before forwarding upstream:

```python
# When thinking is enabled, strip unsigned thinking blocks
sanitized_content = [block for block in content
    if not (isinstance(block, dict) and block.get("type") == "thinking"
            and not isinstance(block.get("signature"), str))]
```

This means: even though Claude Code sends thinking blocks back, FCC **strips them** before converting to OpenAI format. So the `reasoning_content` field is NOT populated from old thinking blocks — it only comes from the provider's own response.

**But wait** — then how does DeepSeek get its `reasoning_content` back? FCC's `ReasoningReplayMode.REASONING_CONTENT` mode DOES reconstruct it from thinking parts. The subtlety: FCC strips blocks without valid signatures, but if Claude Code sends the block back with the text intact (even without signature), it might still work.

The key: **FCC's actual guard is the ReasoningReplayMode.DISABLED option.** Most FCC users who hit this error **disable thinking entirely** for the opencode.ai/DeepSeek provider.

---

#### Approach B: Always inject reasoning_content on assistant messages (OpenCode's approach)

OpenCode (anomalyco/opencode) — a TypeScript AI agent using Vercel AI SDK — solves this definitively:

```typescript
// transform.ts: DeepSeek-specific — FORCE reasoning part on ALL assistant messages
if (model.api.id.toLowerCase().includes("deepseek")) {
  msgs = msgs.map((msg) => {
    if (msg.role !== "assistant") return msg
    if (Array.isArray(msg.content)) {
      if (msg.content.some((part) => part.type === "reasoning")) return msg
      return { ...msg, content: [...msg.content, { type: "reasoning", text: "" }] }
    }
  })
}

// Then for interleaved field models, extract reasoning to provider field:
const reasoningText = reasoningParts.map((part) => part.text).join("")
return {
  ...msg,
  content: filteredContent,
  providerOptions: { openaiCompatible: { reasoning_content: reasoningText } }
}
```

**The critical insight**: OpenCode ALWAYS sets `reasoning_content` on every assistant message — even as empty string `""` — because DeepSeek requires it once thinking mode is active. Comment in code: *"Always set the field even when empty — some providers (e.g. DeepSeek) may return empty reasoning_content which still needs to be sent back in subsequent requests."*

---

#### Approach C: Use DeepSeek's native Anthropic endpoint (claude-code-switch's approach)

claude-code-switch is just a shell script that points `ANTHROPIC_BASE_URL` at `https://api.deepseek.com/anthropic`. DeepSeek provides its own Anthropic-compatible endpoint that handles reasoning internally — no proxy needed.

**Not applicable to freedius** since we're proxying opencode.ai/zen/go (OpenAI format), not DeepSeek directly.

---

### Project-by-Project Findings

#### 1. anomalyco/opencode (TypeScript, Vercel AI SDK)

- **NOT a proxy** — it's a standalone AI coding agent
- Stores `reasoning_content` as `ReasoningPart { type: "reasoning", text: "..." }` in message history
- For Anthropic direction: includes `signature` from `part.encrypted` or `providerMetadata` (captures `signature_delta` SSE events)
- For DeepSeek direction: no signatures — extracts reasoning to `reasoning_content` field
- **Key design**: Forces empty `reasoning_content` on ALL DeepSeek assistant messages even if no reasoning occurred

#### 2. Alishahryar1/free-claude-code (Python)

- IS a proxy (Anthropic SSE ↔ OpenAI-compat)
- Emits thinking blocks WITHOUT `signature` (confirmed)
- Has `ReasoningReplayMode` enum: `DISABLED`, `THINK_TAGS`, `REASONING_CONTENT`
- `sanitize_native_messages_thinking_policy`: strips unsigned thinking blocks from history
- **Config escape hatch**: `enable_thinking: false` completely disables reasoning flow
- When thinking disabled: reasoning_content from upstream is silently dropped, no thinking blocks emitted, no reasoning sent back → error disappears

#### 3. foreveryh/claude-code-switch (Bash)

- Pure env-var switcher — zero translation logic
- Points Claude Code at provider Anthropic-compatible endpoints (e.g. `api.deepseek.com/anthropic`)
- Irrelevant to our proxy architecture

#### 4. openai/openai-go SDK

- Has NO typed `reasoning_content` field on `ChatCompletionMessage`
- Only has `ReasoningEffort` as request param
- `reasoning_content` is a **non-standard DeepSeek extension** — must be handled via raw JSON

#### 5. anthropics/anthropic-sdk-go

- Defines `ThinkingBlock` with **required** `Signature string` field
- Has `SignatureDelta` type for streaming
- Has `ThinkingConfigDisabledParam` for disabling thinking
- Confirms: Anthropic API requires signatures for multi-turn thinking round-trip

---

### What Claude Code Actually Does with Thinking Blocks

From GitHub issues research:

1. **Claude Code preserves thinking blocks in memory during a session** and sends them back in subsequent requests
2. It does NOT validate `signature` client-side — it just passes blocks through
3. Multiple open issues (#63147, #63246, #63269, etc.) show Claude Code has bugs with thinking block persistence (saving empty text + keeping signature → corrupts on resume)
4. **For non-Anthropic backends**: Claude Code expects the same thinking block format regardless — no special handling for custom endpoints
5. **The `clear_thinking_20251015` server strategy**: newer models keep thinking from all turns; older models only keep last turn's thinking

**Critical implication**: If we emit thinking blocks without signatures, Claude Code WILL send them back in the next request. Our Phase 6 code SHOULD see them. The error likely means something else is wrong.

---

### Root Cause Analysis (Revised)

Given that:
- FCC also doesn't include signatures and its users still hit the error (they work around it by disabling thinking)
- Claude Code DOES send thinking blocks back (even unsigned ones)
- Our Phase 6 code DOES convert thinking blocks to `reasoning_content`

The remaining possibilities for why the error persists:

1. **The upstream (opencode.ai) strips `reasoning_content` from requests** — even though we include it, the upstream doesn't forward it to DeepSeek (Phase 8 evidence supports this: 200-with-empty-body when injecting)

2. **Claude Code sends thinking as `content` array blocks but our deserializer doesn't handle the exact format** — need to log what Claude Code actually sends

3. **Multiple thinking blocks across turns need ALL to be preserved** — we might only extract the first one

4. **Empty reasoning_content ≠ missing reasoning_content** — OpenCode always sends the field even as `""`. If the upstream requires the field to be present (even empty) on ALL assistant messages, and we only set it when thinking text exists, that could cause the error on messages where the model didn't think.

---

### Recommended Solution for Freedius

Based on all evidence, the **minimal fix** is a two-part approach combining insights from OpenCode and FCC:

**Part 1: Always set `reasoning_content` on assistant messages (OpenCode pattern)**

In `TranslateRequest`, when converting assistant messages for DeepSeek models, always include `reasoning_content` — even as empty string:

```go
// After all block processing:
// For DeepSeek: always set reasoning_content (even empty)
if isDeepSeekModel {
    // om.ReasoningContent is already set from thinking blocks or top-level field
    // If still empty, set to "" explicitly so the JSON field is present
}
```

**Part 2: Add `enable_thinking` config option (FCC pattern)**

Add a per-provider config to disable thinking entirely:
- When `false`: drop `reasoning_content` from responses (don't emit thinking blocks), strip thinking blocks from requests
- This gives users an escape hatch if the upstream cannot handle the round-trip

**Part 3: Debug logging**

Add logging to see exactly what Claude Code sends back in the content blocks — this will confirm whether our Phase 6 code is actually receiving thinking blocks or not.

---

### SDK Considerations

- **openai-go**: Cannot be used directly for DeepSeek since it has no `reasoning_content` field. Custom types needed.
- **anthropic-sdk-go**: Could be used to properly type the Anthropic request/response. Its `ThinkingBlock` type enforces `signature` as required — confirms our emit format is incomplete but also confirms this is for the Anthropic API's own validation, not Claude Code's client-side check.

---

### References

- https://github.com/anomalyco/opencode — `packages/opencode/src/provider/transform.ts` (DeepSeek reasoning injection)
- https://github.com/Alishahryar1/free-claude-code — `core/anthropic/sse.py`, `core/anthropic/conversion.py`, `providers/openai_compat.py`
- https://github.com/foreveryh/claude-code-switch — `ccm.sh` (env-var switcher only)
- https://github.com/anthropics/anthropic-sdk-go — `packages/anthropic/message.go` (ThinkingBlock types)
- Claude Code issues: #63147, #63246, #63269, #63277, #63335, #63408, #63475, #63792
