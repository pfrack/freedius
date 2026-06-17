# OpenCode Go 401 + NIM SSE Fixes — Implementation Plan

## Overview

Fix three root causes that prevent OpenCode Go Anthropic-format models (MiniMax, Qwen on `/v1/messages`) and NIM streaming from working through freedius: wrong auth headers causing 401, missing reasoning content in SSE causing empty responses, and lack of provider-aware translation + NIM body sanitization.

## Current State Analysis

The research (`research.md`) mapped three root causes with precise code references:

1. **Auth**: `proxy/anthropic_compat.go:38,43` hardcodes `Authorization: Bearer <key>`. OpenCode Go `/v1/messages` and all Anthropic-format endpoints expect `x-api-key: <key>` + `anthropic-version: 2023-06-01`. The `OpenAICompatibleAdapter` (`Authorization: Bearer`) is correct — only `AnthropicCompatibleAdapter` needs the fix. A single 2-line change at lines 38 and 43 fixes all three Anthropic call paths (direct, CustomAdapter wrapper at `proxy/custom.go:18`, MixAdapter Anthropic path at `proxy/mix.go:33`).

2. **SSE reasoning**: `openAIDelta` (`proxy/translate/types.go:90-94`) has no `ReasoningContent` field. The emitter (`proxy/translate/anthropic_openai.go:382-444`) only reads `Delta.Content` — NIM reasoning deltas are silently dropped, producing empty response streams for thinking models.

3. **stream_options + sanitization**: `TranslateRequest` (`anthropic_openai.go:41-43`) unconditionally injects `stream_options: {include_usage: true}` when streaming. Some providers reject it ("feature not supported"). NIM also rejects requests with boolean JSON Schema subschemas in tool definitions and `type`-named parameters.

The user's logs confirm: after fixing model names (NIM prefix on Go entries), stream translation errors appear — the "feature not supported" log at `openai_compat.go:87` and "empty or malformed response (HTTP 200)" from Claude Code.

### Key Discoveries

- `AnthropicCompatibleAdapter` is a simple `httputil.ReverseProxy` passthrough — no body translation, no stream processing. Auth fix is purely header replacement (`proxy/anthropic_compat.go:38,43`).
- `OpenAICompatibleAdapter` already has correct auth (`Authorization: Bearer`) and full request/response translation pipeline — the right base for NIM and Go OpenAI-format calls.
- The emitter (`anthropic_openai.go:382`) already handles `[DONE]`, usage chunks, and `flushPending` for deferred finish — adding thinking blocks extends an existing state machine.
- `NIMAdapter` (`proxy/nim.go:1-20`) is a thin wrapper delegating to `OpenAICompatibleAdapter` — the right injection point for NIM-specific sanitization.
- `zen`/`go` rewrite to `mix` at `config/defaults.go:62-64` — Go Anthropic-format routing goes through `MixAdapter.Anthropic` → `AnthropicCompatibleAdapter`.

## Desired End State

After Phase 1: `curl -H "x-api-key: $KEY" proxy/v1/messages` → routing through `provider: go` or `provider: anthropic` succeeds (200, no 401). Existing tests pass with updated header assertions.

After Phase 2: NIM streaming with a thinking model produces `content_block_start(type=thinking)` + `content_block_delta(type=thinking_delta)` + `content_block_stop` SSE events. Claude Code renders reasoning content natively.

After Phase 3: NIM requests with complex tool schemas (boolean `additionalProperties`, param named `type`) succeed instead of returning 400. `stream_options` is omitted for providers that reject it. All 7 free-claude-code NIM sanitization steps adapted to freedius's architecture.

## What We're NOT Doing

- **NOT handling** deferred post-tool text (`_PendingAfterTools` in free-claude-code)
- **NOT handling** `redacted_thinking` content blocks (skip emitter)
- **NOT adding** stream recovery / retry-on-truncation (free-claude-code complexity, deferred)
- **NOT adding** think-tag parser / heuristic tool parser
- **NOT adding** NIM-specific headers (`HTTP-Referer`, `X-Title`, `X-BILLING-INVOKE-ORIGIN`) — these appear to be analytics tracking, not functional
- **NOT changing** `OpenAICompatibleAdapter` auth header (already correct)
- **NOT adding** live OpenCode Go integration tests (deferred per user decision; existing tests updated)
- **NOT implementing** provider-specific `chat_template_kwargs`, `reasoning_budget`, `parallel_tool_calls`, or `extra_body` (`top_k`/`min_p`/`repetition_penalty`) — these are free-claude-code additions that freedius's architecture doesn't inject; the Anthropic body from Claude Code doesn't contain them

