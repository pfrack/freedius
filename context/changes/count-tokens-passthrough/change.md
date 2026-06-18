---
change_id: count-tokens-passthrough
title: "Support /v1/messages/count_tokens passthrough"
status: implementing
created: 2026-06-18
updated: 2026-06-18
---

## Summary

Add path-aware routing to the proxy so `/v1/messages/count_tokens` requests are correctly passed through to Anthropic-compatible upstreams (and rejected with 501 for providers that don't support it). Currently the dispatcher ignores URL path entirely, which works by accident for `anthropic` provider but breaks for OpenAI-compatible providers.

Phase 1 ships the pass-through + 501 behavior. Phase 2 (separate plan, deferred) will add local token counting so OpenAI-protocol providers also get a useful count_tokens response.