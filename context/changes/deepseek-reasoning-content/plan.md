# Fix reasoning_content Round-Trip for Thinking Models — Implementation Plan

## Overview

Ensure `reasoning_content` is present on assistant messages with `tool_calls` sent to upstream OpenAI-compatible providers that use thinking mode (DeepSeek, Kimi K2, MiniMax M3 via OpenAI format). The fix is generic: if any response in the conversation included `reasoning_content`, all assistant messages that carry `tool_calls` must include the field (at minimum a single space `" "`).

## Problem Statement

DeepSeek (and Kimi, MiniMax) return `reasoning_content` on streaming deltas. Once thinking mode is active, assistant messages **with `tool_calls`** in subsequent requests must include `reasoning_content` as a non-empty string. Our `openAIMessage` struct uses `json:"reasoning_content,omitempty"`, which omits the field when empty. The upstream then rejects with 400: "reasoning_content must be passed back."

Additionally, Claude Code sends thinking blocks back in subsequent requests (confirmed by research), but if reasoning was empty or the thinking block had no text, our conversion produces `""` which gets omitted by `omitempty`.

## Root Cause

```go
type openAIMessage struct {
    ReasoningContent string `json:"reasoning_content,omitempty"` // ← omitempty drops ""
}
```

When any assistant message in the history has `ReasoningContent == ""`, the JSON marshaling drops the field entirely. The upstream provider sees a message without `reasoning_content` and errors.

## Critical Gotchas Discovered (from open-source research)

### G1: DeepSeek requires NON-EMPTY string, not just present

DeepSeek validates that `reasoning_content` is a **non-empty string**. Values that FAIL:
- Field omitted entirely → 400
- `null` → 400
- `""` (empty string) → 400

Values that PASS:
- `" "` (single space) → accepted (minimum placeholder)
- Actual reasoning text → accepted

**Implication**: We CANNOT use `*string` pointing to `""`. We must use `" "` (space) as the minimum placeholder.

### G2: Only required on assistant messages WITH tool_calls

DeepSeek's actual rule is nuanced:
- Assistant messages **without tool_calls**: `reasoning_content` is optional — will be IGNORED if sent
- Assistant messages **with tool_calls**: `reasoning_content` is REQUIRED and must be non-empty
- Once a tool-call message has reasoning, ALL subsequent requests must include it forever

**Implication**: The simple "if any has reasoning, all must" approach is overly broad. The minimal correct approach: if an assistant message has `tool_calls`, ensure `reasoning_content` is present (at minimum `" "`).

### G3: deepseek-reasoner (R1) has OPPOSITE requirement

- `deepseek-reasoner` / R1: reasoning_content must **NOT** be passed back (causes 400 if you do)
- `deepseek-v4-pro` / V4: reasoning_content **MUST** be passed back