## Implementation Approach

Three orthogonal fixes shipped in dependency order:

1. **Auth** (Phase 1) — the simplest fix with the widest blast radius. Ships first so any regressions emerge before SSE/NIM changes layer on top.
2. **SSE reasoning** (Phase 2) — additive emitter logic. Depends on stream not being broken (Phase 3 fixes `stream_options` for NIM), but unit-testable with mock SSE chunks independently.
3. **stream_options + NIM sanitization** (Phase 3) — the largest change. Adds provider-awareness to `TranslateRequest` and a post-translation sanitization hook.

## Critical Implementation Details

- **Auth header conflict**: The original Claude Code request already has `x-api-key` and `anthropic-version` headers (with the dummy `ANTHROPIC_API_KEY`). The `ReverseProxy.Rewrite` function MUST `pr.Out.Header.Set("x-api-key", apiKey)` and `pr.Out.Header.Set("anthropic-version", "2023-06-01")` in the OUTBOUND rewrite to override with the real provider key. Use `Set` not `Add` to replace any existing value. Also remove the old `Authorization` header on the outbound request (`pr.Out.Header.Del("Authorization")`).

- **Emitter block type transitions**: The emitter tracks `openBlock` as a string (`"text"`, `"tool"`, or `""`). Adding `"thinking"` means the `emitText` method's existing block-close logic (line 487-493: "if e.openBlock != 'text', close current, open new") must also close a `"thinking"` block when switching to text. The new `emitThinkingDelta` similarly closes any non-thinking open block before emitting `content_block_start(type=thinking)`. Block index increments are shared across all block types via `e.blockIndex++`.

- **NIM sanitization timing**: Sanitization runs AFTER `TranslateRequest` converts Anthropic→OpenAI and BEFORE the upstream HTTP request is sent. This keeps the sanitizer operating on OpenAI-format JSON (tool schemas preserved through translation) and avoids coupling to Anthropic format.

## Phase 1: Fix Anthropic Auth Headers

### Overview

Replace `Authorization: Bearer <key>` with `x-api-key: <key>` + `anthropic-version: 2023-06-01` in `AnthropicCompatibleAdapter`. This fixes 401 for all Anthropic-format endpoints: OpenCode Go `/v1/messages`, OpenCode Zen `/v1/messages`, any `provider: anthropic`, any `provider: custom`, and MixAdapter Anthropic path.

### Changes Required

#### 1.1 Replace auth headers in AnthropicCompatibleAdapter

**File**: `proxy/anthropic_compat.go`

**Intent**: Change the auth scheme from OpenAI-style `Authorization: Bearer` to Anthropic-style `x-api-key` + `anthropic-version`. This is the single source of truth for all three Anthropic call paths.

**Contract**: Lines 38 and 43 — replace `req.Header.Set("Authorization", "Bearer "+apiKey)` / `pr.Out.Header.Set("Authorization", "Bearer "+apiKey)` with `req.Header.Set("x-api-key", apiKey)` followed by `req.Header.Set("anthropic-version", "2023-06-01")`. In the `Rewrite` callback, also remove the stale `Authorization` header from the outbound request: `pr.Out.Header.Del("Authorization")`.

```go
// Line 38 (inbound request, before ReverseProxy):
r.Header.Set("x-api-key", apiKey)
r.Header.Set("anthropic-version", "2023-06-01")

// Line 43 (outbound rewrite, inside Rewrite callback):
pr.Out.Header.Set("x-api-key", apiKey)
pr.Out.Header.Set("anthropic-version", "2023-06-01")
pr.Out.Header.Del("Authorization")
```

#### 1.2 Update AnthropicCompatAdapter tests

**File**: `proxy/anthropic_compat_test.go`

**Intent**: Verify the upstream request now carries `x-api-key` and `anthropic-version` headers, not `Authorization: Bearer`.

**Contract**: In the `TestAnthropicCompat_PassthroughText` upstream server handler, assert `r.Header.Get("x-api-key")` equals the env var value and `r.Header.Get("anthropic-version")` equals `"2023-06-01"`. Assert `r.Header.Get("Authorization")` is empty.

