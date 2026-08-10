---
project: freedius
version: 2
status: active
created: 2026-06-16
updated: 2026-08-10
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
- Model→mapping matching: compiled case-insensitive regex over every mapping key (most-specific wins), with an always-present, undeletable `default` catch-all
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

None. V-02l (logs-ui-live-tail) shipped and archived 2026-08-10, along with its
follow-ups V-02n (duplicate log entries) and V-02o (provider drawer + live-tail
filter). PR #48 carries S-09a, V-02n and V-02o and is open but unmerged.

## What's Next (ideas, not committed)

These are potential directions. None are planned or in progress.

- **Visual polish** — Geist font, teal accent, tactile button states, grain texture
- **Custom 404 page** — branch `web-404-page` exists, not merged
- **Public launch** — landing page, Product Hunt, HN post, Discord
- **Multi-user / team mode** — auth, usage tracking per user (explicitly out of scope for now)
- **Plugin system** — custom middleware for request/response transformation
- **Windows support** — currently Linux + macOS only
- **More providers** — as new free-tier APIs emerge

## Done (grouped by roadmap item)

Every archived change under its original v1 roadmap group. The base change carries the
group's id (e.g. `V-02`); related follow-ups are lettered (`V-02a`, `V-02b`, ...). Each
archived `change.md` carries the same value in its `roadmap_id` frontmatter key.
`bootstrap-verification` and the `auto-review` CI spike are intentionally unnumbered.

### Foundation — proxy skeleton, tooling & quality

| ID | Change ID | Title | PR | Date |
|----|-----------|-------|----|------|
| F-01 | proxy-skeleton | Proxy skeleton — HTTP server + config loading + dispatch stub | #1 | 2026-06-16 |
| F-01a | go-package-layout | Move main package to cmd/freedius/ (Go convention) | — | 2026-06-21 |
| F-01b | magefile | Replace Makefile with Mage (Go-based build tool) | #16 | 2026-06-17 |
| F-01c | mage-ci-integration | Integrate Mage into GitHub Actions CI | #21, #22, #23 | 2026-07-01 |
| F-01d | quality-gates-in-ci | Audit and harden quality gates in CI | — | 2026-07-02 |
| F-01e | testing-proxy-integration | Testing proxy integration | #24 | 2026-07-02 |
| F-01f | test-plan-refresh-2026-07-05 | Test plan refresh 2026-07-05 | #27 | 2026-07-05 |

### S-01 — First call routed (routing/translation core)

| ID | Change ID | Title | PR | Date |
|----|-----------|-------|----|------|
| S-01 | first-call-routed | First call routed — NIM adapter + custom passthrough | #2 | 2026-06-16 |
| S-01a | error-code-collapse | Error code collapse | #12 | 2026-06-18 |
| S-01b | streaming-edge-cases | Streaming Edge Cases Research | #25 | 2026-07-02 |

### S-02 — Provider-and-mapping (family routing)

| ID | Change ID | Title | PR | Date |
|----|-----------|-------|----|------|
| S-02 | provider-and-mapping | Provider-and-mapping — family-aware mapping + compat providers + in-binary defaults | #3, #4 | 2026-06-16 |
| S-02a | providers-section-refactor | Providers section refactor (config split) | — | 2026-06-19 |
| S-02b | regex-mapping-matching | Loose regex-based model→mapping matching with a protected default catch-all | — | 2026-08-10 |

### S-03 — Zen + Go adapters

| ID | Change ID | Title | PR | Date |
|----|-----------|-------|----|------|
| S-03 | zen-go-adapters | Zen + Go adapters — Opencode Zen and Opencode Go provider support | #5 | 2026-06-16 |

### S-04 — Error hardening

| ID | Change ID | Title | PR | Date |
|----|-----------|-------|----|------|
| S-04 | error-hardening | Error hardening + env auto-injection + config template | #6 | 2026-06-17 |
| S-04a | claude-settings-injection | Safe Claude Code settings injection (backup + freedius env) | — | 2026-08-09 |

### S-05 — OpenCode Go 401 + NIM SSE fixes

| ID | Change ID | Title | PR | Date |
|----|-----------|-------|----|------|
| S-05 | opencode-nim-fixes | OpenCode Go 401 + NVIDIA NIM SSE fixes | #7 | 2026-06-17 |
| S-05a | deepseek-reasoning-content | Investigate DeepSeek reasoning_content requirement | #8 | 2026-06-17 |

### S-06 — Custom → mix protocol

| ID | Change ID | Title | PR | Date |
|----|-----------|-------|----|------|
| S-06 | custom-to-mix-protocol | Replace custom provider with mix rewrite + protocol field | — | 2026-06-17 |
| S-06a | response-model-echo | Echo client's original model name in response (stable across fallbacks) | #33 | 2026-07-28 |
| S-06b | lazy-api-key-check | Lazy API key check (web-only providers) | — | 2026-07-31 |

### S-07 — Provider codegen

| ID | Change ID | Title | PR | Date |
|----|-----------|-------|----|------|
| S-07 | provider-codegen | Provider codegen — go:generate provider boilerplate from providers.yaml | #9 | 2026-06-17 |

