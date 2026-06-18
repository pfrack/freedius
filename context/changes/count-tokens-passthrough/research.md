---
date: 2026-06-18T13:02:00+02:00
researcher: kiro
git_commit: 9c193d4
branch: provider-codegen
repository: pfrack/freedius
topic: "Support /v1/messages/count_tokens passthrough in freedius proxy"
tags: [research, codebase, proxy, count-tokens, anthropic-api, routing]
status: complete
last_updated: 2026-06-18
last_updated_by: kiro
---

# Research: /v1/messages/count_tokens Passthrough

**Date**: 2026-06-18T13:02:00+02:00
**Researcher**: kiro
**Git Commit**: 9c193d4
**Branch**: provider-codegen
**Repository**: pfrack/freedius

## Research Question

How should freedius handle the Anthropic `/v1/messages/count_tokens` endpoint that Claude Code and other SDK clients send to the proxy?

## Summary

The proxy currently ignores URL path and treats every POST as a messages request. For the `anthropic` provider this accidentally works (ReverseProxy preserves path), but for OpenAI-compatible providers it breaks silently. The fix requires path-aware dispatch in `Dispatcher.ServeHTTP()` — a small change with high correctness value.

## Detailed Findings

### The Anthropic count_tokens Endpoint

- **Path:** `POST /v1/messages/count_tokens`
- **Request body:** Same shape as `/v1/messages` (model, messages, system, tools, thinking) — minus `max_tokens` and `stream`
- **Response:** `{"input_tokens": N, "context_management": {"original_input_tokens": N}}`
- **Streaming:** No — sync-only, single JSON response
- **Cost:** Free — no token billing
- **Rate limits:** Separate from messages (100–8000 RPM depending on tier)
- **Auth:** Same `x-api-key` + `anthropic-version: 2023-06-01`
- **Use case:** Pre-flight token estimation, prompt trimming, routing decisions

### Current Proxy Behavior

**Dispatcher** (`proxy/proxy.go:79-152`):
- Checks method is POST (line 82)
- Parses JSON body for `"model"` field (line 113-117)
- Resolves model → provider via config (line 126-139)
- Calls `adapter.Handle()` (line 148-152)
- **Never inspects `r.URL.Path`**

**AnthropicCompatibleAdapter** (`proxy/anthropic_compat.go:47-80`):
- Uses `httputil.ReverseProxy` which preserves the original request path
- So `/v1/messages/count_tokens` flows through correctly to upstream Anthropic
- **Works by accident** — no explicit support

**OpenAICompatibleAdapter** (`proxy/openai_compat.go`):
- Always posts to `m.BaseURL` with a fixed chat completions path
- Runs the translate layer which expects streaming message responses
- **Breaks on count_tokens** — no equivalent OpenAI endpoint exists

**MixAdapter** (`proxy/mix.go:48-70`):
- Routes to anthropic or openai sub-adapter based on `m.Protocol` or URL sniffing
- Same problem as OpenAI if protocol is openai

### What Needs to Change

1. **Path detection in Dispatcher** — check if path ends with `/count_tokens`
2. **Provider capability gate** — only `anthropic`-protocol providers support this
3. **For anthropic providers** — pass through unchanged (already works)
4. **For OpenAI providers** — return `501 Not Implemented` with a clear error message
5. **AccessLogMiddleware** — already logs `path` (line 424-434), no change needed

### Minimal Implementation Sketch

```go
// In Dispatcher.ServeHTTP, after model resolution:
isCountTokens := strings.HasSuffix(r.URL.Path, "/count_tokens")

if isCountTokens {
    if m.Provider != "anthropic" {
        // provider doesn't support count_tokens
        http.Error(w, `{"type":"error","error":{"type":"not_supported","message":"count_tokens not supported for this provider"}}`, 501)
        return
    }
}
```

The `anthropic` adapter already preserves the path via ReverseProxy, so no adapter changes needed.

### Edge Cases

- **mix provider with anthropic protocol** — should work if routed to the anthropic sub-adapter
- **Custom base URLs** — count_tokens path must be preserved (ReverseProxy handles this)
- **Non-streaming response** — the anthropic adapter doesn't force streaming, so sync responses pass through fine

## Code References

- `proxy/proxy.go:79-82` — Dispatcher.ServeHTTP, method check only
- `proxy/proxy.go:113-117` — JSON body parse for model field
- `proxy/proxy.go:126-139` — Model resolution logic
- `proxy/proxy.go:148-152` — Provider dispatch
- `proxy/proxy.go:415-435` — AccessLogMiddleware (already logs path)
- `proxy/anthropic_compat.go:47-80` — ReverseProxy preserves path (works by accident)
- `proxy/openai_compat.go` — Fixed path, would break on count_tokens
- `proxy/mix.go:48-70` — Protocol-based sub-adapter routing
- `proxy/translate/types.go:99-103` — openAIUsage struct (response-time only)
- `proxy/translate/anthropic_openai.go:295-297` — emitter token tracking (response-time only)

## Architecture Insights

- The fix is intentionally minimal: add path awareness to the dispatcher for this one endpoint
- No translation layer changes needed — count_tokens is Anthropic-native, and Anthropic-protocol providers pass it through unchanged
- This is a proxy correctness issue, not a feature — clients already send these requests and expect them to work
- Future consideration: if/when OpenAI adds a similar endpoint, the gate can be relaxed

## Historical Context

- No prior changes address path-based routing or count_tokens
- The dispatcher's path-agnostic design was intentional for simplicity (single-endpoint proxy), but count_tokens breaks that assumption
- Lesson from `proxy/anthropic_compat.go`: the ReverseProxy approach is the right pattern for Anthropic-native endpoints — it preserves paths, headers, and body without translation

## Open Questions

1. Should the `mix` provider support count_tokens when `protocol: anthropic`? (Likely yes — it delegates to the anthropic sub-adapter)
2. Should count_tokens requests be logged differently in the access log? (Probably not — path field already distinguishes them)
3. Should there be a config flag to disable count_tokens passthrough? (Probably not — unnecessary complexity)