#### 1.3 Add MixAdapter Anthropic path header test

**File**: `proxy/mix_test.go`

**Intent**: Verify the MixAdapter routes `/v1/messages` to AnthropicCompAd with correct headers. The existing `TestMixAnthropicTextPassThrough` test should already cover this implicitly through the inner adapter, but it doesn't explicitly verify the headers on the upstream request.

**Contract**: In `TestMixAdapter_AnthropicPassthrough`'s upstream server handler (line 23), REPLACE the existing `Authorization: Bearer sk-test` assertion at lines 24-26 with: `x-api-key == "sk-test"`, `anthropic-version == "2023-06-01"`, `Authorization == ""`. Do NOT change `TestMixAdapter_OpenAITranslation` at lines 54-57 — that path uses `OpenAICompatibleAdapter` which correctly keeps `Authorization: Bearer`.

#### 1.4 Add CustomAdapter header test

**File**: `proxy/custom_test.go`

**Intent**: Verify the CustomAdapter (which wraps AnthropicCompatAdapter) passes `x-api-key` + `anthropic-version` to the upstream.

**Contract**: In `TestCustomAdapter_PassthroughText`'s upstream server handler (line 36), REPLACE the existing `Authorization: Bearer sk-test` assertion at lines 37-39 with: `x-api-key == "sk-test"`, `anthropic-version == "2023-06-01"`, `Authorization == ""`.

### Success Criteria

#### Automated Verification

- `go build ./...` compiles cleanly
- `go vet ./...` passes with no new warnings
- `go test ./proxy/... -run "AnthropicCompat|MixAnthropic|CustomAdapter" -v` — all tests pass with updated header assertions
- `make ci` passes (vet + test + build)

#### Manual Verification

- Start freedius with a Go config that routes a model to an Anthropic-format endpoint: `freedius serve`
- Send a `curl` request to the proxy — verify no 401 response, `x-api-key` header reaches the upstream

---

## Phase 2: Add SSE Reasoning/Thinking Support

### Overview

Add `ReasoningContent` to the `openAIDelta` struct and emit `content_block_start(type=thinking)` + `content_block_delta(type=thinking_delta)` + `content_block_stop` SSE events when `reasoning_content` deltas arrive from the upstream. Clone the existing text-block state machine pattern.

### Changes Required

#### 2.1 Add ReasoningContent to openAIDelta

**File**: `proxy/translate/types.go`

**Intent**: Capture `reasoning_content` from OpenAI stream deltas so the emitter can convert them to Anthropic thinking blocks.

**Contract**: Add `ReasoningContent string \`json:"reasoning_content,omitempty"\`` to the `openAIDelta` struct (after `Content`, before `ToolCalls`).

#### 2.2 Add thinking block emission to emitter

**File**: `proxy/translate/anthropic_openai.go`

**Intent**: When `ch.Delta.ReasoningContent` is non-empty, emit a `content_block_start(type=thinking)` if not already in a thinking block, then emit `content_block_delta(type=thinking_delta, thinking: <text>)`. Handle transitions between thinking and text blocks (close current block before opening the new one).

**Contract**: In the `consume` method (line 419), add a check for `ch.Delta.ReasoningContent` before `ch.Delta.Content`. Call a new `e.emitThinkingDelta(reasoning)` method. The method follows the same pattern as `emitText` (lines 485-523):
- If `e.openBlock != "thinking"`, close the current block via `emitBlockStop`, increment `e.blockIndex`, emit `content_block_start` with `type: "thinking"` and `thinking: ""`, set `e.openBlock = "thinking"`
- Emit `content_block_delta` with `type: "thinking_delta"` and `thinking: text`
- The `emitBlockStop` method at line 611 already handles `"text"` and `"tool"` as recognized block types — add `"thinking"` to the condition on line 613

```go
// In consume(), after role check, before content check:
if ch.Delta.ReasoningContent != "" {
    ev, err := e.emitThinkingDelta(ch.Delta.ReasoningContent)
    if err != nil {
        return nil, err
    }
    events = append(events, ev...)
}
```

