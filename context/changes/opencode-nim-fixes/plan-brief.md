# OpenCode Go 401 + NIM SSE Fixes — Plan Brief

> Full plan: `context/changes/opencode-nim-fixes/plan.md`
> Research: `context/changes/opencode-nim-fixes/research.md`

## What & Why

Freedius returns 401 on OpenCode Go Anthropic-format models (MiniMax, Qwen on `/v1/messages`) and produces empty/malformed SSE responses on NIM. The root causes: wrong auth headers (`Authorization: Bearer` instead of `x-api-key`), missing reasoning content handling in the SSE emitter, and unconditional `stream_options.include_usage` that some providers reject. This plan fixes all three.

## Starting Point

`AnthropicCompatibleAdapter` (`proxy/anthropic_compat.go`) is a plain `httputil.ReverseProxy` passthrough for Anthropic-format endpoints — it hardcodes `Authorization: Bearer` at lines 38 and 43. The `openAIDelta` struct (`proxy/translate/types.go:90`) has no `ReasoningContent` field, so the emitter silently drops NIM thinking content. `TranslateRequest` (`proxy/translate/anthropic_openai.go:41`) unconditionally sets `stream_options: {include_usage: true}` with no provider-awareness. `NIMAdapter` (`proxy/nim.go`) is a thin wrapper with no sanitization hook — NIM 400s on boolean JSON Schema subschemas go unhandled.

## Desired End State

- Auth: `curl` to OpenCode Go `/v1/messages` through freedius returns 200, not 401. All Anthropic-format providers (direct `anthropic`, `custom`, MixAdapter Anthropic path) get `x-api-key` + `anthropic-version: 2023-06-01`.
- SSE: NIM thinking models produce `content_block_start(type=thinking)` + `content_block_delta(type=thinking_delta)` + `content_block_stop` events. Claude Code renders reasoning natively.
- NIM: Requests with boolean JSON Schema subschemas and `type`-named parameters succeed. `stream_options.include_usage` is omitted for NIM.

## Key Decisions Made

| Decision | Choice | Why (1 sentence) | Source |
|---|---|---|---|
| Auth scheme for Anthropic endpoints | `x-api-key` + `anthropic-version: 2023-06-01` | Anthropic API spec and all `@ai-sdk/anthropic` clients use this; `Authorization: Bearer` is OpenAI-specific. | Research |
| Auth for OpenAI endpoints | Keep `Authorization: Bearer` | Correct per OpenAI spec; OpenCode Go `/v1/chat/completions` uses `@ai-sdk/openai-compatible` which sends Bearer. | Research |
| Stream options control | Provider-aware `TranslateOpts.NoStreamUsage` | NIM rejects `stream_options.include_usage`; unconditional removal breaks other providers. | Plan |
| Reasoning content support | Full `REASONING_CONTENT` mode (thinking block SSE events) | Claude Code natively renders thinking blocks; passthrough-as-text loses the UX. | Plan |
| NIM sanitization scope | Full free-claude-code parity adapted to freedius architecture | Boolean subschemas and `type` param aliasing are the two sanitization steps that apply; extra-field steps (chat_template_kwargs, reasoning_budget, etc.) are free-claude-code additions freedius doesn't inject. | Research + Plan |
| Phase ordering | Auth → SSE → NIM (three phases) | Auth is simplest with widest blast radius — ships first. SSE is additive emitter logic. NIM is largest code addition, builds on prior phases. | Plan |
| Test scope | Update existing tests + add header/sanitization assertions | Covers all Anthropic call paths; live integration tests deferred. | Plan |

## Scope

**In scope:**
- Fix `x-api-key` + `anthropic-version` on all Anthropic-format endpoints
- Add `ReasoningContent` to `openAIDelta` + thinking block emission in emitter
- Provider-aware `TranslateOpts` for `stream_options` control
- NIM body sanitization (boolean schema subschemas, `type` param aliasing)

**Out of scope:**
- Deferred post-tool text, `redacted_thinking` handling, stream recovery/retry
- NIM-specific analytics headers (`HTTP-Referer`, `X-Title`, `X-BILLING-INVOKE-ORIGIN`)
- Extra-field injection (`chat_template_kwargs`, `reasoning_budget`, `parallel_tool_calls`, `extra_body`)
- Live OpenCode Go integration tests

## Architecture / Approach

Three independent fixes, three phases:

```
Phase 1 (auth):        AnthropicCompatibleAdapter
                       └── httputil.ReverseProxy passthrough
                           └── header rewrite: Bearer → x-api-key

Phase 2 (SSE):         OpenAICompatibleAdapter
                       └── TranslateStream → emitter.consume()
                           └── new: reasoning_content → thinking blocks

Phase 3 (NIM):         NIMAdapter wraps OpenAICompatibleAdapter
                       └── preSendHook: sanitizeNIMBody (schema + type)
                       └── translateOpts: NoStreamUsage true
```

Auth fix is 2 lines in one file (line 38 + Rewrite callback at line 43), impacts all three Anthropic call paths. SSE fix adds 1 struct field + ~40 lines of emitter method following existing `emitText` pattern. NIM sanitization is a new file (`proxy/nim_sanitize.go`), wired as a post-translation hook in `NIMAdapter`. TranslateOpts is a new struct parameter wired through `OpenAICompatibleAdapter`.

## Phases at a Glance

| Phase | What it delivers | Key risk |
|---|---|---|
| 1. Auth | 200 instead of 401 on all Anthropic-format endpoints | Custom providers that relied on old `Authorization: Bearer` header break — but they were violating Anthropic spec; this is a correctness fix. |
| 2. SSE reasoning | NIM thinking models produce reasoning content in Claude Code | Emitter block-type transitions must correctly close/open blocks between thinking and text. |
| 3. Stream opts + NIM sanitization | No "feature not supported" errors; tools with boolean schemas work on NIM | TranslateOpts API change touches all callers (adapters + tests); backward-compatible with zero-value default. |

**Prerequisites:** S-03 (zen-go-adapters). Builds on `MixAdapter`, `AnthropicCompatibleAdapter`, `OpenAICompatibleAdapter` from S-03.
**Estimated effort:** ~3 sessions across 3 phases (auth: small, SSE: small, NIM: medium).

## Open Risks & Assumptions

- **Assumption**: `stream_options.include_usage` is the "feature not supported" trigger on OpenCode Go. If the trigger is something else (e.g., a specific tool format), the symptom persists after Phase 3 and needs further diagnosis.
- **Assumption**: NIM accepts requests without `stream_options` and with sanitized schemas. Free-claude-code validates these work; freedius inherits that guarantee.
- **Risk**: Custom providers configured with `provider: custom` or `provider: anthropic` may have been relying on `Authorization: Bearer` — if their upstream also expects Bearer (e.g., proxying to OpenAI through an Anthropic-compatible path), the auth fix breaks them. These configurations were already violating the Anthropic API spec.
- **Risk**: The `anthropic-version: 2023-06-01` override replaces whatever version Claude Code sends. If a provider requires a different version, this needs to be configurable per-provider (deferred).

## Success Criteria (Summary)

- OpenCode Go Anthropic-format models return 200 through freedius (no 401)
- NIM streaming produces thinking blocks in Claude Code (not empty responses)
- NIM requests with complex tool schemas return 200 (not 400)
- All existing tests pass with updated assertions; no regressions
