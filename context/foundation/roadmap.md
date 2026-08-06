---
project: freedius
version: 2
status: active
created: 2026-06-16
updated: 2026-08-06
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

None. All changes archived. Clean slate.

## What's Next (ideas, not committed)

These are potential directions. None are planned or in progress.

- **Visual polish** — Geist font, teal accent, tactile button states, grain texture
- **Custom 404 page** — branch `web-404-page` exists, not merged
- **Public launch** — landing page, Product Hunt, HN post, Discord
- **Multi-user / team mode** — auth, usage tracking per user (explicitly out of scope for now)
- **Plugin system** — custom middleware for request/response transformation
- **Windows support** — currently Linux + macOS only
- **More providers** — as new free-tier APIs emerge

## Done (chronological)

Every archived change, in order:

| # | Change ID | Title | PR | Date |
|---|-----------|-------|----|------|
| 1 | proxy-skeleton | Proxy skeleton — HTTP server + config + dispatch stub | #1 | 2026-06-16 |
| 2 | first-call-routed | First call routed — NIM + custom passthrough | #2 | 2026-06-17 |
| 3 | provider-and-mapping | Family routing + compat providers + in-binary defaults | #3, #4 | 2026-06-18 |
| 4 | zen-go-adapters | Zen + Go multi-format adapters | #5 | 2026-06-18 |
| 5 | error-hardening | Error hardening + env injection + config template | #6 | 2026-06-18 |
| 6 | opencode-nim-fixes | OpenCode Go 401 + NIM SSE fixes | #7 | 2026-06-18 |
| 7 | deepseek-reasoning-content | DeepSeek reasoning content passthrough | #8 | 2026-06-18 |
| 8 | provider-codegen | Provider codegen — go:generate from providers.yaml | #9 | 2026-06-18 |
| 9 | openai-count-tokens | Local token counting for OpenAI-protocol upstreams | #10 | 2026-06-18 |
| 10 | count-tokens-passthrough | count_tokens passthrough + Anthropic error propagation | — | 2026-06-18 |
| 11 | error-code-collapse | Error code collapse | #12 | 2026-06-18 |
| 12 | bootstrap-verification | Bootstrap verification | — | 2026-06-18 |
| 13 | tui-config-setup | TUI config setup — mapping/model forms | — | 2026-06-18 |
| 14 | tui-error-detail-provider-defaults | TUI error detail + provider defaults | — | 2026-06-18 |
| 15 | tui-dashboard | TUI dashboard — live terminal monitoring | #11, #13 | 2026-06-20 |
| 16 | tui-all-logs-level-filter | TUI log tab: all slog lines + level filter | — | 2026-06-20 |
| 17 | unified-server-logs-tab | Unified mode: server-log tab + single binary entry | #14 | 2026-06-20 |
| 18 | tui-statusbar-modal | TUI statusbar modal | #15 | 2026-06-20 |
| 19 | providers-section-refactor | Providers section refactor (config split) | — | 2026-06-20 |
| 20 | magefile | Magefile build system | #16 | 2026-06-20 |
| 21 | tui-themes | TUI themes | #17 | 2026-06-20 |
| 22 | add-popular-providers | Add popular providers (NIM, Zen, Go defaults) | #18 | 2026-06-21 |
| 23 | daemon-mode | Daemon mode | #19, #20 | 2026-06-21 |
| 24 | go-package-layout | Move main package to cmd/freedius/ (Go convention) | — | 2026-06-21 |
| 25 | hide-tab-bar | Compact TUI: merge tabs into topbar | — | 2026-06-21 |
| 26 | mouse-support | Mouse support for TUI | — | 2026-06-21 |
| 27 | tui-providers-mappings-split | Split TUI Config into Providers + Mappings tabs | — | 2026-06-22 |
| 28 | mage-ci-integration | Mage CI integration | #21, #22, #23 | 2026-07-01 |
| 29 | quality-gates-in-ci | Audit and harden quality gates in CI | — | 2026-07-02 |
| 30 | testing-proxy-integration | Testing proxy integration | #24 | 2026-07-02 |
| 31 | streaming-edge-cases | Streaming edge cases | #25 | 2026-07-02 |
| 32 | web-ui | Web UI dashboard (replaces TUI) | #26 | 2026-07-02 |
| 33 | test-plan-refresh | Test plan refresh | #27 | 2026-07-05 |
| 34 | provider-model-discovery | Provider model discovery (fetch upstream models) | #28 | 2026-07-05 |
| 35 | web-ui-redesign | Web UI redesign (zinc dark palette, responsive) | #29 | 2026-07-05 |
| 36 | mapping-graph-visualization | Mapping graph visualization (breadcrumb chains) | #30 | 2026-07-06 |
| 37 | provider-fallback-routing | Provider fallback routing (research) | — | 2026-07-06 |
| 38 | routing-visibility | Routing visibility across the web UI | — | 2026-07-07 |
| 39 | web-ui-friendliness | Web UI friendliness improvements | — | 2026-07-07 |
| 40 | auto-review | Auto AI review workflow | #31 | 2026-07-13 |
| 41 | solo-dev-positioning | Solo-dev positioning — install + narrative + scope | — | 2026-07-21 |
| 42 | solo-dev-distribution | Solo-dev distribution (goreleaser, brew, scoop) | #32 | 2026-07-22 |
| 43 | response-model-echo | Response model echo (stable across fallbacks) | #33 | 2026-07-28 |
| 44 | mapping-first-ui-refactor | Mapping-first UI refactor | #34 | 2026-07-30 |
| 45 | readme-ready-to-sell | README + supporting docs — ready to sell | #35 | 2026-07-31 |
| 46 | lazy-api-key-check | Lazy API key check on first use (web providers) | #38 | 2026-08-05 |
| 47 | web-404-page | Custom branded 404 page + static-asset handling | #37 | 2026-08-05 |
| 48 | ui-design-polish | UI design polish — anti-slop visual quality pass | #40 | 2026-08-06 |