This means we cannot blindly always inject reasoning. However, since our proxy translates from Anthropic format (Claude Code's history), the presence of thinking blocks in history IS the signal that reasoning was active. If Claude Code sends thinking blocks back, we convert them. If not, we don't inject.

### G4: Multiple thinking blocks are concatenated

OpenCode joins multiple reasoning parts with no delimiter. FCC joins with `"\n"`. We should concatenate with `"\n"` for readability.

### G5: Kimi K2 / Moonshot has the same requirement

Same error, same fix needed. Not just DeepSeek.

### G6: The problem only exists on the REQUEST side

The response path (SSE emission) already works. The bug is entirely in how we reconstruct `reasoning_content` from Claude Code's thinking blocks when building the upstream request.

### G7: FCC's "unsafe tool follow-up" guard

If conversation has tool_use history but NO replayable thinking before tool_use, FCC DISABLES thinking entirely for that request (rather than risking a 400). This is a defensive fallback.

### G8: Interleaved thinking — positional info is lost

If an assistant message has reasoning → tool_call → reasoning → tool_call, ALL reasoning parts get concatenated into one `reasoning_content` field. The positional information is lost. This is acceptable per DeepSeek's API.

## Solution Design

Based on gotchas, revised approach:

1. **Change `ReasoningContent` to `*string`** on `openAIMessage` — nil omits via omitempty, non-nil serializes.

2. **In `convertOneMessage` for assistant messages**: collect ALL thinking block texts (concatenated with `"\n"`). If any thinking was found OR `m.ReasoningContent` is set, set `om.ReasoningContent` to the collected text.

3. **Post-pass: enforce reasoning on tool_call messages**:
   - If an assistant message has `ToolCalls` AND `ReasoningContent` is nil, set it to `strPtr(" ")` (single space placeholder)
   - This satisfies DeepSeek's "must be non-empty on tool_call messages" requirement
   - Only applied when the conversation contains ANY reasoning (i.e., at least one assistant message already has non-nil ReasoningContent) — this avoids injecting reasoning for non-thinking models

4. **Do NOT universally inject on all assistant messages** — only on those with tool_calls when the conversation is in thinking mode. This avoids breaking deepseek-reasoner (R1) or non-thinking models.

## What We're NOT Doing

- NOT adding `enable_thinking` config option (future change, separate scope)
- NOT generating fake Anthropic `signature` fields on thinking blocks (confirmed unnecessary)
- NOT changing the response/SSE emission path (already works)
- NOT modifying config.go or adding new config fields
- NOT handling the "disable thinking for unsafe tool follow-ups" guard (FCC complexity, deferred)
- NOT handling deepseek-reasoner R1 specifically (it doesn't use thinking blocks in Anthropic format, so Claude Code won't send them back)

## Phase 1: Change `ReasoningContent` to `*string` on `openAIMessage`

### Changes Required

#### 1.1 Update type definition

**File**: `proxy/translate/types.go`

Change:
```go
ReasoningContent string `json:"reasoning_content,omitempty"`
```
To:
```go
ReasoningContent *string `json:"reasoning_content,omitempty"`
```

This affects `openAIMessage` only. The `anthropicMsgItem` (line 68) and `openAIDelta` (line 95) remain `string` — they are deserialization-only types, never marshaled to upstream.

Add helper in `anthropic_openai.go`:
```go
func strPtr(s string) *string { return &s }
```

#### 1.2 Update `convertOneMessage` for assistant role

**File**: `proxy/translate/anthropic_openai.go`

In the assistant message conversion block:

1. Collect ALL thinking blocks (concatenate with `"\n"`):
```go
case "thinking":
    thinking, _ := b["thinking"].(string)
    thinkingParts = append(thinkingParts, thinking)
```

2. After block processing, set ReasoningContent from collected thinking:
```go
if len(thinkingParts) > 0 {
    joined := strings.Join(thinkingParts, "\n")
    om.ReasoningContent = strPtr(joined)
}
```

3. Update the fallback from top-level field:
```go
if om.ReasoningContent == nil && m.ReasoningContent != "" {
    om.ReasoningContent = strPtr(m.ReasoningContent)
}
```

#### 1.3 Add post-pass in `convertMessages` to enforce reasoning on tool_call messages

**File**: `proxy/translate/anthropic_openai.go`

After converting all messages, add:
```go
// If any assistant message has reasoning, ensure all assistant messages
// with tool_calls also have it (DeepSeek/Kimi require non-empty reasoning_content
// on tool_call messages once thinking mode is active).
hasReasoning := false
for _, m := range out {
    if m.Role == "assistant" && m.ReasoningContent != nil {
        hasReasoning = true
        break
    }
}
if hasReasoning {
    for i := range out {
        if out[i].Role == "assistant" && len(out[i].ToolCalls) > 0 && out[i].ReasoningContent == nil {
            out[i].ReasoningContent = strPtr(" ")
        }
    }
}
```

The single space `" "` is the minimum acceptable placeholder per DeepSeek's validation (confirmed by LiteLLM and community testing).

### Tests

#### 1.4 Unit test: reasoning_content present on tool_call messages

Test that when an assistant message has tool_calls AND a thinking block, the output JSON contains `"reasoning_content": "<text>"`.

#### 1.5 Unit test: placeholder injected on tool_call messages without thinking

Test that when conversation has reasoning (one message with thinking) but another assistant message has tool_calls without thinking blocks, the placeholder `" "` is injected.

#### 1.6 Unit test: no reasoning_content when no thinking in conversation

Verify that when no assistant messages have thinking blocks, `reasoning_content` is NOT present in the JSON output (nil pointer + omitempty).

#### 1.7 Unit test: multiple thinking blocks concatenated

Test that two thinking blocks `["hello", "world"]` produce `"hello\nworld"`.

#### 1.8 Unit test: assistant message without tool_calls — no injection

Test that assistant messages without tool_calls do NOT get a placeholder, even when the conversation is in thinking mode. (DeepSeek ignores reasoning on non-tool messages.)

## Phase 2: Handle thinking blocks with empty text

### Changes Required

#### 2.1 Accept empty thinking blocks as signal

**File**: `proxy/translate/anthropic_openai.go`

Current code (before Phase 1 change) only sets reasoning when non-empty. With the Phase 1 `thinkingParts` approach, an empty thinking block text still gets appended to `thinkingParts` — the join produces `""`. But per G1, empty string is rejected by DeepSeek.

Fix: if thinkingParts is non-empty but all texts are empty, use `" "` as placeholder:
```go
if len(thinkingParts) > 0 {
    joined := strings.Join(thinkingParts, "\n")
    if strings.TrimSpace(joined) == "" {
        joined = " "
    }
    om.ReasoningContent = strPtr(joined)
}
```

This ensures that even when Claude Code sends back `{"type": "thinking", "thinking": ""}`, the field is present and non-empty.

## Verification

1. `go test ./proxy/translate/...` — all existing + new tests pass
2. `go build ./...` — compiles cleanly
3. Manual: configure a DeepSeek model, start a multi-turn conversation with tool use, verify no 400 error on turn 2+

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles. See `references/progress-format.md`.

### Phase 1: Change ReasoningContent to *string and add post-pass

#### Automated

- [x] 1.1 Change ReasoningContent to *string in types.go + add strPtr helper — e347cd7
- [x] 1.2 Update convertOneMessage to collect thinkingParts and use strPtr — e347cd7
- [x] 1.3 Add post-pass in convertMessages for tool_call messages — e347cd7
- [x] 1.4 Test: reasoning_content present on tool_call messages with thinking — e347cd7
- [x] 1.5 Test: placeholder injected on tool_call messages without thinking — e347cd7
- [x] 1.6 Test: no reasoning_content when no thinking in conversation — e347cd7
- [x] 1.7 Test: multiple thinking blocks concatenated with newline — e347cd7
- [x] 1.8 Test: assistant message without tool_calls — no injection — e347cd7

### Phase 2: Handle thinking blocks with empty text

#### Automated

- [x] 2.1 Empty thinking blocks produce space placeholder — e347cd7
- [x] 2.2 All tests pass: `go test ./proxy/translate/...` — e347cd7
- [x] 2.3 Build passes: `go build ./...` — e347cd7