```go
// New method following emitText pattern:
func (e *emitter) emitThinkingDelta(thinking string) ([][]byte, error) {
    var events [][]byte
    if e.openBlock != "thinking" {
        if e.openBlock != "" {
            ev, err := e.emitBlockStop()
            if err != nil {
                return nil, err
            }
            events = append(events, ev...)
        }
        start := map[string]any{
            "type":  "content_block_start",
            "index": e.blockIndex,
            "content_block": map[string]any{
                "type":     "thinking",
                "thinking": "",
            },
        }
        ev, err := e.emit("content_block_start", start)
        if err != nil {
            return nil, err
        }
        events = append(events, ev...)
        e.blockIndex++
        e.openBlock = "thinking"
    }
    delta := map[string]any{
        "type":  "content_block_delta",
        "index": e.blockIndex - 1,
        "delta": map[string]any{
            "type":     "thinking_delta",
            "thinking": thinking,
        },
    }
    ev, err := e.emit("content_block_delta", delta)
    if err != nil {
        return nil, err
    }
    events = append(events, ev...)
    return events, nil
}
```

#### 2.3 Add tests for reasoning content deltas

**File**: `proxy/translate/anthropic_openai_test.go`

**Intent**: Verify that OpenAI stream chunks with `reasoning_content` produce well-formed Anthropic thinking block SSE events.

**Contract**: Test cases:
1. Single reasoning delta → `content_block_start(type=thinking)` + `content_block_delta(type=thinking_delta, thinking=<content>)` + `content_block_stop` before `[DONE]`
2. Multiple reasoning deltas → single `content_block_start` followed by multiple `content_block_delta` events (block stays open)
3. Reasoning → text transition → `content_block_stop` for thinking, `content_block_start(type=text)`, then text deltas
4. Text → reasoning transition → `content_block_stop` for text, `content_block_start(type=thinking)`, then thinking deltas

### Success Criteria

#### Automated Verification

- `go build ./...` compiles cleanly
- `go vet ./...` passes
- `go test ./proxy/translate/... -v` — all new and existing translate tests pass
- `make ci` passes

#### Manual Verification

- Set up NIM with a thinking model (e.g., DeepSeek R1) through freedius
- Route a Claude Code request through freedius to NIM
- Verify Claude Code shows reasoning/thinking content in its native thinking UI
- Verify no "empty or malformed response (HTTP 200)" error

---

## Phase 3: Provider-Aware Stream Options + NIM Body Sanitization

### Overview

Add provider-aware `TranslateRequest` options so adapters can disable `stream_options.include_usage`. Implement NIM body sanitization (boolean JSON Schema subschemas, `type` param aliasing) as a post-translation hook on `OpenAICompatibleAdapter`. The `NIMAdapter` configures its inner adapter with `NoStreamUsage: true` and the sanitization hook.

### Changes Required

#### 3.1 Add TranslateOpts to TranslateRequest

**File**: `proxy/translate/anthropic_openai.go`

**Intent**: Allow callers to control whether `stream_options.include_usage` is injected. Defaults to preserving current behavior (include when streaming).

**Contract**: Add a `TranslateOpts` struct with `NoStreamUsage bool`. Change `TranslateRequest` signature from `func TranslateRequest(anthropicBody []byte, targetModel string) ([]byte, error)` to `func TranslateRequest(anthropicBody []byte, targetModel string, opts TranslateOpts) ([]byte, error)`. At line 41-43, only set `StreamOptions` when `!opts.NoStreamUsage`.

```go
type TranslateOpts struct {
    NoStreamUsage bool
}

func TranslateRequest(anthropicBody []byte, targetModel string, opts TranslateOpts) ([]byte, error) {
    // ...
    if req.Stream && !opts.NoStreamUsage {
        out.StreamOptions = &openAIStreamOpts{IncludeUsage: true}
    }
    // ...
}
```

Update all callers: `proxy/openai_compat.go:53` — pass `a.translateOpts`. **Mechanical update**: 25 callers of the form `TranslateRequest(in, "x")` in `proxy/translate/anthropic_openai_test.go` all become `TranslateRequest(in, "x", TranslateOpts{})` — a single `Replace-All` suffices. Update test callers in `proxy/openai_compat_test.go` and `proxy/nim_test.go` similarly.

#### 3.2 Add translateOpts and preSendHook fields to OpenAICompatibleAdapter

**File**: `proxy/openai_compat.go`

