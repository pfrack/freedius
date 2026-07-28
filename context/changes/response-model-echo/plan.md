# Response Model Echo-Back Implementation Plan

## Overview

When Claude Code sends a request with `model: "claude-opus-4-20250514"`, the response should echo back that same model name — regardless of which upstream provider actually served the request. Currently, Freedius passes through the upstream provider's model name (e.g., `deepseek-v4-pro`), which causes inconsistent model names between turns when fallback chains fire.

## Current State Analysis

Two response paths exist:

1. **OpenAI-translate path** (`proxy/openai_compat.go` → `proxy/translate/anthropic_openai.go`):
   - `translate.Stream()` creates an internal `emitter` struct
   - The emitter captures `chunk.Model` from the OpenAI upstream response (line 508-509)
   - Emits that model in `emitMessageStart()` (line 590: `"model": e.model`)
   - The mapping's `ModelString` is used for the REQUEST to upstream, but the RESPONSE model is whatever comes back

2. **Anthropic-compat path** (`proxy/anthropic_compat.go`):
   - Pipes upstream response headers + body directly through (`io.Copy`)
   - The model field in the response body is whatever the Anthropic-compatible upstream returns
   - No rewriting happens

The dispatcher already has the original request model (`req.Model` at `proxy/proxy.go:196`) and sets `X-Freedius-Matched-Model` header. The original request model string is available at dispatch time but never passed into the adapter/translator.

### Key Discoveries:

- `translate.Stream` signature: `func Stream(upstream io.Reader, downstream io.Writer, flush func() error) error` — no model parameter (line 397)
- `newAnthropicEmitter()` creates emitter with empty model (line 484-490); model gets set from first upstream chunk with a non-empty model field
- The `Provider.Handle` interface receives `mapping config.Mapping` which has `ModelString` (the upstream target model), but NOT the original client-requested model
- `X-Freedius-Matched-Model` header is set to `mapping.ModelString` (the upstream target), not the original request model

## Desired End State

After this change:
- Claude Code sends `model: "claude-opus-4-20250514"` and always receives `model: "claude-opus-4-20250514"` back — regardless of which fallback provider served the request
- The `X-Freedius-Matched-Provider` and `X-Freedius-Matched-Model` headers continue to show what actually happened (provider + real upstream model)
- The dashboard and logs still show the real provider/model for debugging
- No config changes needed

### Verification:

- Run `curl` with `model: "claude-opus-4-20250514"` → response `model` field = `"claude-opus-4-20250514"`
- Trigger fallback → response `model` field still = `"claude-opus-4-20250514"` (stable across providers)
- `X-Freedius-Matched-Model` header still shows the real upstream model

## What We're NOT Doing

