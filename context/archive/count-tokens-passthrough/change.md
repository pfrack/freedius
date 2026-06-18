---
change_id: count-tokens-passthrough
title: "Support /v1/messages/count_tokens passthrough and Anthropic-format error propagation"
status: impl_reviewed
created: 2026-06-18
updated: 2026-06-18
---

## Summary

Two proxy correctness fixes so Claude Code works reliably through freedius:

1. **count_tokens passthrough** — Add path-aware routing so `/v1/messages/count_tokens` requests pass through to Anthropic-compatible upstreams and return 501 for providers that don't support it.

2. **Anthropic-format error propagation** — When OpenAI-compatible upstreams (NIM, etc.) return errors or time out, translate those into Anthropic-shaped error responses with `x-should-retry` and `retry-after` headers so Claude Code can retry properly instead of getting broken connections or unrecognized error formats.