**Intent**: Let wrapper adapters (like NIMAdapter) configure translation behavior and post-translation body sanitization without overriding the entire `Handle` method.

**Contract**: Add two unexported fields to `OpenAICompatibleAdapter`:
- `translateOpts translate.TranslateOpts`
- `preSendHook func([]byte) ([]byte, error)`

At line 53, pass `a.translateOpts` to `TranslateRequest`. After line 56 (successful translation), add:
```go
if a.preSendHook != nil {
    upstreamBody, err = a.preSendHook(upstreamBody)
    if err != nil {
        return fmt.Errorf("%s adapter (openai-compat): sanitize body: %w", originalOr(m), err)
    }
}
```

#### 3.3 Wire NIMAdapter with NoStreamUsage and sanitization hook

**File**: `proxy/nim.go`

**Intent**: NIM rejects `stream_options.include_usage` (research §5, item "RISK") and boolean JSON Schema subschemas (`type` param aliasing). Configure the inner `OpenAICompatibleAdapter` accordingly.

**Contract**: In `NewNIMAdapter`, after constructing the inner adapter, set `inner.translateOpts = translate.TranslateOpts{NoStreamUsage: true}` and `inner.preSendHook = sanitizeNIMBody`.

#### 3.4 Implement NIM body sanitization

**New file**: `proxy/nim_sanitize.go`

**Intent**: Strip boolean JSON Schema subschemas from tool `function.parameters` and rename `type`-named parameters to `_fcc_arg_type` — the two sanitization steps from free-claude-code that are applicable to freedius's architecture.

**Contract**: Export `func sanitizeNIMBody(openAIBody []byte) ([]byte, error)`.

The function:
1. Parses the OpenAI JSON body into a `map[string]any`
2. Walks `tools[]` entries, for each tool, finds `function.parameters` and applies:
   - `stripBooleanSubschemas(node)` — recursively walks the schema tree. For each key in `properties`, `additionalProperties`, `items`, `anyOf`, `oneOf`, `allOf`: if the value is a boolean, remove it (NIM rejects boolean schema nodes). If it's an object, recurse.
   - `aliasTypeParams(node)` — walks `properties` of schema objects. For each property where the property name is `"type"`, renames it to `"_fcc_arg_type"` (deletes old key, inserts new). Recurse into nested `properties`, `additionalProperties`, `items`.
3. Re-marshals to JSON and returns

### Success Criteria

#### Automated Verification

- `go build ./...` compiles cleanly
- `go vet ./...` passes
- `go test ./proxy/... -v` — existing OpenAI adapter tests pass (empty opts = default behavior)
- New tests in `proxy/nim_sanitize_test.go`:
  - Boolean `additionalProperties: true` in schema → stripped from output
  - Boolean `additionalProperties: false` in schema → stripped from output
  - Schema with `additionalProperties: {}` → preserved as-is
  - Tool parameter named `type` → renamed to `_fcc_arg_type`
  - Nested `type` params → renamed recursively
  - `type` JSON Schema key (not a param name) → preserved
  - No-tools body → returned unchanged
- **New end-to-end test in `proxy/nim_test.go`**: send a streaming request with a tool schema containing `additionalProperties: true` through `NIMAdapter`, capture the upstream request body, assert (a) `stream_options` is absent (verifies `NoStreamUsage: true` reaches inner adapter) and (b) `additionalProperties: true` is stripped (verifies `preSendHook` is wired). This guards against future refactors that drop either field on the inner `OpenAICompatibleAdapter`.
- `make ci` passes

#### Manual Verification

- Configure freedius with NIM provider and a model that supports tools
- Send a Claude Code request with tool definitions that include `additionalProperties: true` in a schema
- Verify NIM returns 200 (not 400)
- Verify streaming works (SSE events are well-formed)

---

## Testing Strategy

### Unit Tests (Phase 1)

- `proxy/anthropic_compat_test.go` — verify `x-api-key` + `anthropic-version` headers on upstream request, verify `Authorization` is absent
- `proxy/mix_test.go` — verify Anthropic path (`/v1/messages`) sends correct headers
- `proxy/custom_test.go` — verify CustomAdapter passes correct headers via inner adapter

### Unit Tests (Phase 2)

- `proxy/translate/anthropic_openai_test.go` — reasoning deltas → thinking block events (4 scenarios: single delta, multi delta, thinking→text transition, text→thinking transition)