- NOT changing mapping key naming conventions
- NOT adding config options for this behavior (always echo-back, matches LiteLLM's default)
- NOT changing the Anthropic-compat streaming path's binary passthrough for non-model fields
- NOT modifying `X-Freedius-Matched-Model` header (it correctly shows the real upstream model)

## Implementation Approach

Add the original request model as a parameter that flows from dispatcher → adapter → translator. The emitter uses it as an override: if set, it replaces whatever model the upstream returned.

Two paths need fixing:
1. **OpenAI path**: Add a `ModelOverride` parameter to `translate.Stream()` (or a new `StreamWithModel()` variant). The emitter uses it instead of `chunk.Model`.
2. **Anthropic path**: Since this path does raw `io.Copy`, we need to intercept the `message_start` SSE event and rewrite the model field before forwarding. Alternatively, buffer the first event, rewrite, then pipe the rest.

## Critical Implementation Details

**Anthropic path complexity**: The anthropic-compat adapter currently does a simple `io.Copy(w, resp.Body)`. To rewrite the model field, we need to either:
- Parse the first SSE event (which contains `message_start` with the model field), rewrite it, then pipe the rest unchanged
- Or switch to a line-by-line reader that only modifies the `message_start` event

The first approach is simpler and more robust — only the first `data:` line in an Anthropic stream contains the model field (in the `message_start` event).

---

## Phase 1: OpenAI-Translate Path

### Overview

Add model override to `translate.Stream` so the OpenAI-translate response path echoes back the client's original model name.

### Changes Required:

#### 1. Translate package — add model override to Stream

**File**: `proxy/translate/anthropic_openai.go`

**Intent**: Add a model-override parameter to `Stream` that the emitter uses instead of the upstream model chunk. Keep backward compatibility by making it optional (empty string = current behavior).

**Contract**: New signature: `func Stream(upstream io.Reader, downstream io.Writer, flush func() error, modelOverride string) error`. When `modelOverride != ""`, `emitMessageStart` uses it instead of `e.model`.

#### 2. OpenAI-compat adapter — read original model from request context

**File**: `proxy/openai_compat.go`

**Intent**: Read the original client-requested model name from the request context and pass it to `translate.Stream` as the model override.

**Contract**: The adapter calls `RequestModelFromContext(r.Context())` (new helper, mirrors the existing `RequestIDFromContext` pattern) and passes the result as the `modelOverride` argument to `translate.Stream`.

#### 2b. Dispatcher — store original model in request context

**File**: `proxy/proxy.go`

**Intent**: After parsing `req.Model`, store it in the request context before the fallback loop so all adapters can access it without re-parsing the body or changing the `Provider` interface.

**Contract**: Add a `contextKey` constant (e.g., `requestModelKey`) and a `WithRequestModel(ctx, model)` / `RequestModelFromContext(ctx)` helper pair, following the same pattern as the existing `requestIDKey` / `RequestIDFromContext`.

#### 3. Update test coverage

**File**: `proxy/translate/anthropic_openai_test.go`

**Intent**: (a) Update all existing `Stream()` call sites (~4 calls) to pass `""` as the new `modelOverride` parameter (preserves current behavior). (b) Add new test cases verifying that when modelOverride is set, the `message_start` event uses the override model; when empty, it uses the upstream model.

Also update the single call site in `proxy/openai_compat.go` (already covered in step 2 above).

### Success Criteria:

#### Automated Verification:

- All existing tests pass: `go test ./proxy/translate/...`
- New test verifies model override in streaming: `go test -run TestStream_ModelOverride ./proxy/translate/...`
- Full test suite passes: `go test -race ./...`
- Lint passes: `mage lint`

#### Manual Verification:

- `curl` request with `model: "claude-opus-4-20250514"` through OpenAI-route provider → response model = `"claude-opus-4-20250514"`
- `X-Freedius-Matched-Model` header still shows real upstream model

**Implementation Note**: After completing this phase and all automated verification passes, pause for manual confirmation.

---

## Phase 2: Anthropic-Compat Path

### Overview

Rewrite the model field in the Anthropic-compat adapter's response stream so it echoes back the client's original model name.

### Changes Required:

#### 1. Anthropic-compat adapter — intercept and rewrite model in response

**File**: `proxy/anthropic_compat.go`

**Intent**: Replace the raw `io.Copy` with a model-rewriting path that handles both streaming and non-streaming responses.

**Contract**: Two response formats must be handled, distinguished by Content-Type:

**Non-streaming** (`application/json`): Response is a single JSON body with a top-level `model` field. Read full body, unmarshal into a `map[string]any`, rewrite `model` key, re-marshal with `json.Marshal` (NOT `json.NewEncoder` — see SSE encoding lesson in lessons.md), write to client.

**Streaming** (`text/event-stream`): The stream format is `event: message_start\ndata: {...}\n\n` followed by content events. Only the first `data:` payload contains a top-level `message.model` field. After rewriting that one event, the rest is piped through unchanged (no performance impact on long streams).

Approach:
- Check `resp.Header.Get("Content-Type")` before writing
- If `application/json`: read body → unmarshal → rewrite `model` → `json.Marshal` → write
- If `text/event-stream`: read first SSE event using bufio reader → if `message_start`, unmarshal → rewrite `message.model` → `json.Marshal` → write event → `io.Copy` rest
- Fallback: if Content-Type is neither or parsing fails, fall through to raw `io.Copy` (graceful degradation)

#### 2. Pass original model to Anthropic adapter via context

**File**: `proxy/anthropic_compat.go`

**Intent**: Read the original request model from `RequestModelFromContext(r.Context())` (set by the dispatcher in Phase 1, step 2b). Use it as the replacement value when rewriting the model field in the response.

#### 3. Test coverage

**File**: `proxy/anthropic_compat_test.go`

**Intent**: Add test verifying that the response model field is rewritten to the original request model, not the upstream model.

### Success Criteria:

#### Automated Verification:

- All existing tests pass: `go test ./proxy/...`
- New test verifies model rewrite in Anthropic path: `go test -run TestAnthropicCompat_ModelOverride ./proxy/...`
- Full test suite passes: `go test -race ./...`
- Lint passes: `mage lint`

#### Manual Verification:

- `curl` request through an Anthropic-route provider (e.g., Zen with `/v1/messages` path) → response model = original request model
- Fallback chain fires, different provider responds → model field stable across turns

**Implementation Note**: After completing this phase and all automated verification passes, pause for manual confirmation.

---

## Testing Strategy

### Unit Tests:

- `translate.Stream` with empty modelOverride → uses upstream model (regression)
- `translate.Stream` with modelOverride set → uses override in `message_start`
- Anthropic adapter model rewrite with well-formed `message_start` event
- Anthropic adapter model rewrite when upstream response has unexpected format (graceful fallback to passthrough)

### Integration Tests:

- Full request through dispatcher → OpenAI adapter → verify response model matches request model
- Full request through dispatcher → Anthropic adapter → verify response model matches request model
- Fallback chain: primary fails, secondary responds → verify response model is stable

### Manual Testing Steps:

1. Start Freedius with a mapping where opus→go (OpenAI path)
2. Send request with `model: "claude-opus-4-20250514"`
3. Verify response `model` field = `"claude-opus-4-20250514"`
4. Kill the primary provider, trigger fallback
5. Verify response `model` field is still `"claude-opus-4-20250514"`

## Performance Considerations

- **OpenAI path**: Zero overhead — the model override is just a string assignment in the emitter
- **Anthropic path**: Minimal overhead — one JSON parse/rewrite of the first SSE event (~200 bytes), then raw `io.Copy` for the rest of the stream. No impact on long streaming responses.

## References

- Emitter model field: `proxy/translate/anthropic_openai.go:508-509, 590`
- Stream function: `proxy/translate/anthropic_openai.go:397`
- OpenAI adapter response streaming: `proxy/openai_compat.go:155`
- Anthropic adapter response passthrough: `proxy/anthropic_compat.go:126-132`
- Dispatcher request model extraction: `proxy/proxy.go:196`
- LiteLLM same fix (precedent): github.com/BerriAI/litellm/pull/19943

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles. See `references/progress-format.md`.

### Phase 1: OpenAI-Translate Path

#### Automated

- [x] 1.1 All existing translate tests pass
- [x] 1.2 New Stream model override test passes
- [x] 1.3 Full test suite passes with race detection
- [x] 1.4 Lint passes

#### Manual

- [ ] 1.5 curl request through OpenAI-route provider returns original request model

### Phase 2: Anthropic-Compat Path

#### Automated

- [ ] 2.1 All existing proxy tests pass
- [ ] 2.2 New Anthropic model override test passes
- [ ] 2.3 Full test suite passes with race detection
- [ ] 2.4 Lint passes

#### Manual

- [ ] 2.5 curl request through Anthropic-route provider returns original request model
- [ ] 2.6 Fallback chain fires, model field stable across turns
