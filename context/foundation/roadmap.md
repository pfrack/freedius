---
project: freedius
version: 2
status: active
created: 2026-06-16
updated: 2026-07-31
prd_version: 1
---

# Roadmap: freedius

> Rewritten 2026-07-31 to reflect actual shipped state. The v1 roadmap (S-01–S-08, V-01, V-02) is fully delivered. This document now tracks what's next.

## Vision

A solo developer using Claude Code routes all LLM calls through Freedius to cheaper or free providers. One config file, one local process, zero breakage. Install in 30 seconds, configure in 60 seconds, save money immediately.

## Current State (2026-07-31)

Freedius is **feature-complete for solo-dev use**. Everything in the original PRD v1 is shipped and working:

### Core proxy
- Multi-provider routing (NIM, Zen, Go, OpenRouter, Google, Mistral, Groq, any OpenAI/Anthropic-compatible endpoint)
- Streaming translation (Anthropic ↔ OpenAI SSE)
- Fallback chains (primary fails → try alternatives automatically)
- Model family mapping (opus/sonnet/haiku → concrete models)
- Provider codegen (`providers.yaml` + `go generate`)
- Local token counting for OpenAI-protocol upstreams
- Error hardening with clear user-facing messages
- Env auto-injection (`freedius init`)
- Response model echo (stable `model` field across fallbacks)

### Web UI (localhost:8083)
- Live request stream + provider health dashboard
- Full CRUD for providers and mappings
- Mapping-first layout with fallback chain visualization
- Provider model discovery (fetch upstream /v1/models)
- Dark theme, responsive, mobile-ready

### Distribution
- Goreleaser pipeline (brew, scoop, go install, Docker)
- GitHub Actions CI + release workflow
- Free multi-provider default config (works out of the box with NIM + Groq + Google + Mistral)

### Documentation
- README rewritten for "ready to sell, ready to use" (current branch, impl_reviewed)
- Quickstart, config reference, supporting docs

## Active Work

| Change ID | Title | Status | Branch |
|-----------|-------|--------|--------|
| readme-ready-to-sell | README + supporting docs — ready to sell, ready to use | impl_reviewed | rename/readme-and-nim-default |

## What's Next (ideas, not committed)

These are potential directions. None are planned or in progress.

- **Visual polish** — Geist font, teal accent, tactile button states, grain texture (the `web-ui-design-upgrade` brief existed but was removed as superseded; could be revisited)
- **Custom 404 page** — branch `web-404-page` exists, not merged
- **Public launch** — landing page, Product Hunt, HN post, Discord
- **Multi-user / team mode** — auth, usage tracking per user (explicitly out of scope for now)
- **Plugin system** — custom middleware for request/response transformation
- **Windows support** — currently Linux + macOS only
- **More providers** — as new free-tier APIs emerge

## Done (chronological)

All original v1 slices shipped June–July 2026:

| # | What | PR | Date |
|---|------|----|------|
| 1 | Proxy skeleton (F-01) | #1 | 2026-06-16 |
| 2 | First call routed — NIM + custom (S-01) | #2 | 2026-06-17 |
| 3 | Provider-and-mapping — family routing + compat providers (S-02) | #3, #4 | 2026-06-18 |
| 4 | Zen + Go multi-format adapters (S-03) | #5 | 2026-06-18 |
| 5 | Error hardening + env injection + config template (S-04) | #6 | 2026-06-18 |
| 6 | OpenCode Go 401 + NIM SSE fixes (S-05) | #7 | 2026-06-18 |
| 7 | DeepSeek reasoning content (S-05 follow-up) | #8 | 2026-06-18 |
| 8 | Provider codegen (S-07) | #9 | 2026-06-18 |
| 9 | Local token counting (S-08) | #10 | 2026-06-18 |
| 10 | TUI dashboard | #11, #13 | 2026-06-20 |
| 11 | Error code collapse | #12 | 2026-06-18 |
| 12 | Unified server logs tab | #14 | 2026-06-20 |
| 13 | TUI statusbar modal | #15 | 2026-06-20 |
| 14 | Magefile build system | #16 | 2026-06-20 |
| 15 | TUI themes | #17 | 2026-06-20 |
| 16 | Add popular providers | #18 | 2026-06-21 |
| 17 | Daemon mode | #19, #20 | 2026-06-21 |
| 18 | Mage CI integration | #21, #22, #23 | 2026-07-01 |
| 19 | Testing proxy integration | #24 | 2026-07-02 |
| 20 | Streaming edge cases | #25 | 2026-07-02 |
| 21 | Web UI (replaces TUI) | #26 | 2026-07-02 |
| 22 | Test plan refresh | #27 | 2026-07-05 |
| 23 | Provider model discovery | #28 | 2026-07-05 |
| 24 | Web UI redesign (zinc dark palette) | #29 | 2026-07-05 |
| 25 | Mapping graph visualization | #30 | 2026-07-06 |
| 26 | Auto AI review workflow | #31 | 2026-07-13 |
| 27 | Solo-dev distribution (goreleaser) | #32 | 2026-07-22 |
| 28 | Response model echo (stable across fallbacks) | #33 | 2026-07-28 |
| 29 | Mapping-first UI refactor | #34 | 2026-07-30 |
| 30 | Free multi-provider default config | #35 | 2026-07-31 |
| 31 | README ready to sell (in progress on branch) | — | 2026-07-31 |