### Unit Tests (Phase 3)

- `proxy/nim_sanitize_test.go` — boolean subschema stripping (true/false/object), `type` param renaming (flat/nested/non-param), no-tools passthrough
- `proxy/translate/anthropic_openai_test.go` — `NoStreamUsage: true` omits `stream_options`; `NoStreamUsage: false` includes it; zero-value opts preserves current behavior
- `proxy/openai_compat_test.go` — hook applied (body modified) vs no hook (body unchanged)

### Manual Testing Steps

1. Start freedius with OpenCode Go config, send curl to a `/v1/messages`-routed model, verify 200 (not 401)
2. Start freedius with NIM config for a thinking model, send Claude Code request through it, verify thinking content appears
3. Send a Claude Code request with tools that have boolean schema properties through NIM, verify no 400

**Blast-radius verified**: `proxy/phase2_test.go` (301 lines, constructs both adapters) is unaffected by all three phases — no Authorization assertions (Phase 1), no `TranslateRequest` callers (Phase 3), no constructor signature changes.

**Phase 2 SSE blast-radius verified**: 19 `TestTranslateStream_*` tests in `proxy/translate/anthropic_openai_test.go` + `TestMixAdapter_OpenAITranslation` in `proxy/mix_test.go` are unaffected. Zero references to `reasoning_content` / `ReasoningContent` exist in the codebase, so (a) the new emit branch is gated by `if ch.Delta.ReasoningContent != ""` and never triggers for existing tests, (b) the `openAIDelta` struct addition is JSON-backward-compatible (existing chunks without `reasoning_content` unmarshal to empty string), and (c) `emitBlockStop`'s new `"thinking"` condition only activates when `openBlock == "thinking"` — an unreachable state in current tests.

---

## References

- Research: `context/changes/opencode-nim-fixes/research.md`
- Free-claude-code reference: `Alishahryar1/free-claude-code/providers/nvidia_nim/request.py` (NIM sanitization), `providers/openai_compat.py` (reasoning_content handling)
- Prior plan: `context/changes/zen-go-adapters/plan.md` (explicitly decided NOT to handle `anthropic-version` header — this plan reverses that)
- Lessons: `context/foundation/lessons.md` (SSE encoding: `json.Marshal` over `json.NewEncoder`; SSE Reader: `bufio.Reader.ReadBytes` over `bufio.Scanner`; Adapter Return Contract: return nil after WriteHeader)

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles.

### Phase 1: Fix Anthropic Auth Headers

#### Automated

- [x] 1.1 `go build ./...` compiles cleanly — 223c7c0
- [x] 1.2 `go vet ./...` passes — 223c7c0
- [x] 1.3 `go test ./proxy/... -run "AnthropicCompat|MixAnthropic|CustomAdapter" -v` — all tests pass with updated header assertions — 223c7c0
- [x] 1.4 `make ci` passes — 223c7c0

#### Manual

- [ ] 1.5 Manual curl test: Anthropic-format endpoint returns 200 (not 401)

### Phase 2: Add SSE Reasoning/Thinking Support

#### Automated

- [x] 2.1 `go build ./...` compiles cleanly
- [x] 2.2 `go vet ./...` passes
- [x] 2.3 `go test ./proxy/translate/... -v` — new and existing translate tests pass
- [x] 2.4 `make ci` passes

#### Manual

- [ ] 2.5 Manual test: NIM thinking model → Claude Code shows reasoning content natively

### Phase 3: Provider-Aware Stream Options + NIM Body Sanitization

#### Automated

- [ ] 3.1 `go build ./...` compiles cleanly
- [ ] 3.2 `go vet ./...` passes
- [ ] 3.3 `go test ./proxy/... -v` — existing tests pass with new TranslateOpts parameter
- [ ] 3.4 New tests: `proxy/nim_sanitize_test.go` — boolean subschema stripping, type param renaming, no-tools passthrough
- [ ] 3.5 New end-to-end test: `proxy/nim_test.go` — NIMAdapter integration verifies `stream_options` is absent AND boolean schema is stripped in upstream body
- [ ] 3.5 `make ci` passes

#### Manual

- [ ] 3.6 Manual test: NIM with tools containing boolean schema → 200 (not 400), streaming works
- [ ] 3.7 Manual test: NIM without tools → streaming works, no `stream_options` in request body
