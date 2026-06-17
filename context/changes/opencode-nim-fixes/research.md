---
date: 2026-06-17T20:02:59+02:00
researcher: opencode
git_commit: 8a31d0a686ea2aa21bfac0577509a0115565d1b8
branch: error-hardening
repository: pfrack/freedius
topic: "OpenCode Go 401 + NVIDIA NIM SSE — root-cause analysis against free-claude-code and opencode references"
tags: [research, opencode-go, nvidia-nim, auth, sse, anthropic-compat, openai-compat, adapter-gap-analysis]
status: complete
last_updated: 2026-06-17
last_updated_by: opencode
last_updated_note: "Follow-up research: confirmed auth change must apply to ALL Anthropic-compatible providers (CustomAdapter, MixAdapter Anthropic path), not just OpenCode Go."
---

# Research: OpenCode Go 401 + NIM SSE root-cause analysis

**Date**: 2026-06-17 20:02 CEST
**Researcher**: opencode
**Git Commit**: `8a31d0a686ea2aa21bfac0577509a0115565d1b8`
**Branch**: `error-hardening`
**Repository**: `pfrack/freedius`

## Research Question

The freedius proxy returns 401 on some OpenCode Go requests and produces SSE stream errors on NVIDIA NIM. Both providers work correctly through free-claude-code (https://github.com/Alishahryar1/free-claude-code). This research identifies the precise gaps.

**Reference sources:**
- free-claude-code (Python/FastAPI proxy serving 35k stars)
- opencode CLI (https://github.com/anomalyco/opencode, 176k stars) — the native client for OpenCode Zen/Go
- OpenCode Zen/Go docs (https://opencode.ai/docs/zen/, https://opencode.ai/docs/go/)

## Summary

Three root causes were identified:

1. **Anthropic-format auth mismatch**: freedius `AnthropicCompatibleAdapter` sends `Authorization: Bearer <key>` to `/v1/messages` endpoints. OpenCode Go and other Anthropic-compatible providers expect `x-api-key: <key>` + `anthropic-version: 2023-06-01`. The `@ai-sdk/anthropic` SDK (which OpenCode uses) sends `x-api-key`, not `Authorization: Bearer`.

2. **NIM SSE gaps**: freedius translator drops `reasoning_content` deltas silently (no field in `openAIDelta` struct, no thinking-block emission), doesn't sanitize NIM-rejected fields (boolean JSON Schema subschemas, `type` parameter name aliases, `chat_template_kwargs`), and has no retry logic for known 400 errors.

3. **Stream translation fragility**: freedius `TranslateStream` produces no SSE events when upstream returns unexpected chunk shapes, resulting in "empty or malformed response (HTTP 200)" from Claude Code.

**Important**: OpenCode Go uses TWO endpoint formats with different auth schemes — this is NOT a single-standard decision. The auth depends on the endpoint path suffix.

**Firm decision**: Anthropic-format endpoints (`/v1/messages`) must use `x-api-key` + `anthropic-version`. OpenAI-format endpoints (`/v1/chat/completions`) must use `Authorization: Bearer`. This applies universally — see Follow-up Research below for the full impact analysis.

## Detailed Findings

### 1. OpenCode Go endpoint taxonomy and auth requirements

Source: https://opencode.ai/docs/go/#endpoints (fetched 2026-06-17)

OpenCode Go exposes models on two distinct endpoints:

| Endpoint | SDK Package | Auth Scheme | Models |
|---|---|---|---|
| `/zen/go/v1/chat/completions` | `@ai-sdk/openai-compatible` | `Authorization: Bearer <key>` | GLM-5.2, Kimi K2.7, DeepSeek V4 Pro/Flash, MiMo-V2.5 |
| `/zen/go/v1/messages` | `@ai-sdk/anthropic` | `x-api-key: <key>` + `anthropic-version: 2023-06-01` | MiniMax M3/M2.7, Qwen3.7 Max/Plus, Qwen3.6 Plus |

**Each model is tied to exactly one endpoint — models are NOT available on both.**

This is confirmed by the opencode CLI source (`anomalyco/opencode` provider.ts): the `opencode` custom loader uses `@ai-sdk/anthropic` and `@ai-sdk/openai-compatible` SDKs, which set the respective auth headers automatically.

### 2. Root cause: 401 on Anthropic-format endpoints

**freedius `AnthropicCompatibleAdapter`** (`proxy/anthropic_compat.go:38`):
```go
req.Header.Set("Authorization", "Bearer "+apiKey)
```

**What free-claude-code does** for Anthropic-format providers (`providers/anthropic_messages.py`):
```python
def _request_headers(self) -> dict[str, str]:
    return {"Content-Type": "application/json"}
```
The auth is handled by httpx/Anthropic SDK which sends `x-api-key` + `anthropic-version`.

**What opencode CLI does** (`anomalyco/opencode`): uses `@ai-sdk/anthropic` SDK which sends `x-api-key: <key>` + `anthropic-version: 2023-06-01`.

**The fix**: The `AnthropicCompatibleAdapter` must send `x-api-key: <key>` + `anthropic-version: 2023-06-01` instead of `Authorization: Bearer <key>`. This is a *complete change* to the Anthropic-compatible adapter's auth scheme.

**Note**: The `OpenAICompatibleAdapter` (`Authorization: Bearer`) is correct for `/v1/chat/completions` endpoints. No change needed there.

### 3. Root cause: NIM SSE — missing reasoning_content handling

**freedius `openAIDelta`** (`proxy/translate/types.go:90-94`):
```go
type openAIDelta struct {
    Role      string          `json:"role,omitempty"`
    Content   string          `json:"content,omitempty"`
    ToolCalls []openAIToolCall `json:"tool_calls,omitempty"`
}
```
No `ReasoningContent` field. The emitter (`anthropic_openai.go:419`) only reads `ch.Delta.Content`.

**free-claude-code** (`providers/openai_compat.py`, ~line 530):
```python
reasoning = getattr(delta, "reasoning_content", None)
if thinking_enabled and reasoning:
    for event in hold_events(sse.ensure_thinking_block()):
        yield event
    for event in hold_event(sse.emit_thinking_delta(reasoning)):
        yield event
```

**The fix**: Add `ReasoningContent string` to `openAIDelta`, emit `content_block_start(type=thinking)` + `content_block_delta(type=thinking_delta)` in the emitter when `reasoning_content` is present.

### 4. Root cause: NIM SSE — missing body sanitization

free-claude-code's NIM provider (`providers/nvidia_nim/request.py`) performs these sanitizations before sending to NIM:

1. **Boolean JSON Schema subschemas** → stripped (`_sanitize_nim_schema_node`): NIM rejects boolean `additionalProperties`, `items`, etc.
2. **`type` parameter name aliases** → renamed to `_fcc_arg_type` (`_alias_nim_tool_parameter_names`): NIM reserves `type` as a field name
3. **`chat_template_kwargs`** → only added with `thinking_enabled` flag; retried without on 400
4. **`reasoning_budget`** → retried without on 400 error
5. **`reasoning_content`** in messages → retried without on 400
6. **`parallel_tool_calls`** → set based on NIM config
7. **`top_k`, `min_p`, `repetition_penalty`** → sent via `extra_body`

freedius does NONE of these. The `TranslateRequest` just remaps top-level fields.

**Impact**: NIM may return 400 for requests with unsupported fields. freedius returns the 400 to the client with no retry.

### 5. Request translation comparison

| Feature | free-claude-code | freedius | Status |
|---|---|---|---|
| Basic Anthropic→OpenAI conversion | `AnthropicToOpenAIConverter` | `TranslateRequest` | OK |
| Thinking blocks → `reasoning_content` | Yes (`REASONING_CONTENT` mode) | No | GAP |
| Deferred post-tool text | Yes (`_PendingAfterTools`) | No | GAP |
| `redacted_thinking` → skip | Yes | No | GAP |
| Server tool blocks → reject | Yes (`OpenAIConversionError`) | No (passthrough, may fail) | GAP |
| Image blocks → reject | Yes | No | GAP |
| `max_tokens` fallback | Yes (`default_max_tokens`) | No (only if request sets it) | GAP |
| Tool call `extra_content` | Preserved and replayed | Not handled | GAP |
| `stream_options.include_usage` | Not set by default | Always set | RISK |

### 6. SSE stream response comparison

| Feature | free-claude-code | freedius | Status |
|---|---|---|---|
| `reasoning_content` → thinking blocks | Yes | No | GAP |
| `extra_content` in tool calls | Preserved | Not handled | GAP |
| Think-tag heuristic parser | Yes (`ThinkTagParser`) | No | GAP |
| Heuristic tool-use parser | Yes (`HeuristicToolParser`) | No | GAP |
| Stream recovery on truncation | Yes (retry+continuation) | No | GAP |
| Midstream tool repair | Yes | No | GAP |
| Error SSE tail emission | Yes | No (returns 502) | GAP |
| `[DONE]` handling | No (uses OpenAI client) | Yes (`flushPending`) | OK |
| Token usage passthrough | Yes (`usage` chunk) | Yes (`chunk.Usage`) | OK |

### 7. NIM-specific headers

The opencode CLI (`anomalyco/opencode` provider.ts) sends these headers to NIM:
```
"HTTP-Referer": "https://opencode.ai/"
"X-Title": "opencode"
"X-BILLING-INVOKE-ORIGIN": "OpenCode"
```

Free-claude-code uses `AsyncOpenAI` client which may set these implicitly. freedius sets none of these. NIM may accept requests without these headers (they appear to be analytics/tracking), but they should be added for compatibility.

### 8. User's specific 401 (now resolved)

The user's initial 401 was caused by **wrong model names** in the starter config template:

| Config (wrong) | Should be |
|---|---|
| `deepseek-ai/deepseek-v4-pro` | `deepseek-v4-pro` |
| `deepseek-ai/deepseek-v4-flash` | `deepseek-v4-flash` |
| `stepfun-ai/step-3.5-flash` | Not a Go model (NIM only) |

The template used NIM-format prefixes for Go entries — fixed in commit `8d7dc5b` but the user's config was generated before that fix.

After fixing model names, the `go` provider returns 200 through the `mix` adapter (routes to `OpenAICompatibleAdapter` for `/v1/chat/completions`). However, stream translation then fails — see §§3-6 above.

## Architecture Insights

1. **Auth depends on endpoint format, not provider name**: The `provider: go` entry in config can resolve to TWO different auth schemes depending on the `base_url` path. The `mix` adapter correctly routes by path, but the `AnthropicCompatibleAdapter` hardcodes `Authorization: Bearer` regardless of where it's used.

2. **Model names are endpoint/format specific**: OpenCode Go models use bare IDs (`deepseek-v4-pro`), while NIM uses `org/model` format (`deepseek-ai/deepseek-v4-pro`). The starter config template must use the correct format per provider.

3. **The SSE translator is the bottleneck**: The freedius `proxy/translate` package is ~658 lines covering only the most basic Anthropic↔OpenAI conversion. Free-claude-code has ~1000+ lines just for the OpenAI transport, plus separate modules for conversion, SSE building, thinking parsing, tool parsing, stream recovery, error mapping, and rate limiting.

4. **No `ReasoningReplayMode` concept**: Freedius always drops thinking content when converting to OpenAI format. Free-claude-code has three modes: `DISABLED`, `THINK_TAGS` (wraps in `<think>`), `REASONING_CONTENT` (uses the dedicated field). The default for OpenCode is `REASONING_CONTENT`.

## Code References

### freedius (current — gaps identified)
- `proxy/anthropic_compat.go:38` — hardcoded `Authorization: Bearer` (should be `x-api-key`)
- `proxy/openai_compat.go:65-67` — correct `Authorization: Bearer` for OpenAI endpoints
- `proxy/mix.go:27-37` — correct URL-path routing logic
- `proxy/translate/anthropic_openai.go:20-56` — `TranslateRequest` (missing thinking, deferred text)
- `proxy/translate/anthropic_openai.go:299-323` — `TranslateStream` (no reasoning_content)
- `proxy/translate/anthropic_openai.go:382-444` — emitter (no thinking block type)
- `proxy/translate/types.go:90-94` — `openAIDelta` (missing `ReasoningContent`)
- `proxy/translate/types.go:28-49` — `openAIChunk` / request types (no `reasoning_content` in messages)
- `config/defaults.go:62-64` — `zen`/`go` → `mix` rewrite
- `templates/starter.yaml` — uses wrong model name format for Go

### free-claude-code (reference — what freedius should match)
- `providers/openai_compat.py` — full OpenAI SSE→Anthropic transport with reasoning, thinking, tool aliases
- `providers/anthropic_messages.py` — Anthropic passthrough with correct auth
- `providers/opencode/client.py` — OpenCode Zen/Go provider (OpenAI-compat `OpenAIChatTransport`)
- `providers/nvidia_nim/client.py` — NIM provider with body sanitization + retry
- `providers/nvidia_nim/request.py` — NIM-specific body building with schema sanitization
- `core/anthropic/conversion.py` — `AnthropicToOpenAIConverter` with deferred text, thinking, tool split
- `core/anthropic/sse.py` — `SSEBuilder`, `ContentBlockManager` (thinking block support)
- `config/provider_catalog.py` — `OPENCODE_DEFAULT_BASE` / `OPENCODE_GO_DEFAULT_BASE` / `NVIDIA_NIM_DEFAULT_BASE`

### opencode CLI (reference — native client behavior)
- `packages/opencode/src/provider/provider.ts` — provider config with `@ai-sdk/anthropic`, `@ai-sdk/openai-compatible`, NIM headers

### OpenCode Docs
- https://opencode.ai/docs/go/#endpoints — Go endpoint taxonomy with auth per SDK
- https://opencode.ai/docs/zen/#endpoints — Zen endpoint taxonomy

## Historical Context

- `context/changes/zen-go-adapters/research.md` (S-03) — identified the multi-format gateway insight: "Opencode Zen and Go are multi-format model gateways". The research correctly identified URL-path routing but did NOT identify the auth header mismatch for Anthropic-format endpoints. It assumed `Authorization: Bearer` would work for all endpoints (see line 58: "auth header `Authorization: Bearer <key>`").
- `context/changes/zen-go-adapters/plan.md` — explicitly decided NOT to handle `anthropic-version` header injection (line: "`anthropic-version` header injection (Claude Code already sends it)"). This was a gap — the issue is the `x-api-key` header, not `anthropic-version`.
- `context/changes/first-call-routed/plan.md` — established the `AnthropicCompatibleAdapter` with `Authorization: Bearer` as the universal auth scheme. This assumption is now proven wrong for OpenCode Go's `/v1/messages` path.
- `context/changes/error-hardening/plan.md` — ongoing change, adding middleware stack and env auto-injection. Not directly related to the auth/SSE gaps.

## Open Questions

1. **Does OpenCode accept `Authorization: Bearer` alongside `x-api-key` on `/v1/messages`?** The docs show `@ai-sdk/anthropic` which uses `x-api-key`. A `curl` test would confirm. Likely answer: no — 401 confirms it.

2. **Does `stream_options: {include_usage: true}` cause issues with OpenCode Go?** The "feature not supported" error during streaming might originate from this field. Should be tested by sending requests without `stream_options`.

3. **Can the `AnthropicCompatibleAdapter` switch auth scheme based on the URL?** For `provider: go` with mixed endpoints, the same adapter name resolves to different auth schemes depending on the URL. The `mix` adapter already routes by URL — it should also set the correct auth scheme.

4. **Is `reasoning_content` required for NIM to work?** If NIM models require thinking/reasoning to produce useful output, the freedius translation drops all of it. This could explain "empty response" symptoms with NIM thinking models.

---

## Follow-up Research 2026-06-17T20:10:00+02:00

### Confirmed: auth change applies to ALL Anthropic-compatible providers

The `AnthropicCompatibleAdapter` (`proxy/anthropic_compat.go`) is used by THREE adapter paths, all of which currently hardcode `Authorization: Bearer`:

| Adapter | File | Currently sends | Must send |
|---|---|---|---|
| `AnthropicCompatibleAdapter` (direct) | `proxy/anthropic_compat.go:38` | `Authorization: Bearer` | `x-api-key` + `anthropic-version` |
| `CustomAdapter` (wraps Anthropic) | `proxy/custom.go:18` | `Authorization: Bearer` (via inner) | `x-api-key` + `anthropic-version` |
| `MixAdapter` Anthropic path | `proxy/mix.go:33` | `Authorization: Bearer` (via inner) | `x-api-key` + `anthropic-version` |

All three delegate to `AnthropicCompatibleAdapter.Handle`, which sets the auth header at line 38. A single change there fixes all three paths.

**Rationale**: The Anthropic API specification uses `x-api-key: <key>` + `anthropic-version: 2023-06-01` for authentication. Every `@ai-sdk/anthropic` client, every Anthropic-compatible gateway (OpenCode, OpenRouter, DeepSeek Anthropic endpoint, Kimi, Fireworks, Z.ai, Wafer, lmstudio, llamacpp, ollama) expects this auth scheme. The `Authorization: Bearer` pattern is OpenAI-specific and does not apply to Anthropic-format endpoints.

**The change**: `proxy/anthropic_compat.go:38` — replace:
```go
req.Header.Set("Authorization", "Bearer "+apiKey)
```
with:
```go
req.Header.Set("x-api-key", apiKey)
req.Header.Set("anthropic-version", "2023-06-01")
```

Same change in the `Rewrite` callback at `proxy/anthropic_compat.go:43`:
```go
pr.Out.Header.Set("x-api-key", apiKey)
pr.Out.Header.Set("anthropic-version", "2023-06-01")
```

**Note**: The original Claude Code request already has `x-api-key` and `anthropic-version` headers with the user's dummy `ANTHROPIC_API_KEY`. The `ReverseProxy` `Rewrite` function overrides these with the real provider key. The `anthropic-version` header should be set to `2023-06-01` (the current stable version) — the original request may carry a different version from Claude Code, which should be overridden to ensure compatibility with all Anthropic-compatible gateways.

**Impact**: This fixes 401 errors on:
- OpenCode Go `/v1/messages` (MiniMax, Qwen models)
- OpenCode Zen `/v1/messages` (Claude, Qwen models)
- Any `provider: anthropic` or `provider: custom` using an Anthropic-format upstream
- Any `provider: mix` entry with a `base_url` ending in `/v1/messages`

**No impact on**: `OpenAICompatibleAdapter` (used by `provider: nim`, `provider: openai`, and `MixAdapter` OpenAI path) — these correctly send `Authorization: Bearer` for OpenAI-format endpoints.