### S-08 — Token counting

| ID | Change ID | Title | PR | Date |
|----|-----------|-------|----|------|
| S-08 | openai-count-tokens | Local token counting for OpenAI-protocol upstreams (/v1/messages/count_tokens) | #10 | 2026-06-18 |
| S-08a | count-tokens-passthrough | Support /v1/messages/count_tokens passthrough and Anthropic-format error propagation | — | 2026-06-18 |

### S-09 — Add popular providers

| ID | Change ID | Title | PR | Date |
|----|-----------|-------|----|------|
| S-09 | add-popular-providers | Add popular AI providers to providers.yaml | #18 | 2026-06-21 |
| S-09a | swe-starter-models | Register minimax + xiaomi providers; confirm starter model scope | #48 | 2026-08-10 |

### V-01 — TUI era

| ID | Change ID | Title | PR | Date |
|----|-----------|-------|----|------|
| V-01 | tui-dashboard | TUI Dashboard for freedius | #11, #13 | 2026-06-18 |
| V-01a | tui-config-setup | Extend TUI with mapping/model setup and plain error display | — | 2026-06-18 |
| V-01b | tui-error-detail-provider-defaults | TUI error detail + provider defaults | — | 2026-06-18 |
| V-01c | tui-all-logs-level-filter | TUI Log Tab: all slog lines + cycle-level filter | — | 2026-06-20 |
| V-01d | unified-server-logs-tab | Unified mode: server-log tab + single binary entry point | #14 | 2026-06-20 |
| V-01e | tui-statusbar-modal | Pin stats bar to top, show tabs below it, add `?` keyboard shortcuts modal | #15 | 2026-06-20 |
| V-01f | tui-themes | Add user-selectable themes to the freedius TUI | #17 | 2026-06-20 |
| V-01g | hide-tab-bar | Compact TUI: merge tabs into topbar, change shortcuts | — | 2026-06-21 |
| V-01h | mouse-support | Mouse Support for TUI | — | 2026-06-21 |
| V-01i | tui-providers-mappings-split | Split TUI Config tab into separate Providers and Mappings tabs | — | 2026-06-22 |

### V-02 — Web UI

| ID | Change ID | Title | PR | Date |
|----|-----------|-------|----|------|
| V-02 | web-ui | Replace Bubble Tea TUI with embedded web UI | #26 | 2026-07-02 |
| V-02a | provider-model-discovery | Provider model discovery UI — fetch, cache, and refresh model lists | #28 | 2026-07-05 |
| V-02b | web-ui-redesign | Web UI Modernization | #29 | 2026-07-05 |
| V-02c | provider-fallback-routing | Provider fallback routing for model/provider failures | — | 2026-07-06 |
| V-02d | mapping-graph-visualization | Graph-based mapping visualization for web UI | #30 | 2026-07-06 |
| V-02e | routing-visibility | Routing visibility across the web UI | — | 2026-07-07 |
| V-02f | web-ui-friendliness | Web UI friendliness improvements (breadcrumbs polish + global UX gaps) | — | 2026-07-07 |
| V-02g | mapping-first-ui-refactor | Mapping-first UI refactor | #34 | 2026-07-30 |
| V-02h | web-404-page | Custom 404 page for dashboard | #38 | 2026-07-31 |
| V-02i | dashboard-redesign | Dashboard GUI Redesign — Operator-Friendly Monitoring | #38, #39 | 2026-07-31 |
| V-02j | ui-design-polish | UI Design Polish — Anti-Slop Visual Quality Pass | #40 | 2026-08-01 |
| V-02k | web-ui-polish | Web UI Polish — Remainder & Dead-Code Cleanup | | 2026-08-07 |
| V-02l | logs-ui-live-tail | Logs UI live-tail fix (restore dead SSE) + scoped UX pass | — | 2026-08-09 |
| V-02m | misleading-inactive-filter | Fix misleading "Inactive" status filter on mappings page | — | 2026-08-09 |
| V-02n | logs-ui-duplicate-entries | Fix dashboard log view showing each historical entry twice | #48 | 2026-08-10 |
| V-02o | provider-badge-drawer | Provider badge opens a right-side drawer + logs live-tail filter fix | #48 | 2026-08-10 |

### V-03 — Distribution & launch

| ID | Change ID | Title | PR | Date |
|----|-----------|-------|----|------|
| V-03 | daemon-mode | Daemon mode with foreground attach | #19, #20 | 2026-06-21 |
| V-03a | solo-dev-positioning | Solo-dev positioning — install + narrative + scope coherence | — | 2026-07-21 |
| V-03b | solo-dev-distribution | Solo-dev distribution — brew/npx/goreleaser packaging | #32 | 2026-07-22 |
| V-03c | readme-ready-to-sell | README + supporting docs — ready to sell, ready to use | #35 | 2026-07-31 |
| V-03d | nim-nous-kilo-defaults | Modernize default config: NIM tiers + Nous + Kilo fallbacks | — | 2026-08-09 |
| V-03e | config-driven-adapter-opts | Config-driven OpenAI adapter options, eliminate dead wrappers | — | 2026-08-10 |
