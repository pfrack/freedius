# Response Model Echo-Back — Plan Brief

> Full plan: `context/changes/response-model-echo/plan.md`
> Frame brief: `context/changes/leak-positioning-angle/frame.md`

## What & Why

Freedius currently passes through the upstream provider's model name in API responses (e.g., `deepseek-v4-pro` or `step-3.5`). When fallback chains fire, the model name in responses flip-flops between turns depending on which provider won. The fix: always echo back the client's original request model name, giving Claude Code a stable view regardless of which provider actually served the request.

## Starting Point

- OpenAI-translate path (`translate.Stream`) captures model from upstream chunk and emits it in `message_start`
- Anthropic-compat path pipes the upstream response through unchanged via `io.Copy`
- Dispatcher already extracts the original request model at `proxy/proxy.go:196`
- `X-Freedius-Matched-Provider` and `X-Freedius-Matched-Model` headers already expose the real routing info

## Desired End State

Every response from Freedius has a `model` field matching exactly what the client sent in the request. Fallback chains, provider switches, and model mappings are invisible to the client. Operators still see the real routing via response headers and the dashboard.

## Key Decisions Made

| Decision | Choice | Why (1 sentence) | Source |
| --- | --- | --- | --- |
| Always echo vs configurable | Always echo | LiteLLM does this by default; no use case for showing upstream model to the client | Plan |
| How to pass original model | Extract from body JSON in adapter | Avoids interface changes to Provider.Handle; body is already available | Plan |
| Anthropic path rewrite strategy | Parse first SSE event only, pipe rest | Minimal perf impact; model only appears in message_start | Plan |

## Scope

**In scope:**
- Rewrite response model field in OpenAI-translate path
- Rewrite response model field in Anthropic-compat path
- Test coverage for both paths
- Fallback stability verification

**Out of scope:**
- Changing `X-Freedius-Matched-Model` header (stays as real upstream model)
- Config options for this behavior
- Dashboard changes
- Mapping key naming conventions

## Architecture / Approach

```
Client sends model:"claude-opus-4-20250514"
          │
   Dispatcher extracts req.Model
          │
   ┌──────┴──────┐
   │   Adapter   │ ← receives body (contains original model)
   │   Handle()  │
   └──────┬──────┘
          │
   Response stream
          │
   ┌──────┴──────┐
   │  Rewrite    │ ← model field overridden with original request model
   │  model      │
   └──────┬──────┘
          │
   Client sees model:"claude-opus-4-20250514" (always)
```

## Phases at a Glance

| Phase | What it delivers | Key risk |
| --- | --- | --- |
| 1. OpenAI-Translate Path | Model override in `translate.Stream` for all OpenAI-routed providers | Low — straightforward parameter addition |
| 2. Anthropic-Compat Path | Model rewrite in first SSE event for Anthropic-routed providers | Medium — needs careful first-event parsing without breaking the raw pipe |

**Prerequisites:** None — self-contained change.
**Estimated effort:** ~1 session, 2 phases.

## Open Risks & Assumptions

- Assumption: the model field only appears in `message_start` for Anthropic streams (verified from current Anthropic API docs)
- Risk: if an Anthropic-compat upstream sends model in a non-standard location, it won't be rewritten (acceptable — graceful degradation)

## Success Criteria (Summary)

- Response model field always matches the original request model name
- Fallback chain fires → model field stable (no flip-flopping)
- Existing test suite passes unchanged (backward compatible)
