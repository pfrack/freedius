---
change_id: openai-count-tokens
title: "Local token counting for OpenAI-protocol upstreams (/v1/messages/count_tokens)"
status: impl_reviewed
created: 2026-06-18
updated: 2026-06-18
roadmap_id: S-08
prereq: count-tokens-passthrough
prd_refs:
  - FR-001
  - FR-006
  - FR-007
  - FR-008
---

## Summary

Replace the 501 Not Implemented rejection of `/v1/messages/count_tokens` for OpenAI-protocol providers (NIM, OpenCode Go, custom OpenAI-compat) with a locally-computed `input_tokens` estimate that matches Anthropic's response shape. Goal: Claude Code's pre-flight count probe works across all provider protocols.

## Open Questions Resolved

- Counter approach: tiktoken-go with cl100k_base + o200k_base encodings (both bundled, picked heuristically per upstream model name)
- Parse-error behavior: best-effort re-parse with flexible typing, return 0 if still fails
- Image/document token cost: fixed constants (160 per image, 500 per document)
- Log level: debug (default FREEDIUS_LOG_LEVEL=info stays quiet)
- File layout: two new files (proxy/translate/count.go + proxy/count_tokens_local.go)
- Accuracy verification: round-trip test against real Anthropic API, gated on ANTHROPIC_API_KEY, 10% tolerance
